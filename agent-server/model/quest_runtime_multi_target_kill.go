package model

import (
	"context"
	"errors"
	"math"
)

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
