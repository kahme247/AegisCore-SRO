package model

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

var (
	ErrAlchemyRepositoryUnavailable = errors.New("alchemy repository is unavailable")
	ErrAlchemyInvalidRequest        = errors.New("invalid alchemy request")
	ErrAlchemyConflict              = errors.New("alchemy state conflicts with the request")
	ErrAlchemyUnsupported           = errors.New("alchemy material or target is unsupported")
	ErrAlchemyOutcomeUnknown        = errors.New("alchemy transaction outcome is unknown")
)

const (
	alchemyElixirClassProtector      = 16909056
	alchemyElixirClassShield         = 67108864
	alchemyElixirClassAccessory      = 83886080
	alchemyElixirClassWeapon         = 100663296
	MaxBasicAlchemyOptLevel     byte = MaxEquipmentPlus
)

type AlchemyMaterialUpdate struct {
	Slot      byte
	Remaining uint16
}

type AlchemyResult struct {
	Inventory      Inventory
	TargetSlot     byte
	Target         Item
	Success        bool
	Destroyed      bool
	DurabilityLost bool
	Materials      []AlchemyMaterialUpdate
}

type AlchemyMakeResult struct {
	Inventory     Inventory
	ResponseByte  byte
	ProducedValue uint32
	Materials     []AlchemyMaterialUpdate
}

type AlchemyRepository interface {
	FuseElixir(context.Context, int64, uint32, byte, byte, *byte) (AlchemyResult, error)
	FuseStone(context.Context, int64, uint32, byte, byte, byte) (AlchemyResult, error)
	ApplyAdvancedElixir(context.Context, int64, uint32, byte, byte) (AlchemyResult, error)
	InsertSocket(context.Context, int64, uint32, byte, byte, byte) (AlchemyResult, error)
	CreateSocket(context.Context, int64, uint32, byte, byte) (AlchemyResult, error)
	EraseSocket(context.Context, int64, uint32, byte, byte) (AlchemyResult, error)
	ResolveMaterial(context.Context, int64, uint32, byte) (AlchemyMakeResult, error)
	CompoundMaterial(context.Context, int64, uint32, []byte) (AlchemyMakeResult, error)
	DismantleEquipment(context.Context, int64, uint32, []byte) (AlchemyResult, error)
	BlessAvatarItem(context.Context, int64, uint32, byte, string) (AlchemyResult, error)
	EnchantAvatarItem(context.Context, int64, uint32, byte, *byte) (AlchemyResult, error)
}

type SQLAlchemyRepository struct {
	shard *sql.DB
	roll  func() float64
}

func NewAlchemyRepository(shard *sql.DB) (*SQLAlchemyRepository, error) {
	if shard == nil {
		return nil, ErrAlchemyRepositoryUnavailable
	}
	return &SQLAlchemyRepository{shard: shard, roll: secureAlchemyRoll}, nil
}

func secureAlchemyRoll() float64 {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return 1
	}
	return float64(value.Int64()) / 1_000_000
}

func enhancementSucceeds(chance int, roll float64) bool {
	if roll < 0 || roll >= 1 {
		return false
	}
	if chance < 10 {
		chance = 10
	} else if chance > 100 {
		chance = 100
	}
	return int(roll*100) < chance
}

// reinforcementChance decodes the three packed four-byte matrices used by
// the vSRO reinforcement reference object. Param2, Param7 and Param12 are the
// tier rows; the selected byte is 3-min(plus,3).
func reinforcementChance(ref RefItem, plus byte) int {
	tier := int(plus)/4 + 1
	if tier > 3 {
		tier = 3
	}
	packed := [3]uint32{uint32(ref.Param2), uint32(ref.Param7), uint32(ref.Param12)}[tier-1]
	column := 3 - min(int(plus), 3)
	return int(byte(packed >> (8 * column)))
}

func alchemyTargetClass(ref RefItem) (int, bool) {
	if !ref.IsEquipment() {
		return 0, false
	}
	switch ref.TypeID3 {
	case 6:
		return alchemyElixirClassWeapon, true
	case 4:
		return alchemyElixirClassShield, true
	case 5, 12:
		return alchemyElixirClassAccessory, true
	case 1, 2, 3, 9, 10, 11:
		return alchemyElixirClassProtector, true
	default:
		return 0, false
	}
}

func alchemyDegree(ref RefItem) int {
	if ref.ItemClass < 1 {
		return 0
	}
	return (ref.ItemClass-1)/3 + 1
}

func validateElixirMaterials(target, elixir RefItem, powder *RefItem) error {
	targetClass, ok := alchemyTargetClass(target)
	if !ok || alchemyDegree(target) == 0 {
		return ErrAlchemyUnsupported
	}
	if elixir.TypeID1 != 3 || elixir.TypeID2 != 3 || elixir.TypeID3 != 10 || elixir.TypeID4 != 1 || elixir.Param1 != targetClass {
		return ErrAlchemyUnsupported
	}
	if powder != nil && (powder.TypeID1 != 3 || powder.TypeID2 != 3 || powder.TypeID3 != 10 || powder.TypeID4 != 2 || powder.ItemClass != alchemyDegree(target)) {
		return ErrAlchemyUnsupported
	}
	return nil
}

func validateAdvancedElixir(target, elixir RefItem) (byte, bool) {
	if elixir.TypeID1 != 3 || elixir.TypeID2 != 3 || elixir.TypeID3 != 10 || elixir.TypeID4 != 4 ||
		alchemyDegree(target) != elixir.ItemClass {
		return 0, false
	}
	targetClass, ok := alchemyTargetClass(target)
	if !ok || elixir.Param1 != targetClass || (elixir.Rarity != 0 && elixir.Rarity != target.Rarity) {
		return 0, false
	}
	code := strings.ToUpper(elixir.CodeName)
	switch {
	case strings.Contains(code, "_B_") || strings.HasSuffix(code, "_B"):
		return 2, true
	case strings.Contains(code, "_A_") || strings.HasSuffix(code, "_A"):
		return 1, true
	default:
		return 0, false
	}
}

func (r *SQLAlchemyRepository) FuseElixir(ctx context.Context, characterID int64, userID uint32, targetSlot, elixirSlot byte, powderSlot *byte) (result AlchemyResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrAlchemyRepositoryUnavailable
	}
	if characterID <= 0 || userID == 0 || targetSlot < 13 || elixirSlot < 13 || targetSlot >= BaseInventorySize || elixirSlot >= BaseInventorySize || targetSlot == elixirSlot {
		return result, ErrAlchemyInvalidRequest
	}
	if powderSlot != nil && (*powderSlot < 13 || *powderSlot >= BaseInventorySize || *powderSlot == targetSlot || *powderSlot == elixirSlot) {
		return result, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin alchemy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil {
		return result, fmt.Errorf("lock alchemy character: %w", err)
	}
	if owner != userID {
		return result, ErrAlchemyConflict
	}

	locked, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	target, targetOK := locked.Items[targetSlot]
	elixir, elixirOK := locked.Items[elixirSlot]
	if !targetOK || !elixirOK || target.OptLevel >= MaxBasicAlchemyOptLevel || elixir.StackSize < 1 {
		return result, ErrAlchemyConflict
	}
	targetRef, targetRefOK := RefItems[target.RefObjectID]
	elixirRef, elixirRefOK := RefItems[elixir.RefObjectID]
	if !targetRefOK || !elixirRefOK {
		return result, ErrInventoryReferenceItemNotFound
	}
	var powder *Item
	var powderRef *RefItem
	if powderSlot != nil {
		item, ok := locked.Items[*powderSlot]
		if !ok || item.StackSize < 1 {
			return result, ErrAlchemyConflict
		}
		ref, ok := RefItems[item.RefObjectID]
		if !ok {
			return result, ErrInventoryReferenceItemNotFound
		}
		powder, powderRef = &item, &ref
	}
	if increase, advanced := validateAdvancedElixir(targetRef, elixirRef); advanced {
		if powder != nil {
			return result, ErrAlchemyUnsupported
		}
		newLevel, ok := ApplyAdvancedElixir(target.OptLevel, increase)
		if !ok {
			return result, ErrAlchemyConflict
		}
		if err = consumeAlchemyMaterial(ctx, tx, characterID, elixirSlot, elixir); err != nil {
			return result, err
		}
		result.Materials = append(result.Materials, AlchemyMaterialUpdate{Slot: elixirSlot, Remaining: remainingAlchemyStack(elixir)})
		update, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=? AND `OPT_LEVEL`=?", newLevel, target.ID, target.OptLevel)
		if updateErr != nil {
			return result, updateErr
		}
		if affected, affectedErr := update.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrAlchemyConflict
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_BINDING_OPTION` (`FK_ITEM`,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE`) VALUES (?,2,0,?,?) ON DUPLICATE KEY UPDATE `OPTION_ID`=VALUES(`OPTION_ID`),`OPTION_VALUE`=VALUES(`OPTION_VALUE`)", target.ID, elixirRef.ID, increase); err != nil {
			return result, err
		}
		result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
		if err != nil {
			return AlchemyResult{}, err
		}
		result.Target, ok = result.Inventory.Items[targetSlot]
		if !ok {
			return AlchemyResult{}, ErrAlchemyConflict
		}
		result.TargetSlot, result.Success = targetSlot, true
		if err = tx.Commit(); err != nil {
			return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
		}
		return result, nil
	}
	if err = validateElixirMaterials(targetRef, elixirRef, powderRef); err != nil {
		return result, err
	}

	if err = consumeAlchemyMaterial(ctx, tx, characterID, elixirSlot, elixir); err != nil {
		return result, err
	}
	result.Materials = append(result.Materials, AlchemyMaterialUpdate{Slot: elixirSlot, Remaining: remainingAlchemyStack(elixir)})
	if powder != nil {
		if err = consumeAlchemyMaterial(ctx, tx, characterID, *powderSlot, *powder); err != nil {
			return result, err
		}
		result.Materials = append(result.Materials, AlchemyMaterialUpdate{Slot: *powderSlot, Remaining: remainingAlchemyStack(*powder)})
	}
	chance := reinforcementChance(elixirRef, target.OptLevel)
	if powderRef != nil && chance < 100 {
		chance += reinforcementChance(*powderRef, target.OptLevel)
	}
	luck, luckErr := consumeAlchemyMagicCharge(ctx, tx, target, "MATTR_LUCK")
	if luckErr != nil {
		return result, luckErr
	}
	if luck {
		chance += 5
	}
	result.Success = enhancementSucceeds(chance, r.roll())
	if result.Success {
		update, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=? AND `OPT_LEVEL`=?", target.OptLevel+1, target.ID, target.OptLevel)
		if updateErr != nil {
			return result, fmt.Errorf("update alchemy target: %w", updateErr)
		}
		if affected, affectedErr := update.RowsAffected(); affectedErr != nil || affected != 1 {
			return result, ErrAlchemyConflict
		}
	} else {
		newLevel, protection, destroyed, durabilityLost := applyEnhancementFailure(target.OptLevel, target.Protection, r.roll())
		result.Destroyed, result.DurabilityLost = destroyed, durabilityLost
		if destroyed {
			if _, err = tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, targetSlot, target.ID); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", target.ID); err != nil {
				return result, err
			}
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=? AND `OPT_LEVEL`=?", newLevel, target.ID, target.OptLevel); err != nil {
				return result, err
			}
			if durabilityLost {
				if err = incrementAlchemyMagicProperty(ctx, tx, target, "MATTR_DEC_MAXDUR"); err != nil {
					return result, err
				}
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_ALCHEMY_PROTECTION` (`FK_ITEM`,`ASTRAL`,`IMMORTAL`,`STEADY`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `ASTRAL`=VALUES(`ASTRAL`),`IMMORTAL`=VALUES(`IMMORTAL`),`STEADY`=VALUES(`STEADY`)", target.ID, protection.Astral, protection.Immortal, protection.Steady); err != nil {
				return result, err
			}
		}
	}

	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyResult{}, err
	}
	result.Target, targetOK = result.Inventory.Items[targetSlot]
	if !targetOK && !result.Destroyed {
		return AlchemyResult{}, ErrAlchemyConflict
	}
	result.TargetSlot = targetSlot
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

func remainingAlchemyStack(item Item) uint16 {
	if item.StackSize <= 1 {
		return 0
	}
	remaining := item.StackSize - 1
	if remaining > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(remaining)
}

func loadLockedAlchemyInventory(ctx context.Context, tx *sql.Tx, characterID int64) (Inventory, error) {
	rows, err := tx.QueryContext(ctx, SelectCharacterInventory+" ORDER BY inv.SLOT FOR UPDATE", characterID)
	if err != nil {
		return emptyInventory(), fmt.Errorf("lock alchemy inventory: %w", err)
	}
	defer rows.Close()
	inv := emptyInventory()
	for rows.Next() {
		slot, item, scanErr := scanInventoryItem(rows)
		if scanErr != nil {
			return emptyInventory(), fmt.Errorf("scan alchemy inventory: %w", scanErr)
		}
		inv.Items[slot] = item
	}
	if err = rows.Err(); err != nil {
		return emptyInventory(), fmt.Errorf("iterate alchemy inventory: %w", err)
	}
	return inv, nil
}

func consumeAlchemyMaterial(ctx context.Context, tx *sql.Tx, characterID int64, slot byte, item Item) error {
	if item.StackSize > 1 {
		result, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`-1 WHERE `id`=? AND `STACK_COUNT`=?", item.ID, item.StackSize)
		if err != nil {
			return fmt.Errorf("decrement alchemy material: %w", err)
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ErrAlchemyConflict
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, item.ID)
	if err != nil {
		return fmt.Errorf("unlink alchemy material: %w", err)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrAlchemyConflict
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.ID); err != nil {
		return fmt.Errorf("delete alchemy material: %w", err)
	}
	return nil
}

func consumeAlchemyMagicCharge(ctx context.Context, tx *sql.Tx, target Item, code string) (bool, error) {
	for _, option := range target.MagicOptions {
		var optionCode string
		if err := tx.QueryRowContext(ctx, "SELECT `CODE_NAME` FROM `REF_MAGIC_OPTION` WHERE `ID`=?", option.Type).Scan(&optionCode); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return false, err
		}
		if !strings.EqualFold(strings.TrimSpace(optionCode), code) {
			continue
		}
		if option.Value > 1 {
			_, err := tx.ExecContext(ctx, "UPDATE `ITEM_MAGIC_OPTION` SET `VALUE`=`VALUE`-1 WHERE `FK_ITEM`=? AND `TYPE`=? AND `VALUE`=?", target.ID, option.Type, option.Value)
			return true, err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM `ITEM_MAGIC_OPTION` WHERE `FK_ITEM`=? AND `TYPE`=?", target.ID, option.Type)
		return true, err
	}
	return false, nil
}

func incrementAlchemyMagicProperty(ctx context.Context, tx *sql.Tx, target Item, code string) error {
	for _, option := range target.MagicOptions {
		var optionCode string
		if err := tx.QueryRowContext(ctx, "SELECT `CODE_NAME` FROM `REF_MAGIC_OPTION` WHERE `ID`=?", option.Type).Scan(&optionCode); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(optionCode), code) {
			_, err := tx.ExecContext(ctx, "UPDATE `ITEM_MAGIC_OPTION` SET `VALUE`=`VALUE`+1 WHERE `FK_ITEM`=? AND `TYPE`=?", target.ID, option.Type)
			return err
		}
	}
	ref, ok := RefItems[target.RefObjectID]
	if !ok {
		return ErrInventoryReferenceItemNotFound
	}
	var optionID uint32
	if err := tx.QueryRowContext(ctx, "SELECT `ID` FROM `REF_MAGIC_OPTION` WHERE `CODE_NAME`=? AND `OPTION_LEVEL`=?", code, ref.Degree()).Scan(&optionID); err != nil {
		return err
	}
	if len(target.MagicOptions) >= 12 {
		return ErrAlchemyConflict
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO `ITEM_MAGIC_OPTION` (`FK_ITEM`,`SLOT`,`TYPE`,`VALUE`) VALUES (?,?,?,1)", target.ID, len(target.MagicOptions), optionID)
	return err
}
