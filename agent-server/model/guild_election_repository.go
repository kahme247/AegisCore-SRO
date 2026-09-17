package model

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	GuildElectionMasterAbsence = 45 * 24 * time.Hour
	GuildElectionRecentMember  = 15 * 24 * time.Hour
	GuildElectionDuration      = 10 * time.Minute
)

type GuildElectionCandidateState struct {
	CharacterID uint32
	Votes       byte
}

type GuildElectionState struct {
	ID                  uint32
	GuildID             int
	PreviousMasterID    int
	WinnerID            int
	SelectedCandidateID uint32
	StartedAt           time.Time
	EndsAt              time.Time
	Status              string
	Candidates          []GuildElectionCandidateState
}

type GuildElectionRepository interface {
	TouchGuildMemberOnline(context.Context, int, time.Time) error
	StartGuildElection(context.Context, int, uint32, time.Time) (GuildElectionState, error)
	ActiveGuildElection(context.Context, int, time.Time) (GuildElectionState, error)
	GuildElection(context.Context, int, uint32, time.Time) (GuildElectionState, error)
	VoteGuildElection(context.Context, int, uint32, uint32, byte, time.Time) (GuildElectionState, error)
	ExpireGuildElections(context.Context, time.Time, int) ([]GuildElectionState, error)
}

func (r *SQLSocialRepository) TouchGuildMemberOnline(ctx context.Context, actor int, now time.Time) error {
	if r == nil || r.shard == nil || actor <= 0 || now.IsZero() {
		return ErrSocialInvalidRequest
	}
	_, err := r.shard.ExecContext(ctx, "UPDATE GUILD_MEMBER SET LAST_ONLINE_AT=? WHERE FK_CHAR=?", now.UTC(), actor)
	return err
}

func (r *SQLSocialRepository) ActiveGuildElection(ctx context.Context, actor int, now time.Time) (state GuildElectionState, err error) {
	if r == nil || r.shard == nil || actor <= 0 {
		return state, ErrSocialInvalidRequest
	}
	var id uint32
	if err = r.shard.QueryRowContext(ctx, `SELECT e.ID FROM GUILD_ELECTION e JOIN GUILD_MEMBER gm ON gm.FK_GUILD=e.FK_GUILD
WHERE gm.FK_CHAR=? AND e.STATUS='active' ORDER BY e.ID DESC LIMIT 1`, actor).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, ErrSocialNotFound
		}
		return state, err
	}
	return r.GuildElection(ctx, actor, id, now)
}

func (r *SQLSocialRepository) StartGuildElection(ctx context.Context, actor int, inactiveMasterID uint32, now time.Time) (state GuildElectionState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || inactiveMasterID == 0 || now.IsZero() {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var actorGuild, masterID int
		var actorLastOnline, masterLastOnline time.Time
		if scanErr := tx.QueryRowContext(ctx, `SELECT gm.FK_GUILD,COALESCE(gm.LAST_ONLINE_AT,gm.JOINED_AT),master.FK_CHAR,COALESCE(master.LAST_ONLINE_AT,master.JOINED_AT)
FROM GUILD_MEMBER gm JOIN GUILD_MEMBER master ON master.FK_GUILD=gm.FK_GUILD AND master.MEMBER_CLASS=?
WHERE gm.FK_CHAR=? FOR UPDATE`, GuildMemberMaster, actor).Scan(&actorGuild, &actorLastOnline, &masterID, &masterLastOnline); scanErr != nil {
			return ErrSocialAccessDenied
		}
		if uint32(masterID) != inactiveMasterID || now.Sub(masterLastOnline) < RuntimeGuildMasterAbsence() || now.Sub(actorLastOnline) > RuntimeGuildRecentMemberWindow() {
			return ErrSocialAccessDenied
		}
		var active uint32
		if scanErr := tx.QueryRowContext(ctx, "SELECT ID FROM GUILD_ELECTION WHERE FK_GUILD=? AND STATUS='active' FOR UPDATE", actorGuild).Scan(&active); scanErr == nil {
			return ErrSocialConflict
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		state = GuildElectionState{GuildID: actorGuild, PreviousMasterID: masterID, StartedAt: now.UTC(), EndsAt: now.UTC().Add(RuntimeGuildElectionDuration()), Status: "active"}
		result, execErr := tx.ExecContext(ctx, "INSERT INTO GUILD_ELECTION (FK_GUILD,PREVIOUS_MASTER,STARTED_AT,ENDS_AT,STATUS) VALUES (?,?,?,?,?)", actorGuild, masterID, state.StartedAt, state.EndsAt, state.Status)
		if execErr != nil {
			return execErr
		}
		id, execErr := lastInsertUint64(result)
		if execErr != nil {
			return execErr
		}
		state.ID = uint32(id)
		if _, execErr = tx.ExecContext(ctx, `INSERT INTO GUILD_ELECTION_CANDIDATE (FK_ELECTION,FK_CHAR)
SELECT ?,FK_CHAR FROM GUILD_MEMBER WHERE FK_GUILD=? AND COALESCE(LAST_ONLINE_AT,JOINED_AT)>=?`, state.ID, actorGuild, now.UTC().Add(-RuntimeGuildRecentMemberWindow())); execErr != nil {
			return execErr
		}
		return scanGuildElectionCandidates(ctx, tx, actor, &state)
	})
	return state, err
}

func (r *SQLSocialRepository) GuildElection(ctx context.Context, actor int, electionID uint32, now time.Time) (state GuildElectionState, err error) {
	if r == nil || r.shard == nil || actor <= 0 || electionID == 0 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := scanGuildElection(ctx, tx, electionID, &state, false); err != nil {
			return err
		}
		if now.Before(state.EndsAt) && state.Status == "active" {
			return scanGuildElectionCandidates(ctx, tx, actor, &state)
		}
		if state.Status == "active" {
			if err := settleGuildElection(ctx, tx, &state, 0); err != nil {
				return err
			}
		}
		return scanGuildElectionCandidates(ctx, tx, actor, &state)
	})
	return state, err
}

func (r *SQLSocialRepository) VoteGuildElection(ctx context.Context, actor int, electionID, candidateID uint32, selection byte, now time.Time) (state GuildElectionState, err error) {
	if r == nil || r.shard == nil || actor <= 0 || electionID == 0 || candidateID == 0 || selection != 1 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := scanGuildElection(ctx, tx, electionID, &state, true); err != nil {
			return err
		}
		if state.Status != "active" || !now.Before(state.EndsAt) {
			return ErrSocialConflict
		}
		var eligible int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_ELECTION_CANDIDATE WHERE FK_ELECTION=? AND FK_CHAR IN (?,?)", electionID, actor, candidateID).Scan(&eligible); err != nil || eligible != 2 && actor != int(candidateID) || eligible != 1 && actor == int(candidateID) {
			return ErrSocialAccessDenied
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO GUILD_ELECTION_VOTE (FK_ELECTION,FK_VOTER,FK_CANDIDATE,VOTED_AT) VALUES (?,?,?,?)
ON DUPLICATE KEY UPDATE FK_CANDIDATE=VALUES(FK_CANDIDATE),VOTED_AT=VALUES(VOTED_AT)`, electionID, actor, candidateID, now.UTC()); err != nil {
			return err
		}
		state.SelectedCandidateID = candidateID
		var votes, candidates int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_ELECTION_VOTE WHERE FK_ELECTION=? AND FK_CANDIDATE=?", electionID, candidateID).Scan(&votes); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_ELECTION_CANDIDATE WHERE FK_ELECTION=?", electionID).Scan(&candidates); err != nil {
			return err
		}
		if candidates > 0 && votes*100 >= candidates*60 {
			if err := settleGuildElection(ctx, tx, &state, int(candidateID)); err != nil {
				return err
			}
		}
		return scanGuildElectionCandidates(ctx, tx, actor, &state)
	})
	return state, err
}

func (r *SQLSocialRepository) ExpireGuildElections(ctx context.Context, now time.Time, limit int) (states []GuildElectionState, err error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if limit <= 0 {
		limit = 100
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, "SELECT ID FROM GUILD_ELECTION WHERE STATUS='active' AND ENDS_AT<=? ORDER BY ID LIMIT ? FOR UPDATE", now.UTC(), limit)
		if queryErr != nil {
			return queryErr
		}
		var ids []uint32
		for rows.Next() {
			var id uint32
			if scanErr := rows.Scan(&id); scanErr != nil {
				rows.Close()
				return scanErr
			}
			ids = append(ids, id)
		}
		if closeErr := rows.Close(); closeErr != nil {
			return closeErr
		}
		for _, id := range ids {
			var state GuildElectionState
			if scanErr := scanGuildElection(ctx, tx, id, &state, false); scanErr != nil {
				return scanErr
			}
			if settleErr := settleGuildElection(ctx, tx, &state, 0); settleErr != nil {
				return settleErr
			}
			states = append(states, state)
		}
		return nil
	})
	return states, err
}

func scanGuildElection(ctx context.Context, tx *sql.Tx, id uint32, state *GuildElectionState, lock bool) error {
	query := "SELECT ID,FK_GUILD,PREVIOUS_MASTER,COALESCE(WINNER_CHAR,0),STARTED_AT,ENDS_AT,STATUS FROM GUILD_ELECTION WHERE ID=?"
	if lock {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, query, id).Scan(&state.ID, &state.GuildID, &state.PreviousMasterID, &state.WinnerID, &state.StartedAt, &state.EndsAt, &state.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSocialNotFound
		}
		return err
	}
	return nil
}

func scanGuildElectionCandidates(ctx context.Context, tx *sql.Tx, actor int, state *GuildElectionState) error {
	var actorGuild int
	if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=?", actor).Scan(&actorGuild); err != nil || actorGuild != state.GuildID {
		return ErrSocialAccessDenied
	}
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(FK_CANDIDATE,0) FROM GUILD_ELECTION_VOTE WHERE FK_ELECTION=? AND FK_VOTER=?", state.ID, actor).Scan(&state.SelectedCandidateID)
	rows, err := tx.QueryContext(ctx, `SELECT c.FK_CHAR,COUNT(v.FK_VOTER) FROM GUILD_ELECTION_CANDIDATE c
LEFT JOIN GUILD_ELECTION_VOTE v ON v.FK_ELECTION=c.FK_ELECTION AND v.FK_CANDIDATE=c.FK_CHAR
WHERE c.FK_ELECTION=? GROUP BY c.FK_CHAR ORDER BY c.FK_CHAR`, state.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	state.Candidates = nil
	for rows.Next() {
		var candidate GuildElectionCandidateState
		var votes uint64
		if err = rows.Scan(&candidate.CharacterID, &votes); err != nil {
			return err
		}
		if votes > 255 {
			votes = 255
		}
		candidate.Votes = byte(votes)
		state.Candidates = append(state.Candidates, candidate)
	}
	return rows.Err()
}

func settleGuildElection(ctx context.Context, tx *sql.Tx, state *GuildElectionState, winner int) error {
	if winner > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET MEMBER_CLASS=?,PERMISSIONS=0,STORAGE_RIGHTS=0 WHERE FK_GUILD=? AND FK_CHAR=?", GuildMemberNormal, state.GuildID, state.PreviousMasterID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET MEMBER_CLASS=?,PERMISSIONS=?,STORAGE_RIGHTS=1 WHERE FK_GUILD=? AND FK_CHAR=?", GuildMemberMaster, GuildPermissionAll, state.GuildID, winner)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE GUILD_ELECTION SET STATUS='ended',WINNER_CHAR=?,ENDED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS='active'", nullableSocialID(winner), state.ID)
	if err != nil {
		return err
	}
	if err = requireSocialRow(result); err != nil {
		return err
	}
	state.Status, state.WinnerID = "ended", winner
	return nil
}

func nullableSocialID(id int) any {
	if id == 0 {
		return nil
	}
	return id
}
