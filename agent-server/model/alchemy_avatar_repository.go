package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// BlessAvatarItem implements CBless_AvatarItemWithNPC. The operation is
// immediate and guaranteed to reach its blessing branch; Success reports the
// binary's mutation flag rather than a probability roll.
func (r *SQLAlchemyRepository) BlessAvatarItem(ctx context.Context, characterID int64, userID uint32, targetSlot byte, optionCode string) (result AlchemyResult, err error) {
	optionCode = strings.TrimSpace(optionCode)
	if r == nil || r.shard == nil {
		return result, ErrAlchemyRepositoryUnavailable
	}
	if characterID <= 0 || userID == 0 || targetSlot < InventoryEquipmentSlots || targetSlot >= BaseInventorySize || optionCode == "" {
		return result, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin avatar blessing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil {
		return result, fmt.Errorf("lock avatar blessing character: %w", err)
	}
	if owner != userID {
		return result, ErrAlchemyConflict
	}
	locked, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	target, exists := locked.Items[targetSlot]
	if !exists {
		return result, ErrAlchemyConflict
	}
	targetRef, exists := RefItems[target.RefObjectID]
	if !exists {
		return result, ErrInventoryReferenceItemNotFound
	}
	if !targetRef.IsAvatar() || targetRef.MaxMagicOptCount <= 0 || targetRef.MaxMagicOptCount > 12 {
		return result, ErrAlchemyUnsupported
	}

	if err = applyAvatarBlessing(ctx, tx, target, targetRef, optionCode); err != nil {
		return result, err
	}

	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyResult{}, err
	}
	result.Target, exists = result.Inventory.Items[targetSlot]
	if !exists {
		return AlchemyResult{}, ErrAlchemyConflict
	}
	result.TargetSlot = targetSlot
	result.Success = true
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

func applyAvatarBlessing(ctx context.Context, tx *sql.Tx, target Item, targetRef RefItem, optionCode string) error {
	upperCode := strings.ToUpper(strings.TrimSpace(optionCode))
	if !avatarOptionApplies(targetRef, upperCode) {
		return ErrAlchemyUnsupported
	}
	var option MagicOption
	var packedRange uint32
	if err := tx.QueryRowContext(ctx, "SELECT `ID`,`PARAM_TYPE`,`PARAM_VALUE` FROM `REF_MAGIC_OPTION` WHERE `CODE_NAME`=? AND `OPTION_LEVEL`=1 LIMIT 1", optionCode).Scan(&option.Type, &option.ParamType, &packedRange); err != nil {
		return fmt.Errorf("resolve avatar blessing option: %w", err)
	}
	maximum := avatarBlessingMaximum(option.ParamType, packedRange)
	existingSlot := byte(0)
	exists := false
	for _, existing := range target.MagicOptions {
		if existing.Type == option.Type {
			existingSlot, option.Value, exists = existing.Slot, existing.Value, true
			break
		}
	}
	if !exists {
		slot, available := nextMagicOptionSlot(target.MagicOptions, targetRef.MaxMagicOptCount)
		if !available {
			return ErrAlchemyConflict
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO `ITEM_MAGIC_OPTION` (`FK_ITEM`,`SLOT`,`TYPE`,`VALUE`) VALUES (?,?,?,1)", target.ID, slot, option.Type)
		return err
	}
	if option.Value >= maximum {
		return ErrAlchemyConflict
	}
	_, err := tx.ExecContext(ctx, "UPDATE `ITEM_MAGIC_OPTION` SET `VALUE`=? WHERE `FK_ITEM`=? AND `SLOT`=? AND `TYPE`=?", option.Value+1, target.ID, existingSlot, option.Type)
	return err
}

var avatarMagicOptionAssignments = map[int][]string{
	1: {"MATTR_AVATAR_STR", "MATTR_AVATAR_INT", "MATTR_AVATAR_ER", "MATTR_AVATAR_MP", "MATTR_AVATAR_DARA", "MATTR_AVATAR_MPRG"},
	2: {"MATTR_AVATAR_STR", "MATTR_AVATAR_STR_2", "MATTR_AVATAR_STR_3", "MATTR_AVATAR_STR_4", "MATTR_AVATAR_INT", "MATTR_AVATAR_INT_2", "MATTR_AVATAR_INT_3", "MATTR_AVATAR_INT_4", "MATTR_AVATAR_LUCK", "MATTR_AVATAR_LUCK_2", "MATTR_AVATAR_LUCK_3", "MATTR_AVATAR_LUCK_4", "MATTR_AVATAR_MDIA", "MATTR_AVATAR_MDIA_2", "MATTR_AVATAR_MDIA_3", "MATTR_AVATAR_MDIA_4", "MATTR_AVATAR_HR", "MATTR_AVATAR_HP", "MATTR_AVATAR_DRUA", "MATTR_AVATAR_HPRG"},
	3: {"MATTR_AVATAR_HR", "MATTR_AVATAR_HPRG"},
	4: {"MATTR_STR_AVATAR", "MATTR_INT_AVATAR"},
}

func avatarOptionApplies(ref RefItem, code string) bool {
	if !ref.IsAvatar() {
		return false
	}
	for _, candidate := range avatarMagicOptionAssignments[ref.TypeID4] {
		if candidate == code {
			return true
		}
	}
	return false
}

func randomAvatarOption(ref RefItem, roll func() float64) (string, bool) {
	candidates := avatarMagicOptionAssignments[ref.TypeID4]
	if !ref.IsAvatar() || len(candidates) == 0 {
		return "", false
	}
	return candidates[alchemyResidue(roll, len(candidates))], true
}

func avatarBlessingMaximum(paramType, packedRange uint32) uint32 {
	_, maximum, validRange := decodeMagicOptionRange(packedRange)
	switch paramType {
	case 0x61746861, 0x6c75636b, 0x736f6c69, 0x61737472: // atha, luck, soli, astr
		return 5
	case 0x00726570: // rep
		return 6
	default:
		if !validRange || maximum == 0 {
			return 1
		} else if maximum > 0x6a4 {
			return 0x6a4
		}
		return maximum
	}
}

// EnchantAvatarItem implements the delayed CEnchant_EnchantAvatarItem path.
// The binary accepts a target-only request and selects from the target's
// assigned avatar option catalog; when present, the second component must be
// a 3/3/11/1 or 3/3/11/7 stone.
func (r *SQLAlchemyRepository) EnchantAvatarItem(ctx context.Context, characterID int64, userID uint32, targetSlot byte, stoneSlot *byte) (result AlchemyResult, err error) {
	if r == nil || r.shard == nil {
		return result, ErrAlchemyRepositoryUnavailable
	}
	if characterID <= 0 || userID == 0 || targetSlot < InventoryEquipmentSlots || targetSlot >= BaseInventorySize ||
		(stoneSlot != nil && (*stoneSlot < InventoryEquipmentSlots || *stoneSlot >= BaseInventorySize || *stoneSlot == targetSlot)) {
		return result, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin avatar enchant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil {
		return result, fmt.Errorf("lock avatar enchant character: %w", err)
	}
	if owner != userID {
		return result, ErrAlchemyConflict
	}
	locked, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	target, exists := locked.Items[targetSlot]
	if !exists {
		return result, ErrAlchemyConflict
	}
	targetRef, exists := RefItems[target.RefObjectID]
	if !exists {
		return result, ErrInventoryReferenceItemNotFound
	}
	if !targetRef.IsAvatar() || targetRef.MaxMagicOptCount <= 0 || targetRef.MaxMagicOptCount > 12 {
		return result, ErrAlchemyUnsupported
	}
	optionCode, available := randomAvatarOption(targetRef, r.roll)
	if !available {
		return AlchemyResult{}, ErrAlchemyUnsupported
	}
	if err = applyAvatarBlessing(ctx, tx, target, targetRef, optionCode); err != nil {
		return AlchemyResult{}, err
	}
	if stoneSlot != nil {
		stone, stoneExists := locked.Items[*stoneSlot]
		if !stoneExists || stone.StackSize < 1 {
			return AlchemyResult{}, ErrAlchemyConflict
		}
		stoneRef, refExists := RefItems[stone.RefObjectID]
		if !refExists {
			return AlchemyResult{}, ErrInventoryReferenceItemNotFound
		}
		if stoneRef.TypeID1 != 3 || stoneRef.TypeID2 != 3 || stoneRef.TypeID3 != 11 ||
			(stoneRef.TypeID4 != 1 && stoneRef.TypeID4 != 7) {
			return AlchemyResult{}, ErrAlchemyUnsupported
		}
		if err = consumeAlchemyMaterial(ctx, tx, characterID, *stoneSlot, stone); err != nil {
			return AlchemyResult{}, err
		}
		result.Materials = []AlchemyMaterialUpdate{{Slot: *stoneSlot, Remaining: remainingAlchemyStack(stone)}}
	}
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyResult{}, err
	}
	result.Target, exists = result.Inventory.Items[targetSlot]
	if !exists {
		return AlchemyResult{}, ErrAlchemyConflict
	}
	result.Success = true
	result.TargetSlot = targetSlot
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}
