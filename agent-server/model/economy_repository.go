package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type economyCommandState struct {
	identity  InventoryCommandIdentity
	version   uint64
	duplicate bool
	result    []byte
}

type economyReplayResult struct {
	Gold                       uint64      `json:"gold"`
	StorageGold                uint64      `json:"storage_gold,omitempty"`
	Item                       Item        `json:"item"`
	SourceSlot                 byte        `json:"source_slot,omitempty"`
	BuybackSlot                byte        `json:"buyback_slot,omitempty"`
	DestinationSlots           []byte      `json:"destination_slots,omitempty"`
	Amount                     uint16      `json:"amount,omitempty"`
	Specialty                  bool        `json:"specialty,omitempty"`
	TriJobType                 byte        `json:"tri_job_type,omitempty"`
	TriJobReward               uint64      `json:"tri_job_reward,omitempty"`
	TradeScale                 byte        `json:"trade_scale,omitempty"`
	SafeTradeReset             bool        `json:"safe_trade_reset,omitempty"`
	Job                        JobSnapshot `json:"job,omitempty"`
	JobAdvanced                bool        `json:"job_advanced,omitempty"`
	ActivatedDevilSpiritRefID  uint32      `json:"activated_devil_spirit_ref_id,omitempty"`
	ActivatedDevilSpiritItemID int         `json:"activated_devil_spirit_item_id,omitempty"`
	DevilSpiritExpiresAt       time.Time   `json:"devil_spirit_expires_at,omitempty"`
}

type storageCommandState struct {
	identity  InventoryCommandIdentity
	version   uint64
	duplicate bool
	result    []byte
}

func beginStorageCommand(ctx context.Context, tx *sql.Tx, userID uint32) (storageCommandState, error) {
	identity, ok := inventoryCommand(ctx)
	if !ok {
		return storageCommandState{}, nil
	}
	aggregateID := fmt.Sprintf("%d", userID)
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES ('storage',?,0)", aggregateID); err != nil {
		return storageCommandState{}, err
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`='storage' AND `AGGREGATE_ID`=? FOR UPDATE", aggregateID).Scan(&version); err != nil {
		return storageCommandState{}, err
	}
	var name, kind, storedID, status string
	var result []byte
	err := tx.QueryRowContext(ctx, "SELECT `COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`STATUS`,`RESULT_PAYLOAD` FROM `AEGIS_COMMAND_IDEMPOTENCY` WHERE `IDEMPOTENCY_KEY`=? FOR UPDATE", identity.Key).Scan(&name, &kind, &storedID, &status, &result)
	if err == nil {
		if name != identity.Name || kind != "storage" || storedID != aggregateID || status != "completed" {
			return storageCommandState{}, ErrEconomyConflict
		}
		return storageCommandState{identity: identity, version: version, duplicate: true, result: append([]byte(nil), result...)}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storageCommandState{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY` (`IDEMPOTENCY_KEY`,`COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`) VALUES (?,?,'storage',?,?,'in_progress')", identity.Key, identity.Name, aggregateID, version)
	return storageCommandState{identity: identity, version: version}, err
}

func completeStorageCommand(ctx context.Context, tx *sql.Tx, userID uint32, state storageCommandState, topic string, replay economyReplayResult) error {
	if state.identity.Key == "" {
		return nil
	}
	payload, err := json.Marshal(replay)
	if err != nil {
		return err
	}
	aggregateID := fmt.Sprintf("%d", userID)
	next := state.version + 1
	result, err := tx.ExecContext(ctx, "UPDATE `AEGIS_AGGREGATE_VERSION` SET `VERSION`=? WHERE `AGGREGATE_KIND`='storage' AND `AGGREGATE_ID`=? AND `VERSION`=?", next, aggregateID, state.version)
	if err != nil {
		return err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrEconomyConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,'storage',?,?,?,?,CURRENT_TIMESTAMP(6))", state.identity.Key, aggregateID, next, topic, payload); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE `AEGIS_COMMAND_IDEMPOTENCY` SET `STATUS`='completed',`RESULT_VERSION`=?,`RESULT_PAYLOAD`=?,`COMPLETED_AT`=CURRENT_TIMESTAMP(6) WHERE `IDEMPOTENCY_KEY`=? AND `STATUS`='in_progress'", next, payload, state.identity.Key)
	return err
}

func beginEconomyCommand(ctx context.Context, tx *sql.Tx, characterID int64) (economyCommandState, error) {
	identity, ok := inventoryCommand(ctx)
	if !ok {
		return economyCommandState{}, nil
	}
	aggregateID := fmt.Sprintf("%d", characterID)
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES ('inventory',?,0)", aggregateID); err != nil {
		return economyCommandState{}, err
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`='inventory' AND `AGGREGATE_ID`=? FOR UPDATE", aggregateID).Scan(&version); err != nil {
		return economyCommandState{}, err
	}
	var name, kind, storedID, status string
	var result []byte
	err := tx.QueryRowContext(ctx, "SELECT `COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`STATUS`,`RESULT_PAYLOAD` FROM `AEGIS_COMMAND_IDEMPOTENCY` WHERE `IDEMPOTENCY_KEY`=? FOR UPDATE", identity.Key).Scan(&name, &kind, &storedID, &status, &result)
	if err == nil {
		if name != identity.Name || kind != "inventory" || storedID != aggregateID || status != "completed" {
			return economyCommandState{}, ErrEconomyConflict
		}
		return economyCommandState{identity: identity, version: version, duplicate: true, result: append([]byte(nil), result...)}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return economyCommandState{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY` (`IDEMPOTENCY_KEY`,`COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`) VALUES (?,?,'inventory',?,?,'in_progress')", identity.Key, identity.Name, aggregateID, version)
	return economyCommandState{identity: identity, version: version}, err
}

func completeEconomyCommand(ctx context.Context, tx *sql.Tx, characterID int64, state economyCommandState, topic string, replay economyReplayResult) error {
	if state.identity.Key == "" {
		return nil
	}
	payload, err := json.Marshal(replay)
	if err != nil {
		return err
	}
	aggregateID := fmt.Sprintf("%d", characterID)
	next := state.version + 1
	result, err := tx.ExecContext(ctx, "UPDATE `AEGIS_AGGREGATE_VERSION` SET `VERSION`=? WHERE `AGGREGATE_KIND`='inventory' AND `AGGREGATE_ID`=? AND `VERSION`=?", next, aggregateID, state.version)
	if err != nil {
		return err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrEconomyConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,'inventory',?,?,?,?,CURRENT_TIMESTAMP(6))", state.identity.Key, aggregateID, next, topic, payload); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE `AEGIS_COMMAND_IDEMPOTENCY` SET `STATUS`='completed',`RESULT_VERSION`=?,`RESULT_PAYLOAD`=?,`COMPLETED_AT`=CURRENT_TIMESTAMP(6) WHERE `IDEMPOTENCY_KEY`=? AND `STATUS`='in_progress'", next, payload, state.identity.Key)
	return err
}

var (
	ErrEconomyRepositoryUnavailable = errors.New("economy repository is unavailable")
	ErrEconomyConflict              = errors.New("economy state conflicts with the request")
	ErrEconomyInsufficientGold      = errors.New("insufficient gold")
	ErrEconomyInventoryFull         = errors.New("inventory is full")
	ErrEconomyStorageFull           = errors.New("storage is full")
	ErrEconomyShopGoodNotFound      = errors.New("shop good is unavailable")
	ErrEconomyOutcomeUnknown        = errors.New("economy transaction outcome is unknown")
	ErrSpecialtyTraderOnly          = errors.New("specialty goods require a trader")
	ErrSpecialtyNoCart              = errors.New("specialty goods require an active job cart")
	ErrSpecialtyNotBuyer            = errors.New("NPC is not a specialty-goods buyer")
	ErrSpecialtyCartTooFar          = errors.New("job cart is too far away")
	ErrSpecialtyStolen              = errors.New("specialty goods creator does not match")
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
	COSInventory     Inventory
	Item             Item
	SourceSlot       byte
	BuybackSlot      byte
	DestinationSlots []byte
	Amount           uint16
	Specialty        bool
	TriJobType       byte
	TriJobReward     uint64
	TradeScale       byte
	SafeTradeReset   bool
	Job              JobSnapshot
	JobAdvanced      bool
}

type StorageSnapshot struct {
	Capacity byte
	Gold     uint64
	Items    map[byte]Item
}

type EconomyRepository interface {
	Buy(context.Context, int64, uint32, string, byte, byte, uint16) (EconomyResult, error)
	Sell(context.Context, int64, uint32, uint32, byte, uint16, bool) (EconomyResult, error)
	BuyBack(context.Context, int64, uint32, byte) (EconomyResult, error)
	LoadStorage(context.Context, uint32) (StorageSnapshot, error)
	Deposit(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	Withdraw(context.Context, int64, uint32, byte, byte) (EconomyResult, error)
	DepositStorageGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
	WithdrawStorageGold(context.Context, int64, uint32, uint64) (EconomyResult, error)
	MoveStorage(context.Context, uint32, byte, byte, uint16) error
}

type COSEconomyRepository interface {
	SellCOS(context.Context, int64, uint32, uint32, uint64, byte, uint16, bool) (EconomyResult, error)
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
	command, err := beginEconomyCommand(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if err = tx.Rollback(); err != nil {
			return EconomyResult{}, err
		}
		committed = true
		inventory, loadErr := r.loadInventory(ctx, characterID)
		if loadErr != nil {
			return EconomyResult{}, loadErr
		}
		return EconomyResult{Gold: replay.Gold, Inventory: inventory, Item: replay.Item, DestinationSlots: replay.DestinationSlots, Amount: replay.Amount}, nil
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
	if err = completeEconomyCommand(ctx, tx, characterID, command, "inventory.item.purchased", economyReplayResult{Gold: gold, Item: first, DestinationSlots: free, Amount: quantity}); err != nil {
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

func (r *SQLEconomyRepository) Sell(ctx context.Context, characterID int64, userID, npcRefObjectID uint32, source byte, amount uint16, resetBuyback bool) (EconomyResult, error) {
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
	command, err := beginEconomyCommand(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if err = tx.Rollback(); err != nil {
			return EconomyResult{}, err
		}
		committed = true
		inventory, loadErr := r.loadInventory(ctx, characterID)
		if loadErr != nil {
			return EconomyResult{}, loadErr
		}
		return EconomyResult{Gold: replay.Gold, Inventory: inventory, Item: replay.Item, SourceSlot: replay.SourceSlot, BuybackSlot: replay.BuybackSlot, Amount: replay.Amount, Specialty: replay.Specialty, TriJobType: replay.TriJobType, TriJobReward: replay.TriJobReward}, nil
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
	var creatorName string
	var rent RentInfo
	err = tx.QueryRowContext(ctx, "SELECT i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,d.`SellPrice`,it.`VARIANCE`,it.`OPT_LEVEL`,it.`DATA`,it.`CREATOR_NAME`,it.`RENT_TYPE`,it.`RENT_CAN_DELETE`,it.`RENT_CAN_RECHARGE`,it.`RENT_PERIOD_BEGIN_TIME`,it.`RENT_PERIOD_END_TIME`,it.`RENT_METER_RATE_TIME`,it.`RENT_PACKING_TIME` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE i.`FK_CHAR`=? AND i.`SLOT`=? AND d.`CanSell`<>0 FOR UPDATE", characterID, source).Scan(&itemID, &refID, &stack, &sellPrice, &variance, &opt, &data, &creatorName, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime)
	if err != nil || stack < int(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	sellPrice, triJobType, specialty, err := specialtySellPrice(ctx, tx, characterID, npcRefObjectID, refID, sellPrice, creatorName)
	if err != nil {
		return EconomyResult{}, err
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
		inserted, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE `id`=?", amount, itemID)
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
	soldItem, err := economyItem(soldItemID, refID, variance, int(amount), opt, data, rent)
	if err != nil {
		return EconomyResult{}, err
	}
	soldItem.CreatorName = creatorName
	reward := sellPrice * uint64(amount)
	triJobReward := SpecialtyTriJobReward(reward)
	if err = completeEconomyCommand(ctx, tx, characterID, command, "inventory.item.sold", economyReplayResult{Gold: gold, Item: soldItem, SourceSlot: source, BuybackSlot: buybackSlot, Amount: amount, Specialty: specialty, TriJobType: triJobType, TriJobReward: triJobReward}); err != nil {
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
	return EconomyResult{Gold: gold, Inventory: inventory, Item: soldItem, SourceSlot: source, BuybackSlot: buybackSlot, Amount: amount, Specialty: specialty, TriJobType: triJobType, TriJobReward: triJobReward}, nil
}

func (r *SQLEconomyRepository) SellCOS(ctx context.Context, characterID int64, userID, npcRefObjectID uint32, cosID uint64, source byte, amount uint16, resetBuyback bool) (EconomyResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || npcRefObjectID == 0 || cosID == 0 || amount == 0 {
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
	command, err := beginEconomyCommand(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if err = tx.Rollback(); err != nil {
			return EconomyResult{}, err
		}
		committed = true
		cosRepository, repoErr := NewCOSRepository(r.shard)
		if repoErr != nil {
			return EconomyResult{}, repoErr
		}
		cosInventory, loadErr := cosRepository.LoadInventory(ctx, int(characterID), cosID)
		if loadErr != nil {
			return EconomyResult{}, loadErr
		}
		return EconomyResult{Gold: replay.Gold, COSInventory: cosInventory, Item: replay.Item, SourceSlot: replay.SourceSlot, BuybackSlot: replay.BuybackSlot, Amount: replay.Amount, Specialty: replay.Specialty, TriJobType: replay.TriJobType, TriJobReward: replay.TriJobReward, TradeScale: replay.TradeScale, SafeTradeReset: replay.SafeTradeReset, Job: replay.Job, JobAdvanced: replay.JobAdvanced}, nil
	}
	var kind byte
	if err = tx.QueryRowContext(ctx, "SELECT `KIND` FROM `CHAR_COS` WHERE `ID`=? AND `OWNER_CHAR_ID`=? FOR UPDATE", cosID, characterID).Scan(&kind); err != nil || kind != byte(COSJobTransport) {
		return EconomyResult{}, ErrSpecialtyNoCart
	}
	if resetBuyback {
		if err = clearBuybackItems(ctx, tx, characterID); err != nil {
			return EconomyResult{}, err
		}
	}
	var itemID int64
	var refID uint32
	var stack int
	var sellPrice, variance uint64
	var opt byte
	var data uint32
	var creatorName string
	var rent RentInfo
	err = tx.QueryRowContext(ctx, "SELECT ci.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT`,d.`SellPrice`,it.`VARIANCE`,it.`OPT_LEVEL`,it.`DATA`,it.`CREATOR_NAME`,it.`RENT_TYPE`,it.`RENT_CAN_DELETE`,it.`RENT_CAN_RECHARGE`,it.`RENT_PERIOD_BEGIN_TIME`,it.`RENT_PERIOD_END_TIME`,it.`RENT_METER_RATE_TIME`,it.`RENT_PACKING_TIME` FROM `COS_INVENTORY` ci JOIN `ITEM` it ON it.`id`=ci.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=it.`FK_REF_ITEM` WHERE ci.`COS_ID`=? AND ci.`SLOT`=? AND d.`CanSell`<>0 FOR UPDATE", cosID, source).Scan(&itemID, &refID, &stack, &sellPrice, &variance, &opt, &data, &creatorName, &rent.Type, &rent.CanDelete, &rent.CanRecharge, &rent.PeriodBeginTime, &rent.PeriodEndTime, &rent.MeterRateTime, &rent.PackingTime)
	if err != nil || stack < int(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	sellPrice, triJobType, specialty, err := specialtySellPrice(ctx, tx, characterID, npcRefObjectID, refID, sellPrice, creatorName)
	if err != nil || sellPrice > math.MaxUint64/uint64(amount) || gold > math.MaxUint64-sellPrice*uint64(amount) {
		return EconomyResult{}, ErrEconomyConflict
	}
	var preSaleTradeScale byte
	if specialty {
		score, thresholds, scaleErr := safeTradeScaleData(ctx, tx, int(characterID), cosID)
		if scaleErr != nil {
			return EconomyResult{}, scaleErr
		}
		preSaleTradeScale = SafeTradeScale(score, thresholds)
	}
	soldItemID := itemID
	if stack == int(amount) {
		result, deleteErr := tx.ExecContext(ctx, "DELETE FROM `COS_INVENTORY` WHERE `COS_ID`=? AND `SLOT`=? AND `FK_ITEM`=?", cosID, source, itemID)
		if deleteErr != nil || !singleAffectedRow(result) {
			return EconomyResult{}, ErrEconomyConflict
		}
	} else {
		inserted, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE `id`=?", amount, itemID)
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
	reward := sellPrice * uint64(amount)
	var job JobSnapshot
	if specialty {
		jobExperience := SpecialtyDeliveryJobExperience(reward, preSaleTradeScale)
		jobContribution := SpecialtyDeliveryContribution(reward)
		job, err = advanceJobInTransaction(ctx, tx, int(characterID), jobExperience, jobContribution)
		if err != nil {
			return EconomyResult{}, err
		}
	}
	var tradeScale byte
	if specialty {
		score, thresholds, scaleErr := safeTradeScaleData(ctx, tx, int(characterID), cosID)
		if scaleErr != nil {
			return EconomyResult{}, scaleErr
		}
		tradeScale = SafeTradeScale(score, thresholds)
		// SR_GameServer 0x004C8D10 clears the trader's active safe-trade
		// state before completing a specialty delivery. Keep that state change
		// atomic with the cargo removal and gold credit.
		reset, resetErr := tx.ExecContext(ctx, "UPDATE `CHARACTER_SAFE_TRADE` SET `STATUS`=0,`LAST_SAFE_TRADE`=CURRENT_TIMESTAMP(6) WHERE `FK_CHAR`=? AND `STATUS`<>0", characterID)
		if resetErr != nil {
			return EconomyResult{}, resetErr
		}
		if affected, affectedErr := reset.RowsAffected(); affectedErr != nil {
			return EconomyResult{}, affectedErr
		} else if affected > 1 {
			return EconomyResult{}, ErrEconomyConflict
		}
	}
	buybackSlot, err := appendBuybackItem(ctx, tx, characterID, soldItemID, reward)
	if err != nil {
		return EconomyResult{}, err
	}
	gold += reward
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return EconomyResult{}, err
	}
	soldItem, err := economyItem(soldItemID, refID, variance, int(amount), opt, data, rent)
	if err != nil {
		return EconomyResult{}, err
	}
	soldItem.CreatorName = creatorName
	replay := economyReplayResult{Gold: gold, Item: soldItem, SourceSlot: source, BuybackSlot: buybackSlot, Amount: amount, Specialty: specialty, TriJobType: triJobType, TriJobReward: SpecialtyTriJobReward(reward), TradeScale: tradeScale, SafeTradeReset: specialty, Job: job, JobAdvanced: specialty}
	if err = completeEconomyCommand(ctx, tx, characterID, command, "inventory.cos-item.sold", replay); err != nil {
		return EconomyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	cosRepository, err := NewCOSRepository(r.shard)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	cosInventory, err := cosRepository.LoadInventory(ctx, int(characterID), cosID)
	if err != nil {
		return EconomyResult{}, errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	return EconomyResult{Gold: gold, COSInventory: cosInventory, Item: soldItem, SourceSlot: source, BuybackSlot: buybackSlot, Amount: amount, Specialty: specialty, TriJobType: triJobType, TriJobReward: SpecialtyTriJobReward(reward), TradeScale: tradeScale, SafeTradeReset: specialty, Job: job, JobAdvanced: specialty}, nil
}

func SpecialtyDeliveryJobExperience(payout uint64, tradeScale byte) uint64 {
	if payout == 0 {
		return 1
	}
	if tradeScale == 1 {
		return max(uint64(1), payout/5*4+payout%5*4/5)
	}
	return max(uint64(1), payout/2)
}

func SpecialtyDeliveryContribution(payout uint64) uint64 { return payout / 10 }

func specialtySellPrice(ctx context.Context, tx *sql.Tx, characterID int64, npcRefObjectID, refID uint32, ordinarySellPrice uint64, creatorName string) (uint64, byte, bool, error) {
	if creatorName == "" {
		return ordinarySellPrice, 0, false, nil
	}
	var jobType byte
	var alias string
	if err := tx.QueryRowContext(ctx, "SELECT `JOB_TYPE`,COALESCE(`ALIAS`,'') FROM `CHARACTER_JOB` WHERE `FK_CHAR`=? FOR UPDATE", characterID).Scan(&jobType, &alias); err != nil || jobType != JobTrader {
		return 0, 0, true, ErrSpecialtyTraderOnly
	}
	if !strings.EqualFold(alias, creatorName) {
		return 0, 0, true, ErrSpecialtyStolen
	}
	if ordinarySellPrice == 0 || ordinarySellPrice > math.MaxUint32 {
		return 0, 0, true, ErrEconomyConflict
	}
	var baseStock, fluctuate, currentStock int
	if err := tx.QueryRowContext(ctx, "SELECT `BASE_STOCK_AMOUNT`,`FLUCTUATE_AMOUNT`,`CUR_STOCK_AMOUNT` FROM `ITEM_QUOTATION` WHERE `ASSOC_NPC`=? AND `REF_ITEM_ID`=? AND `SERVICE`=1", npcRefObjectID, refID).Scan(&baseStock, &fluctuate, &currentStock); err != nil {
		return 0, 0, true, ErrSpecialtyNotBuyer
	}
	currentQuotation, err := SpecialtyCurrentQuotation(uint32(ordinarySellPrice), baseStock, fluctuate, currentStock)
	if err != nil || currentQuotation < uint32(ordinarySellPrice) {
		return 0, 0, true, ErrEconomyConflict
	}
	return uint64(currentQuotation) - ordinarySellPrice, jobType, true, nil
}

// AddTriJobReward mirrors stock _AddTrijobReward as an intentionally separate
// statement from item sale persistence. The caller logs failure but does not
// roll back an already completed sale.
func (r *SQLEconomyRepository) AddTriJobReward(ctx context.Context, jobType byte, reward uint64) error {
	rewardType, ok := triJobRewardType(jobType)
	if r == nil || r.shard == nil || !ok || reward > math.MaxInt32 {
		return ErrEconomyConflict
	}
	result, err := r.shard.ExecContext(normalizeEconomyContext(ctx), "UPDATE `TRIJOB_REWARDS` SET `REWARD`=`REWARD`+? WHERE `JOB_TYPE`=?", reward, rewardType)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ErrEconomyConflict
	}
	return nil
}

// triJobRewardType mirrors SR_GameServer's _AddTrijobReward adapter: Trader
// role 1 contributes to bucket 3, while Thief role 2 contributes to bucket 2.
func triJobRewardType(jobType byte) (byte, bool) {
	switch jobType {
	case JobTrader:
		return 3, true
	case JobThief:
		return 2, true
	default:
		return 0, false
	}
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
	command, err := beginEconomyCommand(ctx, tx, characterID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if err = tx.Rollback(); err != nil {
			return EconomyResult{}, err
		}
		committed = true
		inventory, loadErr := r.loadInventory(ctx, characterID)
		if loadErr != nil {
			return EconomyResult{}, loadErr
		}
		return EconomyResult{Gold: replay.Gold, Inventory: inventory, Item: replay.Item, SourceSlot: replay.SourceSlot, DestinationSlots: replay.DestinationSlots, Amount: replay.Amount}, nil
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
	item, err := economyItem(itemID, refID, variance, stack, opt, data, rent)
	if err != nil {
		return EconomyResult{}, err
	}
	if err = completeEconomyCommand(ctx, tx, characterID, command, "inventory.item.bought-back", economyReplayResult{Gold: gold, Item: item, SourceSlot: source, DestinationSlots: free, Amount: uint16(stack)}); err != nil {
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
	command, err := beginStorageCommand(ctx, tx, userID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		return EconomyResult{Gold: replay.Gold, StorageGold: replay.StorageGold, Amount: replay.Amount}, nil
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
	topic := "storage.gold.withdrawn"
	if deposit {
		topic = "storage.gold.deposited"
	}
	if err = completeStorageCommand(ctx, tx, userID, command, topic, economyReplayResult{Gold: characterGold, StorageGold: storageGold, Amount: 1}); err != nil {
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
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || (!deposit && destination >= BaseInventorySize) {
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
	command, err := beginStorageCommand(ctx, tx, userID)
	if err != nil {
		return EconomyResult{}, err
	}
	if command.duplicate {
		var replay economyReplayResult
		if err = json.Unmarshal(command.result, &replay); err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if err = tx.Rollback(); err != nil {
			return EconomyResult{}, err
		}
		committed = true
		inv, loadErr := r.loadInventory(ctx, characterID)
		if loadErr != nil {
			return EconomyResult{}, loadErr
		}
		return EconomyResult{Gold: replay.Gold, Inventory: inv, SourceSlot: replay.SourceSlot, DestinationSlots: replay.DestinationSlots}, nil
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO `STORAGE_INFO` (`FK_USER`) VALUES (?)", userID); err != nil {
		return EconomyResult{}, err
	}
	var itemID int64
	if deposit {
		var cosState byte
		err = tx.QueryRowContext(ctx, "SELECT inv.`FK_ITEM`,COALESCE(cos.`STATE`,0) FROM `INVENTORY` inv JOIN `ITEM` it ON it.`ID`=inv.`FK_ITEM` LEFT JOIN `CHAR_COS` cos ON cos.`ID`=it.`DATA` AND cos.`OWNER_CHAR_ID`=inv.`FK_CHAR` WHERE inv.`FK_CHAR`=? AND inv.`SLOT`=? FOR UPDATE", characterID, source).Scan(&itemID, &cosState)
		if err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if cosState == byte(COSItemSummoned) || cosState == byte(COSItemActive) {
			return EconomyResult{}, ErrEconomyConflict
		}
		var capacity int
		if err = tx.QueryRowContext(ctx, "SELECT `CAPACITY` FROM `STORAGE_INFO` WHERE `FK_USER`=? FOR UPDATE", userID).Scan(&capacity); err != nil || capacity <= 0 || capacity > 150 {
			return EconomyResult{}, ErrEconomyConflict
		}
		if int(destination) >= capacity {
			return EconomyResult{}, ErrEconomyStorageFull
		}
		var occupied int64
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
		var cosState byte
		err = tx.QueryRowContext(ctx, "SELECT st.`FK_ITEM`,COALESCE(cos.`STATE`,0) FROM `STORAGE` st JOIN `ITEM` it ON it.`ID`=st.`FK_ITEM` LEFT JOIN `CHAR_COS` cos ON cos.`ID`=it.`DATA` AND cos.`OWNER_CHAR_ID`=? WHERE st.`FK_USER`=? AND st.`SLOT`=? FOR UPDATE", characterID, userID, source).Scan(&itemID, &cosState)
		if err != nil {
			return EconomyResult{}, ErrEconomyConflict
		}
		if cosState == byte(COSItemSummoned) || cosState == byte(COSItemActive) {
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
	topic := "storage.item.withdrawn"
	if deposit {
		topic = "storage.item.deposited"
	}
	if err = completeStorageCommand(ctx, tx, userID, command, topic, economyReplayResult{Gold: gold, SourceSlot: source, DestinationSlots: []byte{destination}}); err != nil {
		return EconomyResult{}, err
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
	command, err := beginStorageCommand(ctx, tx, userID)
	if err != nil {
		return err
	}
	if command.duplicate {
		return nil
	}
	var itemID int64
	var refID uint32
	var stack, maxStack int
	if err = tx.QueryRowContext(ctx, "SELECT s.`FK_ITEM`,i.`FK_REF_ITEM`,i.`STACK_COUNT`,COALESCE(NULLIF(d.`Stack_Size`,0),1) FROM `STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` JOIN `ITEMDATA` d ON d.`ItemID`=i.`FK_REF_ITEM` WHERE s.`FK_USER`=? AND s.`SLOT`=? FOR UPDATE", userID, source).Scan(&itemID, &refID, &stack, &maxStack); err != nil {
		return ErrEconomyConflict
	}
	var occupied int64
	var occupiedRef uint32
	var occupiedStack int
	scanErr := tx.QueryRowContext(ctx, "SELECT s.`FK_ITEM`,i.`FK_REF_ITEM`,i.`STACK_COUNT` FROM `STORAGE` s JOIN `ITEM` i ON i.`id`=s.`FK_ITEM` WHERE s.`FK_USER`=? AND s.`SLOT`=? FOR UPDATE", userID, destination).Scan(&occupied, &occupiedRef, &occupiedStack)
	if resolved, valid := resolveStorageMoveAmount(stack, maxStack, amount, scanErr == nil, occupiedRef == refID, occupiedStack); valid {
		amount = uint16(resolved)
	} else {
		return ErrEconomyConflict
	}
	if scanErr == nil {
		if occupiedRef != refID {
			return ErrEconomyConflict
		}
		if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", occupiedStack+int(amount), occupied, occupiedStack); updateErr != nil || !singleAffectedRow(result) {
			return ErrEconomyConflict
		}
		if int(amount) == stack {
			if result, err := tx.ExecContext(ctx, "DELETE FROM `STORAGE` WHERE `FK_USER`=? AND `SLOT`=? AND `FK_ITEM`=?", userID, source, itemID); err != nil || !singleAffectedRow(result) {
				return ErrEconomyConflict
			}
			if result, err := tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=? AND `STACK_COUNT`=?", itemID, stack); err != nil || !singleAffectedRow(result) {
				return ErrEconomyConflict
			}
		} else if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", stack-int(amount), itemID, stack); updateErr != nil || !singleAffectedRow(result) {
			return ErrEconomyConflict
		}
	} else if !errors.Is(scanErr, sql.ErrNoRows) {
		return scanErr
	} else if int(amount) == stack {
		if result, moveErr := tx.ExecContext(ctx, "UPDATE `STORAGE` SET `SLOT`=? WHERE `FK_USER`=? AND `SLOT`=? AND `FK_ITEM`=?", destination, userID, source, itemID); moveErr != nil || !singleAffectedRow(result) {
			return ErrEconomyConflict
		}
	} else {
		newItemID, splitErr := cloneStorageItemStack(ctx, tx, itemID, amount)
		if splitErr != nil {
			return splitErr
		}
		if result, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", stack-int(amount), itemID, stack); updateErr != nil || !singleAffectedRow(result) {
			return ErrEconomyConflict
		}
		if _, insertErr := tx.ExecContext(ctx, "INSERT INTO `STORAGE` (`FK_USER`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", userID, destination, newItemID); insertErr != nil {
			return insertErr
		}
	}
	if err = completeStorageCommand(ctx, tx, userID, command, "storage.item.moved", economyReplayResult{SourceSlot: source, DestinationSlots: []byte{destination}, Amount: amount}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return errors.Join(ErrEconomyOutcomeUnknown, err)
	}
	committed = true
	return nil
}

func cloneStorageItemStack(ctx context.Context, tx *sql.Tx, itemID int64, amount uint16) (int64, error) {
	result, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) SELECT `VARIANCE`,`FK_REF_ITEM`,?,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME` FROM `ITEM` WHERE `id`=?", amount, itemID)
	if err != nil || !singleAffectedRow(result) {
		return 0, ErrEconomyConflict
	}
	created, err := result.LastInsertId()
	if err != nil || created <= 0 {
		return 0, ErrEconomyConflict
	}
	return created, nil
}

func resolveStorageMoveAmount(sourceStack, maxStack int, requested uint16, targetExists, targetSameReference bool, targetStack int) (int, bool) {
	amount := int(requested)
	if amount == 0 {
		amount = sourceStack
	}
	if sourceStack <= 0 || amount <= 0 || amount > sourceStack {
		return 0, false
	}
	if targetExists && (!targetSameReference || maxStack <= 1 || targetStack <= 0 || targetStack+amount > maxStack) {
		return 0, false
	}
	return amount, true
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
	dbTab := tab
	// NPC shop packets use one-based tab numbers, while SHOP_GOODS is stored zero-based.
	if dbTab > 0 {
		dbTab--
	}
	err := tx.QueryRowContext(normalizeEconomyContext(ctx), "SELECT g.`FK_REF_ITEM`,g.`BUY_PRICE`,g.`SELL_PRICE`,g.`OPT_LEVEL`,g.`VARIANCE`,g.`DATA`,COALESCE(NULLIF(i.`Stack_Size`,0),1),g.`RENT_TYPE`,g.`RENT_CAN_DELETE`,g.`RENT_CAN_RECHARGE`,g.`RENT_PERIOD_BEGIN_TIME`,g.`RENT_PERIOD_END_TIME`,g.`RENT_METER_RATE_TIME`,g.`RENT_PACKING_TIME` FROM `SHOP_GOODS` g JOIN `ITEMDATA` i ON i.`ItemID`=g.`FK_REF_ITEM` WHERE g.`NPC_CODE_NAME`=? AND g.`TAB_INDEX`=? AND g.`SLOT_INDEX`=?", npc, dbTab, slot).Scan(&g.RefItemID, &g.BuyPrice, &g.SellPrice, &g.OptLevel, &g.Variance, &g.Data, &g.MaxStack, &g.RentInfo.Type, &g.RentInfo.CanDelete, &g.RentInfo.CanRecharge, &g.RentInfo.PeriodBeginTime, &g.RentInfo.PeriodEndTime, &g.RentInfo.MeterRateTime, &g.RentInfo.PackingTime)
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
