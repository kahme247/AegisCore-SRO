package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	alchemyTypeMagicStone     byte = 4
	alchemyTypeAttributeStone byte = 5
	alchemyBindingSocket      byte = 1
	alchemyBindingAdvanced    byte = 2
)

func (r *SQLAlchemyRepository) FuseStone(ctx context.Context, characterID int64, userID uint32, targetSlot, stoneSlot byte, alchemyType byte) (result AlchemyResult, err error) {
	if alchemyType != alchemyTypeMagicStone && alchemyType != alchemyTypeAttributeStone {
		return result, ErrAlchemyUnsupported
	}
	tx, target, targetRef, stone, stoneRef, err := r.lockAlchemyPair(ctx, characterID, userID, targetSlot, stoneSlot)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	_, protectionStone := protectionKindForMagicOption(stoneRef.Param1Desc)
	validStoneSubtype := stoneRef.TypeID4 == int(alchemyType-3) ||
		(alchemyType == alchemyTypeMagicStone && stoneRef.TypeID4 == 7 && protectionStone)
	if stoneRef.TypeID1 != 3 || stoneRef.TypeID2 != 3 || stoneRef.TypeID3 != 11 || !validStoneSubtype || targetRef.Degree() != stoneRef.ItemClass {
		return result, ErrAlchemyUnsupported
	}
	if !stoneAppliesToTarget(stoneRef, targetRef) {
		return result, ErrAlchemyUnsupported
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, stoneSlot, stone); err != nil {
		return result, err
	}
	result.Materials = []AlchemyMaterialUpdate{{Slot: stoneSlot, Remaining: remainingAlchemyStack(stone)}}
	chance := stoneRef.Param4
	if chance <= 0 || chance > 100 {
		chance = 50
	}
	result.Success = r.roll()*100 < float64(chance)
	if result.Success {
		switch alchemyType {
		case alchemyTypeMagicStone:
			if protectionKind, isProtectionStone := protectionKindForMagicOption(stoneRef.Param1Desc); isProtectionStone {
				protection := target.Protection
				if !protection.Add(protectionKind) {
					return AlchemyResult{}, ErrAlchemyConflict
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_ALCHEMY_PROTECTION` (`FK_ITEM`,`ASTRAL`,`IMMORTAL`,`STEADY`) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE `ASTRAL`=VALUES(`ASTRAL`),`IMMORTAL`=VALUES(`IMMORTAL`),`STEADY`=VALUES(`STEADY`)", target.ID, protection.Astral, protection.Immortal, protection.Steady)
				break
			}
			var option MagicOption
			var packedRange uint32
			if err = tx.QueryRowContext(ctx, "SELECT `ID`,`PARAM_TYPE`,`PARAM_VALUE` FROM `REF_MAGIC_OPTION` WHERE `CODE_NAME`=? AND `OPTION_LEVEL`=? LIMIT 1", strings.TrimSpace(stoneRef.Param1Desc), stoneRef.ItemClass).Scan(&option.Type, &option.ParamType, &packedRange); err != nil {
				return AlchemyResult{}, fmt.Errorf("resolve magic stone option: %w", err)
			}
			minimum, maximum, valid := decodeMagicOptionRange(packedRange)
			if !valid {
				return AlchemyResult{}, ErrAlchemyConflict
			}
			option.Value = rollMagicOptionValue(minimum, maximum, r.roll())
			existingOption := false
			for _, existing := range target.MagicOptions {
				if existing.Type == option.Type {
					existingOption = true
					break
				}
			}
			if existingOption {
				_, err = tx.ExecContext(ctx, "UPDATE `ITEM_MAGIC_OPTION` SET `VALUE`=? WHERE `FK_ITEM`=? AND `TYPE`=?", option.Value, target.ID, option.Type)
			} else {
				maxOptions := targetRef.MaxMagicOptCount
				if maxOptions <= 0 || maxOptions > 12 {
					maxOptions = 12
				}
				if len(target.MagicOptions) >= maxOptions {
					return AlchemyResult{}, ErrAlchemyConflict
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_MAGIC_OPTION` (`FK_ITEM`,`SLOT`,`TYPE`,`VALUE`) VALUES (?,?,?,?)", target.ID, len(target.MagicOptions), option.Type, option.Value)
			}
		case alchemyTypeAttributeStone:
			variance, ok := applyAttributeStone(target.Variance, targetRef, stoneRef.Param1Desc)
			if !ok {
				return AlchemyResult{}, ErrAlchemyUnsupported
			}
			_, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `VARIANCE`=? WHERE `id`=? AND `VARIANCE`=?", variance, target.ID, target.Variance)
		}
		if err != nil {
			return result, fmt.Errorf("apply alchemy stone: %w", err)
		}
	}
	return r.finishAlchemyTransaction(ctx, tx, characterID, targetSlot, result)
}

func decodeMagicOptionRange(packed uint32) (uint32, uint32, bool) {
	minimum, maximum := packed>>16, packed&0xffff
	return minimum, maximum, minimum <= maximum
}

func rollMagicOptionValue(minimum, maximum uint32, roll float64) uint32 {
	if minimum >= maximum {
		return minimum
	}
	if roll < 0 {
		roll = 0
	} else if roll >= 1 {
		roll = 0.9999999999999999
	}
	return minimum + uint32(roll*float64(maximum-minimum+1))
}

func protectionKindForMagicOption(code string) (ProtectionKind, bool) {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "MATTR_ASTRAL":
		return ProtectionAstral, true
	case "MATTR_ATHANASIA":
		return ProtectionImmortal, true
	case "MATTR_SOLID":
		return ProtectionSteady, true
	default:
		return 0, false
	}
}

func (r *SQLAlchemyRepository) ApplyAdvancedElixir(ctx context.Context, characterID int64, userID uint32, targetSlot, elixirSlot byte) (result AlchemyResult, err error) {
	tx, target, targetRef, elixir, elixirRef, err := r.lockAlchemyPair(ctx, characterID, userID, targetSlot, elixirSlot)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	increase, validElixir := validateAdvancedElixir(targetRef, elixirRef)
	newLevel, validLevel := ApplyAdvancedElixir(target.OptLevel, increase)
	if !validElixir || !validLevel || len(target.AdvancedElixirs) != 0 {
		return result, ErrAlchemyUnsupported
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, elixirSlot, elixir); err != nil {
		return result, err
	}
	result.Materials = []AlchemyMaterialUpdate{{Slot: elixirSlot, Remaining: remainingAlchemyStack(elixir)}}
	if _, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `OPT_LEVEL`=? WHERE `id`=? AND `OPT_LEVEL`=?", newLevel, target.ID, target.OptLevel); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_BINDING_OPTION` (`FK_ITEM`,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE`) VALUES (?,?,?,?,?)", target.ID, alchemyBindingAdvanced, 0, elixirRef.ID, increase); err != nil {
		return result, err
	}
	result.Success = true
	return r.finishAlchemyTransaction(ctx, tx, characterID, targetSlot, result)
}

func (r *SQLAlchemyRepository) InsertSocket(ctx context.Context, characterID int64, userID uint32, targetSlot, stoneSlot byte) (result AlchemyResult, err error) {
	tx, target, targetRef, stone, stoneRef, err := r.lockAlchemyPair(ctx, characterID, userID, targetSlot, stoneSlot)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if stoneRef.TypeID1 != 3 || stoneRef.TypeID2 != 3 || stoneRef.TypeID3 != 11 || stoneRef.TypeID4 != 8 || targetRef.Degree() < stoneRef.ItemClass || !socketStoneApplies(stoneRef, targetRef) || len(target.SocketOptions) >= 3 {
		return result, ErrAlchemyUnsupported
	}
	for _, socket := range target.SocketOptions {
		if socket.ID == uint32(stoneRef.Param1) {
			return result, ErrAlchemyConflict
		}
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, stoneSlot, stone); err != nil {
		return result, err
	}
	result.Materials = []AlchemyMaterialUpdate{{Slot: stoneSlot, Remaining: remainingAlchemyStack(stone)}}
	_, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_BINDING_OPTION` (`FK_ITEM`,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE`) VALUES (?,?,?,?,0)", target.ID, alchemyBindingSocket, len(target.SocketOptions), stoneRef.Param1)
	if err != nil {
		return result, err
	}
	result.Success = true
	return r.finishAlchemyTransaction(ctx, tx, characterID, targetSlot, result)
}

func (r *SQLAlchemyRepository) lockAlchemyPair(ctx context.Context, characterID int64, userID uint32, targetSlot, materialSlot byte) (*sql.Tx, Item, RefItem, Item, RefItem, error) {
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || targetSlot < 13 || materialSlot < 13 || targetSlot >= BaseInventorySize || materialSlot >= BaseInventorySize || targetSlot == materialSlot {
		return nil, Item{}, RefItem{}, Item{}, RefItem{}, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return nil, Item{}, RefItem{}, Item{}, RefItem{}, err
	}
	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil || owner != userID {
		_ = tx.Rollback()
		return nil, Item{}, RefItem{}, Item{}, RefItem{}, ErrAlchemyConflict
	}
	inv, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		_ = tx.Rollback()
		return nil, Item{}, RefItem{}, Item{}, RefItem{}, err
	}
	target, targetOK := inv.Items[targetSlot]
	material, materialOK := inv.Items[materialSlot]
	targetRef, targetRefOK := RefItems[target.RefObjectID]
	materialRef, materialRefOK := RefItems[material.RefObjectID]
	if !targetOK || !materialOK || !targetRefOK || !materialRefOK || material.StackSize < 1 || !targetRef.IsEquipment() {
		_ = tx.Rollback()
		return nil, Item{}, RefItem{}, Item{}, RefItem{}, ErrAlchemyConflict
	}
	return tx, target, targetRef, material, materialRef, nil
}

func (r *SQLAlchemyRepository) finishAlchemyTransaction(ctx context.Context, tx *sql.Tx, characterID int64, targetSlot byte, result AlchemyResult) (AlchemyResult, error) {
	var err error
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyResult{}, err
	}
	result.Target = result.Inventory.Items[targetSlot]
	result.TargetSlot = targetSlot
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

func stoneAppliesToTarget(stone, target RefItem) bool {
	family := strings.ToLower(strings.TrimSpace(stone.Param4Desc))
	if family == "" || family == "xxx" {
		family = strings.ToLower(strings.TrimSpace(stone.Param2Desc))
	}
	switch family {
	case "weapon":
		return target.TypeID3 == 6
	case "shield":
		return target.TypeID3 == 4
	case "armor":
		_, ok := alchemyTargetClass(target)
		return ok && target.TypeID3 != 4 && target.TypeID3 != 5 && target.TypeID3 != 6 && target.TypeID3 != 12
	case "accessory":
		return target.TypeID3 == 5 || target.TypeID3 == 12
	default:
		return true
	}
}

func socketStoneApplies(stone, target RefItem) bool {
	switch strings.ToLower(strings.TrimSpace(stone.Param2Desc)) {
	case "weapon":
		return target.TypeID3 == 6
	case "armor":
		return target.IsEquipment() && target.TypeID3 != 6 && target.TypeID3 != 5 && target.TypeID3 != 12
	default:
		return false
	}
}

func applyAttributeStone(variance uint64, target RefItem, code string) (uint64, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	shift := -1
	switch code {
	case "NATTR_CRITICAL":
		shift = 0
	case "NATTR_HR":
		shift = 15
	case "NATTR_DUR":
		shift = 0
	case "NATTR_ER":
		shift = 25
	case "NATTR_PA", "NATTR_PARRY":
		shift = 20
	case "NATTR_MA":
		shift = 25
	case "NATTR_PR":
		shift = 5
	case "NATTR_MR":
		shift = 10
	case "NATTR_BR":
		shift = 15
	}
	if shift < 0 {
		return variance, false
	}
	mask := uint64(VarianceStatMask) << shift
	return (variance &^ mask) | (uint64(VarianceStatMask) << shift), true
}
