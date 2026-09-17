package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (r *SQLSocialRepository) RegisterFortressStructure(ctx context.Context, structure FortressStructure) error {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	if structure.FortressID <= 0 || structure.RefEventID == 0 || structure.UniqueID == 0 || structure.RefObjectID == 0 || structure.MaximumHP == 0 || structure.CurrentHP > structure.MaximumHP || structure.State > 2 || structure.UpdatedAt.IsZero() {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var period FortressPeriod
		if err := tx.QueryRowContext(ctx, "SELECT PERIOD FROM FORTRESS WHERE ID=? FOR UPDATE", structure.FortressID).Scan(&period); err != nil {
			return ErrSocialNotFound
		}
		if period == FortressPeriodBattle {
			return ErrSocialConflict
		}
		var occupying any
		if structure.OccupyingGuild > 0 {
			occupying = structure.OccupyingGuild
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO FORTRESS_STRUCTURE_RUNTIME (FK_FORTRESS,REF_EVENT_STRUCT_ID,UNIQUE_ID,REF_OBJECT_ID,IS_HEART,CURRENT_HP,MAX_HP,STATE,OCCUPYING_GUILD,UPDATED_AT) VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE UNIQUE_ID=VALUES(UNIQUE_ID),REF_OBJECT_ID=VALUES(REF_OBJECT_ID),IS_HEART=VALUES(IS_HEART),CURRENT_HP=VALUES(CURRENT_HP),MAX_HP=VALUES(MAX_HP),STATE=VALUES(STATE),OCCUPYING_GUILD=VALUES(OCCUPYING_GUILD),UPDATED_AT=VALUES(UPDATED_AT)", structure.FortressID, structure.RefEventID, structure.UniqueID, structure.RefObjectID, structure.Heart, structure.CurrentHP, structure.MaximumHP, structure.State, occupying, structure.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("register fortress structure: %w", err)
		}
		return nil
	})
}

func (r *SQLSocialRepository) DamageFortressStructure(ctx context.Context, fortressID int, uniqueID uint32, actor int, amount uint32, at time.Time) (result FortressDamageResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrSocialRepositoryUnavailable
	}
	if fortressID < 0 || uniqueID == 0 || actor <= 0 || amount == 0 || at.IsZero() {
		return result, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if fortressID == 0 {
			if scanErr := tx.QueryRowContext(ctx, "SELECT FK_FORTRESS FROM FORTRESS_STRUCTURE_RUNTIME WHERE UNIQUE_ID=?", uniqueID).Scan(&fortressID); scanErr != nil {
				return ErrSocialNotFound
			}
		}
		var owner, temporary sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, "SELECT PERIOD,FK_GUILD,TEMP_GUILD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&result.Siege.Period, &owner, &temporary); scanErr != nil {
			return ErrSocialNotFound
		}
		if result.Siege.Period != FortressPeriodBattle {
			return ErrSocialConflict
		}
		guildID, role, roleErr := fortressCombatGuild(ctx, tx, fortressID, actor, owner, temporary)
		if roleErr != nil || role != FortressRequestAttack {
			return ErrSocialAccessDenied
		}
		result.Siege.FortressID = fortressID
		result.Siege.OwnerGuildID = int(owner.Int64)
		result.Siege.TemporaryGuildID = int(temporary.Int64)
		var occupying sql.NullInt64
		if scanErr := tx.QueryRowContext(ctx, "SELECT REF_EVENT_STRUCT_ID,REF_OBJECT_ID,IS_HEART,CURRENT_HP,MAX_HP,STATE,OCCUPYING_GUILD,UPDATED_AT FROM FORTRESS_STRUCTURE_RUNTIME WHERE FK_FORTRESS=? AND UNIQUE_ID=? FOR UPDATE", fortressID, uniqueID).Scan(&result.Structure.RefEventID, &result.Structure.RefObjectID, &result.Structure.Heart, &result.Structure.CurrentHP, &result.Structure.MaximumHP, &result.Structure.State, &occupying, &result.Structure.UpdatedAt); scanErr != nil {
			return ErrSocialNotFound
		}
		result.Structure.FortressID = fortressID
		result.Structure.UniqueID = uniqueID
		result.Structure.OccupyingGuild = int(occupying.Int64)
		if result.Structure.CurrentHP == 0 || result.Structure.State == 1 {
			return ErrSocialConflict
		}
		result.Applied = amount
		if result.Applied >= result.Structure.CurrentHP {
			result.Applied = result.Structure.CurrentHP
			result.Structure.CurrentHP = 0
			result.Structure.State = 1
			result.Destroyed = true
		} else {
			result.Structure.CurrentHP -= result.Applied
		}
		result.Structure.UpdatedAt = at.UTC()
		result.Structure.OccupyingGuild = guildID
		update, execErr := tx.ExecContext(ctx, "UPDATE FORTRESS_STRUCTURE_RUNTIME SET CURRENT_HP=?,STATE=?,OCCUPYING_GUILD=?,UPDATED_AT=? WHERE FK_FORTRESS=? AND UNIQUE_ID=?", result.Structure.CurrentHP, result.Structure.State, guildID, at.UTC(), fortressID, uniqueID)
		if execErr != nil {
			return fmt.Errorf("damage fortress structure: %w", execErr)
		}
		if execErr = requireSocialRow(update); execErr != nil {
			return ErrSocialConflict
		}
		if result.Destroyed && result.Structure.Heart {
			capture, execErr := tx.ExecContext(ctx, "UPDATE FORTRESS SET TEMP_GUILD=?,CAPTURED_AT=?,VERSION=VERSION+1 WHERE ID=? AND PERIOD=?", guildID, at.UTC(), fortressID, FortressPeriodBattle)
			if execErr != nil {
				return fmt.Errorf("capture fortress heart: %w", execErr)
			}
			if execErr = requireSocialRow(capture); execErr != nil {
				return ErrSocialConflict
			}
			result.Captured = true
			result.Siege.TemporaryGuildID = guildID
		}
		return nil
	})
	return result, err
}

func (r *SQLSocialRepository) RecordFortressBattleKill(ctx context.Context, fortressID, killer, victim int, at time.Time) error {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	if fortressID < 0 || killer <= 0 || victim <= 0 || killer == victim || at.IsZero() {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if fortressID == 0 {
			rows, queryErr := tx.QueryContext(ctx, "SELECT ID FROM FORTRESS WHERE PERIOD=? ORDER BY ID LIMIT 2", FortressPeriodBattle)
			if queryErr != nil {
				return fmt.Errorf("resolve active fortress battle: %w", queryErr)
			}
			for rows.Next() {
				if fortressID != 0 {
					_ = rows.Close()
					return ErrSocialConflict
				}
				if queryErr = rows.Scan(&fortressID); queryErr != nil {
					_ = rows.Close()
					return queryErr
				}
			}
			queryErr = rows.Err()
			_ = rows.Close()
			if queryErr != nil {
				return queryErr
			}
			if fortressID == 0 {
				return ErrSocialNotFound
			}
		}
		var period FortressPeriod
		var owner, temporary sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT PERIOD,FK_GUILD,TEMP_GUILD FROM FORTRESS WHERE ID=? FOR UPDATE", fortressID).Scan(&period, &owner, &temporary); err != nil {
			return ErrSocialNotFound
		}
		if period != FortressPeriodBattle {
			return ErrSocialConflict
		}
		killerGuild, killerRole, err := fortressCombatGuild(ctx, tx, fortressID, killer, owner, temporary)
		if err != nil {
			return err
		}
		victimGuild, victimRole, err := fortressCombatGuild(ctx, tx, fortressID, victim, owner, temporary)
		if err != nil {
			return err
		}
		if killerGuild == victimGuild || killerRole == victimRole {
			return ErrSocialAccessDenied
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO FORTRESS_BATTLE_RECORD (FK_FORTRESS,FK_CHAR,KILL_COUNT,DEATH_COUNT,UPDATED_AT) VALUES (?,?,1,0,?) ON DUPLICATE KEY UPDATE KILL_COUNT=KILL_COUNT+1,UPDATED_AT=VALUES(UPDATED_AT)", fortressID, killer, at.UTC()); err != nil {
			return fmt.Errorf("record fortress kill: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO FORTRESS_BATTLE_RECORD (FK_FORTRESS,FK_CHAR,KILL_COUNT,DEATH_COUNT,UPDATED_AT) VALUES (?,?,0,1,?) ON DUPLICATE KEY UPDATE DEATH_COUNT=DEATH_COUNT+1,UPDATED_AT=VALUES(UPDATED_AT)", fortressID, victim, at.UTC()); err != nil {
			return fmt.Errorf("record fortress death: %w", err)
		}
		return nil
	})
}

func fortressCombatGuild(ctx context.Context, tx *sql.Tx, fortressID, actor int, owner, temporary sql.NullInt64) (int, FortressRequestType, error) {
	var guildID int
	if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=?", actor).Scan(&guildID); err != nil {
		return 0, 0, ErrSocialAccessDenied
	}
	if owner.Valid && guildID == int(owner.Int64) {
		return guildID, FortressRequestDefend, nil
	}
	if temporary.Valid && guildID == int(temporary.Int64) {
		return guildID, FortressRequestAttack, nil
	}
	var requestType FortressRequestType
	if err := tx.QueryRowContext(ctx, "SELECT REQUEST_TYPE FROM FORTRESS_APPLICATION WHERE FK_FORTRESS=? AND FK_GUILD=?", fortressID, guildID).Scan(&requestType); err != nil || !requestType.Valid() {
		return 0, 0, ErrSocialAccessDenied
	}
	return guildID, requestType, nil
}
