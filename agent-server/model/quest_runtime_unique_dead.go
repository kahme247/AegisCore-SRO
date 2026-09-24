package model

import (
	"context"
	"database/sql"
	"errors"
)

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
