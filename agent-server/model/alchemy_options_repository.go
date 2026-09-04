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
	result.Success = stoneSucceeds(chance, r.roll())
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
			variance, ok := applyAttributeStoneValue(target.Variance, targetRef, stoneRef.Param1Desc, attributeStoneValue(r.roll()))
			if !ok {
				return AlchemyResult{}, ErrAlchemyUnsupported
			}
			assimilation := int(byte(uint32(stoneRef.Param3) >> 24))
			if assimilation > 0 && int(r.roll()*101) < assimilation {
				protected, protectErr := consumeAlchemyMagicCharge(ctx, tx, target, "MATTR_APE")
				if protectErr != nil {
					return AlchemyResult{}, protectErr
				}
				if !protected {
					candidates := attributeStoneCandidates(targetRef)
					candidateCount := len(candidates) + len(target.MagicOptions)
					if candidateCount > 0 {
						selectedIndex := min(int(r.roll()*float64(candidateCount)), candidateCount-1)
						if selectedIndex >= len(candidates) {
							magic := target.MagicOptions[selectedIndex-len(candidates)]
							var packed uint32
							if queryErr := tx.QueryRowContext(ctx, "SELECT `PARAM_VALUE` FROM `REF_MAGIC_OPTION` WHERE `ID`=? AND `OPTION_LEVEL`=? LIMIT 1", magic.Type, targetRef.Degree()).Scan(&packed); queryErr != nil {
								return AlchemyResult{}, fmt.Errorf("resolve assimilation magic option: %w", queryErr)
							}
							minimum, maximum, valid := decodeMagicOptionRange(packed)
							if !valid {
								return AlchemyResult{}, ErrAlchemyConflict
							}
							value := rollMagicOptionValue(minimum, maximum, r.roll())
							if _, updateErr := tx.ExecContext(ctx, "UPDATE `ITEM_MAGIC_OPTION` SET `VALUE`=? WHERE `FK_ITEM`=? AND `SLOT`=? AND `TYPE`=?", value, target.ID, selectedIndex-len(candidates), magic.Type); updateErr != nil {
								return AlchemyResult{}, updateErr
							}
							break
						}
						selected := candidates[selectedIndex]
						if !strings.EqualFold(selected, stoneRef.Param1Desc) {
							variance, _ = applyAttributeStoneValue(variance, targetRef, selected, attributeStoneValue(r.roll()))
						}
					}
				}
			}
			_, err = tx.ExecContext(ctx, "UPDATE `ITEM` SET `VARIANCE`=? WHERE `id`=? AND `VARIANCE`=?", variance, target.ID, target.Variance)
		}
		if err != nil {
			return result, fmt.Errorf("apply alchemy stone: %w", err)
		}
	}
	return r.finishAlchemyTransaction(ctx, tx, characterID, targetSlot, result)
}

func stoneSucceeds(chance int, roll float64) bool {
	if chance < 5 {
		chance = 5
	} else if chance > 100 {
		chance = 100
	}
	if roll < 0 || roll >= 1 {
		return false
	}
	return int(roll*100) <= chance
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

func (r *SQLAlchemyRepository) InsertSocket(ctx context.Context, characterID int64, userID uint32, targetSlot, stoneSlot, socketIndex byte) (result AlchemyResult, err error) {
	tx, target, targetRef, stone, stoneRef, err := r.lockAlchemyPair(ctx, characterID, userID, targetSlot, stoneSlot)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if stoneRef.TypeID1 != 3 || stoneRef.TypeID2 != 3 || stoneRef.TypeID3 != 11 || stoneRef.TypeID4 != 8 || targetRef.Degree() < stoneRef.ItemClass || !socketStoneApplies(stoneRef, targetRef) || socketIndex >= 3 {
		return result, ErrAlchemyUnsupported
	}
	for _, socket := range target.SocketOptions {
		if socket.Slot == socketIndex || socket.ID == uint32(stoneRef.Param1) {
			return result, ErrAlchemyConflict
		}
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, stoneSlot, stone); err != nil {
		return result, err
	}
	result.Materials = []AlchemyMaterialUpdate{{Slot: stoneSlot, Remaining: remainingAlchemyStack(stone)}}
	_, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_BINDING_OPTION` (`FK_ITEM`,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE`) VALUES (?,?,?,?,0)", target.ID, alchemyBindingSocket, socketIndex, stoneRef.Param1)
	if err != nil {
		return result, err
	}
	result.Success = true
	return r.finishAlchemyTransaction(ctx, tx, characterID, targetSlot, result)
}

func (r *SQLAlchemyRepository) CreateSocket(ctx context.Context, characterID int64, userID uint32, targetSlot, socketIndex byte) (AlchemyResult, error) {
	return r.mutateSocketSlot(ctx, characterID, userID, targetSlot, socketIndex, 9, true)
}

func (r *SQLAlchemyRepository) EraseSocket(ctx context.Context, characterID int64, userID uint32, targetSlot, socketIndex byte) (AlchemyResult, error) {
	return r.mutateSocketSlot(ctx, characterID, userID, targetSlot, socketIndex, 10, false)
}

func (r *SQLAlchemyRepository) mutateSocketSlot(ctx context.Context, characterID int64, userID uint32, targetSlot, socketIndex byte, materialSubtype int, create bool) (result AlchemyResult, err error) {
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 || targetSlot < 13 || targetSlot >= BaseInventorySize || socketIndex >= 3 {
		return result, ErrAlchemyInvalidRequest
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil || owner != userID {
		return result, ErrAlchemyConflict
	}
	inv, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	target, ok := inv.Items[targetSlot]
	targetRef, refOK := RefItems[target.RefObjectID]
	if !ok || !refOK || !targetRef.IsEquipment() {
		return result, ErrAlchemyConflict
	}
	exists := false
	for _, option := range target.SocketOptions {
		if option.Slot == socketIndex {
			exists = true
			break
		}
	}
	if create == exists {
		return result, ErrAlchemyConflict
	}
	var materialSlot byte
	var material Item
	found := false
	for slot, candidate := range inv.Items {
		ref, refOK := RefItems[candidate.RefObjectID]
		if slot >= 13 && refOK && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 11 && ref.TypeID4 == materialSubtype {
			materialSlot, material, found = slot, candidate, true
			break
		}
	}
	if !found {
		return result, ErrAlchemyUnsupported
	}
	if err = consumeAlchemyMaterial(ctx, tx, characterID, materialSlot, material); err != nil {
		return result, err
	}
	result.Materials = []AlchemyMaterialUpdate{{Slot: materialSlot, Remaining: remainingAlchemyStack(material)}}
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO `ITEM_BINDING_OPTION` (`FK_ITEM`,`OPTION_TYPE`,`SLOT`,`OPTION_ID`,`OPTION_VALUE`) VALUES (?,1,?,0,0)", target.ID, socketIndex)
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM `ITEM_BINDING_OPTION` WHERE `FK_ITEM`=? AND `OPTION_TYPE`=1 AND `SLOT`=?", target.ID, socketIndex)
	}
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
	return applyAttributeStoneValue(variance, target, code, int(VarianceStatMask))
}

func applyAttributeStoneValue(variance uint64, target RefItem, code string, value int) (uint64, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	shift := attributeVarianceShift(target, code)
	if shift < 0 {
		return variance, false
	}
	if value < 0 {
		value = 0
	} else if value > int(VarianceStatMask) {
		value = int(VarianceStatMask)
	}
	mask := uint64(VarianceStatMask) << shift
	return (variance &^ mask) | (uint64(value) << shift), true
}

func attributeStoneValue(roll float64) int {
	residue := int(roll * 100)
	bucket := 100
	for _, threshold := range []int{0, 23, 43, 63, 83, 100} {
		if residue <= threshold {
			bucket = threshold
			break
		}
	}
	return bucket * int(VarianceStatMask) / 100
}

func attributeStoneCandidates(target RefItem) []string {
	switch target.TypeID3 {
	case 4:
		return []string{"NATTR_DUR", "NATTR_PDSTR", "NATTR_MDINT", "NATTR_PD", "NATTR_MD", "NATTR_ER"}
	case 6:
		return []string{"NATTR_DUR", "NATTR_PASTR", "NATTR_MAINT", "NATTR_HR", "NATTR_PA", "NATTR_MA", "NATTR_CRITICAL"}
	case 5, 12:
		return []string{"NATTR_PAR", "NATTR_MAR"}
	default:
		return []string{"NATTR_DUR", "NATTR_PDSTR", "NATTR_MDINT", "NATTR_BR", "NATTR_PD", "NATTR_MD"}
	}
}

func attributeVarianceShift(target RefItem, code string) int {
	var shifts map[string]int
	switch target.TypeID3 {
	case 6: // weapon
		shifts = map[string]int{"NATTR_CRITICAL": 0, "NATTR_MA": 5, "NATTR_PA": 10, "NATTR_HR": 15, "NATTR_MAINT": 20, "NATTR_PASTR": 25, "NATTR_DUR": 30}
	case 4: // shield
		shifts = map[string]int{"NATTR_MD": 0, "NATTR_PD": 5, "NATTR_ER": 10, "NATTR_MDINT": 15, "NATTR_PDSTR": 20, "NATTR_DUR": 25}
	case 5, 12: // accessory
		shifts = map[string]int{"NATTR_MAR": 0, "NATTR_PAR": 5}
	default: // armor/protector
		shifts = map[string]int{"NATTR_BR": 0, "NATTR_MD": 5, "NATTR_PD": 10, "NATTR_MDINT": 15, "NATTR_PDSTR": 20, "NATTR_DUR": 25}
	}
	shift, ok := shifts[code]
	if !ok {
		return -1
	}
	return shift
}
