package model

import (
	"context"
	"errors"
)

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
