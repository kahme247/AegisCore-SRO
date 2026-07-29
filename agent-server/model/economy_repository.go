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
	Inventory        Inventory
	Item             Item
	SourceSlot       byte
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
	Sell(context.Context, int64, uint32, byte, uint16) (EconomyResult, error)
	LoadStorage(context.Context, uint32) (StorageSnapshot, error)
	Deposit(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	Withdraw(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
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

func (r *SQLEconomyRepository) Sell(ctx context.Context, characterID int64, userID uint32, source byte, amount uint16) (EconomyResult, error) {
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
	var itemID int64
	var refID uint32
	var stack int
	var sellPrice uint64
	err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,d.`SellPrice` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? AND d.`CanSell`<>0 FOR UPDATE", characterID, source).Scan(&itemID, &refID, &stack, &sellPrice)
	if err != nil || stack < int(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	if sellPrice > math.MaxUint64/uint64(amount) || gold > math.MaxUint64-sellPrice*uint64(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	if stack == int(amount) {
		if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, source, itemID); err != nil {
			return EconomyResult{}, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", itemID); err != nil {
			return EconomyResult{}, err
		}
	} else if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-? WHERE `id`=?", amount, itemID); err != nil {
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
	return EconomyResult{Gold: gold, Inventory: inventory, SourceSlot: source, Amount: amount}, err
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
