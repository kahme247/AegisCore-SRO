package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

var (
	ErrPackageItemUnavailable         = errors.New("package-item repository is unavailable")
	ErrPackageItemNotFound            = errors.New("package-item selector is not authorized by the active shop")
	ErrPackageItemConflict            = errors.New("package-item catalog is ambiguous or inconsistent")
	ErrPackageItemInsufficientPayment = errors.New("package-item payment is insufficient")
	ErrPackageItemOutcomeUnknown      = errors.New("package-item purchase commit outcome is unknown")
)

type PackageItemMember struct {
	Index       int
	RefItemID   uint32
	OptLevel    byte
	Variance    uint64
	Data        uint32
	MagicParams byte
	MaxStack    uint32
	Bionic      byte
	TypeID1     byte
	TypeID2     byte
	TypeID3     byte
	TypeID4     byte
}

type PackageItemGood struct {
	ID            uint32
	CodeName      string
	PaymentDevice uint32
	Cost          uint32
	Members       []PackageItemMember
}

type PackageItemCatalog interface {
	ResolvePackageItem(context.Context, uint32, byte, byte) (PackageItemGood, error)
}

type PackageItemPurchaseResult struct {
	Gold             uint64
	Inventory        Inventory
	DestinationSlots []byte
	Good             PackageItemGood
	Continuations    []PackageItemTokenContinuation
}

type PackageItemTokenContinuation struct {
	Slot      byte
	Remaining uint16
}

type packageTokenStack struct {
	slot     byte
	itemID   int64
	quantity uint32
}

type packageTokenDebit struct {
	stack     packageTokenStack
	remaining uint32
}

func planPackageTokenDebit(stacks []packageTokenStack, required uint64) ([]packageTokenDebit, error) {
	if required == 0 {
		return nil, ErrPackageItemConflict
	}
	debits := make([]packageTokenDebit, 0, len(stacks))
	for _, stack := range stacks {
		if required == 0 {
			break
		}
		if stack.itemID <= 0 || stack.quantity == 0 || stack.quantity > math.MaxUint16 {
			return nil, ErrPackageItemConflict
		}
		remaining := uint64(stack.quantity)
		if required >= remaining {
			required -= remaining
			remaining = 0
		} else {
			remaining -= required
			required = 0
		}
		debits = append(debits, packageTokenDebit{stack: stack, remaining: uint32(remaining)})
	}
	if required != 0 {
		return nil, ErrPackageItemInsufficientPayment
	}
	return debits, nil
}

type PackageItemPurchaser interface {
	PackageItemCatalog
	PurchasePackageItem(context.Context, int64, uint32, byte, byte, uint16) (PackageItemPurchaseResult, error)
	PurchaseTokenPackageItem(context.Context, int64, uint32, byte, byte, uint16) (PackageItemPurchaseResult, error)
}

type SQLPackageItemRepository struct{ shard *sql.DB }

func NewPackageItemRepository(shard *sql.DB) (*SQLPackageItemRepository, error) {
	if shard == nil {
		return nil, ErrPackageItemUnavailable
	}
	return &SQLPackageItemRepository{shard: shard}, nil
}

const resolvePackageItemQuery = `WITH active_tabs AS (
    SELECT t.COUNTRY,t.CODE_NAME,t.TAB_GROUP_CODE_NAME,
           ROW_NUMBER() OVER (PARTITION BY t.COUNTRY,t.TAB_GROUP_CODE_NAME ORDER BY t.ID)-1 AS TAB_INDEX
    FROM REF_SHOP_TAB_VSRO t
    WHERE t.SERVICE=1
), active_goods AS (
    SELECT goods.COUNTRY,goods.TAB_CODE_NAME,goods.PACKAGE_CODE_NAME,
           pp.PAYMENT_DEVICE,pp.COST,
           ROW_NUMBER() OVER (PARTITION BY goods.COUNTRY,goods.TAB_CODE_NAME
                              ORDER BY goods.SLOT_INDEX,pp.PAYMENT_DEVICE)-1 AS ITEM_INDEX
    FROM REF_SHOP_GOODS_PACKAGE_VSRO goods
    JOIN REF_PRICE_POLICY_ITEM_VSRO pp
      ON pp.COUNTRY=goods.COUNTRY AND pp.SERVICE=1 AND pp.PACKAGE_CODE_NAME=goods.PACKAGE_CODE_NAME
    WHERE goods.SERVICE=1
)
SELECT p.ID,p.CODE_NAME,pp.PAYMENT_DEVICE,pp.COST,
       member.MEMBER_INDEX,item.ItemID,member.OPT_LEVEL,member.VARIANCE,
       member.DATA,member.MAG_PARAM_NUM,COALESCE(NULLIF(item.Stack_Size,0),1),
       COALESCE(item.Bionic,0),item.TypeID_1,item.TypeID_2,item.TypeID_3,item.TypeID_4
FROM CHAR_REF_DATA npc
JOIN REF_SHOP_GROUP_VSRO shop_group
  ON shop_group.COUNTRY=15 AND shop_group.SERVICE=1 AND shop_group.NPC_CODE_NAME=npc.CodeName
JOIN REF_MAPPING_SHOP_GROUP_VSRO group_map
  ON group_map.COUNTRY=shop_group.COUNTRY AND group_map.SERVICE=1 AND group_map.SHOP_GROUP_CODE_NAME=shop_group.CODE_NAME
JOIN REF_MAPPING_SHOP_WITH_TAB_VSRO tab_map
  ON tab_map.COUNTRY=group_map.COUNTRY AND tab_map.SERVICE=1 AND tab_map.SHOP_CODE_NAME=group_map.SHOP_CODE_NAME
JOIN active_tabs tab
  ON tab.COUNTRY=tab_map.COUNTRY AND tab.TAB_GROUP_CODE_NAME=tab_map.TAB_GROUP_CODE_NAME AND tab.TAB_INDEX=?
JOIN active_goods pp
  ON pp.COUNTRY=tab.COUNTRY AND pp.TAB_CODE_NAME=tab.CODE_NAME AND pp.ITEM_INDEX=?
JOIN REF_PACKAGE_ITEM_VSRO p
  ON p.COUNTRY=pp.COUNTRY AND p.SERVICE=1 AND p.CODE_NAME=pp.PACKAGE_CODE_NAME
JOIN REF_SCRAP_OF_PACKAGE_ITEM_VSRO member
  ON member.COUNTRY=p.COUNTRY AND member.SERVICE=1 AND member.PACKAGE_CODE_NAME=p.CODE_NAME
JOIN ITEMDATA item ON item.CodeName=member.ITEM_CODE_NAME
WHERE npc.RefObjID=?
ORDER BY member.MEMBER_INDEX`

func (r *SQLPackageItemRepository) ResolvePackageItem(ctx context.Context, npcRefObjectID uint32, tab, slot byte) (PackageItemGood, error) {
	if r == nil || r.shard == nil {
		return PackageItemGood{}, ErrPackageItemUnavailable
	}
	return resolvePackageItemWithQueryer(ctx, r.shard, npcRefObjectID, tab, slot)
}

type packageItemQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func resolvePackageItemWithQueryer(ctx context.Context, queryer packageItemQueryer, npcRefObjectID uint32, tab, slot byte) (PackageItemGood, error) {
	if npcRefObjectID == 0 {
		return PackageItemGood{}, ErrPackageItemNotFound
	}
	rows, err := queryer.QueryContext(ctx, resolvePackageItemQuery, tab, slot, npcRefObjectID)
	if err != nil {
		return PackageItemGood{}, err
	}
	defer rows.Close()
	var good PackageItemGood
	seen := make(map[int]struct{})
	for rows.Next() {
		var header PackageItemGood
		var member PackageItemMember
		if err := rows.Scan(&header.ID, &header.CodeName, &header.PaymentDevice, &header.Cost, &member.Index, &member.RefItemID, &member.OptLevel, &member.Variance, &member.Data, &member.MagicParams, &member.MaxStack, &member.Bionic, &member.TypeID1, &member.TypeID2, &member.TypeID3, &member.TypeID4); err != nil {
			return PackageItemGood{}, err
		}
		if len(good.Members) == 0 {
			good.ID, good.CodeName, good.PaymentDevice, good.Cost = header.ID, header.CodeName, header.PaymentDevice, header.Cost
		} else if good.ID != header.ID || good.CodeName != header.CodeName || good.PaymentDevice != header.PaymentDevice || good.Cost != header.Cost {
			return PackageItemGood{}, ErrPackageItemConflict
		}
		if member.Index < 0 || member.RefItemID == 0 || member.MaxStack == 0 || member.MagicParams > 12 {
			return PackageItemGood{}, ErrPackageItemConflict
		}
		if _, duplicate := seen[member.Index]; duplicate {
			return PackageItemGood{}, ErrPackageItemConflict
		}
		seen[member.Index] = struct{}{}
		good.Members = append(good.Members, member)
	}
	if err := rows.Err(); err != nil {
		return PackageItemGood{}, err
	}
	if len(good.Members) == 0 {
		return PackageItemGood{}, ErrPackageItemNotFound
	}
	if good.ID == 0 || good.CodeName == "" || !validPackagePaymentDevice(good.PaymentDevice) || good.Cost == 0 {
		return PackageItemGood{}, fmt.Errorf("%w: package header", ErrPackageItemConflict)
	}
	return good, nil
}

func validPackagePaymentDevice(device uint32) bool {
	return device == 1 || device == 64 || device == 128 || device == 256 || device == 512 || device == 1024
}

func packageTokenCodeName(device uint32) (string, bool) {
	switch device {
	case 64:
		return "ITEM_ETC_SD_TOKEN_01", true
	case 128:
		return "ITEM_ETC_SD_TOKEN_02", true
	case 256:
		return "ITEM_ETC_SD_TOKEN_03", true
	case 512:
		return "ITEM_ETC_SD_TOKEN_04", true
	case 1024:
		return "ITEM_ETC_ARENA_COIN", true
	default:
		return "", false
	}
}

func (r *SQLPackageItemRepository) PurchasePackageItem(ctx context.Context, characterID int64, npcRefObjectID uint32, tab, slot byte, count uint16) (result PackageItemPurchaseResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrPackageItemUnavailable
	}
	if characterID <= 0 || npcRefObjectID == 0 || count == 0 {
		return result, ErrPackageItemConflict
	}
	tx, err := r.shard.BeginTx(normalizeEconomyContext(ctx), nil)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	good, err := resolvePackageItemWithQueryer(ctx, tx, npcRefObjectID, tab, slot)
	if err != nil {
		return result, err
	}
	if good.PaymentDevice != 1 {
		return result, ErrPackageItemConflict
	}
	var gold uint64
	var capacity byte
	if err = tx.QueryRowContext(ctx, "SELECT `GOLD`,`INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&gold, &capacity); err != nil || capacity <= InventoryEquipmentSlots {
		return result, ErrPackageItemConflict
	}
	if uint64(good.Cost) > math.MaxUint64/uint64(count) {
		return result, ErrPackageItemConflict
	}
	total := uint64(good.Cost) * uint64(count)
	if gold < total {
		return result, ErrPackageItemInsufficientPayment
	}
	creator := ""
	for _, member := range good.Members {
		if member.TypeID1 != TypeID1Item || member.TypeID2 != 3 || member.TypeID3 != 8 || member.MagicParams != 0 {
			return result, ErrPackageItemConflict
		}
	}
	var jobType byte
	if err = tx.QueryRowContext(ctx, "SELECT `JOB_TYPE`,COALESCE(`ALIAS`,'') FROM `CHARACTER_JOB` WHERE `FK_CHAR`=? FOR UPDATE", characterID).Scan(&jobType, &creator); err != nil || jobType != JobTrader || creator == "" || len(creator) > 17 {
		return result, ErrSafeTradeAccessDenied
	}
	occupied := make(map[byte]bool)
	rows, err := tx.QueryContext(ctx, "SELECT `SLOT` FROM `INVENTORY` WHERE `FK_CHAR`=? FOR UPDATE", characterID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var occupiedSlot byte
		if err = rows.Scan(&occupiedSlot); err != nil {
			rows.Close()
			return result, err
		}
		occupied[occupiedSlot] = true
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	expanded, err := expandPackageItemMembers(good.Members, count)
	if err != nil {
		return result, err
	}
	free := make([]byte, 0, len(expanded))
	for candidate := InventoryEquipmentSlots; candidate < capacity && len(free) < len(expanded); candidate++ {
		if !occupied[candidate] {
			free = append(free, candidate)
		}
	}
	if len(free) != len(expanded) {
		return result, ErrGroundItemInventoryFull
	}
	for index, expandedMember := range expanded {
		inserted, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`) VALUES (?,?,?,?,?,?)", expandedMember.member.Variance, expandedMember.member.RefItemID, expandedMember.stack, expandedMember.member.OptLevel, expandedMember.stack, creator)
		if insertErr != nil {
			return result, insertErr
		}
		itemID, insertErr := inserted.LastInsertId()
		if insertErr != nil {
			return result, insertErr
		}
		if _, insertErr = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, free[index], itemID); insertErr != nil {
			return result, insertErr
		}
	}
	gold -= total
	if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=? WHERE `id`=?", gold, characterID); err != nil {
		return result, err
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return PackageItemPurchaseResult{}, errors.Join(ErrPackageItemOutcomeUnknown, err)
	}
	committed = true
	result.Gold, result.DestinationSlots, result.Good = gold, free, good
	return result, nil
}

func (r *SQLPackageItemRepository) PurchaseTokenPackageItem(ctx context.Context, characterID int64, npcRefObjectID uint32, tab, slot byte, count uint16) (result PackageItemPurchaseResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrPackageItemUnavailable
	}
	if characterID <= 0 || npcRefObjectID == 0 || count == 0 {
		return result, ErrPackageItemConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	good, err := resolvePackageItemWithQueryer(ctx, tx, npcRefObjectID, tab, slot)
	if err != nil {
		return result, err
	}
	tokenCodeName, tokenPolicy := packageTokenCodeName(good.PaymentDevice)
	if !tokenPolicy || uint64(good.Cost) > math.MaxUint64/uint64(count) {
		return result, ErrPackageItemConflict
	}
	var capacity byte
	if err = tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&capacity); err != nil || capacity <= InventoryEquipmentSlots {
		return result, ErrPackageItemConflict
	}
	occupied := make(map[byte]bool)
	tokens := make([]packageTokenStack, 0)
	rows, err := tx.QueryContext(ctx, "SELECT inv.`SLOT`,inv.`FK_ITEM`,item.`STACK_COUNT`,data.`CodeName` FROM `INVENTORY` inv JOIN `ITEM` item ON item.`id`=inv.`FK_ITEM` JOIN `ITEMDATA` data ON data.`ItemID`=item.`FK_REF_ITEM` WHERE inv.`FK_CHAR`=? ORDER BY inv.`SLOT` FOR UPDATE", characterID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var current packageTokenStack
		var codeName string
		if err = rows.Scan(&current.slot, &current.itemID, &current.quantity, &codeName); err != nil {
			rows.Close()
			return result, err
		}
		occupied[current.slot] = true
		if codeName == tokenCodeName {
			tokens = append(tokens, current)
		}
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	required := uint64(good.Cost) * uint64(count)
	debits, err := planPackageTokenDebit(tokens, required)
	if err != nil {
		return result, err
	}
	expanded, err := expandPackageItemMembers(good.Members, count)
	if err != nil {
		return result, err
	}
	free := make([]byte, 0, len(expanded))
	for candidate := InventoryEquipmentSlots; candidate < capacity && len(free) < len(expanded); candidate++ {
		if !occupied[candidate] {
			free = append(free, candidate)
		}
	}
	if len(free) < len(expanded) {
		return result, ErrGroundItemInventoryFull
	}
	continuations := make([]PackageItemTokenContinuation, 0, len(tokens))
	for _, debit := range debits {
		if debit.remaining == 0 {
			if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, debit.stack.slot, debit.stack.itemID); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", debit.stack.itemID); err != nil {
				return result, err
			}
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=?,`DATA`=? WHERE `id`=?", debit.remaining, debit.remaining, debit.stack.itemID); err != nil {
				return result, err
			}
		}
		continuations = append(continuations, PackageItemTokenContinuation{Slot: debit.stack.slot, Remaining: uint16(debit.remaining)})
	}
	creator := ""
	_ = tx.QueryRowContext(ctx, "SELECT COALESCE(`ALIAS`,'') FROM `CHARACTER_JOB` WHERE `FK_CHAR`=?", characterID).Scan(&creator)
	for index, expandedMember := range expanded {
		data := expandedMember.member.Data
		if expandedMember.member.TypeID2 == 3 && data == 0 {
			data = expandedMember.stack
		}
		inserted, insertErr := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`CREATOR_NAME`) VALUES (?,?,?,?,?,?)", expandedMember.member.Variance, expandedMember.member.RefItemID, expandedMember.stack, expandedMember.member.OptLevel, data, creator)
		if insertErr != nil {
			return result, insertErr
		}
		itemID, insertErr := inserted.LastInsertId()
		if insertErr != nil {
			return result, insertErr
		}
		if _, insertErr = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, free[index], itemID); insertErr != nil {
			return result, insertErr
		}
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return PackageItemPurchaseResult{}, errors.Join(ErrPackageItemOutcomeUnknown, err)
	}
	committed = true
	result.DestinationSlots, result.Good, result.Continuations = free, good, continuations
	return result, nil
}

type expandedPackageItemMember struct {
	member PackageItemMember
	stack  uint32
}

func expandPackageItemMembers(members []PackageItemMember, count uint16) ([]expandedPackageItemMember, error) {
	if len(members) == 0 || count == 0 {
		return nil, ErrPackageItemConflict
	}
	copies := int(count)
	if len(members) == 1 && members[0].Bionic == 0 && members[0].TypeID1 == TypeID1Item && members[0].TypeID2 == 3 && members[0].Data == 0 {
		copies = 1
	} else if copies > 5 {
		copies = 5
	}
	expanded := make([]expandedPackageItemMember, 0, len(members)*copies)
	for _, member := range members {
		stack := member.Data
		if stack == 0 && member.TypeID2 == 3 {
			stack = uint32(count)
		}
		if stack == 0 {
			stack = 1
		}
		if member.MaxStack == 0 || stack > member.MaxStack {
			return nil, ErrPackageItemConflict
		}
		for copyIndex := 0; copyIndex < copies; copyIndex++ {
			expanded = append(expanded, expandedPackageItemMember{member: member, stack: stack})
		}
	}
	return expanded, nil
}
