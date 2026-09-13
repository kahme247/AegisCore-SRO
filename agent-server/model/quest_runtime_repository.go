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
	GrantMissionItem(context.Context, int, int) (QuestItemGrantResult, error)
	RecordMobKill(context.Context, int, uint32, MonsterRarity) ([]QuestProgressResult, error)
	RecordCollectedItem(context.Context, int, uint32, uint32) ([]QuestProgressResult, error)
	ListDialogsAtNPC(context.Context, int, uint32) ([]QuestDialogPrompt, error)
	CompleteDialog(context.Context, int, int, uint32) (QuestProgressResult, error)
	ListDeliveriesAtNPC(context.Context, int, uint32) ([]QuestDeliveryPrompt, error)
	DeliverItemsAtNPC(context.Context, int, int, uint32) (QuestDeliveryBatch, error)
	Complete(context.Context, int, int) (QuestCompletionResult, error)
}

func (r *SQLQuestRuntimeRepository) ListDialogsAtNPC(ctx context.Context, characterID int, npcRefObjectID uint32) ([]QuestDialogPrompt, error) {
	if r == nil || r.shard == nil || characterID <= 0 || npcRefObjectID == 0 {
		return nil, ErrQuestStateConflict
	}
	rows, err := r.shard.QueryContext(normalizeEconomyContext(ctx), `SELECT quest.QUEST_ID,objective.DIALOGUE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_DIALOG_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?)
		  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0 AND objective.RUNTIME_ENABLED=1
		  AND objective.TARGET_NPC_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID`, characterID, QuestActive, QuestProgressed, npcRefObjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prompts := make([]QuestDialogPrompt, 0)
	for rows.Next() {
		var prompt QuestDialogPrompt
		if err = rows.Scan(&prompt.QuestID, &prompt.DialogueKey); err != nil {
			return nil, err
		}
		if prompt.QuestID <= 0 || prompt.DialogueKey == "" {
			return nil, ErrQuestReferenceMissing
		}
		prompts = append(prompts, prompt)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return prompts, nil
}

func (r *SQLQuestRuntimeRepository) CompleteDialog(ctx context.Context, characterID, questID int, npcRefObjectID uint32) (QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || questID <= 0 || npcRefObjectID == 0 {
		return QuestProgressResult{}, ErrQuestStateConflict
	}
	results, err := r.recordObjectiveProgress(ctx, characterID, 1, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,1,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_DIALOG_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.QUEST_ID=? AND quest.STATUS IN (?,?)
		  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0 AND objective.RUNTIME_ENABLED=1
		  AND objective.TARGET_NPC_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID FOR UPDATE`, characterID, questID, QuestActive, QuestProgressed, npcRefObjectID)
	if err != nil {
		return QuestProgressResult{}, err
	}
	if len(results) != 1 || !results[0].Changed {
		return QuestProgressResult{}, ErrQuestStateConflict
	}
	return results[0], nil
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

const uniqueDeadGrantQuery = `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
	quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.OBJECTIVE_KEY,
	objective.ITEM_REF_OBJECT_ID,precursor.ID
	FROM CHAR_QUEST quest
	JOIN REF_QUEST reference ON reference.ID=quest.QUEST_ID
	JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
	  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0 AND objective.RUNTIME_ENABLED=1
	JOIN REF_QUEST_COLLECTION_ITEM_SOURCE source ON source.QUEST_ID=objective.QUEST_ID
	  AND source.VARIANT_INDEX=0 AND source.MISSION_INDEX=0 AND source.SOURCE_INDEX=0
	JOIN REF_OBJ_COMMON_VSRO precursor ON precursor.CODE_NAME=CONCAT('ITEM_',reference.CODE_NAME,'_1')
	WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND quest.QUEST_DATA_1<1
	  AND reference.CODE_NAME IN ('QNO_SD_MA_029','QNO_SD_MA_030','QNO_SD_MA_031','QNO_SD_MA_032','QNO_SD_MA_033','QNO_SD_MA_034')
	  AND source.SOURCE_PARAMETER=0 AND source.MONSTER_REF_OBJECT_ID=?
	  AND ?=CASE reference.CODE_NAME
	    WHEN 'QNO_SD_MA_029' THEN -32758 WHEN 'QNO_SD_MA_030' THEN -32757
	    WHEN 'QNO_SD_MA_031' THEN -32756 WHEN 'QNO_SD_MA_032' THEN -32755
	    WHEN 'QNO_SD_MA_033' THEN -32754 WHEN 'QNO_SD_MA_034' THEN -32753 END
	FOR UPDATE`

// GrantUniqueDeadMissionItem reproduces the retained QNO_SD_MA_029..034
// UNIQUE_DEAD handlers. The Lua bytecode qualifies the current player region
// and unique code, optionally consumes ITEM_<quest>_1, then directly adds the
// declared ITEM_<quest>_2 collection item.
func (r *SQLQuestRuntimeRepository) GrantUniqueDeadMissionItem(ctx context.Context, characterID int, monsterRefObjectID uint32, regionID int16) (QuestUniqueDeadGrantResult, bool, error) {
	if r == nil || r.shard == nil || characterID <= 0 || monsterRefObjectID == 0 {
		return QuestUniqueDeadGrantResult{}, false, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	row := tx.QueryRowContext(ctx, uniqueDeadGrantQuery, characterID, QuestActive, QuestProgressed, monsterRefObjectID, regionID)
	var quest CharacterQuest
	var missionItemRefID, precursorRefID uint32
	if err = row.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt,
		&quest.Data1, &quest.Data2, &quest.ObjectiveKey, &missionItemRefID, &precursorRefID); errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		committed = true
		return QuestUniqueDeadGrantResult{}, false, nil
	} else if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	quest.TaskCount = 1
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	var removedSlot *byte
	if slot, _, exists := findQuestItem(items, precursorRefID); exists {
		changed := make(map[byte]struct{}, 1)
		if err = consumeQuestInventoryReference(ctx, tx, characterID, items, precursorRefID, 1, changed); err != nil {
			return QuestUniqueDeadGrantResult{}, false, err
		}
		removed := slot
		removedSlot = &removed
	}
	slot, ok := freeQuestInventorySlot(items, capacity)
	if !ok {
		return QuestUniqueDeadGrantResult{}, false, ErrQuestInventoryFull
	}
	item, err := insertQuestItem(ctx, tx, characterID, slot, missionItemRefID)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	previousStatus := quest.Status
	quest.Status, err = TransitionQuest(quest.Status, QuestTransitionRewardReady)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	quest.Data1 = 1
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=1
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1<1`, quest.Status, characterID, quest.QuestID, previousStatus)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	if err = requireQuestRow(updated, "grant UNIQUE_DEAD quest item"); err != nil {
		return QuestUniqueDeadGrantResult{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return QuestUniqueDeadGrantResult{}, false, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	inventory, err := loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestUniqueDeadGrantResult{}, false, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	if persisted, exists := inventory.Items[slot]; exists {
		item = persisted
	}
	return QuestUniqueDeadGrantResult{Quest: quest, Inventory: inventory, Added: QuestItemMutation{Slot: slot, Item: item}, RemovedSlot: removedSlot}, true, nil
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

func (r *SQLQuestRuntimeRepository) recordMultiTargetExtendedKill(ctx context.Context, characterID int, mobRefObjectID uint32) ([]QuestProgressResult, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,objective.OBJECTIVE_KEY,
		CAST(JSON_UNQUOTE(JSON_EXTRACT(objective.PARAMETERS,CONCAT('$[',objective.TARGET_COUNT+6,'].Number'))) AS UNSIGNED),
		EXISTS(SELECT 1 FROM REF_QUEST_EXTENDED_KILL_TARGET matched WHERE matched.QUEST_ID=objective.QUEST_ID
		  AND matched.VARIANT_INDEX=objective.VARIANT_INDEX AND matched.MISSION_INDEX=objective.MISSION_INDEX
		  AND matched.TARGET_REF_OBJECT_ID=?)
		FROM CHAR_QUEST quest JOIN REF_QUEST_EXTENDED_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 AND objective.DECLARATION_KIND='multi_target'
		  AND (SELECT COUNT(*) FROM REF_QUEST_EXTENDED_KILL_OBJECTIVE objective_count
		       WHERE objective_count.QUEST_ID=objective.QUEST_ID AND objective_count.VARIANT_INDEX=0
		         AND objective_count.RUNTIME_ENABLED=1 AND objective_count.DECLARATION_KIND='multi_target') BETWEEN 2 AND 4
		  AND EXISTS(SELECT 1 FROM REF_QUEST_EXTENDED_KILL_TARGET any_match WHERE any_match.QUEST_ID=objective.QUEST_ID
		    AND any_match.VARIANT_INDEX=objective.VARIANT_INDEX AND any_match.TARGET_REF_OBJECT_ID=?)
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, mobRefObjectID, characterID, QuestActive, QuestProgressed, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	type pendingQuest struct {
		quest   CharacterQuest
		goals   [4]uint16
		matched [4]bool
		count   int
	}
	pending := make([]pendingQuest, 0)
	for rows.Next() {
		var quest CharacterQuest
		var mission uint32
		var key string
		var goal uint64
		var matched bool
		if err = rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt,
			&quest.Data1, &quest.Data2, &mission, &key, &goal, &matched); err != nil {
			return nil, err
		}
		if mission > 3 || key == "" || goal == 0 || goal > math.MaxUint16 {
			return nil, ErrQuestReferenceUnsupported
		}
		if len(pending) == 0 || pending[len(pending)-1].quest.QuestID != quest.QuestID {
			pending = append(pending, pendingQuest{quest: quest})
		}
		entry := &pending[len(pending)-1]
		if int(mission) != entry.count {
			return nil, ErrQuestReferenceUnsupported
		}
		entry.quest.ObjectiveKeys[mission] = key
		entry.goals[mission], entry.matched[mission] = uint16(goal), matched
		entry.count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.count < 2 || entry.count > 4 {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData1, previousStatus := entry.quest.Data1, entry.quest.Status
		updated, changed, updateErr := advanceMultiTargetExtendedQuest(entry.quest, entry.goals, entry.matched, entry.count)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount, updated.ObjectiveKey = byte(entry.count), updated.ObjectiveKeys[0]
		update, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, updated.Status, updated.Data1,
			characterID, updated.QuestID, previousStatus, previousData1)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record multi-target extended kill"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: updated, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func advanceMultiTargetExtendedQuest(quest CharacterQuest, goals [4]uint16, matched [4]bool, count int) (CharacterQuest, bool, error) {
	if count < 2 || count > 4 {
		return CharacterQuest{}, false, ErrQuestReferenceUnsupported
	}
	changed, complete := false, true
	for mission := 0; mission < count; mission++ {
		if goals[mission] == 0 || quest.ObjectiveKeys[mission] == "" {
			return CharacterQuest{}, false, ErrQuestReferenceUnsupported
		}
		shift := uint(mission * 16)
		current := uint16(quest.Data1 >> shift)
		if matched[mission] && current < goals[mission] {
			current++
			quest.Data1 = quest.Data1&^(uint64(math.MaxUint16)<<shift) | uint64(current)<<shift
			changed = true
		}
		if current < goals[mission] {
			complete = false
		}
	}
	if !changed {
		return quest, false, nil
	}
	transition := QuestTransitionProgress
	if complete {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Status = status
	return quest, true, nil
}

func (r *SQLQuestRuntimeRepository) recordSharedKillMission(ctx context.Context, characterID int, mobRefObjectID uint32) ([]QuestProgressResult, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,objective.OBJECTIVE_KEY,
		objective.REQUIRED_COUNT,objective.TARGET_REF_OBJECT_ID=?
		FROM CHAR_QUEST quest JOIN REF_QUEST_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0 AND objective.RUNTIME_ENABLED=1
		  AND (SELECT COUNT(*) FROM REF_QUEST_KILL_OBJECTIVE objective_count
		       WHERE objective_count.QUEST_ID=quest.QUEST_ID AND objective_count.VARIANT_INDEX=0 AND objective_count.RUNTIME_ENABLED=1) BETWEEN 2 AND 3
		  AND EXISTS(SELECT 1 FROM REF_QUEST_KILL_OBJECTIVE matched WHERE matched.QUEST_ID=quest.QUEST_ID
		             AND matched.VARIANT_INDEX=0 AND matched.RUNTIME_ENABLED=1 AND matched.TARGET_REF_OBJECT_ID=?)
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, mobRefObjectID, characterID, QuestActive, QuestProgressed, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	type sharedKillQuest struct {
		quest   CharacterQuest
		goals   [3]uint16
		matched [3]bool
		count   int
	}
	pending := make([]sharedKillQuest, 0)
	for rows.Next() {
		var quest CharacterQuest
		var mission uint32
		var key string
		var goal uint64
		var matched bool
		if err = rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt,
			&quest.Data1, &quest.Data2, &mission, &key, &goal, &matched); err != nil {
			return nil, err
		}
		if mission > 2 || key == "" || goal == 0 || goal > math.MaxUint16 {
			return nil, ErrQuestReferenceUnsupported
		}
		if len(pending) == 0 || pending[len(pending)-1].quest.QuestID != quest.QuestID {
			pending = append(pending, sharedKillQuest{quest: quest})
		}
		entry := &pending[len(pending)-1]
		if int(mission) != entry.count {
			return nil, ErrQuestReferenceUnsupported
		}
		entry.quest.ObjectiveKeys[mission] = key
		entry.goals[mission] = uint16(goal)
		entry.matched[mission] = matched
		entry.count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.count < 2 || entry.count > 3 {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData1, previousData2, previousStatus := entry.quest.Data1, entry.quest.Data2, entry.quest.Status
		updated, changed, updateErr := advanceSharedKillQuest(entry.quest, entry.goals, entry.matched, entry.count)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount = byte(entry.count)
		updated.ObjectiveKey = updated.ObjectiveKeys[0]
		updated.SharedProgress = true
		update, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?,QUEST_DATA_2=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=? AND QUEST_DATA_2=?`, updated.Status, updated.Data1, updated.Data2,
			characterID, updated.QuestID, previousStatus, previousData1, previousData2)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record shared kill mission"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: updated, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func advanceSharedKillQuest(quest CharacterQuest, goals [3]uint16, matched [3]bool, count int) (CharacterQuest, bool, error) {
	if count < 2 || count > 3 {
		return CharacterQuest{}, false, ErrQuestReferenceUnsupported
	}
	changed := false
	for mission := 0; mission < count; mission++ {
		if goals[mission] == 0 || quest.ObjectiveKeys[mission] == "" {
			return CharacterQuest{}, false, ErrQuestReferenceUnsupported
		}
		if !matched[mission] {
			continue
		}
		if quest.Data1 < uint64(goals[mission]) {
			quest.Data1++
			changed = true
		}
		if quest.Data1 >= uint64(goals[mission]) {
			bit := uint64(1) << mission
			if quest.Data2&bit == 0 {
				quest.Data2 |= bit
				changed = true
			}
		}
	}
	if !changed {
		return quest, false, nil
	}
	transition := QuestTransitionProgress
	if quest.Data2&((uint64(1)<<count)-1) == (uint64(1)<<count)-1 {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Status = status
	return quest, true, nil
}

func (r *SQLQuestRuntimeRepository) recordClassifiedMobKill(ctx context.Context, characterID int, mobRefObjectID uint32, rarity MonsterRarity) ([]QuestProgressResult, error) {
	classKey := classifiedMonsterClass(rarity)
	if classKey == "" {
		return nil, nil
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,
		CAST(JSON_UNQUOTE(JSON_EXTRACT(objective.PARAMETERS,'$[8].Number')) AS UNSIGNED)
		FROM CHAR_QUEST quest
		JOIN REF_QUEST_EXTENDED_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		JOIN REF_QUEST_EXTENDED_KILL_TARGET target ON target.QUEST_ID=objective.QUEST_ID
		  AND target.VARIANT_INDEX=objective.VARIANT_INDEX AND target.MISSION_INDEX=objective.MISSION_INDEX
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 AND objective.DECLARATION_KIND='classified'
		  AND objective.TARGET_CLASS=? AND target.TARGET_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, characterID, QuestActive, QuestProgressed, classKey, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	type classifiedMatch struct {
		quest        CharacterQuest
		missionIndex int
		goal         uint16
	}
	matches := make([]classifiedMatch, 0)
	for rows.Next() {
		var match classifiedMatch
		var goal uint64
		if err = rows.Scan(&match.quest.QuestID, &match.quest.Status, &match.quest.AchievementCount, &match.quest.StartedAt,
			&match.quest.CompletedAt, &match.quest.Data1, &match.quest.Data2, &match.missionIndex, &goal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if match.missionIndex < 0 || match.missionIndex >= len(match.quest.ObjectiveKeys) || goal == 0 || goal > math.MaxUint16 {
			_ = rows.Close()
			return nil, ErrQuestReferenceUnsupported
		}
		match.goal = uint16(goal)
		matches = append(matches, match)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(matches))
	for _, match := range matches {
		objectiveRows, queryErr := tx.QueryContext(ctx, `SELECT MISSION_INDEX,OBJECTIVE_KEY,
			CAST(JSON_UNQUOTE(JSON_EXTRACT(PARAMETERS,'$[8].Number')) AS UNSIGNED)
			FROM REF_QUEST_EXTENDED_KILL_OBJECTIVE
			WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND RUNTIME_ENABLED=1 AND DECLARATION_KIND='classified'
			ORDER BY MISSION_INDEX FOR SHARE`, match.quest.QuestID)
		if queryErr != nil {
			return nil, queryErr
		}
		goals := make([]uint16, 0, 3)
		keys := make([]string, 0, 3)
		for objectiveRows.Next() {
			var index int
			var key string
			var goal uint64
			if queryErr = objectiveRows.Scan(&index, &key, &goal); queryErr != nil {
				_ = objectiveRows.Close()
				return nil, queryErr
			}
			if index != len(goals) || index > 2 || key == "" || goal == 0 || goal > math.MaxUint16 {
				_ = objectiveRows.Close()
				return nil, ErrQuestReferenceUnsupported
			}
			goals = append(goals, uint16(goal))
			keys = append(keys, key)
		}
		if queryErr = objectiveRows.Err(); queryErr != nil {
			_ = objectiveRows.Close()
			return nil, queryErr
		}
		if queryErr = objectiveRows.Close(); queryErr != nil {
			return nil, queryErr
		}
		if len(goals) < 2 || len(goals) > 3 || match.missionIndex >= len(goals) || goals[match.missionIndex] != match.goal {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData, previousStatus := match.quest.Data1, match.quest.Status
		updated, changed, updateErr := advanceClassifiedQuest(match.quest, match.missionIndex, goals)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount = byte(len(keys))
		copy(updated.ObjectiveKeys[:], keys)
		updated.ObjectiveKey = keys[0]
		result, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, updated.Status, updated.Data1,
			characterID, updated.QuestID, previousStatus, previousData)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(result, "record classified mob kill"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: updated, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func classifiedMonsterClass(rarity MonsterRarity) string {
	switch rarity.BaseClass() {
	case MonsterNormal:
		return "MONSTER_CLASS_NORMAL"
	case MonsterChampion:
		return "MONSTER_CLASS_CHAMPION"
	case MonsterGiant:
		return "MONSTER_CLASS_GIANT"
	default:
		return ""
	}
}

func advanceClassifiedQuest(quest CharacterQuest, missionIndex int, goals []uint16) (CharacterQuest, bool, error) {
	if missionIndex < 0 || missionIndex >= len(goals) || len(goals) < 2 || len(goals) > 3 || goals[missionIndex] == 0 {
		return CharacterQuest{}, false, ErrQuestStateConflict
	}
	shift := uint(missionIndex * 16)
	current := uint16((quest.Data1 >> shift) & math.MaxUint16)
	if current >= goals[missionIndex] {
		return quest, false, nil
	}
	current++
	quest.Data1 = (quest.Data1 & ^(uint64(math.MaxUint16) << shift)) | uint64(current)<<shift
	complete := true
	for index, goal := range goals {
		if goal == 0 || uint16((quest.Data1>>uint(index*16))&math.MaxUint16) < goal {
			complete = false
			break
		}
	}
	transition := QuestTransitionProgress
	if complete {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Status = status
	return quest, true, nil
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

func (r *SQLQuestRuntimeRepository) recordMultiCollectionProgress(ctx context.Context, characterID int, itemRefObjectID uint32) ([]QuestProgressResult, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,objective.OBJECTIVE_KEY,
		objective.REQUIRED_COUNT,COALESCE((SELECT SUM(item.STACK_COUNT) FROM INVENTORY inventory_item
		  JOIN ITEM item ON item.id=inventory_item.FK_ITEM
		  WHERE inventory_item.FK_CHAR=quest.FK_CHAR AND item.FK_REF_ITEM=objective.ITEM_REF_OBJECT_ID),0)
		FROM CHAR_QUEST quest JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 AND objective.DECLARATION_KIND='standard'
		  AND (SELECT COUNT(*) FROM REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective_count
		       WHERE objective_count.QUEST_ID=objective.QUEST_ID AND objective_count.VARIANT_INDEX=0 AND objective_count.RUNTIME_ENABLED=1) BETWEEN 2 AND 4
		  AND EXISTS(SELECT 1 FROM REF_QUEST_COLLECTION_ITEM_OBJECTIVE matched
		             WHERE matched.QUEST_ID=quest.QUEST_ID AND matched.VARIANT_INDEX=0
		               AND matched.RUNTIME_ENABLED=1 AND matched.DECLARATION_KIND='standard'
		               AND matched.ITEM_REF_OBJECT_ID=?)
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, characterID, QuestActive, QuestProgressed, itemRefObjectID)
	if err != nil {
		return nil, err
	}
	type multiCollection struct {
		quest  CharacterQuest
		goals  [4]uint16
		counts [4]uint16
		count  int
	}
	pending := make([]multiCollection, 0)
	for rows.Next() {
		var quest CharacterQuest
		var mission uint32
		var key string
		var goal, count uint64
		if err = rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt,
			&quest.Data1, &quest.Data2, &mission, &key, &goal, &count); err != nil {
			return nil, err
		}
		if mission > 3 || key == "" || goal == 0 || goal > math.MaxUint16 {
			return nil, ErrQuestReferenceUnsupported
		}
		if count > goal {
			count = goal
		}
		if len(pending) == 0 || pending[len(pending)-1].quest.QuestID != quest.QuestID {
			pending = append(pending, multiCollection{quest: quest})
		}
		entry := &pending[len(pending)-1]
		if int(mission) != entry.count {
			return nil, ErrQuestReferenceUnsupported
		}
		entry.quest.ObjectiveKeys[mission] = key
		entry.goals[mission] = uint16(goal)
		entry.counts[mission] = uint16(count)
		entry.count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.count < 2 || entry.count > 4 {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData, previousStatus := entry.quest.Data1, entry.quest.Status
		updated, changed, updateErr := applyMultiCollectionSnapshot(entry.quest, entry.goals, entry.counts, entry.count)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount = byte(entry.count)
		updated.ObjectiveKey = updated.ObjectiveKeys[0]
		update, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, updated.Status, updated.Data1,
			characterID, updated.QuestID, previousStatus, previousData)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record multi collection item"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: updated, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func applyMultiCollectionSnapshot(quest CharacterQuest, goals, counts [4]uint16, count int) (CharacterQuest, bool, error) {
	if count < 2 || count > 4 {
		return CharacterQuest{}, false, ErrQuestReferenceUnsupported
	}
	data := uint64(0)
	complete := true
	for mission := 0; mission < count; mission++ {
		if goals[mission] == 0 || quest.ObjectiveKeys[mission] == "" {
			return CharacterQuest{}, false, ErrQuestReferenceUnsupported
		}
		if counts[mission] > goals[mission] {
			counts[mission] = goals[mission]
		}
		data |= uint64(counts[mission]) << (16 * mission)
		complete = complete && counts[mission] >= goals[mission]
	}
	if data == quest.Data1 && (!complete || quest.Status == QuestRewardReady) {
		return quest, false, nil
	}
	transition := QuestTransitionProgress
	if complete {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Data1, quest.Status = data, status
	return quest, true, nil
}

func (r *SQLQuestRuntimeRepository) recordExtendedCollectionProgress(ctx context.Context, characterID int, itemRefObjectID uint32) ([]QuestProgressResult, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,objective.OBJECTIVE_KEY,
		objective.REQUIRED_COUNT,COALESCE((SELECT SUM(item.STACK_COUNT) FROM INVENTORY inventory_item
		  JOIN ITEM item ON item.id=inventory_item.FK_ITEM
		  WHERE inventory_item.FK_CHAR=quest.FK_CHAR AND item.FK_REF_ITEM=objective.ITEM_REF_OBJECT_ID),0)
		FROM CHAR_QUEST quest JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 AND objective.DECLARATION_KIND='extended'
		  AND EXISTS(SELECT 1 FROM REF_QUEST_COLLECTION_ITEM_OBJECTIVE matched
		             WHERE matched.QUEST_ID=quest.QUEST_ID AND matched.VARIANT_INDEX=0
		               AND matched.RUNTIME_ENABLED=1 AND matched.DECLARATION_KIND='extended'
		               AND matched.ITEM_REF_OBJECT_ID=?)
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, characterID, QuestActive, QuestProgressed, itemRefObjectID)
	if err != nil {
		return nil, err
	}
	type extendedCollection struct {
		quest  CharacterQuest
		goals  [3]uint16
		counts [3]uint16
		count  int
	}
	pending := make([]extendedCollection, 0)
	for rows.Next() {
		var quest CharacterQuest
		var mission uint32
		var key string
		var goal, count uint64
		if err = rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt,
			&quest.Data1, &quest.Data2, &mission, &key, &goal, &count); err != nil {
			return nil, err
		}
		if mission > 2 || goal == 0 || goal > math.MaxUint16 {
			return nil, ErrQuestReferenceUnsupported
		}
		if count > goal {
			count = goal
		}
		if len(pending) == 0 || pending[len(pending)-1].quest.QuestID != quest.QuestID {
			pending = append(pending, extendedCollection{quest: quest})
		}
		entry := &pending[len(pending)-1]
		if int(mission) != entry.count {
			return nil, ErrQuestReferenceUnsupported
		}
		entry.quest.ObjectiveKeys[mission] = key
		entry.goals[mission] = uint16(goal)
		entry.counts[mission] = uint16(count)
		entry.count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.count != 2 {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData1, previousData2, previousStatus := entry.quest.Data1, entry.quest.Data2, entry.quest.Status
		updated, changed, updateErr := applyExtendedCollectionSnapshot(entry.quest, entry.goals, entry.counts)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount = 1
		updated.ObjectiveKey = entry.quest.ObjectiveKeys[0]
		updated.ObjectiveKeys = [4]string{}
		update, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?,QUEST_DATA_2=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=? AND QUEST_DATA_2=?`, updated.Status, updated.Data1, updated.Data2,
			characterID, updated.QuestID, previousStatus, previousData1, previousData2)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record extended collection item"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: updated, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
}

func applyExtendedCollectionSnapshot(quest CharacterQuest, goals, counts [3]uint16) (CharacterQuest, bool, error) {
	if goals[0] == 0 || goals[1] == 0 {
		return CharacterQuest{}, false, ErrQuestReferenceUnsupported
	}
	data1, data2 := uint64(counts[0]), uint64(counts[1])
	complete := counts[0] >= goals[0] && counts[1] >= goals[1]
	if data1 == quest.Data1 && data2 == quest.Data2 && (!complete || quest.Status == QuestRewardReady) {
		return quest, false, nil
	}
	transition := QuestTransitionProgress
	if complete {
		transition = QuestTransitionRewardReady
	}
	status, err := TransitionQuest(quest.Status, transition)
	if err != nil {
		return CharacterQuest{}, false, err
	}
	quest.Data1, quest.Data2, quest.Status = data1, data2, status
	return quest, true, nil
}

// RecordPlayerKill reproduces the authored MKP_LEVEL/MKP_JOB/MKP_REGION
// predicate used by CMissionKillPlayer before its counter increment.
const recordPlayerKillQuery = `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.PARAMETER_6,objective.OBJECTIVE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_KILL_PLAYER_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1
		  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0
		  AND objective.LEVEL_FILTER_KEY='MKP_LEVEL' AND ?>=objective.PARAMETER_12
		  AND objective.JOB_FILTER_KEY='MKP_JOB'
		  AND EXISTS (SELECT 1 FROM REF_QUEST_KILL_PLAYER_JOB_STATE job_state
		              WHERE job_state.QUEST_ID=objective.QUEST_ID AND job_state.VARIANT_INDEX=objective.VARIANT_INDEX
		                AND job_state.MISSION_INDEX=objective.MISSION_INDEX AND job_state.JOB_STATE_KEY=?)
		  AND objective.REGION_FILTER_KEY='MKP_REGION' AND objective.REGION_VALUE=?
		ORDER BY quest.QUEST_ID FOR UPDATE`

func (r *SQLQuestRuntimeRepository) RecordPlayerKill(ctx context.Context, characterID int, target QuestPlayerKillTarget) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || target.Level <= 0 || target.Level > math.MaxUint8 ||
		target.JobType < PlayerJobTrader || target.JobType > PlayerJobHunter {
		return nil, ErrQuestStateConflict
	}
	jobState := ""
	switch target.JobType {
	case PlayerJobTrader:
		jobState = "OBJ_JOBSTATE_TRADER"
	case PlayerJobThief:
		jobState = "OBJ_JOBSTATE_ROBBER"
	case PlayerJobHunter:
		jobState = "OBJ_JOBSTATE_HUNTER"
	}
	return r.recordObjectiveProgress(ctx, characterID, 1, recordPlayerKillQuery,
		characterID, QuestActive, QuestProgressed, target.Level, jobState, target.RegionID)
}

// CollectionDropsForKill resolves only active, runtime-enabled collection
// missions that have not reached their authored item count. The legacy
// CMissionGatherItemFromMonster path performs this qualification before it
// asks CGObjPC to materialize the quest item.
const collectionDropsForKillQuery = `SELECT quest.QUEST_ID,objective.ITEM_REF_OBJECT_ID,objective.PARAMETER_6,source.SOURCE_PARAMETER
		FROM CHAR_QUEST quest
		JOIN REF_QUEST_COLLECTION_ITEM_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		  AND objective.VARIANT_INDEX=0 AND objective.RUNTIME_ENABLED=1
		JOIN REF_QUEST_COLLECTION_ITEM_SOURCE source ON source.QUEST_ID=objective.QUEST_ID
		  AND source.VARIANT_INDEX=objective.VARIANT_INDEX AND source.MISSION_INDEX=objective.MISSION_INDEX
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?)
		  AND (CASE objective.DECLARATION_KIND
		       WHEN 'standard' THEN ((quest.QUEST_DATA_1 >> (16*objective.MISSION_INDEX)) & 65535)
		       WHEN 'extended' THEN CASE objective.MISSION_INDEX WHEN 0 THEN quest.QUEST_DATA_1 WHEN 1 THEN quest.QUEST_DATA_2 ELSE objective.REQUIRED_COUNT END
		       ELSE objective.REQUIRED_COUNT END)<objective.REQUIRED_COUNT
		  AND source.MONSTER_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX,source.SOURCE_INDEX`

func (r *SQLQuestRuntimeRepository) CollectionDropsForKill(ctx context.Context, characterID int, monsterRefObjectID uint32) ([]QuestCollectionDrop, error) {
	if r == nil || r.shard == nil || characterID <= 0 || monsterRefObjectID == 0 {
		return nil, ErrQuestStateConflict
	}
	rows, err := r.shard.QueryContext(normalizeEconomyContext(ctx), collectionDropsForKillQuery,
		characterID, QuestActive, QuestProgressed, monsterRefObjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]QuestCollectionDrop, 0)
	for rows.Next() {
		var entry QuestCollectionDrop
		var stack uint32
		if err = rows.Scan(&entry.QuestID, &entry.ReferenceItemID, &stack, &entry.ChancePercent); err != nil {
			return nil, err
		}
		if entry.ReferenceItemID == 0 || stack == 0 || stack > math.MaxUint16 || entry.ChancePercent <= 0 || math.IsNaN(entry.ChancePercent) || math.IsInf(entry.ChancePercent, 0) {
			continue
		}
		entry.StackCount = uint16(stack)
		result = append(result, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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

const listDeliveriesAtNPCQuery = `SELECT quest.QUEST_ID,objective.DIALOGUE_KEY
		FROM CHAR_QUEST quest JOIN REF_QUEST_DELIVER_ITEM objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.RUNTIME_ENABLED=1
		  AND objective.VARIANT_INDEX=0 AND objective.MISSION_INDEX=0
		  AND objective.TARGET_NPC_REF_OBJECT_ID=?
		  AND NOT EXISTS (
		    SELECT 1 FROM REF_QUEST_DELIVER_ITEM_ENTRY entry_item
		    WHERE entry_item.QUEST_ID=objective.QUEST_ID
		      AND entry_item.VARIANT_INDEX=objective.VARIANT_INDEX AND entry_item.MISSION_INDEX=objective.MISSION_INDEX
		    GROUP BY entry_item.ITEM_REF_OBJECT_ID
		    HAVING (SELECT COALESCE(SUM(item.STACK_COUNT),0) FROM INVENTORY inventory
		           JOIN ITEM item ON item.ID=inventory.FK_ITEM
		           WHERE inventory.FK_CHAR=quest.FK_CHAR AND item.FK_REF_ITEM=entry_item.ITEM_REF_OBJECT_ID) < SUM(entry_item.QUANTITY))
		ORDER BY quest.QUEST_ID`

// ListDeliveriesAtNPC returns delivery conversations whose complete authored
// item set is currently present. Confirmation revalidates the same predicate
// under locks before consuming anything.
func (r *SQLQuestRuntimeRepository) ListDeliveriesAtNPC(ctx context.Context, characterID int, npcRefObjectID uint32) ([]QuestDeliveryPrompt, error) {
	if r == nil || r.shard == nil || characterID <= 0 || npcRefObjectID == 0 {
		return nil, ErrQuestStateConflict
	}
	rows, err := r.shard.QueryContext(normalizeEconomyContext(ctx), listDeliveriesAtNPCQuery,
		characterID, QuestActive, QuestProgressed, npcRefObjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prompts := make([]QuestDeliveryPrompt, 0)
	for rows.Next() {
		var prompt QuestDeliveryPrompt
		if err = rows.Scan(&prompt.QuestID, &prompt.DialogueKey); err != nil {
			return nil, err
		}
		if prompt.QuestID <= 0 || prompt.DialogueKey == "" {
			return nil, ErrQuestReferenceMissing
		}
		prompts = append(prompts, prompt)
	}
	return prompts, rows.Err()
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
	err := tx.QueryRowContext(ctx, "SELECT c.`GOLD`,c.`CURRENT_LEVEL`,c.`EXP`,c.`SKILL_EXP`,c.`SKILL_POINTS`,c.`STAT_POINTS`,c.`INVENTORY_SIZE`,r.`Country` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` WHERE c.`id`=? FOR UPDATE").Scan(&state.gold, &state.level, &state.experience, &state.skillExperience, &state.skillPoints, &state.statPoints, &state.inventorySize, &state.country)
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

func lockNextQuestAchievementCount(ctx context.Context, tx *sql.Tx, quest CharacterQuest) (uint16, error) {
	if tx == nil || quest.QuestID <= 0 || quest.AchievementCount == math.MaxUint16 {
		return 0, ErrQuestStateConflict
	}
	var limit sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT ACHIEVEMENT_LIMIT FROM REF_QUEST_COMPLETION_POLICY
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 FOR SHARE`, quest.QuestID).Scan(&limit); err != nil {
		return 0, err
	}
	return nextQuestAchievementCount(quest.AchievementCount, limit)
}

func nextQuestAchievementCount(current uint16, limit sql.NullInt64) (uint16, error) {
	if current == math.MaxUint16 {
		return 0, ErrQuestStateConflict
	}
	next := uint64(current) + 1
	if limit.Valid && (limit.Int64 <= 0 || next > uint64(limit.Int64)) {
		return 0, ErrQuestStateConflict
	}
	return uint16(next), nil
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
