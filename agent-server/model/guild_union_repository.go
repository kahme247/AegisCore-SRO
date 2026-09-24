package model

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type GuildUnionMember struct {
	GuildID    int
	Name       string
	Level      byte
	MasterName string
	Members    byte
}

type GuildUnionSnapshot struct {
	ID          int
	LeaderGuild int
	Guilds      []GuildUnionMember
}

func scanGuildUnion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, characterID int) (GuildUnionSnapshot, error) {
	var union GuildUnionSnapshot
	if err := q.QueryRowContext(ctx, "SELECT u.ID,u.LEADER_GUILD FROM GUILD_MEMBER actor JOIN GUILD_UNION_MEMBER own ON own.FK_GUILD=actor.FK_GUILD JOIN GUILD_UNION u ON u.ID=own.FK_UNION WHERE actor.FK_CHAR=?", characterID).Scan(&union.ID, &union.LeaderGuild); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GuildUnionSnapshot{}, ErrSocialNotFound
		}
		return GuildUnionSnapshot{}, err
	}
	rows, err := q.QueryContext(ctx, "SELECT g.ID,g.NAME,g.LEVEL,c.CHAR_NAME,COUNT(gm.FK_CHAR) FROM GUILD_UNION_MEMBER um JOIN GUILD g ON g.ID=um.FK_GUILD JOIN GUILD_MEMBER master ON master.FK_GUILD=g.ID AND master.MEMBER_CLASS=0 JOIN `CHAR` c ON c.id=master.FK_CHAR JOIN GUILD_MEMBER gm ON gm.FK_GUILD=g.ID WHERE um.FK_UNION=? GROUP BY g.ID,g.NAME,g.LEVEL,c.CHAR_NAME ORDER BY (g.ID=? ) DESC,g.ID", union.ID, union.LeaderGuild)
	if err != nil {
		return GuildUnionSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var member GuildUnionMember
		if err = rows.Scan(&member.GuildID, &member.Name, &member.Level, &member.MasterName, &member.Members); err != nil {
			return GuildUnionSnapshot{}, err
		}
		union.Guilds = append(union.Guilds, member)
	}
	return union, rows.Err()
}

func (r *SQLSocialRepository) GuildUnion(ctx context.Context, characterID int) (GuildUnionSnapshot, error) {
	return scanGuildUnion(socialContext(ctx), r.shard, characterID)
}

func (r *SQLSocialRepository) InviteGuildUnion(ctx context.Context, actor, target int) error {
	if actor <= 0 || target <= 0 || actor == target {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var inviterGuild, targetGuild int
		var inviterLevel, targetLevel byte
		var permissions uint32
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL,gm.PERMISSIONS FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&inviterGuild, &inviterLevel, &permissions); err != nil || permissions&GuildPermissionUnion == 0 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, inviterGuild)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", target).Scan(&targetGuild, &targetLevel); err != nil {
			return ErrSocialAccessDenied
		}
		if inviterGuild == targetGuild {
			return ErrSocialConflict
		}
		if inviterLevel < 2 || targetLevel < 2 {
			return ErrSocialInsufficientLevel
		}
		var targetUnion int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", targetGuild).Scan(&targetUnion); err != nil {
			return err
		}
		if targetUnion != 0 {
			return ErrSocialConflict
		}
		var unionID sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT FK_UNION FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", inviterGuild).Scan(&unionID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if unionID.Valid {
			var leader, count int
			if err := tx.QueryRowContext(ctx, "SELECT u.LEADER_GUILD,COUNT(um.FK_GUILD) FROM GUILD_UNION u JOIN GUILD_UNION_MEMBER um ON um.FK_UNION=u.ID WHERE u.ID=? GROUP BY u.LEADER_GUILD", unionID.Int64).Scan(&leader, &count); err != nil {
				return err
			}
			if leader != inviterGuild {
				return ErrSocialAccessDenied
			}
			if count >= 8 {
				return ErrSocialCapacity
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_INVITATION (INVITER_GUILD,INVITEE_GUILD,EXPIRES_AT) VALUES (?,?,DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 60 SECOND)) ON DUPLICATE KEY UPDATE INVITER_GUILD=VALUES(INVITER_GUILD),EXPIRES_AT=VALUES(EXPIRES_AT)", inviterGuild, targetGuild); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.invitation.created")
	})
}

func (r *SQLSocialRepository) AnswerGuildUnionInvite(ctx context.Context, actor int, accept bool) (GuildUnionSnapshot, error) {
	var snapshot GuildUnionSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if !strings.HasPrefix(replay.topic, "guild.union.") {
				return ErrSocialNotFound
			}
			if !accept {
				return nil
			}
			snapshot, err = scanGuildUnion(ctx, tx, actor)
			return err
		}
		var targetGuild, inviterGuild int
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,i.INVITER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION_INVITATION i ON i.INVITEE_GUILD=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 AND i.EXPIRES_AT>CURRENT_TIMESTAMP(6) FOR UPDATE", actor).Scan(&targetGuild, &inviterGuild); err != nil {
			return ErrSocialNotFound
		}
		command, err := beginGuildCommand(ctx, tx, targetGuild)
		if err != nil || command.duplicate {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_INVITATION WHERE INVITEE_GUILD=?", targetGuild); err != nil {
			return err
		}
		if !accept {
			return completeGuildCommand(ctx, tx, command, "guild.union.invitation.rejected")
		}
		var unionID int
		err = tx.QueryRowContext(ctx, "SELECT FK_UNION FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", inviterGuild).Scan(&unionID)
		if errors.Is(err, sql.ErrNoRows) {
			created, createErr := tx.ExecContext(ctx, "INSERT INTO GUILD_UNION (LEADER_GUILD) VALUES (?)", inviterGuild)
			if createErr != nil {
				return createErr
			}
			id, idErr := created.LastInsertId()
			if idErr != nil {
				return idErr
			}
			unionID = int(id)
			if _, err = tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_MEMBER (FK_UNION,FK_GUILD) VALUES (?,?)", unionID, inviterGuild); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_MEMBER (FK_UNION,FK_GUILD) VALUES (?,?)", unionID, targetGuild); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=? WHERE ID IN (?,?)", unionID, inviterGuild, targetGuild); err != nil {
			return err
		}
		snapshot, err = scanGuildUnion(ctx, tx, actor)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.joined")
	})
	return snapshot, err
}

func (r *SQLSocialRepository) LeaveGuildUnion(ctx context.Context, actor int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if !strings.HasPrefix(replay.topic, "guild.union.") {
				return ErrSocialConflict
			}
			return nil
		}
		var guildID, unionID, leader int
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,um.FK_UNION,u.LEADER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION_MEMBER um ON um.FK_GUILD=gm.FK_GUILD JOIN GUILD_UNION u ON u.ID=um.FK_UNION WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&guildID, &unionID, &leader); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil || command.duplicate {
			return err
		}
		if leader == guildID {
			if _, err := tx.ExecContext(ctx, "UPDATE GUILD g JOIN GUILD_UNION_MEMBER um ON um.FK_GUILD=g.ID SET g.UNION_ID=NULL WHERE um.FK_UNION=?", unionID); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION WHERE ID=?", unionID)
			if err != nil {
				return err
			}
			if err = requireSocialRow(result); err != nil {
				return err
			}
			return completeGuildCommand(ctx, tx, command, "guild.union.disbanded")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=NULL WHERE ID=?", guildID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_MEMBER WHERE FK_UNION=? AND FK_GUILD=?", unionID, guildID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.left")
	})
}

func (r *SQLSocialRepository) KickGuildUnion(ctx context.Context, actor, targetGuild int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var unionID, leader int
		if err := tx.QueryRowContext(ctx, "SELECT u.ID,u.LEADER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION u ON u.LEADER_GUILD=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&unionID, &leader); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, leader)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		if targetGuild == leader {
			return ErrSocialInvalidRequest
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=NULL WHERE ID=?", targetGuild); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_MEMBER WHERE FK_UNION=? AND FK_GUILD=?", unionID, targetGuild)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.member.kicked")
	})
}
