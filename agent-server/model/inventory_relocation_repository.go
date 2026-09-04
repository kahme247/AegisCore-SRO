package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	// LockCharacterForInventoryRelocation serializes all inventory kernels for
	// one character, including moves into an empty slot where an item-row lock
	// alone would not protect the absent target row.
	LockCharacterForInventoryRelocation = "SELECT `id` FROM `SRO_SHARD`.`CHAR` WHERE `id`=? FOR UPDATE"
	// LockCharacterInventoryForRelocation locks and verifies the complete
	// normal inventory snapshot before a source/target mutation is attempted.
	LockCharacterInventoryForRelocation = "SELECT `SLOT`, `FK_ITEM` FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? ORDER BY `SLOT` ASC FOR UPDATE"

	MoveInventoryRelocationSource   = "UPDATE `SRO_SHARD`.`INVENTORY` SET `SLOT`=? WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?"
	DeleteInventoryRelocationSource = "DELETE FROM `SRO_SHARD`.`INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?"
	MoveInventoryRelocationTarget   = "UPDATE `SRO_SHARD`.`INVENTORY` SET `SLOT`=? WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?"
	InsertInventoryRelocationSource = "INSERT INTO `SRO_SHARD`.`INVENTORY` (`FK_CHAR`, `SLOT`, `FK_ITEM`) VALUES(?, ?, ?)"
)

var (
	ErrInventoryRelocationRepositoryUnavailable = errors.New("inventory relocation repository is unavailable")
	ErrInventoryRelocationCharacterIDRequired   = errors.New("inventory relocation character ID is required")
	ErrInventoryRelocationCharacterNotFound     = errors.New("inventory relocation character was not found")
	ErrInventoryRelocationConflict              = errors.New("inventory relocation conflicts with persisted inventory")
	// ErrInventoryRelocationOutcomeUnknown means Commit returned an error after
	// the database may already have applied the transaction. Callers must not
	// retain or mutate their in-memory inventory; they should fail closed and
	// rebuild state from persistence on a fresh session.
	ErrInventoryRelocationOutcomeUnknown = errors.New("inventory relocation commit outcome is unknown")
)

// InventoryRelocationRepositoryError names the failed database phase while
// retaining a root error for errors.Is/errors.As. It is intentionally a
// separate type from InventoryRepositoryError because relocation starts and
// owns a transaction rather than operating over an arbitrary DBTX.
type InventoryRelocationRepositoryError struct {
	Operation string
	Err       error
}

func (e *InventoryRelocationRepositoryError) Error() string {
	if e == nil {
		return "<nil>"
	}
	// Driver errors can include deployment-specific server information. Keep
	// the failed phase available to operators while preserving the root cause
	// only through Unwrap for callers that need retry classification.
	if e.Operation == "" {
		return "inventory relocation failed"
	}
	return fmt.Sprintf("inventory relocation %s failed", e.Operation)
}

func (e *InventoryRelocationRepositoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// InventoryRelocator is the narrow persistence boundary required by the
// packet handler. Implementations must return only after the database outcome
// is known; the runtime inventory applies the plan after this call succeeds.
type InventoryRelocator interface {
	Relocate(ctx context.Context, characterID int64, plan InventoryRelocationPlan) error
}

// SQLInventoryRelocationRepository owns relocation transactions over the
// Agent's long-lived shard pool. A *sql.DB is required because the boundary
// must call BeginTx itself and must not create an implicit request-scoped pool.
type SQLInventoryRelocationRepository struct {
	shard *sql.DB
}

var _ InventoryRelocator = (*SQLInventoryRelocationRepository)(nil)

func NewInventoryRelocationRepository(shard *sql.DB) (*SQLInventoryRelocationRepository, error) {
	if shard == nil {
		return nil, newInventoryRelocationRepositoryError("configure shard database", ErrInventoryRelocationRepositoryUnavailable)
	}
	return &SQLInventoryRelocationRepository{shard: shard}, nil
}

// Relocate commits one already-validated, single-item relocation. It first
// locks the character and complete inventory snapshot, verifies that both
// planned item IDs still occupy their expected slots, then performs a move or
// a delete/update/insert swap under the same transaction. No runtime caller
// should apply its plan until this method returns nil.
func (r *SQLInventoryRelocationRepository) Relocate(ctx context.Context, characterID int64, plan InventoryRelocationPlan) error {
	if r == nil || r.shard == nil {
		return newInventoryRelocationRepositoryError("begin relocation transaction", ErrInventoryRelocationRepositoryUnavailable)
	}
	if characterID <= 0 {
		return fmt.Errorf("%w: %d", ErrInventoryRelocationCharacterIDRequired, characterID)
	}
	if err := validateInventoryRelocationPlan(plan); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return newInventoryRelocationRepositoryError("begin relocation transaction", err)
	}

	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return newInventoryRelocationRepositoryError("begin relocation transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Preserve the operation error. database/sql makes this a harmless
			// no-op when Commit already ended the transaction.
			_ = tx.Rollback()
		}
	}()

	if err := lockCharacterForInventoryRelocation(ctx, tx, characterID); err != nil {
		return err
	}
	items, err := lockInventoryForRelocation(ctx, tx, characterID)
	if err != nil {
		return err
	}
	if err := verifyInventoryRelocationPlan(items, plan); err != nil {
		return err
	}
	if err := executeInventoryRelocation(ctx, tx, characterID, plan); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return newInventoryRelocationRepositoryError(
			"commit relocation transaction",
			errors.Join(ErrInventoryRelocationOutcomeUnknown, err),
		)
	}
	committed = true
	return nil
}

func lockCharacterForInventoryRelocation(ctx context.Context, tx *sql.Tx, characterID int64) error {
	var lockedCharacterID int64
	if err := tx.QueryRowContext(ctx, LockCharacterForInventoryRelocation, characterID).Scan(&lockedCharacterID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return newInventoryRelocationRepositoryError("lock character", ErrInventoryRelocationCharacterNotFound)
		}
		return newInventoryRelocationRepositoryError("lock character", err)
	}
	if lockedCharacterID != characterID {
		return newInventoryRelocationRepositoryError("verify locked character", ErrInventoryRelocationConflict)
	}
	return nil
}

func lockInventoryForRelocation(ctx context.Context, tx *sql.Tx, characterID int64) (map[byte]int64, error) {
	rows, err := tx.QueryContext(ctx, LockCharacterInventoryForRelocation, characterID)
	if err != nil {
		return nil, newInventoryRelocationRepositoryError("lock character inventory", err)
	}
	defer func() { _ = rows.Close() }()

	items := make(map[byte]int64)
	for rows.Next() {
		var persistedSlot, itemID int64
		if err := rows.Scan(&persistedSlot, &itemID); err != nil {
			return nil, newInventoryRelocationRepositoryError("scan locked character inventory", err)
		}
		slot, err := validatedPersistedInventorySlot(persistedSlot)
		if err != nil {
			return nil, newInventoryRelocationRepositoryError("validate locked character inventory", err)
		}
		if itemID <= 0 {
			return nil, newInventoryRelocationRepositoryError("validate locked character inventory", ErrInventoryRelocationConflict)
		}
		if _, exists := items[slot]; exists {
			return nil, newInventoryRelocationRepositoryError("validate locked character inventory", ErrInventoryRelocationConflict)
		}
		items[slot] = itemID
	}
	if err := rows.Err(); err != nil {
		return nil, newInventoryRelocationRepositoryError("iterate locked character inventory", err)
	}
	if err := rows.Close(); err != nil {
		return nil, newInventoryRelocationRepositoryError("close locked character inventory", err)
	}
	return items, nil
}

func verifyInventoryRelocationPlan(items map[byte]int64, plan InventoryRelocationPlan) error {
	sourceItemID, sourceExists := items[plan.SourceSlot]
	if !sourceExists || sourceItemID != plan.SourceItemID {
		return newInventoryRelocationRepositoryError("verify relocation source", ErrInventoryRelocationConflict)
	}

	targetItemID, targetExists := items[plan.TargetSlot]
	if plan.TargetItemID == 0 {
		if targetExists {
			return newInventoryRelocationRepositoryError("verify relocation target", ErrInventoryRelocationConflict)
		}
	} else if !targetExists || targetItemID != plan.TargetItemID {
		return newInventoryRelocationRepositoryError("verify relocation target", ErrInventoryRelocationConflict)
	}

	// Verify the entire paired transition against a copy using the same order
	// as the runtime application. This permits an extra item to occupy the
	// normal source slot only after the primary item has vacated it.
	next := make(map[byte]int64, len(items))
	for slot, itemID := range items {
		next[slot] = itemID
	}
	next[plan.TargetSlot] = plan.SourceItemID
	if plan.TargetItemID == 0 {
		delete(next, plan.SourceSlot)
	} else {
		next[plan.SourceSlot] = plan.TargetItemID
	}
	for _, move := range plan.AdditionalMoves {
		itemID, exists := next[move.SourceSlot]
		if !exists || itemID != move.SourceItemID {
			return newInventoryRelocationRepositoryError("verify displaced relocation source", ErrInventoryRelocationConflict)
		}
		if _, occupied := next[move.TargetSlot]; occupied {
			return newInventoryRelocationRepositoryError("verify displaced relocation target", ErrInventoryRelocationConflict)
		}
		next[move.TargetSlot] = itemID
		delete(next, move.SourceSlot)
	}
	return nil
}

func executeInventoryRelocation(ctx context.Context, tx *sql.Tx, characterID int64, plan InventoryRelocationPlan) error {
	if plan.TargetItemID == 0 {
		if err := executeInventoryRelocationStatement(
			ctx,
			tx,
			"move source item",
			MoveInventoryRelocationSource,
			plan.TargetSlot,
			characterID,
			plan.SourceSlot,
			plan.SourceItemID,
		); err != nil {
			return err
		}
	} else if err := executeInventoryRelocationStatement(
		ctx,
		tx,
		"remove source item for swap",
		DeleteInventoryRelocationSource,
		characterID,
		plan.SourceSlot,
		plan.SourceItemID,
	); err != nil {
		return err
	} else if err := executeInventoryRelocationStatement(
		ctx,
		tx,
		"move target item for swap",
		MoveInventoryRelocationTarget,
		plan.SourceSlot,
		characterID,
		plan.TargetSlot,
		plan.TargetItemID,
	); err != nil {
		return err
	} else if err := executeInventoryRelocationStatement(
		ctx,
		tx,
		"insert source item for swap",
		InsertInventoryRelocationSource,
		characterID,
		plan.TargetSlot,
		plan.SourceItemID,
	); err != nil {
		return err
	}
	for _, move := range plan.AdditionalMoves {
		if err := executeInventoryRelocationStatement(ctx, tx, "move displaced equipment item", MoveInventoryRelocationSource, move.TargetSlot, characterID, move.SourceSlot, move.SourceItemID); err != nil {
			return err
		}
	}
	return nil
}

func executeInventoryRelocationStatement(ctx context.Context, tx *sql.Tx, operation, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return newInventoryRelocationRepositoryError(operation, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return newInventoryRelocationRepositoryError(operation, err)
	}
	if rowsAffected != 1 {
		return newInventoryRelocationRepositoryError(operation, ErrInventoryRelocationConflict)
	}
	return nil
}

func newInventoryRelocationRepositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &InventoryRelocationRepositoryError{Operation: operation, Err: err}
}
