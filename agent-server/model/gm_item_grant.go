package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrGMItemInvalidCode = errors.New("GM item code is invalid")
	ErrGMItemInventoryFull = errors.New("GM item inventory is full")
)

type GMItemGrantResult struct {
	Inventory Inventory
	Added     []QuestItemMutation
}

type GMItemGrantRepository interface {
	GrantGMItem(context.Context, int, string, int) (GMItemGrantResult, error)
}

// GrantGMItem persists ordinary inventory items atomically, splitting a requested
// stack by the reference maximum and returning the complete post-commit snapshot.
func (r *SQLQuestRuntimeRepository) GrantGMItem(ctx context.Context, characterID int, codeName string, quantity int) (GMItemGrantResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || quantity <= 0 {
		return GMItemGrantResult{}, ErrGMItemInvalidCode
	}
	var ref RefItem
	found := false
	for _, candidate := range RefItems {
		if candidate.CodeName == codeName {
			ref, found = candidate, true
			break
		}
	}
	if !found || ref.ID == 0 {
		return GMItemGrantResult{}, fmt.Errorf("%w: %s", ErrGMItemInvalidCode, codeName)
	}
	if !isGMPlainStackableExpendable(ref) {
		return GMItemGrantResult{}, fmt.Errorf("%w: %s has an unsupported item payload", ErrGMItemInvalidCode, codeName)
	}
	maxStack := ref.StackSize
	if maxStack <= 0 {
		maxStack = 1
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return GMItemGrantResult{}, err
	}
	committed := false
	defer func() { if !committed { _ = tx.Rollback() } }()
	state, err := lockQuestCharacter(ctx, tx, characterID)
	if err != nil {
		return GMItemGrantResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return GMItemGrantResult{}, err
	}
	result := GMItemGrantResult{}
	for quantity > 0 {
		stack := min(quantity, maxStack)
		slot, available := freeQuestInventorySlot(items, state.inventorySize)
		if !available {
			return GMItemGrantResult{}, ErrGMItemInventoryFull
		}
		item, err := insertGMExpendable(ctx, tx, characterID, slot, ref.ID, stack)
		if err != nil {
			return GMItemGrantResult{}, err
		}
		items[slot] = questInventoryItem{id: int64(item.ID), refID: ref.ID, stack: stack}
		result.Added = append(result.Added, QuestItemMutation{Slot: slot, Item: item})
		quantity -= stack
	}
	if err = tx.Commit(); err != nil {
		return GMItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return GMItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	for index := range result.Added {
		if item, exists := result.Inventory.Items[result.Added[index].Slot]; exists {
			result.Added[index].Item = item
		}
	}
	return result, nil
}

func insertGMExpendable(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32, quantity int) (Item, error) {
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (0,?,?,0,0,0,0,0,0,0,0,0)", refID, quantity)
	if err != nil {
		return Item{}, err
	}
	itemID, err := insert.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
		return Item{}, err
	}
	return economyItem(itemID, refID, 0, quantity, 0, 0, RentInfo{})
}

func isGMPlainStackableExpendable(ref RefItem) bool {
	if !ref.TypeInfo.IsExpendable() {
		return false
	}
	if ref.TypeID3 == 11 {
		return false
	}
	return !(ref.TypeID3 == 5 && ref.TypeID4 == 0) && !(ref.TypeID3 == 14 && ref.TypeID4 == 2)
}
