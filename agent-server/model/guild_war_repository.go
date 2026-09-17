package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

const guildWarUnlimitedPeriod uint32 = math.MaxInt32

func guildWarPeriodDuration(value uint32) (time.Duration, bool, error) {
	if value == guildWarUnlimitedPeriod {
		return 0, true, nil
	}
	if value&0xFC0003FF != 0 {
		return 0, false, ErrSocialInvalidRequest
	}
	days, hours, minutes := (value>>10)&0x1f, (value>>15)&0x1f, (value>>20)&0x3f
	if days > 30 || hours > 23 || minutes > 50 || minutes%10 != 0 {
		return 0, false, ErrSocialInvalidRequest
	}
	d := time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
	if d == 0 {
		return 0, false, ErrSocialInvalidRequest
	}
	return d, false, nil
}

const GuildWarMaximumWager uint32 = 500_000_000

type GuildWarProposal struct {
	TargetGuildName string
	WarPeriodID     uint32
	VictoryPointID  byte
	Wager           uint32
}

type GuildWarState struct {
	ID               uint64
	ChallengerGuild  int
	TargetGuild      int
	ChallengerMaster int
	TargetMaster     int
	ChallengerName   string
	TargetName       string
	WarPeriodID      uint32
	VictoryPointID   byte
	Wager            uint32
	Status           string
	ChallengerGold   uint64
	TargetGold       uint64
	WinnerGuild      int
	SurrenderedGuild int
	WinnerMaster     int
	WinnerGold       uint64
	DueAt            *time.Time
	ChallengerScore  uint32
	TargetScore      uint32
	ScoringGuild     int
	ScoringCharacter int
	ScoreDelta       uint32
	RewardGold       uint32
}

type GuildWarRepository interface {
	ProposeGuildWar(context.Context, int, GuildWarProposal) (GuildWarState, error)
	AnswerGuildWar(context.Context, int, bool) (GuildWarState, error)
	EndGuildWar(context.Context, int, uint32) (GuildWarState, error)
	ActiveGuildWar(context.Context, int) (GuildWarState, error)
	ExpireGuildWars(context.Context, time.Time, int) ([]GuildWarState, error)
	RecordGuildWarKill(context.Context, int, int, uint32) (GuildWarState, error)
	AvailableGuildWarReward(context.Context, int, uint32) (GuildWarState, error)
	ClaimGuildWarReward(context.Context, int, uint32) (GuildWarState, error)
}

var guildWarVictoryPoints = [...]uint32{0, 5_000, 10_000, 50_000, 100_000, 150_000, 250_000, 300_000}

func guildWarVictoryTarget(id byte) (uint32, error) {
	if int(id) >= len(guildWarVictoryPoints) {
		return 0, ErrSocialInvalidRequest
	}
	return guildWarVictoryPoints[id], nil
}

func (r *SQLSocialRepository) ActiveGuildWar(ctx context.Context, characterID int) (state GuildWarState, err error) {
	err = r.shard.QueryRowContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS,w.CHALLENGER_SCORE,w.TARGET_SCORE
FROM GUILD_MEMBER gm JOIN GUILD_WAR_RUNTIME w ON (w.CHALLENGER_GUILD=gm.FK_GUILD OR w.TARGET_GUILD=gm.FK_GUILD)
JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD
WHERE gm.FK_CHAR=? AND w.STATUS='active' ORDER BY w.ID DESC LIMIT 1`, characterID).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status, &state.ChallengerScore, &state.TargetScore)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrSocialNotFound
	}
	return state, err
}

func (r *SQLSocialRepository) EndGuildWar(ctx context.Context, actor int, warID uint32) (state GuildWarState, err error) {
	if actor <= 0 || warID == 0 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var actorGuild int
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=? AND MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&actorGuild); err != nil {
			return ErrSocialAccessDenied
		}
		if err := tx.QueryRowContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS
FROM GUILD_WAR_RUNTIME w JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD
WHERE w.ID=? AND w.STATUS='active' AND (w.CHALLENGER_GUILD=? OR w.TARGET_GUILD=?) FOR UPDATE`, warID, actorGuild, actorGuild).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSocialNotFound
			}
			return err
		}
		state.SurrenderedGuild = actorGuild
		if actorGuild == state.ChallengerGuild {
			state.WinnerGuild = state.TargetGuild
			state.WinnerMaster = state.TargetMaster
		} else if actorGuild == state.TargetGuild {
			state.WinnerGuild = state.ChallengerGuild
			state.WinnerMaster = state.ChallengerMaster
		} else {
			return ErrSocialAccessDenied
		}
		state.RewardGold = state.Wager * 2
		result, err := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='ended',WINNER_GUILD=?,SURRENDERED_GUILD=?,REWARD_GOLD=?,ENDED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS='active'", state.WinnerGuild, state.SurrenderedGuild, state.RewardGold, state.ID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		state.Status = "ended"
		return nil
	})
	return state, err
}

func guildWarWinnerGold(current uint64, wager uint32) (uint64, error) {
	payout := uint64(wager) * 2
	if current > math.MaxUint64-payout {
		return 0, ErrSocialConflict
	}
	return current + payout, nil
}

func (r *SQLSocialRepository) AvailableGuildWarReward(ctx context.Context, actor int, warID uint32) (state GuildWarState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || warID == 0 {
		return state, ErrSocialInvalidRequest
	}
	if scanErr := r.shard.QueryRowContext(ctx, `SELECT ID,CHALLENGER_GUILD,TARGET_GUILD,CHALLENGER_MASTER,TARGET_MASTER,WINNER_GUILD,REWARD_GOLD,STATUS
FROM GUILD_WAR_RUNTIME WHERE ID=? AND STATUS='ended' AND WINNER_GUILD IS NOT NULL AND REWARD_GOLD IS NOT NULL AND REWARD_CLAIMED_AT IS NULL`, warID).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.WinnerGuild, &state.RewardGold, &state.Status); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return state, ErrSocialNotFound
		}
		return state, scanErr
	}
	state.WinnerMaster = state.ChallengerMaster
	if state.WinnerGuild == state.TargetGuild {
		state.WinnerMaster = state.TargetMaster
	}
	if actor != state.WinnerMaster {
		return state, ErrSocialAccessDenied
	}
	return state, nil
}

func (r *SQLSocialRepository) ClaimGuildWarReward(ctx context.Context, actor int, reward uint32) (state GuildWarState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || reward == 0 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if scanErr := tx.QueryRowContext(ctx, `SELECT ID,CHALLENGER_GUILD,TARGET_GUILD,CHALLENGER_MASTER,TARGET_MASTER,WINNER_GUILD,REWARD_GOLD,STATUS
FROM GUILD_WAR_RUNTIME
WHERE STATUS='ended' AND WINNER_GUILD IS NOT NULL AND REWARD_GOLD=? AND REWARD_CLAIMED_AT IS NULL
AND ((WINNER_GUILD=CHALLENGER_GUILD AND CHALLENGER_MASTER=?) OR (WINNER_GUILD=TARGET_GUILD AND TARGET_MASTER=?))
ORDER BY ID LIMIT 1 FOR UPDATE`, reward, actor, actor).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.WinnerGuild, &state.RewardGold, &state.Status); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return ErrSocialNotFound
			}
			return scanErr
		}
		state.WinnerMaster = actor
		var current uint64
		if scanErr := tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=? FOR UPDATE", actor).Scan(&current); scanErr != nil {
			return scanErr
		}
		if current > math.MaxUint64-uint64(state.RewardGold) {
			return ErrSocialConflict
		}
		state.WinnerGold = current + uint64(state.RewardGold)
		if _, execErr := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.WinnerGold, actor); execErr != nil {
			return execErr
		}
		result, execErr := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET REWARD_CLAIMED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND REWARD_CLAIMED_AT IS NULL", state.ID)
		if execErr != nil {
			return execErr
		}
		return requireSocialRow(result)
	})
	return state, err
}

func (r *SQLSocialRepository) ProposeGuildWar(ctx context.Context, actor int, proposal GuildWarProposal) (state GuildWarState, err error) {
	_, victoryErr := guildWarVictoryTarget(proposal.VictoryPointID)
	if _, _, periodErr := guildWarPeriodDuration(proposal.WarPeriodID); actor <= 0 || proposal.TargetGuildName == "" || proposal.Wager > RuntimeGuildWarMaximumWager() || periodErr != nil || victoryErr != nil {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		identity, hasIdentity := ctx.Value(socialCommandContextKey{}).(SocialCommandIdentity)
		if hasIdentity {
			if loadErr := scanGuildWarByCommand(ctx, tx, identity.Key, &state); loadErr == nil {
				return nil
			} else if !errors.Is(loadErr, sql.ErrNoRows) {
				return loadErr
			}
		}

		var challengerGold uint64
		if err := tx.QueryRowContext(ctx, "SELECT g.ID,g.NAME,c.GOLD FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD JOIN `CHAR` c ON c.id=gm.FK_CHAR WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&state.ChallengerGuild, &state.ChallengerName, &challengerGold); err != nil {
			return ErrSocialAccessDenied
		}
		if err := tx.QueryRowContext(ctx, "SELECT g.ID,g.NAME,gm.FK_CHAR FROM GUILD g JOIN GUILD_MEMBER gm ON gm.FK_GUILD=g.ID AND gm.MEMBER_CLASS=? WHERE g.NAME=? FOR UPDATE", GuildMemberMaster, proposal.TargetGuildName).Scan(&state.TargetGuild, &state.TargetName, &state.TargetMaster); err != nil {
			return ErrSocialNotFound
		}
		if state.ChallengerGuild == state.TargetGuild || challengerGold < uint64(proposal.Wager) {
			return ErrSocialConflict
		}
		if err := lockGuildWarPair(ctx, tx, state.ChallengerGuild, state.TargetGuild); err != nil {
			return err
		}
		if err := ensureGuildWarAvailable(ctx, tx, state.ChallengerGuild, state.TargetGuild); err != nil {
			return err
		}

		state.ChallengerMaster = actor
		state.WarPeriodID = proposal.WarPeriodID
		state.VictoryPointID = proposal.VictoryPointID
		state.Wager = proposal.Wager
		state.Status = "pending"
		state.ChallengerGold = challengerGold - uint64(proposal.Wager)
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.ChallengerGold, actor); err != nil {
			return err
		}
		var commandKey any
		if hasIdentity {
			commandKey = identity.Key
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO GUILD_WAR_RUNTIME (CHALLENGER_GUILD,TARGET_GUILD,CHALLENGER_MASTER,TARGET_MASTER,WAR_PERIOD_ID,VICTORY_POINT_ID,WAGER,COMMAND_KEY,STATUS) VALUES (?,?,?,?,?,?,?,?,?)", state.ChallengerGuild, state.TargetGuild, state.ChallengerMaster, state.TargetMaster, state.WarPeriodID, state.VictoryPointID, state.Wager, commandKey, state.Status)
		if err != nil {
			return err
		}
		state.ID, err = lastInsertUint64(result)
		return err
	})
	return state, err
}

func (r *SQLSocialRepository) RecordGuildWarKill(ctx context.Context, killerID, victimID int, points uint32) (state GuildWarState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if killerID <= 0 || victimID <= 0 || killerID == victimID || points == 0 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if scanErr := tx.QueryRowContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS,w.CHALLENGER_SCORE,w.TARGET_SCORE,kgm.FK_GUILD
FROM GUILD_MEMBER kgm JOIN GUILD_MEMBER vgm ON vgm.FK_CHAR=?
JOIN GUILD_WAR_RUNTIME w ON w.STATUS='active' AND ((w.CHALLENGER_GUILD=kgm.FK_GUILD AND w.TARGET_GUILD=vgm.FK_GUILD) OR (w.TARGET_GUILD=kgm.FK_GUILD AND w.CHALLENGER_GUILD=vgm.FK_GUILD))
JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD
WHERE kgm.FK_CHAR=? LIMIT 1 FOR UPDATE`, victimID, killerID).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status, &state.ChallengerScore, &state.TargetScore, &state.WinnerGuild); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return ErrSocialNotFound
			}
			return scanErr
		}
		killerGuild := state.WinnerGuild
		state.WinnerGuild = 0
		state.ScoringGuild, state.ScoringCharacter, state.ScoreDelta = killerGuild, killerID, points
		if killerGuild == state.ChallengerGuild {
			if state.ChallengerScore > math.MaxUint32-points {
				return ErrSocialConflict
			}
			state.ChallengerScore += points
		} else if killerGuild == state.TargetGuild {
			if state.TargetScore > math.MaxUint32-points {
				return ErrSocialConflict
			}
			state.TargetScore += points
		} else {
			return ErrSocialAccessDenied
		}
		target, targetErr := guildWarVictoryTarget(state.VictoryPointID)
		if targetErr != nil {
			return targetErr
		}
		if _, execErr := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET CHALLENGER_SCORE=?,TARGET_SCORE=? WHERE ID=? AND STATUS='active'", state.ChallengerScore, state.TargetScore, state.ID); execErr != nil {
			return execErr
		}
		if target == 0 || (killerGuild == state.ChallengerGuild && state.ChallengerScore < target) || (killerGuild == state.TargetGuild && state.TargetScore < target) {
			return nil
		}
		state.WinnerGuild = killerGuild
		if killerGuild == state.ChallengerGuild {
			state.WinnerMaster = state.ChallengerMaster
		} else {
			state.WinnerMaster = state.TargetMaster
		}
		state.RewardGold = state.Wager * 2
		result, execErr := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='ended',WINNER_GUILD=?,REWARD_GOLD=?,ENDED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS='active'", state.WinnerGuild, state.RewardGold, state.ID)
		if execErr != nil {
			return execErr
		}
		if rowErr := requireSocialRow(result); rowErr != nil {
			return rowErr
		}
		state.Status = "ended"
		return nil
	})
	return state, err
}

func (r *SQLSocialRepository) AnswerGuildWar(ctx context.Context, actor int, accept bool) (state GuildWarState, err error) {
	if actor <= 0 {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS
FROM GUILD_WAR_RUNTIME w JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD
WHERE w.TARGET_MASTER=? AND w.STATUS='pending' ORDER BY w.ID DESC LIMIT 1 FOR UPDATE`, actor).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSocialNotFound
			}
			return err
		}
		if !accept {
			var challengerGold uint64
			if err := tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=? FOR UPDATE", state.ChallengerMaster).Scan(&challengerGold); err != nil {
				return err
			}
			if challengerGold > math.MaxUint64-uint64(state.Wager) {
				return ErrSocialConflict
			}
			state.ChallengerGold = challengerGold + uint64(state.Wager)
			if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.ChallengerGold, state.ChallengerMaster); err != nil {
				return err
			}
			state.Status = "rejected"
			_, err := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='rejected',ENDED_AT=CURRENT_TIMESTAMP(6) WHERE ID=? AND STATUS='pending'", state.ID)
			return err
		}

		var targetGold uint64
		if err := tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=? FOR UPDATE", actor).Scan(&targetGold); err != nil {
			return err
		}
		if targetGold < uint64(state.Wager) {
			return ErrSocialInsufficientGold
		}
		state.TargetGold = targetGold - uint64(state.Wager)
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.TargetGold, actor); err != nil {
			return err
		}
		state.Status = "active"
		duration, unlimited, periodErr := guildWarPeriodDuration(state.WarPeriodID)
		if periodErr != nil {
			return periodErr
		}
		if unlimited {
			_, err = tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='active',STARTED_AT=CURRENT_TIMESTAMP(6),DUE_AT=NULL WHERE ID=? AND STATUS='pending'", state.ID)
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='active',STARTED_AT=CURRENT_TIMESTAMP(6),DUE_AT=DATE_ADD(CURRENT_TIMESTAMP(6), INTERVAL ? MICROSECOND) WHERE ID=? AND STATUS='pending'", duration.Microseconds(), state.ID)
		}
		return err
	})
	return state, err
}

func (r *SQLSocialRepository) ExpireGuildWars(ctx context.Context, now time.Time, limit int) (states []GuildWarState, err error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if limit <= 0 {
		return nil, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS,w.CHALLENGER_SCORE,w.TARGET_SCORE
FROM GUILD_WAR_RUNTIME w JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD
WHERE w.STATUS='active' AND w.DUE_AT IS NOT NULL AND w.DUE_AT<=? ORDER BY w.DUE_AT,w.ID LIMIT ? FOR UPDATE`, now.UTC(), limit)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		for rows.Next() {
			var state GuildWarState
			if scanErr := rows.Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status, &state.ChallengerScore, &state.TargetScore); scanErr != nil {
				return scanErr
			}
			states = append(states, state)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return rowsErr
		}
		for i := range states {
			state := &states[i]
			var challengerGold, targetGold uint64
			if scanErr := tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=? FOR UPDATE", state.ChallengerMaster).Scan(&challengerGold); scanErr != nil {
				return scanErr
			}
			if scanErr := tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=? FOR UPDATE", state.TargetMaster).Scan(&targetGold); scanErr != nil {
				return scanErr
			}
			if state.ChallengerScore == state.TargetScore {
				if challengerGold > math.MaxUint64-uint64(state.Wager) || targetGold > math.MaxUint64-uint64(state.Wager) {
					return ErrSocialConflict
				}
				state.ChallengerGold, state.TargetGold = challengerGold+uint64(state.Wager), targetGold+uint64(state.Wager)
				if _, execErr := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.ChallengerGold, state.ChallengerMaster); execErr != nil {
					return execErr
				}
				if _, execErr := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", state.TargetGold, state.TargetMaster); execErr != nil {
					return execErr
				}
			} else {
				state.WinnerGuild, state.WinnerMaster = state.ChallengerGuild, state.ChallengerMaster
				if state.TargetScore > state.ChallengerScore {
					state.WinnerGuild, state.WinnerMaster = state.TargetGuild, state.TargetMaster
				}
				state.RewardGold = state.Wager * 2
			}
			result, execErr := tx.ExecContext(ctx, "UPDATE GUILD_WAR_RUNTIME SET STATUS='ended',WINNER_GUILD=NULLIF(?,0),REWARD_GOLD=NULLIF(?,0),ENDED_AT=? WHERE ID=? AND STATUS='active'", state.WinnerGuild, state.RewardGold, now.UTC(), state.ID)
			if execErr != nil {
				return execErr
			}
			if rowErr := requireSocialRow(result); rowErr != nil {
				return rowErr
			}
			state.Status = "ended"
		}
		return nil
	})
	return states, err
}

func lockGuildWarPair(ctx context.Context, tx *sql.Tx, first, second int) error {
	rows, err := tx.QueryContext(ctx, "SELECT ID FROM GUILD WHERE ID IN (?,?) ORDER BY ID FOR UPDATE", first, second)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != 2 {
		return ErrSocialNotFound
	}
	return nil
}

func ensureGuildWarAvailable(ctx context.Context, tx *sql.Tx, first, second int) error {
	var id uint64
	err := tx.QueryRowContext(ctx, "SELECT ID FROM GUILD_WAR_RUNTIME WHERE STATUS IN ('pending','active') AND (CHALLENGER_GUILD IN (?,?) OR TARGET_GUILD IN (?,?)) LIMIT 1 FOR UPDATE", first, second, first, second).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrSocialConflict
}

func scanGuildWarByCommand(ctx context.Context, tx *sql.Tx, commandKey string, state *GuildWarState) error {
	return tx.QueryRowContext(ctx, `SELECT w.ID,w.CHALLENGER_GUILD,w.TARGET_GUILD,w.CHALLENGER_MASTER,w.TARGET_MASTER,cg.NAME,tg.NAME,w.WAR_PERIOD_ID,w.VICTORY_POINT_ID,w.WAGER,w.STATUS
FROM GUILD_WAR_RUNTIME w JOIN GUILD cg ON cg.ID=w.CHALLENGER_GUILD JOIN GUILD tg ON tg.ID=w.TARGET_GUILD WHERE w.COMMAND_KEY=? FOR UPDATE`, commandKey).Scan(&state.ID, &state.ChallengerGuild, &state.TargetGuild, &state.ChallengerMaster, &state.TargetMaster, &state.ChallengerName, &state.TargetName, &state.WarPeriodID, &state.VictoryPointID, &state.Wager, &state.Status)
}

func lastInsertUint64(result sql.Result) (uint64, error) {
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		return 0, ErrSocialConflict
	}
	return uint64(id), nil
}
