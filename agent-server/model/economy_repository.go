package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

var (
	ErrEconomyRepositoryUnavailable = errors.New("economy repository is unavailable")
	ErrEconomyConflict              = errors.New("economy state conflicts with the request")
	ErrEconomyInsufficientGold      = errors.New("insufficient gold")
	ErrEconomyInventoryFull         = errors.New("inventory is full")
	ErrEconomyStorageFull           = errors.New("storage is full")
	ErrEconomyShopGoodNotFound      = errors.New("shop good is unavailable")
	ErrEconomyOutcomeUnknown        = errors.New("economy transaction outcome is unknown")
)

type ShopGood struct {
	NPCCodeName string
	TabIndex    byte
	SlotIndex   byte
	RefItemID   uint32
	BuyPrice    uint64
	SellPrice   uint64
	OptLevel    byte
	Variance    uint64
	Data        uint32
	MaxStack    int
	RentInfo    RentInfo
}

type EconomyResult struct {
	Gold             uint64
	StorageGold      uint64
	Inventory        Inventory
	Item             Item
	SourceSlot       byte
	BuybackSlot      byte
	DestinationSlots []byte
	Amount           uint16
}

type StorageSnapshot struct {
	Capacity byte
	Gold     uint64
	Items    map[byte]Item
}

type EconomyRepository interface {
	Buy(context.Context, int64, uint32, string, byte, byte, uint16) (EconomyResult, error)
	Sell(context.Context, int64, uint32, byte, uint16, bool) (EconomyResult, error)
	BuyBack(context.Context, int64, uint32, byte) (EconomyResult, error)
	LoadStorage(context.Context, uint32) (StorageSnapshot, error)
	Deposit(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	Withdraw(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	DepositStorageGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
	WithdrawStorageGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
	MoveStorage(context.Context, uint32, byte, byte, uint16) error
}

type SQLEconomyRepository struct{ shard *sql.DB }

func NewEconomyRepository(shard *sql.DB) (*SQLEconomyRepository, error) {
	if shard == nil {
		return nil, ErrEconomyRepositoryUnavailable
	}
	return &SQLEconomyRepository{shard: shard}, nil
}

func (r *SQLEconomyRepository) Buy(ctx context.Context, characterID int64, userID uint32, npc string, tab, shopSlot byte, quantity uint16) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || npc == "" || quantity == 0 {
		return EconomyResult{}, ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return EconomyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return EconomyResult{}, ErrEconomyConflict
	}
	good, err := loadShopGood(ctx, tx, npc, tab, shopSlot)
	if err != nil {
		return EconomyResult{}, err
	}
	if good.BuyPrice > math.MaxUint64/uint64(quantity) {
		return EconomyResult{}, ErrEconomyConflict
	}
	total := good.BuyPrice * uint64(quantity)
	if gold < total {
		return EconomyResult{}, ErrEconomyInsufficientGold
	}

	items, err := lockCharacterInventoryEconomy(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	stacks := purchaseStacks(good.MaxStack, quantity)
	if len(stacks) == 0 {
		return EconomyResult{}, ErrEconomyConflict
	}
	free := freeInventorySlots(items, len(stacks))
	if len(free) != len(stacks) {
		return EconomyResult{}, ErrEconomyInventoryFull
	}

	var first Item
	for index, slot := range free {
		stack := stacks[index]
		result, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", good.Variance, good.RefItemID, stack, good.OptLevel, good.Data, good.RentInfo.Type, good.RentInfo.CanDelete, good.RentInfo.CanRecharge, good.RentInfo.PeriodBeginTime, good.RentInfo.PeriodEndTime, good.RentInfo.MeterRateTime, good.RentInfo.PackingTime)
		if err != nil {
			return EconomyResult{}, err
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return EconomyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
			return EconomyResult{}, err
		}
		if index == 0 {
			first, err = economyItem(itemID, good.RefItemID, good.Variance, stack, good.OptLevel, good.Data, good.RentInfo)
			if err != nil {
				return EconomyResult{}, err
			}
		}
	}
	gold -= total
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return EconomyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	inventory, err := r.loadInventory(ctx, characterID)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	return EconomyResult{Gold: gold, Inventory: inventory, Item: first, DestinationSlots: free, Amount: quantity}, err
}

func (r *SQLEconomyRepository) Sell(ctx context.Context, characterID int64, userID uint32, source byte, amount uint16, resetBuyback bool) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || amount == 0 {
		return EconomyResult{}, ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return EconomyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return EconomyResult{}, ErrEconomyConflict
	}
	if resetBuyback {
		if err = clearBuybackItems(ctx, tx, characterID); err != nil {
			return EconomyResult{}, err
		}
	}
	var itemID int64
	var refID uint32
	var stack int
	var sellPrice uint64
	var variance uint64
	var opt byte
	var data uint32
	var rent RentInfo
	err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,d.`SellPrice`,it.`VARIANCE`,it.`OPT_LEVEL`,it.`DATA`,it.`RENT_TYPE`,it.`RENT_CAN_DELETE`,it.`RENT_CAN_RECHARGE`,it.`RENT_PERIOD_BEGIN_TIME`,it.`RENT_PERIOD_END_TIME`,it.`RENT_METER_RATE_TIME`,it.`RENT_PACKING_TIME` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? AND d.`CanSell`<>0 FOR UPDATE", characterID, source).Scan(&itemID, &refID, &stack, &sellPrice, &variance, &opt, &data, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime)
	if err != nil || stack < int(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	if sellPrice > math.MaxUint64/uint64(amount) || gold > math.MaxUint64-sellPrice*uint64(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	soldItemID := itemID
	if stack == int(amount) {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, source, itemID); err != nil {
			return EconomyResult{}, err
		}
	} else {
		inserted, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE `id`=?", amount, itemID)
		if insertErr != nil {
			return EconomyResult{}, insertErr
		}
		if soldItemID, err = inserted.LastInsertId(); err != nil {
			return EconomyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-? WHERE `id`=?", amount, itemID); err != nil {
			return EconomyResult{}, err
		}
	}
	buybackSlot, err := appendBuybackItem(ctx, tx, characterID, soldItemID, sellPrice*uint64(amount))
	if err != nil {
		return EconomyResult{}, err
	}
	gold += sellPrice * uint64(amount)
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return EconomyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	inventory, err := r.loadInventory(ctx, characterID)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	soldItem, err := economyItem(soldItemID, refID, variance, int(amount), opt, data, rent)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	return EconomyResult{Gold: gold, Inventory: inventory, Item: soldItem, SourceSlot: source, BuybackSlot: buybackSlot, Amount: amount}, nil
}

func clearBuybackItems(ctx context.Context, tx *sql.Tx, characterID int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT `FK_ITEM` FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=? ORDER BY `SLOT` FOR UPDATE", characterID)
	if err != nil {
		return err
	}
	itemIDs := make([]int64, 0, 5)
	for rows.Next() {
		var itemID int64
		if err = rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return err
		}
		itemIDs = append(itemIDs, itemID)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=?", characterID); err != nil {
		return err
	}
	for _, itemID := range itemIDs {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", itemID); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLEconomyRepository) BuyBack(ctx context.Context, characterID int64, userID uint32, source byte) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || source >= 5 {
		return EconomyResult{}, ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return EconomyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return EconomyResult{}, ErrEconomyConflict
	}
	var itemID int64
	var refID uint32
	var stack int
	var price uint64
	var variance uint64
	var opt byte
	var data uint32
	var rent RentInfo
	err = tx.QueryRowContext(ctx, "SELECT b.`FK_ITEM`,i.`FK_REF_ITEM`,i.`STACK_COUNT`,b.`PRICE`,i.`VARIANCE`,i.`OPT_LEVEL`,i.`DATA`,i.`RENT_TYPE`,i.`RENT_CAN_DELETE`,i.`RENT_CAN_RECHARGE`,i.`RENT_PERIOD_BEGIN_TIME`,i.`RENT_PERIOD_END_TIME`,i.`RENT_METER_RATE_TIME`,i.`RENT_PACKING_TIME` FROM `CHAR_BUYBACK` b JOIN `ITEM` i ON i.`id`=b.`FK_ITEM` WHERE b.`FK_CHAR`=? AND b.`SLOT`=? FOR UPDATE", characterID, source).Scan(&itemID, &refID, &stack, &price, &variance, &opt, &data, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime)
	if err != nil || stack < 1 || gold < price {
		return EconomyResult{}, ErrEconomyConflict
	}
	items, err := lockCharacterInventoryEconomy(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	free := freeInventorySlots(items, 1)
	if len(free) != 1 {
		return EconomyResult{}, ErrEconomyInventoryFull
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, free[0], itemID); err != nil {
		return EconomyResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, source, itemID); err != nil {
		return EconomyResult{}, err
	}
	if err = compactBuybackSlots(ctx, tx, characterID, source); err != nil {
		return EconomyResult{}, err
	}
	gold -= price
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return EconomyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	inventory, err := r.loadInventory(ctx, characterID)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	item, err := economyItem(itemID, refID, variance, stack, opt, data, rent)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	return EconomyResult{Gold: gold, Inventory: inventory, Item: item, SourceSlot: source, DestinationSlots: free, Amount: uint16(stack)}, nil
}

func appendBuybackItem(ctx context.Context, tx *sql.Tx, characterID, itemID int64, price uint64) (byte, error) {
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT`,`FK_ITEM` FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=? ORDER BY `SLOT` FOR UPDATE", characterID)
	if err != nil {
		return 0, err
	}
	type entry struct {
		slot   byte
		itemID int64
	}
	entries := make([]entry, 0, 5)
	for rows.Next() {
		var value entry
		if err = rows.Scan(&value.slot, &value.itemID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		entries = append(entries, value)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if len(entries) > 5 {
		return 0, ErrEconomyConflict
	}
	for index, value := range entries {
		if value.slot != byte(index) {
			return 0, ErrEconomyConflict
		}
	}
	if len(entries) == 5 {
		oldest := entries[0]
		if oldest.slot != 0 {
			return 0, ErrEconomyConflict
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=? AND `SLOT`=0 AND `FK_ITEM`=?", characterID, oldest.itemID); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", oldest.itemID); err != nil {
			return 0, err
		}
		if err = compactBuybackSlots(ctx, tx, characterID, 0); err != nil {
			return 0, err
		}
		entries = entries[1:]
	}
	slot := byte(len(entries))
	if _, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_BUYBACK` (`FK_CHAR`,`SLOT`,`FK_ITEM`,`PRICE`) VALUES (?,?,?,?)", characterID, slot, itemID, price); err != nil {
		return 0, err
	}
	return slot + 1, nil
}

func compactBuybackSlots(ctx context.Context, tx *sql.Tx, characterID int64, removed byte) error {
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT` FROM `CHAR_BUYBACK` WHERE `FK_CHAR`=? AND `SLOT`>? ORDER BY `SLOT` FOR UPDATE", characterID, removed)
	if err != nil {
		return err
	}
	slots := make([]byte, 0, 4)
	for rows.Next() {
		var slot byte
		if err = rows.Scan(&slot); err != nil {
			_ = rows.Close()
			return err
		}
		slots = append(slots, slot)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, slot := range slots {
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR_BUYBACK` SET `SLOT`=? WHERE `FK_CHAR`=? AND `SLOT`=?", slot-1, characterID, slot); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLEconomyRepository) LoadStorage(ctx context.Context, userID uint32) (StorageSnapshot, error) {
	if r == nil || r.shard == nil || userID == 0 {
		return StorageSnapshot{}, ErrEconomyConflict
	}
	ctx = normalizeEconomyContext(ctx)
	if _, err := r.shard.ExecContext(ctx, "INSERT IGNORE INTO `STORAGE_INFO` (`FK_USER`) VALUES (?)", userID); err != nil {
		return StorageSnapshot{}, err
	}
	var snapshot StorageSnapshot
	if err := r.shard.QueryRowContext(ctx, "SELECT `CAPACITY`,`GOLD` FROM `STORAGE_INFO` WHERE `FK_USER`=?", userID).Scan(&snapshot.Capacity, &snapshot.Gold); err != nil {
		return StorageSnapshot{}, err
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT s.`SLOT`,i.`id`,i.`VARIANCE`,i.`FK_REF_ITEM`,i.`STACK_COUNT`,i.`OPT_LEVEL`,i.`DATA`,i.`RENT_TYPE`,i.`RENT_CAN_DELETE`,i.`RENT_CAN_RECHARGE`,i.`RENT_PERIOD_BEGIN_TIME`,i.`RENT_PERIOD_END_TIME`,i.`RENT_METER_RATE_TIME`,i.`RENT_PACKING_TIME` FROM `STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` WHERE s.`FK_USER`=? ORDER BY s.`SLOT`", userID)
	if err != nil {
		return StorageSnapshot{}, err
	}
	defer rows.Close()
	snapshot.Items = make(map[byte]Item)
	for rows.Next() {
		var slot byte
		var id int64
		var variance uint64
		var refID uint32
		var stack int
		var opt byte
		var data uint32
		var rent RentInfo
		if err = rows.Scan(&slot, &id, &variance, &refID, &stack, &opt, &data, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime); err != nil {
			return StorageSnapshot{}, err
		}
		item, itemErr := economyItem(id, refID, variance, stack, opt, data, rent)
		if itemErr != nil {
			return StorageSnapshot{}, itemErr
		}
		snapshot.Items[slot] = item
	}
	return snapshot, rows.Err()
}

func (r *SQLEconomyRepository) Deposit(ctx context.Context, characterID int64, userID uint32, source, destination byte) (EconomyResult, error) {
	return r.transfer(ctx, characterID, userID, source, destination, true)
}

func (r *SQLEconomyRepository) Withdraw(ctx context.Context, characterID int64, userID uint32, source, destination byte) (EconomyResult, error) {
	return r.transfer(ctx, characterID, userID, source, destination, false)
}

func (r *SQLEconomyRepository) DepositStorageGold(ctx context.Context, characterID int64, userID uint32, amount uint64) (EconomyResult, error) {
	return r.transferStorageGold(ctx, characterID, userID, amount, true)
}

func (r *SQLEconomyRepository) WithdrawStorageGold(ctx context.Context, characterID int64, userID uint32, amount uint64) (EconomyResult, error) {
	return r.transferStorageGold(ctx, characterID, userID, amount, false)
}

func (r *SQLEconomyRepository) transferStorageGold(ctx context.Context, characterID int64, userID uint32, amount uint64, deposit bool) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || amount == 0 {
		return EconomyResult{}, ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return EconomyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	owner, characterGold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return EconomyResult{}, ErrEconomyConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO `STORAGE_INFO` (`FK_USER`) VALUES (?)", userID); err != nil {
		return EconomyResult{}, err
	}
	var storageGold uint64
	if err = tx.QueryRowContext(ctx, "SELECT `GOLD` FROM `STORAGE_INFO` WHERE `FK_USER`=? FOR UPDATE", userID).Scan(&storageGold); err != nil {
		return EconomyResult{}, err
	}
	if deposit {
		if characterGold < amount || storageGold > math.MaxUint64-amount {
			return EconomyResult{}, ErrEconomyConflict
		}
		characterGold -= amount
		storageGold += amount
	} else {
		if storageGold < amount || characterGold > math.MaxUint64-amount {
			return EconomyResult{}, ErrEconomyConflict
		}
		storageGold -= amount
		characterGold += amount
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=? AND `FK_USER`=?", characterGold, characterID, userID); err != nil {
		return EconomyResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `STORAGE_INFO` SET `GOLD`=? WHERE `FK_USER`=?", storageGold, userID); err != nil {
		return EconomyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	return EconomyResult{Gold: characterGold, StorageGold: storageGold, Amount: 1}, nil
}

func (r *SQLEconomyRepository) transfer(ctx context.Context, characterID int64, userID uint32, source, destination byte, deposit bool) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || destination >= baseStorageCapacity(deposit) {
		return EconomyResult{}, ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return EconomyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	owner, gold, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return EconomyResult{}, ErrEconomyConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO `STORAGE_INFO` (`FK_USER`) VALUES (?)", userID); err != nil {
		return EconomyResult{}, err
	}
	var itemID int64
	if deposit {
		err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, source).Scan(&itemID)
		if err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		var occupied int
		scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `STORAGE` WHERE `FK_USER`=? AND `SLOT`=? FOR UPDATE", userID, destination).Scan(&occupied)
		if scanErr == nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return EconomyResult{}, scanErr
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, source, itemID); err != nil {
			return EconomyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `STORAGE` (`FK_USER`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", userID, destination, itemID); err != nil {
			return EconomyResult{}, err
		}
	} else {
		err = tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `STORAGE` WHERE `FK_USER`=? AND `SLOT`=? FOR UPDATE", userID, source).Scan(&itemID)
		if err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		var occupied int
		scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? FOR UPDATE", characterID, destination).Scan(&occupied)
		if scanErr == nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return EconomyResult{}, scanErr
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `STORAGE` WHERE `FK_USER`=? AND `SLOT`=? AND `FK_ITEM`=?", userID, source, itemID); err != nil {
			return EconomyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, destination, itemID); err != nil {
			return EconomyResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	inv, err := r.loadInventory(ctx, characterID)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	return EconomyResult{Gold: gold, Inventory: inv, SourceSlot: source, DestinationSlots: []byte{destination}}, err
}

func (r *SQLEconomyRepository) MoveStorage(ctx context.Context, userID uint32, source, destination byte, amount uint16) error {
	if r == nil || r.shard == nil || userID == 0 || source >= 150 || destination >= 150 || source == destination {
		return ErrEconomyConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var itemID int64
	var stack int
	if err = tx.QueryRowContext(ctx, "SELECT s.`FK_ITEM`,i.`STACK_COUNT` FROM `STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` WHERE s.`FK_USER`=? AND s.`SLOT`=? FOR UPDATE", userID, source).Scan(&itemID, &stack); err != nil {
		return ErrEconomyConflict
	}
	if amount == 0 {
		amount = uint16(stack)
	}
	if int(amount) != stack {
		return ErrEconomyConflict
	}
	var occupied int
	scanErr := tx.QueryRowContext(ctx, "SELECT `FK_ITEM` FROM `STORAGE` WHERE `FK_USER`=? AND `SLOT`=? FOR UPDATE", userID, destination).Scan(&occupied)
	if scanErr == nil {
		return ErrEconomyConflict
	}
	if !errors.Is(scanErr, sql.ErrNoRows) {
		return scanErr
	}
	if _, err = tx.ExecContext(ctx, "UPDATE `STORAGE` SET `SLOT`=? WHERE `FK_USER`=? AND `SLOT`=? AND `FK_ITEM`=?", destination, userID, source, itemID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	return nil
}

func lockEconomyCharacter(ctx context.Context, tx *sql.Tx, characterID int64) (uint32, uint64, error) {
	var owner uint32
	var gold uint64
	err := tx.QueryRowContext(normalizeEconomyContext(ctx), "SELECT `FK_USER`,`GOLD` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner, &gold)
	return owner, gold, err
}
func loadShopGood(ctx context.Context, tx *sql.Tx, npc string, tab, slot byte) (ShopGood, error) {
	var g ShopGood
	g.NPCCodeName = npc
	g.TabIndex = tab
	g.SlotIndex = slot
	err := tx.QueryRowContext(normalizeEconomyContext(ctx), "SELECT g.`FK_REF_ITEM`,g.`BUY_PRICE`,g.`SELL_PRICE`,g.`OPT_LEVEL`,g.`VARIANCE`,g.`DATA`,COALESCE(NULLIF(i.`Stack_Size`,0),1),g.`RENT_TYPE`,g.`RENT_CAN_DELETE`,g.`RENT_CAN_RECHARGE`,g.`RENT_PERIOD_BEGIN_TIME`,g.`RENT_PERIOD_END_TIME`,g.`RENT_METER_RATE_TIME`,g.`RENT_PACKING_TIME` FROM `SHOP_GOODS` g JOIN `ITEMDATA` i ON i.`ItemID`=g.`FK_REF_ITEM` WHERE g.`NPC_CODE_NAME`=? AND g.`TAB_INDEX`=? AND g.`SLOT_INDEX`=?", npc, tab, slot).Scan(&g.RefItemID, &g.BuyPrice, &g.SellPrice, &g.OptLevel, &g.Variance, &g.Data, &g.MaxStack, &g.RentInfo.Type, &g.RentInfo.CanDelete, &g.RentInfo.CanRecharge, &g.RentInfo.PeriodBeginTime, &g.RentInfo.PeriodEndTime, &g.RentInfo.MeterRateTime, &g.RentInfo.PackingTime)
	if errors.Is(err, sql.ErrNoRows) {
		return ShopGood{}, ErrEconomyShopGoodNotFound
	}
	return g, err
}
func lockCharacterInventoryEconomy(ctx context.Context, tx *sql.Tx, characterID int64) (map[byte]int64, error) {
	rows, err := tx.QueryContext(normalizeEconomyContext(ctx), "SELECT `SLOT`,`FK_ITEM` FROM `INVENTORY` WHERE `FK_CHAR`=? FOR UPDATE", characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := map[byte]int64{}
	for rows.Next() {
		var s byte
		var id int64
		if err = rows.Scan(&s, &id); err != nil {
			return nil, err
		}
		items[s] = id
	}
	return items, rows.Err()
}
func freeInventorySlots(items map[byte]int64, count int) []byte {
	slots := make([]byte, 0, count)
	for s := byte(13); s < BaseInventorySize && len(slots) < count; s++ {
		if _, ok := items[s]; !ok {
			slots = append(slots, s)
		}
	}
	return slots
}

func purchaseStacks(maxStack int, quantity uint16) []int {
	if maxStack < 1 || quantity == 0 || int(quantity) > maxStack {
		return nil
	}
	if maxStack == 1 {
		return []int{1}
	}
	return []int{int(quantity)}
}
func normalizeEconomyContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
func baseStorageCapacity(deposit bool) byte {
	if deposit {
		return 150
	}
	return BaseInventorySize
}
func (r *SQLEconomyRepository) loadInventory(ctx context.Context, characterID int64) (Inventory, error) {
	repo, err := NewInventoryRepository(r.shard)
	if err != nil {
		return Inventory{}, err
	}
	return repo.GetCharacterInventory(normalizeEconomyContext(ctx), characterID)
}
func economyItem(id int64, refID uint32, variance uint64, stack int, opt byte, data uint32, rent RentInfo) (Item, error) {
	ref, ok := RefItems[refID]
	if !ok {
		return Item{}, fmt.Errorf("%w: %d", ErrInventoryReferenceItemNotFound, refID)
	}
	item := Item{ID: int(id), Name: ref.CodeName, Variance: variance, StackSize: stack, OptLevel: opt, Data: data, RentInfo: rent}
	item.RefObjectID = ref.ID
	item.LevelInfo = LevelInfo{RequiredLevelType1: ref.ReqLevelType1, RequiredLevelType2: ref.ReqLevelType2, RequiredLevelType3: ref.ReqLevelType3, RequiredLevelType4: ref.ReqLevelType4, RequiredLevel1: ref.ReqLevel1, RequiredLevel2: ref.ReqLevel2, RequiredLevel3: ref.ReqLevel3, RequiredLevel4: ref.ReqLevel4}
	item.SetTypeInfo(TypeInfo{TypeID1: ref.TypeID1, TypeID2: ref.TypeID2, TypeID3: ref.TypeID3, TypeID4: ref.TypeID4})
	return item, nil
}
