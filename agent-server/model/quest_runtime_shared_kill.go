package model

import (
	"context"
	"errors"
	"math"
)

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
