package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

// EventRewardReference mirrors the binary's separate _RefEventReward catalog.
// It is deliberately not a questRewardReference: event rewards are applied to
// the current quest object by the 0x7515 continuation and do not imply a
// CHAR_QUEST close.
type EventRewardReference struct {
	EventID          uint32
	Gold             int64
	Experience       int64
	SkillExperience  int64
	SkillPoints      int64
	Hwan             uint8
	InventorySlots   uint8
	ItemRewardType   uint8
	SelectionCount   uint8
	IsBasicReward    bool
	IsItemReward     bool
	IsCheckCondition bool
	IsCheckCountry   bool
	IsCheckClass     bool
	IsCheckGender    bool
	Param1           int64
	Param2           int64
	Param3           int64
	Items            []EventRewardItemReference
}

// EventRewardItemReference is resolved to ITEMDATA.ItemID before it reaches
// the wire. PayCount is retained separately from AchieveRatio because the
// legacy catalog exposes both fields and the latter is not a stack quantity.
type EventRewardItemReference struct {
	RefID        uint32
	PayCount     int
	AchieveRatio float32
}

type EventRewardResult struct {
	EventID    uint32
	Inventory  Inventory
	Gold       uint64
	Progress   ProgressionResult
	Experience uint64
	SkillExp   uint64
	AddedItems []QuestItemMutation
}

type EventRewardSelectionExecutor interface {
	SelectEventReward(context.Context, int, uint32, []uint32) (EventRewardResult, error)
}

// SelectEventReward executes the event half of the binary 0x7515 path. It
// intentionally does not update or remove CHAR_QUEST: the legacy continuation
// applies the selected entry to its in-memory current quest object, while the
// durable economic effects are the event reward's own transaction.
func (r *SQLQuestRuntimeRepository) SelectEventReward(ctx context.Context, characterID int, eventID uint32, entryIDs []uint32) (EventRewardResult, error) {
	if r == nil || r.shard == nil || r.levels == nil || characterID <= 0 || eventID < 10000 {
		return EventRewardResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return EventRewardResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	state, err := lockQuestCharacter(ctx, tx, characterID)
	if err != nil {
		return EventRewardResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return EventRewardResult{}, err
	}
	reward, err := lockEventRewardReference(ctx, tx, eventID)
	if err != nil {
		return EventRewardResult{}, err
	}
	selected, err := ValidateEventRewardSelection(reward, entryIDs)
	if err != nil {
		return EventRewardResult{}, err
	}

	result := EventRewardResult{EventID: eventID, Gold: state.gold}
	for _, selectedItem := range selected {
		ref, exists := RefItems[selectedItem.RefID]
		if !exists {
			return EventRewardResult{}, fmt.Errorf("%w: event %d item %d", ErrQuestReferenceUnsupported, eventID, selectedItem.RefID)
		}
		remaining := selectedItem.PayCount
		maxStack := ref.StackSize
		if maxStack <= 0 {
			maxStack = 1
		}
		for remaining > 0 {
			stack := min(remaining, maxStack)
			slot, ok := freeQuestInventorySlot(items, state.inventorySize)
			if !ok {
				return EventRewardResult{}, ErrQuestInventoryFull
			}
			item, insertErr := insertQuestItemStack(ctx, tx, characterID, slot, selectedItem.RefID, stack)
			if insertErr != nil {
				return EventRewardResult{}, insertErr
			}
			items[slot] = questInventoryItem{id: int64(item.ID), refID: selectedItem.RefID, stack: stack}
			result.AddedItems = append(result.AddedItems, QuestItemMutation{Slot: slot, Item: item})
			remaining -= stack
		}
	}
	if reward.Gold > math.MaxInt64 || state.gold > math.MaxUint64-uint64(reward.Gold) {
		return EventRewardResult{}, ErrQuestStateConflict
	}
	result.Gold += uint64(reward.Gold)
	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, uint64(reward.Experience), uint64(reward.SkillExperience), state.skillPoints, state.statPoints)
	updated, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", result.Gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, progress.Level, characterID)
	if err != nil {
		return EventRewardResult{}, err
	}
	if err = requireQuestRow(updated, "apply event rewards"); err != nil {
		return EventRewardResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return EventRewardResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Progress = progress
	result.Experience = uint64(reward.Experience)
	result.SkillExp = uint64(reward.SkillExperience)
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return EventRewardResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	for index := range result.AddedItems {
		if persisted, exists := result.Inventory.Items[result.AddedItems[index].Slot]; exists {
			result.AddedItems[index].Item = persisted
		}
	}
	return result, nil
}

func lockEventRewardReference(ctx context.Context, tx *sql.Tx, eventID uint32) (EventRewardReference, error) {
	var reward EventRewardReference
	var view, basic, item, checkCondition, checkCountry, checkClass, checkGender uint8
	err := tx.QueryRowContext(ctx, `SELECT EVENT_ID,GOLD,EXPERIENCE,SKILL_EXPERIENCE,SKILL_POINTS,HWAN,INVENTORY_SLOTS,ITEM_REWARD_TYPE,SELECTION_COUNT,
		IS_VIEW,IS_BASIC_REWARD,IS_ITEM_REWARD,IS_CHECK_CONDITION,IS_CHECK_COUNTRY,IS_CHECK_CLASS,IS_CHECK_GENDER,PARAM_1,PARAM_2,PARAM_3
		FROM REF_EVENT_REWARD WHERE EVENT_ID=? AND SERVICE=1 FOR SHARE`, eventID).Scan(
		&reward.EventID, &reward.Gold, &reward.Experience, &reward.SkillExperience, &reward.SkillPoints,
		&reward.Hwan, &reward.InventorySlots, &reward.ItemRewardType, &reward.SelectionCount, &view, &basic,
		&item, &checkCondition, &checkCountry, &checkClass, &checkGender, &reward.Param1, &reward.Param2, &reward.Param3)
	if errors.Is(err, sql.ErrNoRows) {
		return EventRewardReference{}, ErrQuestEventNotFound
	}
	if err != nil {
		return EventRewardReference{}, err
	}
	reward.IsBasicReward, reward.IsItemReward = basic != 0, item != 0
	reward.IsCheckCondition, reward.IsCheckCountry = checkCondition != 0, checkCountry != 0
	reward.IsCheckClass, reward.IsCheckGender = checkClass != 0, checkGender != 0
	rows, err := tx.QueryContext(ctx, `SELECT item.ItemID,reward.PAY_COUNT,reward.ACHIEVE_RATIO
		FROM REF_EVENT_REWARD_ITEM reward JOIN ITEMDATA item ON item.CodeName=reward.ITEM_CODE_NAME
		WHERE reward.EVENT_ID=? AND reward.SERVICE=1 ORDER BY reward.ITEM_CODE_NAME FOR SHARE`, eventID)
	if err != nil {
		return EventRewardReference{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item EventRewardItemReference
		if err = rows.Scan(&item.RefID, &item.PayCount, &item.AchieveRatio); err != nil {
			return EventRewardReference{}, err
		}
		reward.Items = append(reward.Items, item)
	}
	if err = rows.Err(); err != nil {
		return EventRewardReference{}, err
	}
	return reward, nil
}

// ValidateEventRewardSelection applies the binary-confirmed count and
// identity checks without deciding how the transaction persists its result.
// The caller must perform this validation against a row locked in its own
// transaction.
func ValidateEventRewardSelection(reward EventRewardReference, entryIDs []uint32) ([]EventRewardItemReference, error) {
	if reward.EventID < 10000 || reward.SelectionCount == 0 || len(entryIDs) != int(reward.SelectionCount) {
		return nil, ErrQuestStateConflict
	}
	if !reward.IsBasicReward || !reward.IsItemReward || reward.IsCheckCondition || reward.IsCheckCountry || reward.IsCheckClass || reward.IsCheckGender {
		return nil, fmt.Errorf("%w: event %d reward flags are unsupported", ErrQuestReferenceUnsupported, reward.EventID)
	}
	if reward.Gold < 0 || reward.Experience < 0 || reward.SkillExperience < 0 || reward.SkillPoints != 0 || reward.Hwan != 0 || reward.InventorySlots != 0 || reward.ItemRewardType != 0 || reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0 {
		return nil, fmt.Errorf("%w: event %d reward mechanism is unsupported", ErrQuestReferenceUnsupported, reward.EventID)
	}
	byID := make(map[uint32]EventRewardItemReference, len(reward.Items))
	for _, item := range reward.Items {
		if item.RefID == 0 || item.PayCount <= 0 {
			return nil, fmt.Errorf("%w: event %d item reference is invalid", ErrQuestReferenceUnsupported, reward.EventID)
		}
		if _, exists := byID[item.RefID]; exists {
			return nil, fmt.Errorf("%w: event %d item %d is ambiguous", ErrQuestReferenceUnsupported, reward.EventID, item.RefID)
		}
		byID[item.RefID] = item
	}
	selected := make([]EventRewardItemReference, 0, len(entryIDs))
	seen := make(map[uint32]struct{}, len(entryIDs))
	for _, entryID := range entryIDs {
		if _, exists := seen[entryID]; exists {
			return nil, ErrQuestStateConflict
		}
		seen[entryID] = struct{}{}
		item, exists := byID[entryID]
		if !exists {
			return nil, ErrQuestStateConflict
		}
		selected = append(selected, item)
	}
	return selected, nil
}
