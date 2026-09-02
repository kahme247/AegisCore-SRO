package model

import (
	"context"
	"errors"
	"fmt"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var ErrQuestOfferRepositoryUnavailable = errors.New("quest offer repository is unavailable")

type QuestOffer struct {
	QuestID        int
	CodeName       string
	NameKey        string
	PayKey         string
	PayContentsKey string
	ObjectiveKey   string
	RequiredCount  uint32
	TaskCount      byte
	SelectionCount byte
}

type QuestOfferRepository interface {
	ListAvailable(context.Context, int, uint32) ([]QuestOffer, error)
}

type SQLQuestOfferRepository struct {
	shard frameworkdb.Queryer
}

func NewQuestOfferRepository(shard frameworkdb.Queryer) (*SQLQuestOfferRepository, error) {
	if shard == nil {
		return nil, ErrQuestOfferRepositoryUnavailable
	}
	return &SQLQuestOfferRepository{shard: shard}, nil
}

// selectAvailableQuestOffers deliberately admits only the two objective
// shapes whose active-record task arrays are currently understood. Every
// unsupported qualification or reward mechanism fails closed.
const selectAvailableQuestOffers = `SELECT quest.ID,quest.CODE_NAME,quest.NAME_STRING,quest.PAY_STRING,quest.PAY_CONTENTS,
    COALESCE(kill_objective.OBJECTIVE_KEY,deliver.OBJECTIVE_KEY,standard_deliver.OBJECTIVE_KEY),COALESCE(kill_objective.REQUIRED_COUNT,0),
    CASE WHEN kill_objective.QUEST_ID IS NULL THEN 0 ELSE 1 END,
    reward.SELECTION_COUNT
FROM SRO_SHARD.REF_QUEST quest
JOIN SRO_SHARD.REF_QUEST_START_CONDITION start_condition
  ON start_condition.QUEST_ID=quest.ID AND start_condition.VARIANT_INDEX=0
JOIN SRO_SHARD.REF_QUEST_START_METHOD start_method
  ON start_method.QUEST_ID=quest.ID AND start_method.VARIANT_INDEX=0 AND start_method.METHOD_KEY='QM_CONVERSATION'
JOIN SRO_SHARD.REF_QUEST_START_NPC start_npc
  ON start_npc.QUEST_ID=start_method.QUEST_ID AND start_npc.VARIANT_INDEX=start_method.VARIANT_INDEX
JOIN SRO_SHARD.REF_QUEST_COMPLETION_POLICY policy
  ON policy.QUEST_ID=quest.ID AND policy.VARIANT_INDEX=0
JOIN SRO_SHARD.REF_QUEST_SCRIPT_REWARD script_reward
  ON script_reward.QUEST_ID=quest.ID AND script_reward.VARIANT_INDEX=0 AND script_reward.RUNTIME_ENABLED=1
JOIN SRO_SHARD.REF_QUEST_REWARD reward ON reward.QUEST_ID=quest.ID AND reward.SERVICE=1
JOIN SRO_SHARD.CHAR character_state ON character_state.id=?
LEFT JOIN SRO_SHARD.CHAR_QUEST current_quest
  ON current_quest.FK_CHAR=character_state.id AND current_quest.QUEST_ID=quest.ID
LEFT JOIN SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_objective
  ON kill_objective.QUEST_ID=quest.ID AND kill_objective.VARIANT_INDEX=0 AND kill_objective.MISSION_INDEX=0 AND kill_objective.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM_EX deliver
  ON deliver.QUEST_ID=quest.ID AND deliver.VARIANT_INDEX=0 AND deliver.MISSION_INDEX=0 AND deliver.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM standard_deliver
  ON standard_deliver.QUEST_ID=quest.ID AND standard_deliver.VARIANT_INDEX=0 AND standard_deliver.MISSION_INDEX=0 AND standard_deliver.RUNTIME_ENABLED=1
WHERE quest.SERVICE=1 AND start_npc.NPC_REF_OBJECT_ID=?
  AND (current_quest.STATUS IN (1,6,7,8) OR
       character_state.CURRENT_LEVEL BETWEEN start_condition.LEVEL_LOWER AND start_condition.LEVEL_UPPER)
  AND start_condition.SYSTEM_REQUIRED=0
  AND policy.REAL_TIME=0 AND policy.DAY_LIMIT IS NULL AND policy.DAY_OR_NIGHT IS NULL
  AND policy.LIMIT_TIME IS NULL AND policy.HWAN_LEVEL IS NULL
  AND reward.IS_BASIC_REWARD=1 AND reward.IS_CHECK_CONDITION=0 AND reward.IS_CHECK_COUNTRY=0
  AND reward.IS_CHECK_CLASS=0 AND reward.IS_CHECK_GENDER=0 AND reward.SKILL_POINTS=0
  AND reward.ACADEMY_POINTS=0 AND reward.HWAN=0 AND reward.INVENTORY_SLOTS=0
  AND reward.ITEM_REWARD_TYPE IN (0,1)
  AND (reward.SELECTION_COUNT=0 OR (reward.IS_ITEM_REWARD=1 AND reward.SELECTION_COUNT<=script_reward.ITEM_COUNT))
  AND reward.PARAM_1=0 AND reward.PARAM_2=0 AND reward.PARAM_3=0 AND script_reward.ITEM_COUNT<=1
  AND NOT EXISTS (SELECT 1 FROM SRO_SHARD.REF_QUEST_SCRIPT_POINT_REWARD point_reward
                  WHERE point_reward.QUEST_ID=quest.ID AND point_reward.VARIANT_INDEX=0
                    AND (point_reward.REWARD_KIND<>'skill_point' OR point_reward.AMOUNT<>0))
  AND NOT EXISTS (SELECT 1 FROM SRO_SHARD.REF_QUEST_JOB_START_CONDITION job_condition
                  WHERE job_condition.QUEST_ID=quest.ID AND job_condition.VARIANT_INDEX=0)
  AND NOT EXISTS (SELECT 1 FROM SRO_SHARD.REF_QUEST_RACE_START_CONDITION race_condition
                  WHERE race_condition.QUEST_ID=quest.ID AND race_condition.VARIANT_INDEX=0)
  AND ((kill_objective.QUEST_ID IS NOT NULL) + (deliver.QUEST_ID IS NOT NULL) +
       (standard_deliver.QUEST_ID IS NOT NULL)) = 1
  AND (current_quest.QUEST_ID IS NULL OR current_quest.STATUS IN (1,4,6,7,8))
  AND (current_quest.STATUS IN (1,6,7,8) OR policy.ACHIEVEMENT_LIMIT IS NULL OR current_quest.QUEST_ID IS NULL OR
       current_quest.ACHIEVEMENT_COUNT < policy.ACHIEVEMENT_LIMIT)
  AND NOT EXISTS (
    SELECT 1 FROM SRO_SHARD.REF_QUEST_DEPENDENCY_GROUP dependency_group
    WHERE dependency_group.QUEST_ID=quest.ID AND dependency_group.VARIANT_INDEX=0 AND (
      (dependency_group.DEPENDENCY_MODE='all_clear' AND EXISTS (
        SELECT 1 FROM SRO_SHARD.REF_QUEST_DEPENDENCY_ENTRY dependency_entry
        LEFT JOIN SRO_SHARD.CHAR_QUEST dependency_state
          ON dependency_state.FK_CHAR=character_state.id
         AND dependency_state.QUEST_ID=dependency_entry.DEPENDENCY_QUEST_ID
         AND dependency_state.STATUS = 5
        WHERE dependency_entry.QUEST_ID=dependency_group.QUEST_ID
          AND dependency_entry.VARIANT_INDEX=dependency_group.VARIANT_INDEX
          AND dependency_entry.GROUP_INDEX=dependency_group.GROUP_INDEX
          AND dependency_state.QUEST_ID IS NULL
      )) OR
      (dependency_group.DEPENDENCY_MODE='one_clear' AND NOT EXISTS (
        SELECT 1 FROM SRO_SHARD.REF_QUEST_DEPENDENCY_ENTRY dependency_entry
        JOIN SRO_SHARD.CHAR_QUEST dependency_state
          ON dependency_state.FK_CHAR=character_state.id
         AND dependency_state.QUEST_ID=dependency_entry.DEPENDENCY_QUEST_ID
         AND dependency_state.STATUS = 5
        WHERE dependency_entry.QUEST_ID=dependency_group.QUEST_ID
          AND dependency_entry.VARIANT_INDEX=dependency_group.VARIANT_INDEX
          AND dependency_entry.GROUP_INDEX=dependency_group.GROUP_INDEX
      ))
    )
  )
ORDER BY quest.LEVEL,quest.ID`

func (r *SQLQuestOfferRepository) ListAvailable(ctx context.Context, characterID int, npcRefObjectID uint32) ([]QuestOffer, error) {
	if r == nil || r.shard == nil {
		return nil, ErrQuestOfferRepositoryUnavailable
	}
	if characterID <= 0 || npcRefObjectID == 0 {
		return nil, fmt.Errorf("invalid quest offer identity")
	}
	rows, err := r.shard.QueryContext(ctx, selectAvailableQuestOffers, characterID, npcRefObjectID)
	if err != nil {
		return nil, fmt.Errorf("list available quest offers: %w", err)
	}
	defer rows.Close()
	offers := make([]QuestOffer, 0)
	for rows.Next() {
		var offer QuestOffer
		if err = rows.Scan(&offer.QuestID, &offer.CodeName, &offer.NameKey, &offer.PayKey, &offer.PayContentsKey,
			&offer.ObjectiveKey, &offer.RequiredCount, &offer.TaskCount, &offer.SelectionCount); err != nil {
			return nil, fmt.Errorf("scan available quest offer: %w", err)
		}
		if offer.QuestID <= 0 || offer.CodeName == "" || offer.NameKey == "" || offer.ObjectiveKey == "" || offer.TaskCount > 1 {
			return nil, fmt.Errorf("quest offer %d has invalid protocol metadata", offer.QuestID)
		}
		offers = append(offers, offer)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate available quest offers: %w", err)
	}
	return offers, nil
}
