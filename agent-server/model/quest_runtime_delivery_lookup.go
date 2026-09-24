package model

import "context"

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
