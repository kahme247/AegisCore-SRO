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

const (
	QuestWeaponListID   = 3
	QuestChulsanAngerID = 58
)

var (
	ErrQuestRuntimeUnavailable   = errors.New("quest runtime repository is unavailable")
	ErrQuestStateConflict        = errors.New("quest state conflicts with the request")
	ErrQuestInventoryFull        = errors.New("quest inventory is full")
	ErrQuestRequiredItem         = errors.New("required quest item is missing")
	ErrQuestOutcomeUnknown       = errors.New("quest transaction outcome is unknown")
	ErrQuestReferenceMissing     = errors.New("quest reward reference is missing")
	ErrQuestReferenceUnsupported = errors.New("quest reward reference uses an unsupported mechanism")
)

type questRewardReference struct {
	Gold             uint64
	Experience       uint64
	SkillExperience  uint64
	SkillPoints      uint64
	AcademyPoints    uint64
	Hwan             uint8
	InventorySlots   uint8
	IsBasicReward    bool
	IsItemReward     bool
	IsCheckCondition bool
	IsCheckCountry   bool
	IsCheckClass     bool
	IsCheckGender    bool
	ItemRewardType   uint8
	SelectionCount   uint8
	Param1           int64
	Param2           int64
	Param3           int64
	ItemRefID        uint32
	ItemQuantity     int
	Items            []questRewardItemReference
}

type questRewardItemReference struct {
	RefID      uint32
	Quantity   int
	RewardType uint8
}

type QuestItemMutation struct {
	Slot byte
	Item Item
}

type QuestProgressResult struct {
	Quest   CharacterQuest
	Changed bool
}

type QuestItemGrantResult struct {
	Quest     CharacterQuest
	Inventory Inventory
	Added     QuestItemMutation
}

type QuestDeliveryResult struct {
	Quest       CharacterQuest
	DialogueKey string
}

type QuestDeliveryBatch struct {
	Results      []QuestDeliveryResult
	Inventory    Inventory
	ChangedSlots []byte
}

type QuestCompletionResult struct {
	Quest       CharacterQuest
	Inventory   Inventory
	Gold        uint64
	Progress    ProgressionResult
	Experience  uint64
	SkillExp    uint64
	AddedItems  []QuestItemMutation
	RemovedSlot *byte
}

type QuestRuntimeRepository interface {
	GrantMissionItem(context.Context, int, int) (QuestItemGrantResult, error)
	RecordMobKill(context.Context, int, uint32) ([]QuestProgressResult, error)
	RecordCollectedItem(context.Context, int, uint32, uint32) ([]QuestProgressResult, error)
	DeliverItemsAtNPC(context.Context, int, uint32) (QuestDeliveryBatch, error)
	Complete(context.Context, int, int) (QuestCompletionResult, error)
}

type SQLQuestRuntimeRepository struct {
	shard  *sql.DB
	levels *LevelTable
}

func NewQuestRuntimeRepository(shard *sql.DB, levels *LevelTable) (*SQLQuestRuntimeRepository, error) {
	if shard == nil || levels == nil {
		return nil, ErrQuestRuntimeUnavailable
	}
	return &SQLQuestRuntimeRepository{shard: shard, levels: levels}, nil
}

func (r *SQLQuestRuntimeRepository) GrantMissionItem(ctx context.Context, characterID, questID int) (QuestItemGrantResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || questID != QuestWeaponListID {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	nextStatus, err := TransitionQuest(quest.Status, QuestTransitionRewardReady)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	missionItemRefID, err := lockDeliverItemReference(ctx, tx, questID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	if _, _, exists := findQuestItem(items, missionItemRefID); exists {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	slot, ok := freeQuestInventorySlot(items, capacity)
	if !ok {
		return QuestItemGrantResult{}, ErrQuestInventoryFull
	}
	item, err := insertQuestItem(ctx, tx, characterID, slot, missionItemRefID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	previousStatus := quest.Status
	quest.Status = nextStatus
	quest.Data1 = 1
	update, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`QUEST_DATA_1`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", quest.Status, quest.Data1, characterID, questID, previousStatus)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	if err = requireQuestRow(update, "grant mission item"); err != nil {
		return QuestItemGrantResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	inventory, err := loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	if persisted, exists := inventory.Items[slot]; exists {
		item = persisted
	}
	return QuestItemGrantResult{Quest: quest, Inventory: inventory, Added: QuestItemMutation{Slot: slot, Item: item}}, nil
}

func (r *SQLQuestRuntimeRepository) RecordMobKill(ctx context.Context, characterID int, mobRefObjectID uint32) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || mobRefObjectID == 0 {
		return nil, ErrQuestStateConflict
	}
	return r.recordObjectiveProgress(ctx, characterID, 1, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.REQUIRED_COUNT,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1 AND objective.TARGET_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID FOR UPDATE`, characterID, QuestActive, QuestProgressed, mobRefObjectID)
}

// RecordCollectedItem advances only explicitly runtime-enabled collection
// objectives whose authored item identity matches the committed pickup. It
// does not treat ordinary delivery objectives as collection quests: those
// require their separate NPC/item-consumption event path.
func (r *SQLQuestRuntimeRepository) RecordCollectedItem(ctx context.Context, characterID int, itemRefObjectID, quantity uint32) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || itemRefObjectID == 0 || quantity == 0 {
		return nil, ErrQuestStateConflict
	}
	return r.recordObjectiveProgress(ctx, characterID, uint64(quantity), `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.REQUIRED_COUNT,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1 AND objective.ITEM_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID FOR UPDATE`, characterID, QuestActive, QuestProgressed, itemRefObjectID)
}

func (r *SQLQuestRuntimeRepository) recordObjectiveProgress(ctx context.Context, characterID int, amount uint64, query string, arguments ...any) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || amount == 0 || query == "" {
		return nil, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pendingProgress struct {
		quest CharacterQuest
		goal  uint64
	}
	pending := make([]pendingProgress, 0)
	for rows.Next() {
		var entry pendingProgress
		if err = rows.Scan(&entry.quest.QuestID, &entry.quest.Status, &entry.quest.AchievementCount, &entry.quest.StartedAt,
			&entry.quest.CompletedAt, &entry.quest.Data1, &entry.quest.Data2, &entry.goal, &entry.quest.ObjectiveKey); err != nil {
			return nil, err
		}
		entry.quest.TaskCount = 1
		pending = append(pending, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.goal == 0 || entry.quest.Data1 >= entry.goal {
			continue
		}
		previous := entry.quest.Data1
		previousStatus := entry.quest.Status
		updated, changed, updateErr := advanceQuestObjective(entry.quest, entry.goal, amount)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		entry.quest = updated
		update, updateErr := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`QUEST_DATA_1`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=? AND `QUEST_DATA_1`=?", entry.quest.Status, entry.quest.Data1, characterID, entry.quest.QuestID, previousStatus, previous)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record mob kill"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: entry.quest, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func advanceQuestObjective(quest CharacterQuest, goal, amount uint64) (CharacterQuest, bool, error) {
	if goal == 0 || amount == 0 || quest.Data1 >= goal {
		return quest, false, nil
	}
	next := quest.Data1 + amount
	if next < quest.Data1 || next >= goal {
		next = goal
	}
	transition := QuestTransitionProgress
	if next == goal {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Data1 = next
	quest.Status = status
	return quest, true, nil
}

// DeliverItemsAtNPC implements the standard LuaSetDeliverMissionData shape.
// Every eligible quest and all required inventory stacks are locked together;
// only objectives with sufficient authored items are consumed and promoted.
func (r *SQLQuestRuntimeRepository) DeliverItemsAtNPC(ctx context.Context, characterID int, npcRefObjectID uint32) (QuestDeliveryBatch, error) {
	if r == nil || r.shard == nil || characterID <= 0 || npcRefObjectID == 0 {
		return QuestDeliveryBatch{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestDeliveryBatch{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	type deliveryObjective struct {
		quest       CharacterQuest
		variant     uint16
		mission     uint32
		dialogueKey string
	}
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.VARIANT_INDEX,objective.MISSION_INDEX,
		objective.OBJECTIVE_KEY,objective.DIALOGUE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_DELIVER_ITEM objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1
		AND objective.TARGET_NPC_REF_OBJECT_ID=? ORDER BY quest.QUEST_ID,objective.VARIANT_INDEX,objective.MISSION_INDEX FOR UPDATE`,
		characterID, QuestActive, QuestProgressed, npcRefObjectID)
	if err != nil {
		return QuestDeliveryBatch{}, err
	}
	objectives := make([]deliveryObjective, 0)
	for rows.Next() {
		var objective deliveryObjective
		if err = rows.Scan(&objective.quest.QuestID, &objective.quest.Status, &objective.quest.AchievementCount,
			&objective.quest.StartedAt, &objective.quest.CompletedAt, &objective.quest.Data1, &objective.quest.Data2,
			&objective.variant, &objective.mission, &objective.quest.ObjectiveKey, &objective.dialogueKey); err != nil {
			_ = rows.Close()
			return QuestDeliveryBatch{}, err
		}
		objectives = append(objectives, objective)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return QuestDeliveryBatch{}, err
	}
	if err = rows.Close(); err != nil {
		return QuestDeliveryBatch{}, err
	}
	if len(objectives) == 0 {
		if err = tx.Commit(); err != nil {
			return QuestDeliveryBatch{}, errors.Join(ErrQuestOutcomeUnknown, err)
		}
		committed = true
		return QuestDeliveryBatch{}, nil
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestDeliveryBatch{}, err
	}
	changed := make(map[byte]struct{})
	result := QuestDeliveryBatch{Results: make([]QuestDeliveryResult, 0, len(objectives))}
	for _, objective := range objectives {
		requiredRows, queryErr := tx.QueryContext(ctx, `SELECT ITEM_REF_OBJECT_ID,QUANTITY FROM REF_QUEST_DELIVER_ITEM_ENTRY
			WHERE QUEST_ID=? AND VARIANT_INDEX=? AND MISSION_INDEX=? ORDER BY ITEM_INDEX FOR SHARE`,
			objective.quest.QuestID, objective.variant, objective.mission)
		if queryErr != nil {
			return QuestDeliveryBatch{}, queryErr
		}
		required := make(map[uint32]uint64)
		for requiredRows.Next() {
			var refID uint32
			var quantity uint64
			if queryErr = requiredRows.Scan(&refID, &quantity); queryErr != nil {
				_ = requiredRows.Close()
				return QuestDeliveryBatch{}, queryErr
			}
			if refID == 0 || quantity == 0 || required[refID] > math.MaxUint64-quantity {
				_ = requiredRows.Close()
				return QuestDeliveryBatch{}, ErrQuestReferenceUnsupported
			}
			required[refID] += quantity
		}
		if queryErr = requiredRows.Err(); queryErr != nil {
			_ = requiredRows.Close()
			return QuestDeliveryBatch{}, queryErr
		}
		if queryErr = requiredRows.Close(); queryErr != nil {
			return QuestDeliveryBatch{}, queryErr
		}
		if len(required) == 0 || !questInventoryContains(items, required) {
			continue
		}
		referenceIDs := make([]uint32, 0, len(required))
		for refID := range required {
			referenceIDs = append(referenceIDs, refID)
		}
		sort.Slice(referenceIDs, func(left, right int) bool { return referenceIDs[left] < referenceIDs[right] })
		for _, refID := range referenceIDs {
			if err = consumeQuestInventoryReference(ctx, tx, characterID, items, refID, required[refID], changed); err != nil {
				return QuestDeliveryBatch{}, err
			}
		}
		previousStatus := objective.quest.Status
		objective.quest.Status, err = TransitionQuest(previousStatus, QuestTransitionRewardReady)
		if err != nil {
			return QuestDeliveryBatch{}, err
		}
		objective.quest.TaskCount = 0
		update, updateErr := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?",
			objective.quest.Status, characterID, objective.quest.QuestID, previousStatus)
		if updateErr != nil {
			return QuestDeliveryBatch{}, updateErr
		}
		if updateErr = requireQuestRow(update, "deliver quest items"); updateErr != nil {
			return QuestDeliveryBatch{}, updateErr
		}
		result.Results = append(result.Results, QuestDeliveryResult{Quest: objective.quest, DialogueKey: objective.dialogueKey})
	}
	if err = tx.Commit(); err != nil {
		return QuestDeliveryBatch{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	if len(result.Results) == 0 {
		return result, nil
	}
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestDeliveryBatch{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	for slot := byte(0); slot < math.MaxUint8; slot++ {
		if _, exists := changed[slot]; exists {
			result.ChangedSlots = append(result.ChangedSlots, slot)
		}
	}
	return result, nil
}

func questInventoryContains(items map[byte]questInventoryItem, required map[uint32]uint64) bool {
	totals := make(map[uint32]uint64, len(required))
	for _, item := range items {
		if _, wanted := required[item.refID]; wanted && item.stack > 0 {
			amount := uint64(item.stack)
			if totals[item.refID] > math.MaxUint64-amount {
				return false
			}
			totals[item.refID] += amount
		}
	}
	for refID, quantity := range required {
		if totals[refID] < quantity {
			return false
		}
	}
	return true
}

func consumeQuestInventoryReference(ctx context.Context, tx *sql.Tx, characterID int, items map[byte]questInventoryItem, refID uint32, quantity uint64, changed map[byte]struct{}) error {
	for slot := byte(13); slot < math.MaxUint8 && quantity > 0; slot++ {
		item, exists := items[slot]
		if !exists || item.refID != refID || item.stack <= 0 {
			continue
		}
		available := uint64(item.stack)
		if available <= quantity {
			removed, err := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `FK_ITEM`=? AND `SLOT`=?", characterID, item.id, slot)
			if err != nil {
				return err
			}
			if err = requireQuestRow(removed, "unlink delivered quest item"); err != nil {
				return err
			}
			removed, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.id)
			if err != nil {
				return err
			}
			if err = requireQuestRow(removed, "delete delivered quest item"); err != nil {
				return err
			}
			delete(items, slot)
			quantity -= available
		} else {
			remaining := available - quantity
			updated, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=? WHERE `id`=? AND `STACK_COUNT`=?", remaining, item.id, item.stack)
			if err != nil {
				return err
			}
			if err = requireQuestRow(updated, "decrement delivered quest item"); err != nil {
				return err
			}
			item.stack = int(remaining)
			items[slot] = item
			quantity = 0
		}
		changed[slot] = struct{}{}
	}
	if quantity != 0 {
		return ErrQuestRequiredItem
	}
	return nil
}

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
		return QuestCompletionResult{}, err
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	closedStatus, err := TransitionQuest(quest.Status, QuestTransitionClose)
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
	if err = validateQuestRewardReference(questID, reward); err != nil {
		return QuestCompletionResult{}, err
	}
	result := QuestCompletionResult{Quest: quest, Gold: state.gold}
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
		result.RemovedSlot = &slot
	}
	if reward.IsItemReward {
		slot, ok := freeQuestInventorySlot(items, state.inventorySize)
		if !ok {
			return QuestCompletionResult{}, ErrQuestInventoryFull
		}
		item, insertErr := insertQuestItem(ctx, tx, characterID, slot, reward.ItemRefID)
		if insertErr != nil {
			return QuestCompletionResult{}, insertErr
		}
		result.AddedItems = append(result.AddedItems, QuestItemMutation{Slot: slot, Item: item})
	}
	if state.gold > math.MaxUint64-reward.Gold {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	state.gold += reward.Gold

	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, reward.Experience, reward.SkillExperience, state.skillPoints, state.statPoints)
	updatedCharacter, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", state.gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, progress.Level, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(updatedCharacter, "apply quest rewards"); err != nil {
		return QuestCompletionResult{}, err
	}
	now := time.Now().UTC()
	completed, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`COMPLETED_AT`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", closedStatus, now, characterID, questID, quest.Status)
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
	result.Quest.CompletedAt = &now
	result.Gold = state.gold
	result.Progress = progress
	result.Experience = reward.Experience
	result.SkillExp = reward.SkillExperience
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
	closedStatus, err := TransitionQuest(quest.Status, QuestTransitionClose)
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
	selected, err := selectQuestRewardItems(questID, reward, entryIDs)
	if err != nil {
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
	updatedCharacter, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", state.gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, progress.Level, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(updatedCharacter, "apply selected quest rewards"); err != nil {
		return QuestCompletionResult{}, err
	}
	now := time.Now().UTC()
	completed, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`COMPLETED_AT`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", closedStatus, now, characterID, questID, quest.Status)
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
	result.Quest.CompletedAt = &now
	result.Gold = state.gold
	result.Progress = progress
	result.Experience = reward.Experience
	result.SkillExp = reward.SkillExperience
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

func selectQuestRewardItems(questID int, reward questRewardReference, entryIDs []uint32) ([]questRewardItemReference, error) {
	unsupported := !reward.IsBasicReward || !reward.IsItemReward || reward.SelectionCount == 0 ||
		reward.IsCheckCondition || reward.IsCheckCountry || reward.IsCheckClass || reward.IsCheckGender ||
		reward.SkillPoints != 0 || reward.AcademyPoints != 0 || reward.Hwan != 0 || reward.InventorySlots != 0 ||
		reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0
	if unsupported || len(reward.Items) == 0 {
		return nil, fmt.Errorf("%w: quest %d", ErrQuestReferenceUnsupported, questID)
	}
	if len(entryIDs) != int(reward.SelectionCount) {
		return nil, fmt.Errorf("%w: quest %d selected %d entries, want %d", ErrQuestStateConflict, questID, len(entryIDs), reward.SelectionCount)
	}
	options := make(map[uint32]questRewardItemReference, len(reward.Items))
	for _, item := range reward.Items {
		if item.RefID == 0 || item.Quantity <= 0 {
			return nil, fmt.Errorf("%w: quest %d has an invalid reward item", ErrQuestReferenceUnsupported, questID)
		}
		if _, duplicate := options[item.RefID]; duplicate {
			return nil, fmt.Errorf("%w: quest %d has ambiguous item %d", ErrQuestReferenceUnsupported, questID, item.RefID)
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

func lockDeliverItemReference(ctx context.Context, tx *sql.Tx, questID int) (uint32, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ITEM_REF_OBJECT_ID FROM REF_QUEST_DELIVER_ITEM_EX
		WHERE QUEST_ID=? AND RUNTIME_ENABLED=1 ORDER BY VARIANT_INDEX,MISSION_INDEX FOR SHARE`, questID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var itemRefID uint32
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&itemRefID); err != nil {
			return 0, err
		}
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if count != 1 || itemRefID == 0 {
		return 0, fmt.Errorf("%w: quest %d resolved %d enabled deliver-item objectives", ErrQuestReferenceUnsupported, questID, count)
	}
	return itemRefID, nil
}

func lockQuestRewardReference(ctx context.Context, tx *sql.Tx, questID int) (questRewardReference, error) {
	var reward questRewardReference
	var basic, item, checkCondition, checkCountry, checkClass, checkGender uint8
	var scriptExperience, scriptSkillExperience, scriptGold uint64
	var scriptItemCount uint16
	err := tx.QueryRowContext(ctx, `SELECT reward.GOLD,reward.EXPERIENCE,reward.SKILL_EXPERIENCE,reward.SKILL_POINTS,
		reward.ACADEMY_POINTS,reward.HWAN,reward.INVENTORY_SLOTS,reward.IS_BASIC_REWARD,reward.IS_ITEM_REWARD,
		reward.IS_CHECK_CONDITION,reward.IS_CHECK_COUNTRY,reward.IS_CHECK_CLASS,reward.IS_CHECK_GENDER,
		reward.ITEM_REWARD_TYPE,reward.SELECTION_COUNT,reward.PARAM_1,reward.PARAM_2,reward.PARAM_3,
		script.EXPERIENCE,script.SKILL_EXPERIENCE,script.GOLD,script.ITEM_COUNT
		FROM REF_QUEST_REWARD reward
		JOIN REF_QUEST_SCRIPT_REWARD script ON script.QUEST_ID=reward.QUEST_ID AND script.RUNTIME_ENABLED=1
		WHERE reward.QUEST_ID=? FOR SHARE`, questID).Scan(
		&reward.Gold, &reward.Experience, &reward.SkillExperience, &reward.SkillPoints, &reward.AcademyPoints,
		&reward.Hwan, &reward.InventorySlots, &basic, &item, &checkCondition, &checkCountry, &checkClass,
		&checkGender, &reward.ItemRewardType, &reward.SelectionCount, &reward.Param1, &reward.Param2, &reward.Param3,
		&scriptExperience, &scriptSkillExperience, &scriptGold, &scriptItemCount)
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
	if reward.Experience != scriptExperience || reward.SkillExperience != scriptSkillExperience || reward.Gold != scriptGold || reward.IsItemReward != (scriptItemCount > 0) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d SQL and script rewards disagree", ErrQuestReferenceUnsupported, questID)
	}
	if !reward.IsItemReward {
		return reward, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT item.ItemID,rewards.ACHIEVE_QUANTITY,rewards.REWARD_TYPE,
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
	if count != 1 {
		if reward.SelectionCount == 0 {
			return questRewardReference{}, fmt.Errorf("%w: quest %d resolved %d reward items", ErrQuestReferenceUnsupported, questID, count)
		}
		return reward, nil
	}
	reward.ItemRefID = reward.Items[0].RefID
	reward.ItemQuantity = reward.Items[0].Quantity
	return reward, nil
}

func validateQuestRewardReference(questID int, reward questRewardReference) error {
	unsupported := !reward.IsBasicReward || reward.IsCheckCondition || reward.IsCheckCountry || reward.IsCheckClass || reward.IsCheckGender ||
		reward.SkillPoints != 0 || reward.AcademyPoints != 0 || reward.Hwan != 0 || reward.InventorySlots != 0 ||
		reward.ItemRewardType != 0 || reward.SelectionCount != 0 || reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0
	if unsupported {
		return fmt.Errorf("%w: quest %d", ErrQuestReferenceUnsupported, questID)
	}
	if reward.IsItemReward {
		if reward.ItemRefID == 0 || reward.ItemQuantity != 1 {
			return fmt.Errorf("%w: quest %d requires one exact item reward", ErrQuestReferenceUnsupported, questID)
		}
	} else if reward.ItemRefID != 0 || reward.ItemQuantity != 0 {
		return fmt.Errorf("%w: quest %d has inconsistent item reward", ErrQuestReferenceUnsupported, questID)
	}
	return nil
}

type questCharacterState struct {
	gold            uint64
	level           int
	experience      uint64
	skillExperience uint64
	skillPoints     int
	statPoints      int
	inventorySize   byte
}

func lockQuestCharacter(ctx context.Context, tx *sql.Tx, characterID int) (questCharacterState, error) {
	var state questCharacterState
	err := tx.QueryRowContext(ctx, "SELECT `GOLD`,`CURRENT_LEVEL`,`EXP`,`SKILL_EXP`,`SKILL_POINTS`,`STAT_POINTS`,`INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&state.gold, &state.level, &state.experience, &state.skillExperience, &state.skillPoints, &state.statPoints, &state.inventorySize)
	return state, err
}

func lockQuestCharacterCapacity(ctx context.Context, tx *sql.Tx, characterID int) (byte, error) {
	var capacity byte
	err := tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&capacity)
	return capacity, err
}

func lockCharacterQuest(ctx context.Context, tx *sql.Tx, characterID, questID int) (CharacterQuest, error) {
	var quest CharacterQuest
	quest.QuestID = questID
	err := tx.QueryRowContext(ctx, "SELECT `STATUS`,`ACHIEVEMENT_COUNT`,`STARTED_AT`,`COMPLETED_AT`,`QUEST_DATA_1`,`QUEST_DATA_2` FROM `CHAR_QUEST` WHERE `FK_CHAR`=? AND `QUEST_ID`=? FOR UPDATE", characterID, questID).Scan(&quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt, &quest.Data1, &quest.Data2)
	return quest, err
}

type questInventoryItem struct {
	id    int64
	refID uint32
	stack int
}

func lockQuestInventory(ctx context.Context, tx *sql.Tx, characterID int) (map[byte]questInventoryItem, error) {
	rows, err := tx.QueryContext(ctx, "SELECT i.`SLOT`,i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` WHERE i.`FK_CHAR`=? ORDER BY i.`SLOT` FOR UPDATE", characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[byte]questInventoryItem)
	for rows.Next() {
		var slot byte
		var item questInventoryItem
		if err = rows.Scan(&slot, &item.id, &item.refID, &item.stack); err != nil {
			return nil, err
		}
		items[slot] = item
	}
	return items, rows.Err()
}

func findQuestItem(items map[byte]questInventoryItem, refID uint32) (byte, questInventoryItem, bool) {
	for slot := byte(13); slot < 255; slot++ {
		if item, ok := items[slot]; ok && item.refID == refID && item.stack > 0 {
			return slot, item, true
		}
	}
	return 0, questInventoryItem{}, false
}

func freeQuestInventorySlot(items map[byte]questInventoryItem, capacity byte) (byte, bool) {
	if capacity == 0 {
		capacity = BaseInventorySize
	}
	for slot := byte(13); slot < capacity; slot++ {
		if _, occupied := items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func insertQuestItem(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32) (Item, error) {
	return insertQuestItemStack(ctx, tx, characterID, slot, refID, 1)
}

func insertQuestItemStack(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32, quantity int) (Item, error) {
	if _, ok := RefItems[refID]; !ok {
		return Item{}, fmt.Errorf("%w: %d", ErrInventoryReferenceItemNotFound, refID)
	}
	if quantity <= 0 {
		return Item{}, ErrQuestStateConflict
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (0,?,?,0,0,0,0,0,0,0,0,0)", refID, quantity)
	if err != nil {
		return Item{}, err
	}
	itemID, err := insert.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
		return Item{}, err
	}
	return economyItem(itemID, refID, 0, quantity, 0, 0, RentInfo{})
}

func loadQuestInventory(ctx context.Context, shard *sql.DB, characterID int) (Inventory, error) {
	repository, err := NewInventoryRepository(shard)
	if err != nil {
		return Inventory{}, err
	}
	return repository.GetCharacterInventory(ctx, int64(characterID))
}

func requireQuestRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrQuestStateConflict, operation, affected)
	}
	return nil
}
