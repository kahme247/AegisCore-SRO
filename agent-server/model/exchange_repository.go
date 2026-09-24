package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
)

var (
	ErrExchangeRepositoryUnavailable = errors.New("exchange repository is unavailable")
	ErrExchangeConflict              = errors.New("exchange state conflicts with persistence")
	ErrExchangeInsufficientGold      = errors.New("insufficient exchange gold")
	ErrExchangeInventoryFull         = errors.New("exchange destination inventory is full")
	ErrExchangeOutcomeUnknown        = errors.New("exchange transaction outcome is unknown")
)

type ExchangeOfferedItem struct {
	SourceSlot   byte
	ExchangeSlot byte
	ItemID       int
}

type ExchangeOffer struct {
	CharacterID int64
	UserID      uint32
	Gold        uint64
	Items       []ExchangeOfferedItem
}

type ExchangeCommitResult struct {
	FirstGold           uint64
	SecondGold          uint64
	FirstInventory      Inventory
	SecondInventory     Inventory
	FirstReceivedSlots  []byte
	SecondReceivedSlots []byte
}

type ExchangeRepository interface {
	CommitExchange(context.Context, ExchangeOffer, ExchangeOffer) (ExchangeCommitResult, error)
}

type SQLExchangeRepository struct{ shard *sql.DB }

func NewExchangeRepository(shard *sql.DB) (*SQLExchangeRepository, error) {
	if shard == nil {
		return nil, ErrExchangeRepositoryUnavailable
	}
	return &SQLExchangeRepository{shard: shard}, nil
}

type lockedExchangeItem struct {
	slot      byte
	itemID    int
	tradable  bool
	reference uint32
}

type lockedExchangeCharacter struct {
	id        int64
	userID    uint32
	gold      uint64
	inventory map[byte]lockedExchangeItem
}

func (r *SQLExchangeRepository) CommitExchange(ctx context.Context, first, second ExchangeOffer) (ExchangeCommitResult, error) {
	if r == nil || r.shard == nil {
		return ExchangeCommitResult{}, ErrExchangeRepositoryUnavailable
	}
	if first.CharacterID <= 0 || second.CharacterID <= 0 || first.CharacterID == second.CharacterID || first.UserID == 0 || second.UserID == 0 {
		return ExchangeCommitResult{}, ErrExchangeConflict
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return ExchangeCommitResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	offers := []ExchangeOffer{first, second}
	if offers[1].CharacterID < offers[0].CharacterID {
		offers[0], offers[1] = offers[1], offers[0]
	}
	locked := make(map[int64]*lockedExchangeCharacter, 2)
	for _, offer := range offers {
		character, lockErr := lockExchangeCharacter(ctx, tx, offer.CharacterID)
		if lockErr != nil || character.userID != offer.UserID {
			return ExchangeCommitResult{}, ErrExchangeConflict
		}
		locked[offer.CharacterID] = character
	}
	for _, offer := range offers {
		if err = lockExchangeInventory(ctx, tx, locked[offer.CharacterID]); err != nil {
			return ExchangeCommitResult{}, err
		}
	}
	if err = validateExchangeOffer(first, locked[first.CharacterID]); err != nil {
		return ExchangeCommitResult{}, err
	}
	if err = validateExchangeOffer(second, locked[second.CharacterID]); err != nil {
		return ExchangeCommitResult{}, err
	}
	firstState, secondState := locked[first.CharacterID], locked[second.CharacterID]
	if firstState.gold < first.Gold || secondState.gold < second.Gold {
		return ExchangeCommitResult{}, ErrExchangeInsufficientGold
	}
	if firstState.gold-first.Gold > math.MaxUint64-second.Gold || secondState.gold-second.Gold > math.MaxUint64-first.Gold {
		return ExchangeCommitResult{}, ErrExchangeConflict
	}
	firstGold := firstState.gold - first.Gold + second.Gold
	secondGold := secondState.gold - second.Gold + first.Gold

	firstSlots, err := exchangeDestinationSlots(firstState.inventory, first.Items, len(second.Items))
	if err != nil {
		return ExchangeCommitResult{}, err
	}
	secondSlots, err := exchangeDestinationSlots(secondState.inventory, second.Items, len(first.Items))
	if err != nil {
		return ExchangeCommitResult{}, err
	}
	for _, transfer := range append(append([]ExchangeOfferedItem(nil), first.Items...), second.Items...) {
		var owner int64
		if containsExchangeItem(first.Items, transfer.ItemID) {
			owner = first.CharacterID
		} else {
			owner = second.CharacterID
		}
		if _, deleteErr := tx.ExecContext(ctx, "DELETE FROM `CHAR_ITEM_SERVICE` WHERE `CHAR_ID`=? AND `TARGET_SLOT`=? AND `SERVICE_KIND` IN (?,?,?)", owner, transfer.SourceSlot, ItemServiceReinforceWeapon, ItemServiceReinforceArmor, ItemServiceReinforceAccessory); deleteErr != nil {
			return ExchangeCommitResult{}, deleteErr
		}
		result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", owner, transfer.SourceSlot, transfer.ItemID)
		if deleteErr != nil {
			return ExchangeCommitResult{}, deleteErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ExchangeCommitResult{}, ErrExchangeConflict
		}
	}
	for i, item := range second.Items {
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", first.CharacterID, firstSlots[i], item.ItemID); err != nil {
			return ExchangeCommitResult{}, err
		}
	}
	for i, item := range first.Items {
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", second.CharacterID, secondSlots[i], item.ItemID); err != nil {
			return ExchangeCommitResult{}, err
		}
	}
	if err = updateExchangeGold(ctx, tx, first.CharacterID, firstGold); err != nil {
		return ExchangeCommitResult{}, err
	}
	if err = updateExchangeGold(ctx, tx, second.CharacterID, secondGold); err != nil {
		return ExchangeCommitResult{}, err
	}
	firstInventory, err := loadInventoryInExchangeTransaction(ctx, tx, first.CharacterID)
	if err != nil {
		return ExchangeCommitResult{}, err
	}
	secondInventory, err := loadInventoryInExchangeTransaction(ctx, tx, second.CharacterID)
	if err != nil {
		return ExchangeCommitResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ExchangeCommitResult{}, errors.Join(ErrExchangeOutcomeUnknown, err)
	}
	committed = true
	return ExchangeCommitResult{
		FirstGold: firstGold, SecondGold: secondGold,
		FirstInventory: firstInventory, SecondInventory: secondInventory,
		FirstReceivedSlots: firstSlots, SecondReceivedSlots: secondSlots,
	}, nil
}

func lockExchangeCharacter(ctx context.Context, tx *sql.Tx, id int64) (*lockedExchangeCharacter, error) {
	state := &lockedExchangeCharacter{id: id, inventory: make(map[byte]lockedExchangeItem)}
	err := tx.QueryRowContext(ctx, "SELECT `FK_USER`,`GOLD` FROM `CHAR` WHERE `id`=? FOR UPDATE", id).Scan(&state.userID, &state.gold)
	return state, err
}

func lockExchangeInventory(ctx context.Context, tx *sql.Tx, state *lockedExchangeCharacter) error {
	rows, err := tx.QueryContext(ctx, "SELECT inv.`SLOT`,it.`id`,it.`FK_REF_ITEM`,d.`CanTrade` FROM `INVENTORY` inv JOIN `ITEM` it ON it.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? ORDER BY inv.`SLOT` FOR UPDATE", state.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item lockedExchangeItem
		if err = rows.Scan(&item.slot, &item.itemID, &item.reference, &legacyBoolScanner{target: &item.tradable}); err != nil {
			return err
		}
		state.inventory[item.slot] = item
	}
	return rows.Err()
}

func validateExchangeOffer(offer ExchangeOffer, state *lockedExchangeCharacter) error {
	if state == nil || len(offer.Items) > 12 {
		return ErrExchangeConflict
	}
	seenItems := make(map[int]struct{}, len(offer.Items))
	seenExchangeSlots := make(map[byte]struct{}, len(offer.Items))
	for _, offered := range offer.Items {
		item, ok := state.inventory[offered.SourceSlot]
		if !ok || offered.SourceSlot < 13 || offered.ExchangeSlot >= 12 || item.itemID != offered.ItemID || !item.tradable {
			return ErrExchangeConflict
		}
		if _, duplicate := seenItems[offered.ItemID]; duplicate {
			return ErrExchangeConflict
		}
		if _, duplicate := seenExchangeSlots[offered.ExchangeSlot]; duplicate {
			return ErrExchangeConflict
		}
		seenItems[offered.ItemID] = struct{}{}
		seenExchangeSlots[offered.ExchangeSlot] = struct{}{}
	}
	return nil
}

func exchangeDestinationSlots(inventory map[byte]lockedExchangeItem, outgoing []ExchangeOfferedItem, incoming int) ([]byte, error) {
	occupied := make(map[byte]struct{}, len(inventory))
	for slot := range inventory {
		occupied[slot] = struct{}{}
	}
	for _, item := range outgoing {
		delete(occupied, item.SourceSlot)
	}
	result := make([]byte, 0, incoming)
	for slot := byte(13); slot < BaseInventorySize && len(result) < incoming; slot++ {
		if _, used := occupied[slot]; !used {
			occupied[slot] = struct{}{}
			result = append(result, slot)
		}
	}
	if len(result) != incoming {
		return nil, ErrExchangeInventoryFull
	}
	return result, nil
}

func containsExchangeItem(items []ExchangeOfferedItem, itemID int) bool {
	for _, item := range items {
		if item.ItemID == itemID {
			return true
		}
	}
	return false
}

func updateExchangeGold(ctx context.Context, tx *sql.Tx, characterID int64, gold uint64) error {
	result, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ErrExchangeConflict
	}
	return nil
}

func loadInventoryInExchangeTransaction(ctx context.Context, tx *sql.Tx, characterID int64) (Inventory, error) {
	rows, err := tx.QueryContext(ctx, SelectCharacterInventory, characterID)
	if err != nil {
		return emptyInventory(), fmt.Errorf("reload exchange inventory: %w", err)
	}
	defer rows.Close()
	inventory := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), scanErr
		}
		inventory.Items[slot] = item
	}
	return inventory, rows.Err()
}

func sortedExchangeItems(items map[byte]ExchangeOfferedItem) []ExchangeOfferedItem {
	result := make([]ExchangeOfferedItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExchangeSlot < result[j].ExchangeSlot })
	return result
}
