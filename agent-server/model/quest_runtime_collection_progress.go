package model

import (
	"context"
	"errors"
	"math"
)

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
