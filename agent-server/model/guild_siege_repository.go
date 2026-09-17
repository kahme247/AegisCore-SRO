package model

import (
	"context"
	"database/sql"
)

const (
	GuildSiegeAuthorityNone       byte = 0
	GuildSiegeAuthorityMaster     byte = 1
	GuildSiegeAuthorityCommander  byte = 2
	GuildSiegeAuthorityDefense    byte = 4
	GuildSiegeAuthorityOffense    byte = 8
	GuildSiegeAuthorityProduction byte = 16
	GuildSiegeAuthorityCombatant  byte = 32
)

type GuildSiegeRepository interface {
	UpdateGuildSiegeAuthority(context.Context, int, int, byte) (GuildSnapshot, error)
}

func guildSiegeAuthorityLimit(authority byte) (int, bool) {
	switch authority {
	case GuildSiegeAuthorityCommander:
		return 4, true
	case GuildSiegeAuthorityDefense, GuildSiegeAuthorityOffense, GuildSiegeAuthorityProduction:
		return 1, true
	case GuildSiegeAuthorityCombatant:
		return 10, true
	default:
		return 0, false
	}
}

func (r *SQLSocialRepository) UpdateGuildSiegeAuthority(ctx context.Context, actor, target int, authority byte) (snapshot GuildSnapshot, err error) {
	if r == nil || r.shard == nil {
		return snapshot, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || target <= 0 || authority == GuildSiegeAuthorityMaster {
		return snapshot, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		if scanErr := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=? AND MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID); scanErr != nil {
			return ErrSocialAccessDenied
		}
		var current byte
		if scanErr := tx.QueryRowContext(ctx, "SELECT SIEGE_AUTHORITY FROM GUILD_MEMBER WHERE FK_GUILD=? AND FK_CHAR=? FOR UPDATE", guildID, target).Scan(&current); scanErr != nil {
			return ErrSocialNotFound
		}
		if authority == GuildSiegeAuthorityNone {
			if current == GuildSiegeAuthorityNone || current == GuildSiegeAuthorityMaster {
				return ErrSocialConflict
			}
		} else {
			limit, valid := guildSiegeAuthorityLimit(authority)
			if !valid || current != GuildSiegeAuthorityNone {
				return ErrSocialConflict
			}
			var assigned int
			if countErr := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_GUILD=? AND SIEGE_AUTHORITY=?", guildID, authority).Scan(&assigned); countErr != nil {
				return countErr
			}
			if assigned >= limit {
				return ErrSocialConflict
			}
		}
		result, execErr := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET SIEGE_AUTHORITY=? WHERE FK_GUILD=? AND FK_CHAR=?", authority, guildID, target)
		if execErr != nil {
			return execErr
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return execErr
		}
		snapshot, execErr = scanGuild(ctx, tx, actor)
		return execErr
	})
	return snapshot, err
}
