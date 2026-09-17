package model

import (
	"context"
	"database/sql"
	"math/bits"
)

const (
	GuildMercenaryMinimumLevel  byte   = 4
	GuildMercenaryAddGold       uint64 = 50_000
	GuildMercenaryClearGold     uint64 = 10_000
	GuildMercenaryGuildPoints   uint32 = 500_000
	GuildMercenaryAttributeMask byte   = 0x1F
)

type GuildMercenaryRepository interface {
	UpdateGuildMercenaryAttribute(context.Context, int, byte) (GuildSnapshot, uint64, error)
}

func guildMercenaryAttributeCost(current, requested byte) (gold uint64, valid bool) {
	if requested == 0 {
		return GuildMercenaryClearGold, current != 0
	}
	if requested&^GuildMercenaryAttributeMask != 0 || bits.OnesCount8(requested) != 1 || current&requested != 0 || bits.OnesCount8(current) >= 2 {
		return 0, false
	}
	return GuildMercenaryAddGold, true
}

func (r *SQLSocialRepository) UpdateGuildMercenaryAttribute(ctx context.Context, actor int, requested byte) (snapshot GuildSnapshot, gold uint64, err error) {
	if r == nil || r.shard == nil {
		return snapshot, 0, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 {
		return snapshot, 0, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var level, current byte
		var guildPoints uint32
		if scanErr := tx.QueryRowContext(ctx, "SELECT g.ID,g.LEVEL,g.MERCENARY_ATTR,g.GATHERED_SP,c.GOLD "+
			"FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD JOIN `CHAR` c ON c.id=gm.FK_CHAR "+
			"WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID, &level, &current, &guildPoints, &gold); scanErr != nil {
			return ErrSocialAccessDenied
		}
		cost, valid := guildMercenaryAttributeCost(current, requested)
		if level < GuildMercenaryMinimumLevel || !valid || gold < cost || guildPoints < GuildMercenaryGuildPoints {
			return ErrSocialConflict
		}
		next := current | requested
		if requested == 0 {
			next = 0
		}
		gold -= cost
		guildPoints -= GuildMercenaryGuildPoints
		if _, execErr := tx.ExecContext(ctx, "UPDATE GUILD SET MERCENARY_ATTR=?,GATHERED_SP=? WHERE ID=?", next, guildPoints, guildID); execErr != nil {
			return execErr
		}
		if _, execErr := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=? WHERE id=?", gold, actor); execErr != nil {
			return execErr
		}
		snapshot, err = scanGuild(ctx, tx, actor)
		return err
	})
	return snapshot, gold, err
}
