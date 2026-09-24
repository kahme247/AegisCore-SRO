package model

import "context"

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
