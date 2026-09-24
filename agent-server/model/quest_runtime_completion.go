package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

func (r *SQLQuestRuntimeRepository) Complete(ctx context.Context, characterID, questID int) (QuestCompletionResult, error) {
	if r == nil || r.shard == nil || r.levels == nil || characterID <= 0 || questID <= 0 {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	state, err := lockQuestCharacter(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf("lock quest character: %w", err)
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf("lock character quest: %w", err)
	}
	closedStatus, err := TransitionQuest(quest.Status, QuestTransitionAchieve)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	nextAchievementCount, err := lockNextQuestAchievementCount(ctx, tx, quest)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf("lock quest achievement policy: %w", err)
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf("lock quest inventory: %w", err)
	}
	reward, err := lockQuestRewardReference(ctx, tx, questID)
	if err != nil {
		return QuestCompletionResult{}, fmt.Errorf("lock quest reward reference: %w", err)
	}
	if err = validateQuestRewardReference(questID, reward); err != nil {
		return QuestCompletionResult{}, err
	}
	if err = applyQuestNamedPoint(ctx, tx, characterID, reward); err != nil {
		return QuestCompletionResult{}, err
	}
	result := QuestCompletionResult{Quest: quest, Gold: state.gold}
	collectionRequirements, collectionExists, collectionErr := lockCollectionItemRequirements(ctx, tx, questID)
	if collectionErr != nil {
		return QuestCompletionResult{}, collectionErr
	}
	if collectionExists {
		if !questInventoryContains(items, collectionRequirements) {
			return QuestCompletionResult{}, ErrQuestRequiredItem
		}
		changed := make(map[byte]struct{})
		referenceIDs := make([]uint32, 0, len(collectionRequirements))
		for refID := range collectionRequirements {
			referenceIDs = append(referenceIDs, refID)
		}
		sort.Slice(referenceIDs, func(left, right int) bool { return referenceIDs[left] < referenceIDs[right] })
		for _, refID := range referenceIDs {
			if err = consumeQuestInventoryReference(ctx, tx, characterID, items, refID, collectionRequirements[refID], changed); err != nil {
				return QuestCompletionResult{}, err
			}
		}
		for slot := byte(13); slot < math.MaxUint8; slot++ {
			if _, exists := changed[slot]; exists {
				result.ChangedSlots = append(result.ChangedSlots, slot)
			}
		}
	}
	if questID == QuestWeaponListID {
		missionItemRefID, referenceErr := lockDeliverItemReference(ctx, tx, questID)
		if referenceErr != nil {
			return QuestCompletionResult{}, referenceErr
		}
		slot, item, exists := findQuestItem(items, missionItemRefID)
		if !exists {
			return QuestCompletionResult{}, ErrQuestRequiredItem
		}
		removed, removeErr := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, item.id)
		if removeErr != nil {
			return QuestCompletionResult{}, removeErr
		}
		if err = requireQuestRow(removed, "remove mission item slot"); err != nil {
			return QuestCompletionResult{}, err
		}
		removed, removeErr = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.id)
		if removeErr != nil {
			return QuestCompletionResult{}, removeErr
		}
		if err = requireQuestRow(removed, "remove mission item"); err != nil {
			return QuestCompletionResult{}, err
		}
		delete(items, slot)
		result.RemovedSlot = &slot
	}
	if reward.IsItemReward {
		for _, rewardItem := range ordinaryQuestRewardItems(reward) {
			ref, exists := RefItems[rewardItem.RefID]
			if !exists || rewardItem.Quantity <= 0 {
				return QuestCompletionResult{}, fmt.Errorf("%w: quest %d item %d", ErrQuestReferenceUnsupported, questID, rewardItem.RefID)
			}
			maxStack := ref.StackSize
			if maxStack <= 0 {
				maxStack = 1
			}
			for remaining := rewardItem.Quantity; remaining > 0; {
				stack := min(remaining, maxStack)
				slot, ok := freeQuestInventorySlot(items, state.inventorySize)
				if !ok {
					return QuestCompletionResult{}, ErrQuestInventoryFull
				}
				item, insertErr := insertQuestItemStack(ctx, tx, characterID, slot, rewardItem.RefID, stack)
				if insertErr != nil {
					return QuestCompletionResult{}, insertErr
				}
				items[slot] = questInventoryItem{id: int64(item.ID), refID: rewardItem.RefID, stack: stack}
				result.AddedItems = append(result.AddedItems, QuestItemMutation{Slot: slot, Item: item})
				remaining -= stack
			}
		}
	}
	if state.gold > math.MaxUint64-reward.Gold {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	state.gold += reward.Gold

	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, reward.Experience, reward.SkillExperience, state.skillPoints, state.statPoints)
	if err = applyFixedQuestSkillPoints(&progress, reward.SkillPoints); err != nil {
		return QuestCompletionResult{}, err
	}
	inventorySize, expanded, sizeErr := applyQuestInventoryExpansion(state.inventorySize, reward.InventorySlots)
	if sizeErr != nil {
		return QuestCompletionResult{}, sizeErr
	}
	updatedCharacter, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`INVENTORY_SIZE`=?,`HWAN_LEVEL`=IF(?=0,`HWAN_LEVEL`,?),`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", state.gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, inventorySize, reward.Hwan, reward.Hwan, progress.Level, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(updatedCharacter, "apply quest rewards"); err != nil {
		return QuestCompletionResult{}, err
	}
	now := time.Now().UTC()
	completed, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`ACHIEVEMENT_COUNT`=?,`COMPLETED_AT`=?,`TIMER_REMAINING`=0 WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", closedStatus, nextAchievementCount, now, characterID, questID, quest.Status)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(completed, "complete quest"); err != nil {
		return QuestCompletionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Quest.Status = closedStatus
	result.Quest.AchievementCount = nextAchievementCount
	result.Quest.CompletedAt = &now
	result.Gold = state.gold
	result.Progress = progress
	result.Experience = reward.Experience
	result.SkillExp = reward.SkillExperience
	result.FixedSkillPoints = reward.SkillPoints
	result.InventorySize = inventorySize
	result.InventoryExpanded = expanded
	result.HwanLevel = reward.Hwan
	result.HwanChanged = reward.Hwan != 0
	result.NamedPointKey = reward.NamedPointKey
	result.NamedPointAmount = reward.NamedPointAmount
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	for index := range result.AddedItems {
		if persisted, exists := result.Inventory.Items[result.AddedItems[index].Slot]; exists {
			result.AddedItems[index].Item = persisted
		}
	}
	return result, nil
}

// SelectQuestReward commits the 0x7515 quest-reward path. The stock client
// sends resolved reference item IDs, so every requested ID is resolved again
// under the transaction against this quest's authored reward rows.
func (r *SQLQuestRuntimeRepository) SelectQuestReward(ctx context.Context, characterID int, rewardID uint32, entryIDs []uint32) (QuestCompletionResult, error) {
	// The binary uses rewardID >= 10000 for the separate RefEventReward
	// catalog. That catalog carries server-event context and is intentionally
	// not coerced into a quest ID here; accepting it would make a valid-looking
	// but semantically wrong CHAR_QUEST mutation.
	if r == nil || r.shard == nil || r.levels == nil || characterID <= 0 || rewardID == 0 || rewardID >= 10000 {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	questID := int(rewardID)
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	state, err := lockQuestCharacter(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	closedStatus, err := TransitionQuest(quest.Status, QuestTransitionAchieve)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	nextAchievementCount, err := lockNextQuestAchievementCount(ctx, tx, quest)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	reward, err := lockQuestRewardReference(ctx, tx, questID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	selected, err := selectQuestRewardItems(questID, reward, entryIDs, state.country)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = applyQuestNamedPoint(ctx, tx, characterID, reward); err != nil {
		return QuestCompletionResult{}, err
	}

	result := QuestCompletionResult{Quest: quest, Gold: state.gold}
	for _, selectedItem := range selected {
		ref, exists := RefItems[selectedItem.RefID]
		if !exists || selectedItem.Quantity <= 0 {
			return QuestCompletionResult{}, fmt.Errorf("%w: quest %d item %d", ErrQuestReferenceUnsupported, questID, selectedItem.RefID)
		}
		maxStack := ref.StackSize
		if maxStack <= 0 {
			maxStack = 1
		}
		remaining := selectedItem.Quantity
		for remaining > 0 {
			stack := min(remaining, maxStack)
			slot, ok := freeQuestInventorySlot(items, state.inventorySize)
			if !ok {
				return QuestCompletionResult{}, ErrQuestInventoryFull
			}
			item, insertErr := insertQuestItemStack(ctx, tx, characterID, slot, selectedItem.RefID, stack)
			if insertErr != nil {
				return QuestCompletionResult{}, insertErr
			}
			items[slot] = questInventoryItem{id: int64(item.ID), refID: selectedItem.RefID, stack: stack}
			result.AddedItems = append(result.AddedItems, QuestItemMutation{Slot: slot, Item: item})
			remaining -= stack
		}
	}
	if state.gold > math.MaxUint64-reward.Gold {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	state.gold += reward.Gold
	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, reward.Experience, reward.SkillExperience, state.skillPoints, state.statPoints)
	if err = applyFixedQuestSkillPoints(&progress, reward.SkillPoints); err != nil {
		return QuestCompletionResult{}, err
	}
	inventorySize, expanded, sizeErr := applyQuestInventoryExpansion(state.inventorySize, reward.InventorySlots)
	if sizeErr != nil {
		return QuestCompletionResult{}, sizeErr
	}
	updatedCharacter, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`INVENTORY_SIZE`=?,`HWAN_LEVEL`=IF(?=0,`HWAN_LEVEL`,?),`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", state.gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, inventorySize, reward.Hwan, reward.Hwan, progress.Level, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(updatedCharacter, "apply selected quest rewards"); err != nil {
		return QuestCompletionResult{}, err
	}
	now := time.Now().UTC()
	completed, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`ACHIEVEMENT_COUNT`=?,`COMPLETED_AT`=?,`TIMER_REMAINING`=0 WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", closedStatus, nextAchievementCount, now, characterID, questID, quest.Status)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(completed, "complete selected-reward quest"); err != nil {
		return QuestCompletionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Quest.Status = closedStatus
	result.Quest.AchievementCount = nextAchievementCount
	result.Quest.CompletedAt = &now
	result.Gold = state.gold
	result.Progress = progress
	result.Experience = reward.Experience
	result.SkillExp = reward.SkillExperience
	result.FixedSkillPoints = reward.SkillPoints
	result.InventorySize = inventorySize
	result.InventoryExpanded = expanded
	result.HwanLevel = reward.Hwan
	result.HwanChanged = reward.Hwan != 0
	result.NamedPointKey = reward.NamedPointKey
	result.NamedPointAmount = reward.NamedPointAmount
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	for index := range result.AddedItems {
		if persisted, exists := result.Inventory.Items[result.AddedItems[index].Slot]; exists {
			result.AddedItems[index].Item = persisted
		}
	}
	return result, nil
}

func selectQuestRewardItems(questID int, reward questRewardReference, entryIDs []uint32, country int) ([]questRewardItemReference, error) {
	selectionCount := questRewardSelectionCount(reward)
	conditionalTypeOne := reward.IsCheckCondition && reward.ItemRewardType == 1 && !reward.IsCheckCountry && !reward.IsCheckClass && !reward.IsCheckGender && len(reward.Items) > 1
	unsupported := !reward.IsBasicReward || !reward.IsItemReward || selectionCount == 0 ||
		(!reward.IsCheckCountry && reward.IsCheckCondition && !conditionalTypeOne) || reward.IsCheckClass || reward.IsCheckGender ||
		reward.AcademyPoints != 0 ||
		reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0
	if unsupported || len(reward.Items) == 0 {
		return nil, fmt.Errorf("%w: quest %d", ErrQuestReferenceUnsupported, questID)
	}
	if len(entryIDs) != int(selectionCount) {
		return nil, fmt.Errorf("%w: quest %d selected %d entries, want %d", ErrQuestStateConflict, questID, len(entryIDs), selectionCount)
	}
	options := make(map[uint32]questRewardItemReference, len(reward.Items))
	for _, item := range reward.Items {
		if item.RefID == 0 || item.Quantity <= 0 {
			return nil, fmt.Errorf("%w: quest %d has an invalid reward item", ErrQuestReferenceUnsupported, questID)
		}
		if _, duplicate := options[item.RefID]; duplicate {
			return nil, fmt.Errorf("%w: quest %d has ambiguous item %d", ErrQuestReferenceUnsupported, questID, item.RefID)
		}
		if reward.IsCheckCountry {
			ref, exists := RefItems[item.RefID]
			if !exists || (ref.Country != country && ref.Country != 3) {
				continue
			}
		}
		options[item.RefID] = item
	}
	selected := make([]questRewardItemReference, 0, len(entryIDs))
	seen := make(map[uint32]struct{}, len(entryIDs))
	for _, entryID := range entryIDs {
		if _, duplicate := seen[entryID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate reward item %d", ErrQuestStateConflict, entryID)
		}
		item, exists := options[entryID]
		if !exists {
			return nil, fmt.Errorf("%w: quest %d does not offer item %d", ErrQuestStateConflict, questID, entryID)
		}
		seen[entryID] = struct{}{}
		selected = append(selected, item)
	}
	return selected, nil
}

func questRewardSelectionCount(reward questRewardReference) byte {
	if reward.SelectionCount != 0 {
		return reward.SelectionCount
	}
	// The pinned catalog's conditional type-1 reward (QNO_EU_EASTEU_5)
	// exposes multiple authored items as one client choice despite a raw zero.
	if reward.IsCheckCondition && reward.ItemRewardType == 1 && !reward.IsCheckCountry && !reward.IsCheckClass && !reward.IsCheckGender && len(reward.Items) > 1 {
		return 1
	}
	return 0
}

const maxCollectionPaymentObjectives = 4

func lockCollectionItemRequirements(ctx context.Context, tx *sql.Tx, questID int) (map[uint32]uint64, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ITEM_REF_OBJECT_ID,REQUIRED_COUNT FROM REF_QUEST_COLLECTION_ITEM_OBJECTIVE
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND RUNTIME_ENABLED=1 ORDER BY MISSION_INDEX FOR SHARE`, questID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	required := make(map[uint32]uint64)
	rowCount := 0
	for rows.Next() {
		var refID uint32
		var quantity uint64
		if err = rows.Scan(&refID, &quantity); err != nil {
			return nil, false, err
		}
		rowCount++
		if refID == 0 || quantity == 0 || required[refID] > math.MaxUint64-quantity {
			return nil, false, ErrQuestReferenceUnsupported
		}
		required[refID] += quantity
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(required) == 0 {
		return nil, false, nil
	}
	if rowCount > maxCollectionPaymentObjectives {
		return nil, false, ErrQuestReferenceUnsupported
	}
	return required, true, nil
}

func lockQuestRewardReference(ctx context.Context, tx *sql.Tx, questID int) (questRewardReference, error) {
	var reward questRewardReference
	var basic, item, checkCondition, checkCountry, checkClass, checkGender uint8
	var scriptExperience, scriptSkillExperience, scriptGold, scriptSkillPoints uint64
	var scriptItemCount uint16
	err := tx.QueryRowContext(ctx, `SELECT reward.GOLD,reward.EXPERIENCE,reward.SKILL_EXPERIENCE,reward.SKILL_POINTS,
		reward.ACADEMY_POINTS,reward.HWAN,reward.INVENTORY_SLOTS,reward.IS_BASIC_REWARD,reward.IS_ITEM_REWARD,
		reward.IS_CHECK_CONDITION,reward.IS_CHECK_COUNTRY,reward.IS_CHECK_CLASS,reward.IS_CHECK_GENDER,
		reward.ITEM_REWARD_TYPE,reward.SELECTION_COUNT,reward.PARAM_1,reward.PARAM_2,reward.PARAM_3,
		script.EXPERIENCE,script.SKILL_EXPERIENCE,script.GOLD,script.ITEM_COUNT,
		COALESCE((SELECT SUM(point_reward.AMOUNT) FROM REF_QUEST_SCRIPT_POINT_REWARD point_reward
		          WHERE point_reward.QUEST_ID=reward.QUEST_ID AND point_reward.VARIANT_INDEX=0
		            AND point_reward.REWARD_KIND='skill_point' AND point_reward.RUNTIME_ENABLED=1),0)
		FROM REF_QUEST_REWARD reward
		JOIN REF_QUEST_SCRIPT_REWARD script ON script.QUEST_ID=reward.QUEST_ID AND script.RUNTIME_ENABLED=1
		WHERE reward.QUEST_ID=? FOR SHARE`, questID).Scan(
		&reward.Gold, &reward.Experience, &reward.SkillExperience, &reward.SkillPoints, &reward.AcademyPoints,
		&reward.Hwan, &reward.InventorySlots, &basic, &item, &checkCondition, &checkCountry, &checkClass,
		&checkGender, &reward.ItemRewardType, &reward.SelectionCount, &reward.Param1, &reward.Param2, &reward.Param3,
		&scriptExperience, &scriptSkillExperience, &scriptGold, &scriptItemCount, &scriptSkillPoints)
	if errors.Is(err, sql.ErrNoRows) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d", ErrQuestReferenceMissing, questID)
	}
	if err != nil {
		return questRewardReference{}, err
	}
	reward.IsBasicReward = basic != 0
	reward.IsItemReward = item != 0
	reward.IsCheckCondition = checkCondition != 0
	reward.IsCheckCountry = checkCountry != 0
	reward.IsCheckClass = checkClass != 0
	reward.IsCheckGender = checkGender != 0
	if reward.Experience != scriptExperience || reward.SkillExperience != scriptSkillExperience || reward.Gold != scriptGold ||
		reward.SkillPoints != scriptSkillPoints || reward.IsItemReward != (scriptItemCount > 0) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d SQL and script rewards disagree", ErrQuestReferenceUnsupported, questID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT POINT_KEY,AMOUNT FROM REF_QUEST_SCRIPT_POINT_REWARD
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND REWARD_KIND='named_point' AND RUNTIME_ENABLED=1
		ORDER BY REWARD_INDEX FOR SHARE`, questID)
	if err != nil {
		return questRewardReference{}, err
	}
	for rows.Next() {
		if reward.NamedPointKey != "" {
			_ = rows.Close()
			return questRewardReference{}, fmt.Errorf("%w: quest %d has multiple named-point rewards", ErrQuestReferenceUnsupported, questID)
		}
		if err = rows.Scan(&reward.NamedPointKey, &reward.NamedPointAmount); err != nil {
			_ = rows.Close()
			return questRewardReference{}, err
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return questRewardReference{}, err
	}
	if err = rows.Close(); err != nil {
		return questRewardReference{}, err
	}
	if reward.NamedPointKey != "" && (reward.NamedPointAmount == 0 ||
		(reward.NamedPointKey != "TEMPLE_ANUBIS" && reward.NamedPointKey != "TEMPLE_HAROERIS")) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d has invalid named-point reward", ErrQuestReferenceUnsupported, questID)
	}
	if !reward.IsItemReward {
		return reward, nil
	}
	rows, err = tx.QueryContext(ctx, `SELECT item.ItemID,rewards.ACHIEVE_QUANTITY,rewards.REWARD_TYPE,
		scriptItem.ITEM_REF_OBJECT_ID,scriptItem.QUANTITY
		FROM REF_QUEST_REWARD_ITEM rewards
		JOIN ITEMDATA item ON item.CodeName=rewards.ITEM_CODE_NAME
		JOIN REF_QUEST_SCRIPT_REWARD script ON script.QUEST_ID=rewards.QUEST_ID AND script.RUNTIME_ENABLED=1
		JOIN REF_QUEST_SCRIPT_REWARD_ITEM scriptItem ON scriptItem.QUEST_ID=script.QUEST_ID
			AND scriptItem.VARIANT_INDEX=script.VARIANT_INDEX AND scriptItem.ITEM_CODE_NAME=rewards.ITEM_CODE_NAME
		WHERE rewards.QUEST_ID=? ORDER BY rewards.ID FOR SHARE`, questID)
	if err != nil {
		return questRewardReference{}, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var itemReference questRewardItemReference
		var scriptItemRefID uint32
		var scriptQuantity int
		if err = rows.Scan(&itemReference.RefID, &itemReference.Quantity, &itemReference.RewardType, &scriptItemRefID, &scriptQuantity); err != nil {
			return questRewardReference{}, err
		}
		if itemReference.RefID != scriptItemRefID || itemReference.Quantity != scriptQuantity {
			return questRewardReference{}, fmt.Errorf("%w: quest %d SQL and script reward items disagree", ErrQuestReferenceUnsupported, questID)
		}
		reward.Items = append(reward.Items, itemReference)
	}
	if err = rows.Err(); err != nil {
		return questRewardReference{}, err
	}
	if count == 0 {
		return questRewardReference{}, fmt.Errorf("%w: quest %d resolved no reward items", ErrQuestReferenceUnsupported, questID)
	}
	if count != 1 {
		return reward, nil
	}
	reward.ItemRefID = reward.Items[0].RefID
	reward.ItemQuantity = reward.Items[0].Quantity
	return reward, nil
}

func applyQuestNamedPoint(ctx context.Context, tx *sql.Tx, characterID int, reward questRewardReference) error {
	if reward.NamedPointKey == "" && reward.NamedPointAmount == 0 {
		return nil
	}
	if tx == nil || characterID <= 0 || reward.NamedPointAmount == 0 ||
		(reward.NamedPointKey != "TEMPLE_ANUBIS" && reward.NamedPointKey != "TEMPLE_HAROERIS") {
		return ErrQuestReferenceUnsupported
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO CHAR_QUEST_NAMED_POINT (CHAR_ID,POINT_KEY,BALANCE)
		VALUES (?,?,?) ON DUPLICATE KEY UPDATE BALANCE=BALANCE+VALUES(BALANCE)`,
		characterID, reward.NamedPointKey, reward.NamedPointAmount)
	if err != nil {
		return err
	}
	if err = requireQuestRow(result, "apply named quest points"); err != nil {
		return err
	}
	return nil
}

func validateQuestRewardReference(questID int, reward questRewardReference) error {
	academyDuplicate := reward.AcademyPoints == 0 ||
		(reward.NamedPointAmount == reward.AcademyPoints &&
			(reward.NamedPointKey == "TEMPLE_ANUBIS" || reward.NamedPointKey == "TEMPLE_HAROERIS"))
	unsupported := !reward.IsBasicReward || reward.IsCheckCondition || reward.IsCheckCountry || reward.IsCheckClass || reward.IsCheckGender ||
		!academyDuplicate ||
		reward.ItemRewardType != 0 || reward.SelectionCount != 0 || reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0
	if unsupported {
		return fmt.Errorf("%w: quest %d", ErrQuestReferenceUnsupported, questID)
	}
	if reward.IsItemReward {
		items := ordinaryQuestRewardItems(reward)
		if len(items) == 0 {
			return fmt.Errorf("%w: quest %d has no item rewards", ErrQuestReferenceUnsupported, questID)
		}
		for _, item := range items {
			if item.RefID == 0 || item.Quantity <= 0 {
				return fmt.Errorf("%w: quest %d has an invalid item reward", ErrQuestReferenceUnsupported, questID)
			}
		}
	} else if reward.ItemRefID != 0 || reward.ItemQuantity != 0 {
		return fmt.Errorf("%w: quest %d has inconsistent item reward", ErrQuestReferenceUnsupported, questID)
	}
	return nil
}

func ordinaryQuestRewardItems(reward questRewardReference) []questRewardItemReference {
	if len(reward.Items) != 0 {
		return reward.Items
	}
	if reward.ItemRefID == 0 || reward.ItemQuantity <= 0 {
		return nil
	}
	return []questRewardItemReference{{RefID: reward.ItemRefID, Quantity: reward.ItemQuantity}}
}

func applyFixedQuestSkillPoints(progress *ProgressionResult, reward uint64) error {
	if progress == nil || reward == 0 {
		return nil
	}
	// The stock balance packet is u32 even though imported reference data is
	// held in a wider type. Reject impossible state instead of truncating it.
	if reward > math.MaxUint32 || progress.SkillPoints < 0 || uint64(progress.SkillPoints) > math.MaxUint32-reward {
		return ErrQuestStateConflict
	}
	progress.SkillPoints += int(reward)
	return nil
}

func applyQuestInventoryExpansion(current, reward byte) (byte, bool, error) {
	if current == 0 {
		current = BaseInventorySize
	}
	if reward == 0 {
		return current, false, nil
	}
	if current > questMaxInventorySize || int(current)+int(reward) > int(questMaxInventorySize) {
		return 0, false, ErrQuestStateConflict
	}
	return current + reward, true, nil
}
