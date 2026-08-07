package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrItemMallRepositoryUnavailable = errors.New("item mall repository is unavailable")
	ErrItemMallConflict              = errors.New("item mall state conflicts with the request")
	ErrItemMallGoodNotFound          = errors.New("item mall good is unavailable")
	ErrItemMallInsufficientBalance   = errors.New("insufficient item mall balance")
	ErrItemMallInventoryFull         = errors.New("inventory is full")
	ErrItemMallOutcomeUnknown        = errors.New("item mall transaction outcome is unknown")
)

type ItemMallCurrency byte

const (
	ItemMallSilk ItemMallCurrency = iota
	ItemMallGiftSilk
	ItemMallPoint
)

type ItemMallBalances struct {
	Silk     uint32
	GiftSilk uint32
	Point    uint32
}

type ItemMallPosition struct {
	RefShopGroupID uint16
	GroupIndex     byte
	TabIndex       byte
	SlotIndex      byte
}

type ItemMallPurchaseResult struct {
	Balances         ItemMallBalances
	Inventory        Inventory
	Item             Item
	Position         ItemMallPosition
	DestinationSlots []byte
	Quantity         uint16
	ItemService      *ItemServiceEffect
}

type ItemMallRepository interface {
	Balances(context.Context, uint32) (ItemMallBalances, error)
	Purchase(context.Context, int64, uint32, ItemMallPosition, uint16) (ItemMallPurchaseResult, error)
}

type SQLItemMallRepository struct{ shard *sql.DB }

func NewItemMallRepository(shard *sql.DB) (*SQLItemMallRepository, error) {
	if shard == nil {
		return nil, ErrItemMallRepositoryUnavailable
	}
	return &SQLItemMallRepository{shard: shard}, nil
}

func (r *SQLItemMallRepository) Balances(ctx context.Context, userID uint32) (ItemMallBalances, error) {
	if r == nil || r.shard == nil || userID == 0 {
		return ItemMallBalances{}, ErrItemMallConflict
	}
	var balances ItemMallBalances
	err := r.shard.QueryRowContext(normalizeEconomyContext(ctx), "SELECT `SILK`,`GIFT_SILK`,`SILK_POINT` FROM `SRO_ACCOUNT`.`USER` WHERE `id`=?", userID).Scan(&balances.Silk, &balances.GiftSilk, &balances.Point)
	if errors.Is(err, sql.ErrNoRows) {
		return ItemMallBalances{}, ErrItemMallConflict
	}
	return balances, err
}

type itemMallGood struct {
	Position        ItemMallPosition
	PackageID       uint32
	PackageCodeName string
	RefItemID       uint32
	Currency        ItemMallCurrency
	Price           uint32
	Quantity        uint16
	OptLevel        byte
	Variance        uint64
	Data            uint32
	MaxStack        int
	RentInfo        RentInfo
}

func (r *SQLItemMallRepository) Purchase(ctx context.Context, characterID int64, userID uint32, position ItemMallPosition, purchaseCount uint16) (ItemMallPurchaseResult, error) {
	ctx = normalizeEconomyContext(ctx)
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || position.RefShopGroupID == 0 || purchaseCount == 0 {
		return ItemMallPurchaseResult{}, ErrItemMallConflict
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return ItemMallPurchaseResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	owner, _, err := lockEconomyCharacter(ctx, tx, characterID)
	if err != nil || owner != userID {
		return ItemMallPurchaseResult{}, ErrItemMallConflict
	}
	balances, err := lockItemMallBalances(ctx, tx, userID)
	if err != nil {
		return ItemMallPurchaseResult{}, err
	}
	good, err := loadItemMallGood(ctx, tx, position)
	if err != nil {
		return ItemMallPurchaseResult{}, err
	}
	if uint64(good.Price)*uint64(purchaseCount) > math.MaxUint32 || uint64(good.Quantity)*uint64(purchaseCount) > math.MaxUint16 {
		return ItemMallPurchaseResult{}, ErrItemMallConflict
	}
	totalPrice := uint32(uint64(good.Price) * uint64(purchaseCount))
	totalQuantity := uint16(uint64(good.Quantity) * uint64(purchaseCount))
	purchasedQuantity := totalQuantity
	if itemMallBalance(balances, good.Currency) < totalPrice {
		return ItemMallPurchaseResult{}, ErrItemMallInsufficientBalance
	}

	var free []byte
	var first Item
	var serviceEffect *ItemServiceEffect
	if ref, exists := RefItems[good.RefItemID]; exists {
		if effect, effectErr := itemServiceFromReference(ref, time.Now().UTC()); effectErr == nil {
			if purchaseCount != 1 {
				return ItemMallPurchaseResult{}, ErrItemMallConflict
			}
			if err = persistItemService(ctx, tx, characterID, &effect); err != nil {
				return ItemMallPurchaseResult{}, err
			}
			serviceEffect = &effect
			totalQuantity = 0
		}
	}
	if serviceEffect == nil {
		occupied, lockErr := lockCharacterInventoryEconomy(ctx, tx, characterID)
		if lockErr != nil {
			return ItemMallPurchaseResult{}, lockErr
		}
		stacks := itemMallStacks(good.MaxStack, totalQuantity)
		if len(stacks) == 0 {
			return ItemMallPurchaseResult{}, ErrItemMallConflict
		}
		free = freeInventorySlots(occupied, len(stacks))
		if len(free) != len(stacks) {
			return ItemMallPurchaseResult{}, ErrItemMallInventoryFull
		}
		for index, slot := range free {
			stack := stacks[index]
			insert, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)", good.Variance, good.RefItemID, stack, good.OptLevel, good.Data, good.RentInfo.Type, good.RentInfo.CanDelete, good.RentInfo.CanRecharge, good.RentInfo.PeriodBeginTime, good.RentInfo.PeriodEndTime, good.RentInfo.MeterRateTime, good.RentInfo.PackingTime)
			if insertErr != nil {
				return ItemMallPurchaseResult{}, insertErr
			}
			itemID, insertErr := insert.LastInsertId()
			if insertErr != nil {
				return ItemMallPurchaseResult{}, insertErr
			}
			if _, insertErr = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); insertErr != nil {
				return ItemMallPurchaseResult{}, insertErr
			}
			if index == 0 {
				first, insertErr = economyItem(itemID, good.RefItemID, good.Variance, stack, good.OptLevel, good.Data, good.RentInfo)
				if insertErr != nil {
					return ItemMallPurchaseResult{}, insertErr
				}
			}
		}
	}

	balances, err = debitItemMallBalance(ctx, tx, userID, balances, good.Currency, totalPrice)
	if err != nil {
		return ItemMallPurchaseResult{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_MALL_PURCHASE` (`FK_USER`,`FK_CHAR`,`PACKAGE_ID`,`PACKAGE_CODE_NAME`,`FK_REF_ITEM`,`CURRENCY`,`PRICE`,`QUANTITY`) VALUES (?,?,?,?,?,?,?,?)", userID, characterID, good.PackageID, good.PackageCodeName, good.RefItemID, good.Currency, totalPrice, purchasedQuantity); err != nil {
		return ItemMallPurchaseResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ItemMallPurchaseResult{}, errors.Join(ErrItemMallOutcomeUnknown, err)
	}
	committed = true
	inventory, err := NewInventoryRepository(r.shard)
	if err != nil {
		return ItemMallPurchaseResult{}, errors.Join(ErrItemMallOutcomeUnknown, err)
	}
	snapshot, err := inventory.GetCharacterInventory(ctx, characterID)
	if err != nil {
		return ItemMallPurchaseResult{}, errors.Join(ErrItemMallOutcomeUnknown, err)
	}
	return ItemMallPurchaseResult{Balances: balances, Inventory: snapshot, Item: first, Position: position, DestinationSlots: free, Quantity: totalQuantity, ItemService: serviceEffect}, nil
}

func persistItemService(ctx context.Context, tx *sql.Tx, characterID int64, effect *ItemServiceEffect) error {
	var currentExpiration time.Time
	err := tx.QueryRowContext(ctx, "SELECT `EXPIRES_AT` FROM `CHAR_ITEM_SERVICE` WHERE `CHAR_ID`=? AND `SERVICE_KIND`=? FOR UPDATE", characterID, effect.Kind).Scan(&currentExpiration)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lock character item service: %w", err)
	}
	base := effect.ActivatedAt
	if currentExpiration.After(base) {
		base = currentExpiration
	}
	effect.ExpiresAt = base.Add(effect.ExpiresAt.Sub(effect.ActivatedAt))
	_, err = tx.ExecContext(ctx, "INSERT INTO `CHAR_ITEM_SERVICE` (`CHAR_ID`,`SERVICE_KIND`,`SOURCE_REF_ITEM_ID`,`EXPERIENCE_BONUS_PERCENT`,`SKILL_EXPERIENCE_BONUS_PERCENT`,`DAILY_BUDGET_MS`,`ACTIVATED_AT`,`EXPIRES_AT`) VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE `SOURCE_REF_ITEM_ID`=VALUES(`SOURCE_REF_ITEM_ID`),`EXPERIENCE_BONUS_PERCENT`=VALUES(`EXPERIENCE_BONUS_PERCENT`),`SKILL_EXPERIENCE_BONUS_PERCENT`=VALUES(`SKILL_EXPERIENCE_BONUS_PERCENT`),`DAILY_BUDGET_MS`=VALUES(`DAILY_BUDGET_MS`),`ACTIVATED_AT`=VALUES(`ACTIVATED_AT`),`EXPIRES_AT`=VALUES(`EXPIRES_AT`)", characterID, effect.Kind, effect.SourceRefItemID, effect.ExperienceBonusPercent, effect.SkillExperienceBonusPercent, effect.DailyBudget.Milliseconds(), effect.ActivatedAt, effect.ExpiresAt)
	if err != nil {
		return fmt.Errorf("persist character item service: %w", err)
	}
	return nil
}

func lockItemMallBalances(ctx context.Context, tx *sql.Tx, userID uint32) (ItemMallBalances, error) {
	var balances ItemMallBalances
	err := tx.QueryRowContext(ctx, "SELECT `SILK`,`GIFT_SILK`,`SILK_POINT` FROM `SRO_ACCOUNT`.`USER` WHERE `id`=? FOR UPDATE", userID).Scan(&balances.Silk, &balances.GiftSilk, &balances.Point)
	if errors.Is(err, sql.ErrNoRows) {
		return ItemMallBalances{}, ErrItemMallConflict
	}
	return balances, err
}

func loadItemMallGood(ctx context.Context, tx *sql.Tx, position ItemMallPosition) (itemMallGood, error) {
	good := itemMallGood{Position: position}
	err := tx.QueryRowContext(ctx, "SELECT g.`PACKAGE_ID`,g.`PACKAGE_CODE_NAME`,g.`FK_REF_ITEM`,g.`CURRENCY`,g.`PRICE`,g.`QUANTITY`,g.`OPT_LEVEL`,g.`VARIANCE`,g.`DATA`,COALESCE(NULLIF(i.`Stack_Size`,0),1),g.`RENT_TYPE`,g.`RENT_CAN_DELETE`,g.`RENT_CAN_RECHARGE`,g.`RENT_PERIOD_BEGIN_TIME`,g.`RENT_PERIOD_END_TIME`,g.`RENT_METER_RATE_TIME`,g.`RENT_PACKING_TIME` FROM `ITEM_MALL_GOODS` g JOIN `ITEMDATA` i ON i.`ItemID`=g.`FK_REF_ITEM` AND i.`CashItem`<>0 WHERE g.`REF_SHOP_GROUP_ID`=? AND g.`GROUP_INDEX`=? AND g.`TAB_INDEX`=? AND g.`SLOT_INDEX`=? AND g.`ENABLED`<>0", position.RefShopGroupID, position.GroupIndex, position.TabIndex, position.SlotIndex).Scan(&good.PackageID, &good.PackageCodeName, &good.RefItemID, &good.Currency, &good.Price, &good.Quantity, &good.OptLevel, &good.Variance, &good.Data, &good.MaxStack, &good.RentInfo.Type, &good.RentInfo.CanDelete, &good.RentInfo.CanRecharge, &good.RentInfo.PeriodBeginTime, &good.RentInfo.PeriodEndTime, &good.RentInfo.MeterRateTime, &good.RentInfo.PackingTime)
	if errors.Is(err, sql.ErrNoRows) {
		return itemMallGood{}, ErrItemMallGoodNotFound
	}
	if err == nil && good.Currency > ItemMallPoint {
		return itemMallGood{}, ErrItemMallConflict
	}
	return good, err
}

func itemMallBalance(balances ItemMallBalances, currency ItemMallCurrency) uint32 {
	switch currency {
	case ItemMallSilk:
		return balances.Silk
	case ItemMallGiftSilk:
		return balances.GiftSilk
	case ItemMallPoint:
		return balances.Point
	default:
		return 0
	}
}

func debitItemMallBalance(ctx context.Context, tx *sql.Tx, userID uint32, balances ItemMallBalances, currency ItemMallCurrency, amount uint32) (ItemMallBalances, error) {
	columns := [...]string{"SILK", "GIFT_SILK", "SILK_POINT"}
	if currency > ItemMallPoint {
		return ItemMallBalances{}, ErrItemMallConflict
	}
	column := columns[currency]
	result, err := tx.ExecContext(ctx, fmt.Sprintf("UPDATE `SRO_ACCOUNT`.`USER` SET `%s`=`%s`-? WHERE `id`=? AND `%s`>=?", column, column, column), amount, userID, amount)
	if err != nil {
		return ItemMallBalances{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ItemMallBalances{}, err
	}
	if affected != 1 {
		return ItemMallBalances{}, ErrItemMallInsufficientBalance
	}
	switch currency {
	case ItemMallSilk:
		balances.Silk -= amount
	case ItemMallGiftSilk:
		balances.GiftSilk -= amount
	case ItemMallPoint:
		balances.Point -= amount
	}
	return balances, nil
}

func itemMallStacks(maxStack int, quantity uint16) []int {
	if maxStack < 1 || quantity == 0 {
		return nil
	}
	remaining := int(quantity)
	stacks := make([]int, 0, (remaining+maxStack-1)/maxStack)
	for remaining > 0 {
		stack := min(remaining, maxStack)
		stacks = append(stacks, stack)
		remaining -= stack
	}
	return stacks
}
