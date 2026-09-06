package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

func (r *SQLCharacterSkillRepository) WithdrawSkill(ctx context.Context, characterID int, command SkillWithdrawCommand) (result SkillRestorationResult, resultErr error) {
	if r == nil || r.shard == nil {
		return result, ErrSkillRestorationRepositoryAbsent
	}
	if characterID <= 0 || command.ItemRefID == 0 || command.CurrentSkillID == 0 || command.RequiredItemQuantity == 0 {
		return result, ErrSkillRestorationInvalid
	}
	result, resultErr = r.withRestorationTransaction(ctx, characterID, command.ItemRefID, command.RequiredItemQuantity, command.RefundSkillPoints, command.GoldCost, func(tx *sql.Tx) error {
		var enabled byte
		if err := tx.QueryRowContext(ctx, "SELECT `ENABLED` FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? AND `SKILL_ID`=? FOR UPDATE", characterID, command.CurrentSkillID).Scan(&enabled); err != nil {
			return restorationStateError(err)
		}
		if enabled > 1 {
			return ErrSkillRestorationConflict
		}
		if err := rejectForbiddenRestorationSkills(ctx, tx, characterID, command.ForbiddenSkillIDs); err != nil {
			return err
		}
		if command.ResultingSkillID != 0 {
			var exists int
			err := tx.QueryRowContext(ctx, "SELECT 1 FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? AND `SKILL_ID`=? FOR UPDATE", characterID, command.ResultingSkillID).Scan(&exists)
			if err == nil || !errors.Is(err, sql.ErrNoRows) {
				if err != nil {
					return fmt.Errorf("lock resulting skill: %w", err)
				}
				return ErrSkillRestorationConflict
			}
		}
		deleteResult, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? AND `SKILL_ID`=?", characterID, command.CurrentSkillID)
		if err != nil {
			return fmt.Errorf("delete withdrawn skill: %w", err)
		}
		if affected, err := deleteResult.RowsAffected(); err != nil || affected != 1 {
			return ErrSkillRestorationConflict
		}
		if command.ResultingSkillID != 0 {
			if _, err := tx.ExecContext(ctx, "INSERT INTO `SRO_SHARD`.`CHAR_SKILL` (`CHAR_ID`,`SKILL_ID`,`ENABLED`) VALUES (?,?,1)", characterID, command.ResultingSkillID); err != nil {
				return fmt.Errorf("save resulting skill: %w", err)
			}
		}
		return nil
	})
	if resultErr == nil {
		result.ResultingSkillID = command.ResultingSkillID
	}
	return result, resultErr
}

func (r *SQLCharacterSkillRepository) WithdrawMastery(ctx context.Context, characterID int, command MasteryWithdrawCommand) (result SkillRestorationResult, resultErr error) {
	if r == nil || r.shard == nil {
		return result, ErrSkillRestorationRepositoryAbsent
	}
	if characterID <= 0 || command.ItemRefID == 0 || command.MasteryID == 0 || command.ExpectedLevel <= command.TargetLevel || command.RequiredItemQuantity == 0 {
		return result, ErrSkillRestorationInvalid
	}
	result, resultErr = r.withRestorationTransaction(ctx, characterID, command.ItemRefID, command.RequiredItemQuantity, command.RefundSkillPoints, command.GoldCost, func(tx *sql.Tx) error {
		var currentLevel byte
		if err := tx.QueryRowContext(ctx, "SELECT `LEVEL` FROM `SRO_SHARD`.`CHAR_MASTERY` WHERE `CHAR_ID`=? AND `MASTERY_ID`=? FOR UPDATE", characterID, command.MasteryID).Scan(&currentLevel); err != nil {
			return restorationStateError(err)
		}
		if currentLevel != command.ExpectedLevel {
			return ErrSkillRestorationConflict
		}
		if err := rejectForbiddenRestorationSkills(ctx, tx, characterID, command.ForbiddenSkillIDs); err != nil {
			return err
		}
		update, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR_MASTERY` SET `LEVEL`=? WHERE `CHAR_ID`=? AND `MASTERY_ID`=? AND `LEVEL`=?", command.TargetLevel, characterID, command.MasteryID, command.ExpectedLevel)
		if err != nil {
			return fmt.Errorf("save withdrawn mastery: %w", err)
		}
		if affected, err := update.RowsAffected(); err != nil || affected != 1 {
			return ErrSkillRestorationConflict
		}
		return nil
	})
	if resultErr == nil {
		result.Mastery = MasteryProgress{ID: command.MasteryID, Level: command.TargetLevel}
	}
	return result, resultErr
}

func (r *SQLCharacterSkillRepository) withRestorationTransaction(ctx context.Context, characterID int, itemRefID uint32, quantity uint16, refund, goldCost uint64, mutate func(*sql.Tx) error) (result SkillRestorationResult, resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin skill restoration transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, rollbackErr)
		}
	}()

	var skillPoints, gold uint64
	if err := tx.QueryRowContext(ctx, "SELECT `SKILL_POINTS`,`GOLD` FROM `SRO_SHARD`.`CHAR` WHERE `ID`=? FOR UPDATE", characterID).Scan(&skillPoints, &gold); err != nil {
		return result, restorationStateError(err)
	}
	if gold < goldCost {
		return result, ErrSkillRestorationGold
	}
	var itemSlot byte
	var itemID int64
	var stack uint16
	if err := tx.QueryRowContext(ctx, "SELECT inv.`SLOT`,inv.`FK_ITEM`,it.`STACK_COUNT` FROM `SRO_SHARD`.`INVENTORY` inv JOIN `SRO_SHARD`.`ITEM` it ON it.`ID`=inv.`FK_ITEM` WHERE inv.`FK_CHAR`=? AND it.`FK_REF_ITEM`=? AND it.`STACK_COUNT`>=? ORDER BY inv.`SLOT` LIMIT 1 FOR UPDATE", characterID, itemRefID, quantity).Scan(&itemSlot, &itemID, &stack); err != nil {
		return result, restorationStateError(err)
	}
	if mutate == nil {
		return result, ErrSkillRestorationConflict
	}
	if err := mutate(tx); err != nil {
		return result, err
	}
	if refund > math.MaxUint32 || skillPoints > math.MaxUint32-refund {
		return result, ErrSkillRestorationConflict
	}
	skillPoints += refund
	gold -= goldCost
	if _, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`CHAR` SET `SKILL_POINTS`=?,`GOLD`=? WHERE `ID`=?", skillPoints, gold, characterID); err != nil {
		return result, fmt.Errorf("refund restoration skill points: %w", err)
	}
	remaining := stack - quantity
	if remaining == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, itemSlot, itemID); err != nil {
			return result, fmt.Errorf("unlink consumed restoration item: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`ITEM` WHERE `ID`=?", itemID); err != nil {
			return result, fmt.Errorf("delete consumed restoration item: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`ITEM` SET `STACK_COUNT`=? WHERE `ID`=?", remaining, itemID); err != nil {
		return result, fmt.Errorf("consume restoration item quantity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, errors.Join(ErrSkillRestorationConflict, err)
	}
	committed = true
	result.SkillPoints = uint32(skillPoints)
	result.ItemSlot = itemSlot
	result.ItemRemaining = remaining
	result.Gold = gold
	result.GoldCharged = goldCost
	return result, nil
}

func restorationStateError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSkillRestorationConflict
	}
	return fmt.Errorf("lock skill restoration state: %w", err)
}

func rejectForbiddenRestorationSkills(ctx context.Context, tx *sql.Tx, characterID int, skillIDs []uint32) error {
	for _, skillID := range skillIDs {
		var exists int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM `SRO_SHARD`.`CHAR_SKILL` WHERE `CHAR_ID`=? AND `SKILL_ID`=? FOR UPDATE", characterID, skillID).Scan(&exists)
		if err == nil {
			return ErrSkillRestorationDependency
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lock restoration dependency %d: %w", skillID, err)
		}
	}
	return nil
}
