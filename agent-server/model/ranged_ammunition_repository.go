package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

var (
	ErrRangedAmmoRepositoryUnavailable = errors.New("ranged ammunition repository is unavailable")
	ErrRangedAmmoInvalidRequest        = errors.New("invalid ranged ammunition request")
	ErrRangedAmmoUnavailable           = errors.New("equipped ranged ammunition is unavailable")
	ErrRangedAmmoOutcomeUnknown        = errors.New("ranged ammunition transaction outcome is unknown")
)

// RangedAmmoRepository consumes one item from the secondary equipment slot.
// The item ID binds the request to the exact equipped arrow or bolt that was
// admitted by the combat handler, rather than trusting a client-provided slot.
type RangedAmmoRepository interface {
	ConsumeEquippedRangedAmmo(context.Context, int64, int64) (uint16, error)
}

// SQLRangedAmmoRepository owns the transaction that makes a ranged attack's
// ammunition debit durable. It deliberately deletes an exhausted stack and
// its inventory link so a relog cannot restore zero-count ammunition.
type SQLRangedAmmoRepository struct {
	shard *sql.DB
}

var _ RangedAmmoRepository = (*SQLRangedAmmoRepository)(nil)

func NewRangedAmmoRepository(shard *sql.DB) (*SQLRangedAmmoRepository, error) {
	if shard == nil {
		return nil, ErrRangedAmmoRepositoryUnavailable
	}
	return &SQLRangedAmmoRepository{shard: shard}, nil
}

func (r *SQLRangedAmmoRepository) ConsumeEquippedRangedAmmo(ctx context.Context, characterID, itemID int64) (remaining uint16, resultErr error) {
	if r == nil || r.shard == nil {
		return 0, ErrRangedAmmoRepositoryUnavailable
	}
	if characterID <= 0 || itemID <= 0 {
		return 0, ErrRangedAmmoInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("begin ranged ammunition transaction: %w", err)
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

	var stack int
	err = tx.QueryRowContext(ctx, "SELECT it.`STACK_COUNT` FROM `SRO_SHARD`.`INVENTORY` inv JOIN `SRO_SHARD`.`ITEM` it ON it.`ID`=inv.`FK_ITEM` WHERE inv.`FK_CHAR`=? AND inv.`SLOT`=? AND inv.`FK_ITEM`=? FOR UPDATE", characterID, SlotSecondaryWeapon, itemID).Scan(&stack)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRangedAmmoUnavailable
	}
	if err != nil {
		return 0, fmt.Errorf("lock equipped ranged ammunition: %w", err)
	}
	if stack < 1 || stack > math.MaxUint16 {
		return 0, ErrRangedAmmoUnavailable
	}

	stack--
	if stack == 0 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, SlotSecondaryWeapon, itemID); err != nil {
			return 0, fmt.Errorf("unlink exhausted ranged ammunition: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `SRO_SHARD`.`ITEM` WHERE `ID`=?", itemID); err != nil {
			return 0, fmt.Errorf("delete exhausted ranged ammunition: %w", err)
		}
	} else if _, err = tx.ExecContext(ctx, "UPDATE `SRO_SHARD`.`ITEM` SET `STACK_COUNT`=? WHERE `ID`=? AND `STACK_COUNT`=?", stack, itemID, stack+1); err != nil {
		return 0, fmt.Errorf("debit ranged ammunition: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, errors.Join(ErrRangedAmmoOutcomeUnknown, err)
	}
	committed = true
	return uint16(stack), nil
}
