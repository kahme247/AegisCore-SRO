package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	QuestWeaponListID     = 3
	QuestChulsanAngerID   = 58
	questMaxInventorySize = byte(112)
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
	NamedPointKey    string
	NamedPointAmount uint64
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

type QuestDialogPrompt struct {
	QuestID     int
	DialogueKey string
}

// QuestCollectionDrop is an authored gather-from-monster item eligible for
// generation after this character kills the matching source monster.
type QuestCollectionDrop struct {
	QuestID         int
	ReferenceItemID uint32
	StackCount      uint16
	ChancePercent   float64
}

type QuestPlayerKillTarget struct {
	Level    int
	JobType  byte
	RegionID int16
}

type QuestItemGrantResult struct {
	Quest     CharacterQuest
	Inventory Inventory
	Added     QuestItemMutation
}

// QuestActivationResult is the committed quest and inventory snapshot created
// together when accepting a standard delivery quest.
type QuestActivationResult struct {
	Quest      CharacterQuest
	Inventory  Inventory
	AddedItems []QuestItemMutation
}

// QuestUniqueDeadGrantResult is the atomic inventory/progress mutation made
// by the six retained UNIQUE_DEAD Lua handlers. RemovedSlot is populated only
// when the handler consumed the optional phase-1 marker item.
type QuestUniqueDeadGrantResult struct {
	Quest       CharacterQuest
	Inventory   Inventory
	Added       QuestItemMutation
	RemovedSlot *byte
}

type QuestDeliveryResult struct {
	Quest       CharacterQuest
	DialogueKey string
}

type QuestDeliveryPrompt struct {
	QuestID     int
	DialogueKey string
}

type QuestDeliveryBatch struct {
	Results      []QuestDeliveryResult
	Inventory    Inventory
	ChangedSlots []byte
}

type QuestCompletionResult struct {
	Quest      CharacterQuest
	Inventory  Inventory
	Gold       uint64
	Progress   ProgressionResult
	Experience uint64
	SkillExp   uint64
	// FixedSkillPoints is the authored point grant applied after skill-exp
	// conversion. It is kept separate so the handler can emit the stock
	// point-balance update in addition to the experience notification.
	FixedSkillPoints  uint64
	InventorySize     byte
	InventoryExpanded bool
	HwanLevel         byte
	HwanChanged       bool
	AddedItems        []QuestItemMutation
	RemovedSlot       *byte
	ChangedSlots      []byte
	NamedPointKey     string
	NamedPointAmount  uint64
}

type QuestRuntimeRepository interface {
	ActivateDeliveryQuest(context.Context, int, CharacterQuest) (QuestActivationResult, error)
	GrantMissionItem(context.Context, int, int) (QuestItemGrantResult, error)
	RecordMobKill(context.Context, int, uint32, MonsterRarity) ([]QuestProgressResult, error)
	RecordCollectedItem(context.Context, int, uint32, uint32) ([]QuestProgressResult, error)
	ListDialogsAtNPC(context.Context, int, uint32) ([]QuestDialogPrompt, error)
	CompleteDialog(context.Context, int, int, uint32) (QuestProgressResult, error)
	ListDeliveriesAtNPC(context.Context, int, uint32) ([]QuestDeliveryPrompt, error)
	DeliverItemsAtNPC(context.Context, int, int, uint32) (QuestDeliveryBatch, error)
	Complete(context.Context, int, int) (QuestCompletionResult, error)
}

// ActivateDeliveryQuest atomically activates one standard delivery quest and
// places every authored delivery item in the character inventory. A failed
// capacity or reference check rolls back both the quest and item mutations.
func (r *SQLQuestRuntimeRepository) ActivateDeliveryQuest(ctx context.Context, characterID int, quest CharacterQuest) (QuestActivationResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || quest.QuestID <= 0 || quest.Status != QuestActive {
		return QuestActivationResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestActivationResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return QuestActivationResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT entry.ITEM_REF_OBJECT_ID,entry.QUANTITY
		FROM REF_QUEST_DELIVER_ITEM objective
		JOIN REF_QUEST_DELIVER_ITEM_ENTRY entry ON entry.QUEST_ID=objective.QUEST_ID
		  AND entry.VARIANT_INDEX=objective.VARIANT_INDEX AND entry.MISSION_INDEX=objective.MISSION_INDEX
		WHERE objective.QUEST_ID=? AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 ORDER BY entry.ITEM_INDEX FOR SHARE`, quest.QuestID)
	if err != nil {
		return QuestActivationResult{}, err
	}
	type deliveryItem struct {
		refID    uint32
		quantity int
	}
	deliveryItems := make([]deliveryItem, 0)
	for rows.Next() {
		var item deliveryItem
		if err = rows.Scan(&item.refID, &item.quantity); err != nil {
			_ = rows.Close()
			return QuestActivationResult{}, err
		}
		if item.refID == 0 || item.quantity <= 0 {
			_ = rows.Close()
			return QuestActivationResult{}, ErrQuestReferenceUnsupported
		}
		deliveryItems = append(deliveryItems, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return QuestActivationResult{}, err
	}
	if err = rows.Close(); err != nil {
		return QuestActivationResult{}, err
	}
	if len(deliveryItems) == 0 {
		return QuestActivationResult{}, ErrQuestReferenceMissing
	}

	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestActivationResult{}, err
	}
	availableSlots := make([]byte, 0, len(deliveryItems))
	for range deliveryItems {
		slot, ok := freeQuestInventorySlot(items, capacity)
		if !ok {
			return QuestActivationResult{}, ErrQuestInventoryFull
		}
		availableSlots = append(availableSlots, slot)
		items[slot] = questInventoryItem{}
	}

	quest, err = activateCharacterQuestRecord(ctx, tx, characterID, quest)
	if err != nil {
		return QuestActivationResult{}, err
	}
	result := QuestActivationResult{Quest: quest, AddedItems: make([]QuestItemMutation, 0, len(deliveryItems))}
	for index, definition := range deliveryItems {
		item, insertErr := insertQuestItemStack(ctx, tx, characterID, availableSlots[index], definition.refID, definition.quantity)
		if insertErr != nil {
			return QuestActivationResult{}, insertErr
		}
		result.AddedItems = append(result.AddedItems, QuestItemMutation{Slot: availableSlots[index], Item: item})
	}
	if err = tx.Commit(); err != nil {
		return QuestActivationResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestActivationResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	return result, nil
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

func (r *SQLQuestRuntimeRepository) RecordMobKill(ctx context.Context, characterID int, mobRefObjectID uint32, rarity MonsterRarity) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || mobRefObjectID == 0 {
		return nil, ErrQuestStateConflict
	}
	ordinary, err := r.recordObjectiveProgress(ctx, characterID, 1, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,
		COALESCE(ordinary.REQUIRED_COUNT,
		  CAST(JSON_UNQUOTE(JSON_EXTRACT(extended.PARAMETERS,CONCAT('$[',extended.TARGET_COUNT+6,'].Number'))) AS UNSIGNED)),
		COALESCE(ordinary.OBJECTIVE_KEY,extended.OBJECTIVE_KEY)
		FROM CHAR_QUEST quest
		LEFT JOIN REF_QUEST_KILL_OBJECTIVE ordinary ON ordinary.QUEST_ID=quest.QUEST_ID
		  AND ordinary.RUNTIME_ENABLED=1 AND ordinary.TARGET_REF_OBJECT_ID=?
		  AND (SELECT COUNT(*) FROM REF_QUEST_KILL_OBJECTIVE ordinary_count WHERE ordinary_count.QUEST_ID=ordinary.QUEST_ID AND ordinary_count.VARIANT_INDEX=0 AND ordinary_count.RUNTIME_ENABLED=1)=1
		LEFT JOIN REF_QUEST_EXTENDED_KILL_TARGET target ON target.QUEST_ID=quest.QUEST_ID
		  AND target.VARIANT_INDEX=0 AND target.MISSION_INDEX=0 AND target.TARGET_REF_OBJECT_ID=?
		LEFT JOIN REF_QUEST_EXTENDED_KILL_OBJECTIVE extended ON extended.QUEST_ID=target.QUEST_ID
		  AND extended.VARIANT_INDEX=target.VARIANT_INDEX AND extended.MISSION_INDEX=target.MISSION_INDEX
		  AND extended.RUNTIME_ENABLED=1 AND extended.DECLARATION_KIND='multi_target'
		  AND (SELECT COUNT(*) FROM REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_count
		       WHERE extended_count.QUEST_ID=extended.QUEST_ID AND extended_count.VARIANT_INDEX=0
		         AND extended_count.RUNTIME_ENABLED=1 AND extended_count.DECLARATION_KIND='multi_target')=1
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?)
		  AND ((ordinary.QUEST_ID IS NOT NULL)+(extended.QUEST_ID IS NOT NULL))=1
		ORDER BY quest.QUEST_ID FOR UPDATE`, mobRefObjectID, mobRefObjectID, characterID, QuestActive, QuestProgressed)
	if err != nil {
		return nil, err
	}
	multiTarget, err := r.recordMultiTargetExtendedKill(ctx, characterID, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	shared, err := r.recordSharedKillMission(ctx, characterID, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	classified, err := r.recordClassifiedMobKill(ctx, characterID, mobRefObjectID, rarity)
	if err != nil {
		return nil, err
	}
	return append(append(append(ordinary, multiTarget...), shared...), classified...), nil
}

// RecordCollectedItem advances only explicitly runtime-enabled collection
// objectives whose authored item identity matches the committed pickup. It
// does not treat ordinary delivery objectives as collection quests: those
// require their separate NPC/item-consumption event path.
func (r *SQLQuestRuntimeRepository) RecordCollectedItem(ctx context.Context, characterID int, itemRefObjectID, quantity uint32) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || itemRefObjectID == 0 || quantity == 0 {
		return nil, ErrQuestStateConflict
	}
	ordinary, err := r.recordObjectiveProgress(ctx, characterID, uint64(quantity), `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.REQUIRED_COUNT,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1
		  AND objective.DECLARATION_KIND='standard' AND objective.ITEM_REF_OBJECT_ID=?
		  AND (SELECT COUNT(*) FROM REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective_count
		       WHERE objective_count.QUEST_ID=objective.QUEST_ID AND objective_count.VARIANT_INDEX=0 AND objective_count.RUNTIME_ENABLED=1)=1
		ORDER BY quest.QUEST_ID FOR UPDATE`, characterID, QuestActive, QuestProgressed, itemRefObjectID)
	if err != nil {
		return nil, err
	}
	multi, err := r.recordMultiCollectionProgress(ctx, characterID, itemRefObjectID)
	if err != nil {
		return nil, err
	}
	extended, err := r.recordExtendedCollectionProgress(ctx, characterID, itemRefObjectID)
	if err != nil {
		return nil, err
	}
	return append(append(ordinary, multi...), extended...), nil
}

// DeliverItemsAtNPC implements the standard LuaSetDeliverMissionData shape.
// Every eligible quest and all required inventory stacks are locked together;
// the selected objective is consumed and promoted only after confirmation.
func (r *SQLQuestRuntimeRepository) DeliverItemsAtNPC(ctx context.Context, characterID, questID int, npcRefObjectID uint32) (QuestDeliveryBatch, error) {
	if r == nil || r.shard == nil || characterID <= 0 || questID <= 0 || npcRefObjectID == 0 {
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
		AND objective.TARGET_NPC_REF_OBJECT_ID=? AND quest.QUEST_ID=?
		ORDER BY quest.QUEST_ID,objective.VARIANT_INDEX,objective.MISSION_INDEX FOR UPDATE`,
		characterID, QuestActive, QuestProgressed, npcRefObjectID, questID)
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

type questCharacterState struct {
	gold            uint64
	level           int
	experience      uint64
	skillExperience uint64
	skillPoints     int
	statPoints      int
	inventorySize   byte
	country         int
}

func lockQuestCharacter(ctx context.Context, tx *sql.Tx, characterID int) (questCharacterState, error) {
	var state questCharacterState
	err := tx.QueryRowContext(ctx, "SELECT c.`GOLD`,c.`CURRENT_LEVEL`,c.`EXP`,c.`SKILL_EXP`,c.`SKILL_POINTS`,c.`STAT_POINTS`,c.`INVENTORY_SIZE`,r.`Country` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` WHERE c.`id`=? FOR UPDATE", characterID).Scan(&state.gold, &state.level, &state.experience, &state.skillExperience, &state.skillPoints, &state.statPoints, &state.inventorySize, &state.country)
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
