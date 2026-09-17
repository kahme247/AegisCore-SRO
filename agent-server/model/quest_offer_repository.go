package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	ObjectiveKeys  []string
	RequiredCount  uint32
	TaskCount      byte
	SelectionCount byte
	TimerDuration  uint32
	DayLimit       uint32
	DayOrNight     uint32
	SharedProgress bool
	InstantReady   bool
	DeliveryItems  bool
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

// selectAvailableQuestOffers deliberately admits only objective
// shapes whose active-record task arrays are currently understood. Every
// unsupported qualification or reward mechanism fails closed.
const selectAvailableQuestOffers = `SELECT quest.ID,quest.CODE_NAME,quest.NAME_STRING,quest.PAY_STRING,quest.PAY_CONTENTS,
	COALESCE(CASE WHEN kill_objective.QUEST_ID IS NOT NULL AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(kill_all.OBJECTIVE_KEY ORDER BY kill_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1) ELSE kill_objective.OBJECTIVE_KEY END,deliver.OBJECTIVE_KEY,standard_deliver.OBJECTIVE_KEY,dialog.OBJECTIVE_KEY,kill_player.OBJECTIVE_KEY,
	  CASE WHEN collection.QUEST_ID IS NOT NULL AND collection.DECLARATION_KIND='standard' AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(collection_all.OBJECTIVE_KEY ORDER BY collection_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1) ELSE collection.OBJECTIVE_KEY END,
	  CASE WHEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(extended_all.OBJECTIVE_KEY ORDER BY extended_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1) ELSE extended_kill.OBJECTIVE_KEY END,
	  talisman.OBJECTIVE_KEY,CASE WHEN quest.CODE_NAME='QNO_WC_FERRY4_1' THEN '' END),
	COALESCE(kill_objective.REQUIRED_COUNT,kill_player.PARAMETER_6,collection.REQUIRED_COUNT,
	  CAST(JSON_UNQUOTE(JSON_EXTRACT(extended_kill.PARAMETERS,CASE WHEN extended_kill.DECLARATION_KIND='classified' THEN '$[8].Number' ELSE CONCAT('$[',extended_kill.TARGET_COUNT+6,'].Number') END)) AS UNSIGNED),0),
	CASE WHEN kill_objective.QUEST_ID IS NOT NULL THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)
	     WHEN collection.QUEST_ID IS NOT NULL AND collection.DECLARATION_KIND='standard' THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1)
	     WHEN extended_kill.QUEST_ID IS NOT NULL THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1)
	     WHEN kill_objective.QUEST_ID IS NULL AND dialog.QUEST_ID IS NULL AND kill_player.QUEST_ID IS NULL AND collection.QUEST_ID IS NULL AND extended_kill.QUEST_ID IS NULL AND talisman.QUEST_ID IS NULL THEN 0 ELSE 1 END,
	CASE WHEN reward.SELECTION_COUNT=0 AND reward.IS_CHECK_CONDITION=1 AND reward.IS_CHECK_COUNTRY=0
	          AND reward.IS_CHECK_CLASS=0 AND reward.IS_CHECK_GENDER=0 AND reward.ITEM_REWARD_TYPE=1
	          AND script_reward.ITEM_COUNT>1 THEN 1 ELSE reward.SELECTION_COUNT END,
	COALESCE(policy.LIMIT_TIME,0),COALESCE(policy.DAY_LIMIT,0),COALESCE(policy.DAY_OR_NIGHT,0),
	CASE WHEN kill_objective.QUEST_ID IS NOT NULL AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)>1 THEN 1 ELSE 0 END,
	quest.CODE_NAME='QNO_WC_FERRY4_1',standard_deliver.QUEST_ID IS NOT NULL
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
JOIN SRO_SHARD.CHAR_REF_DATA character_ref ON character_ref.RefObjID=character_state.REF_OBJ_ID
LEFT JOIN SRO_SHARD.CHAR_QUEST current_quest
  ON current_quest.FK_CHAR=character_state.id AND current_quest.QUEST_ID=quest.ID
LEFT JOIN SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_objective
  ON kill_objective.QUEST_ID=quest.ID AND kill_objective.VARIANT_INDEX=0 AND kill_objective.MISSION_INDEX=0 AND kill_objective.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM_EX deliver
  ON deliver.QUEST_ID=quest.ID AND deliver.VARIANT_INDEX=0 AND deliver.MISSION_INDEX=0 AND deliver.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM standard_deliver
  ON standard_deliver.QUEST_ID=quest.ID AND standard_deliver.VARIANT_INDEX=0 AND standard_deliver.MISSION_INDEX=0 AND standard_deliver.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_DIALOG_OBJECTIVE dialog
  ON dialog.QUEST_ID=quest.ID AND dialog.VARIANT_INDEX=0 AND dialog.MISSION_INDEX=0 AND dialog.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_KILL_PLAYER_OBJECTIVE kill_player
  ON kill_player.QUEST_ID=quest.ID AND kill_player.VARIANT_INDEX=0 AND kill_player.MISSION_INDEX=0 AND kill_player.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection
  ON collection.QUEST_ID=quest.ID AND collection.VARIANT_INDEX=0 AND collection.MISSION_INDEX=0 AND collection.RUNTIME_ENABLED=1
LEFT JOIN SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_kill
  ON extended_kill.QUEST_ID=quest.ID AND extended_kill.VARIANT_INDEX=0 AND extended_kill.MISSION_INDEX=0
  AND extended_kill.RUNTIME_ENABLED=1 AND extended_kill.DECLARATION_KIND IN ('multi_target','classified')
LEFT JOIN SRO_SHARD.REF_QUEST_TALISMAN_OBJECTIVE talisman
  ON talisman.QUEST_ID=quest.ID AND talisman.VARIANT_INDEX=0 AND talisman.MISSION_INDEX=0 AND talisman.RUNTIME_ENABLED=1
WHERE quest.SERVICE=1 AND start_npc.NPC_REF_OBJECT_ID=?
	AND (dialog.QUEST_ID IS NULL OR current_quest.QUEST_ID IS NOT NULL OR dialog.TARGET_NPC_REF_OBJECT_ID<>start_npc.NPC_REF_OBJECT_ID)
	AND (standard_deliver.QUEST_ID IS NULL OR current_quest.STATUS IN (1,7,8) OR standard_deliver.TARGET_NPC_REF_OBJECT_ID<>start_npc.NPC_REF_OBJECT_ID)
	AND (current_quest.STATUS IN (1,6,7,8) OR
       character_state.CURRENT_LEVEL >= start_condition.LEVEL_LOWER)
	AND ((start_condition.SYSTEM_REQUIRED=0 AND NOT EXISTS (
	       SELECT 1 FROM SRO_SHARD.REF_QUEST_JOB_START_CONDITION job_condition
	       WHERE job_condition.QUEST_ID=quest.ID AND job_condition.VARIANT_INDEX=0))
	     OR (start_condition.SYSTEM_REQUIRED=1 AND EXISTS (
	       SELECT 1 FROM SRO_SHARD.REF_QUEST_JOB_START_CONDITION job_condition
	       JOIN SRO_SHARD.CHARACTER_JOB character_job ON character_job.FK_CHAR=character_state.id
	       WHERE job_condition.QUEST_ID=quest.ID AND job_condition.VARIANT_INDEX=0
	         AND job_condition.RUNTIME_ENABLED=1
	         AND character_job.JOB_TYPE=CASE job_condition.JOB_STATE_KEY
	           WHEN 'OBJ_JOBSTATE_TRADER' THEN 1 WHEN 'OBJ_JOBSTATE_ROBBER' THEN 2
	           WHEN 'OBJ_JOBSTATE_HUNTER' THEN 3 ELSE 0 END)))
  AND policy.RUNTIME_ENABLED=1
  AND ((policy.DAY_LIMIT IS NULL AND policy.DAY_OR_NIGHT IS NULL) OR
	   (quest.CODE_NAME='QNO_KT_SOLDIER_WE2_1' AND policy.DAY_LIMIT=20 AND policy.DAY_OR_NIGHT=1) OR
	   (quest.CODE_NAME='QNO_WC_GENARAL_SW_1' AND policy.DAY_LIMIT=5 AND policy.DAY_OR_NIGHT=2))
  AND (policy.LIMIT_TIME IS NULL OR (policy.LIMIT_TIME=120 AND quest.CODE_NAME IN ('QNO_KT_ACCESSORY_1','QNO_WC_SMITH_1')))
  AND (policy.HWAN_LEVEL IS NULL OR policy.HWAN_LEVEL=character_state.HWAN_LEVEL)
	AND reward.IS_BASIC_REWARD=1 AND reward.IS_CHECK_CLASS=0 AND reward.IS_CHECK_GENDER=0
	AND (reward.IS_CHECK_CONDITION=0 OR (reward.IS_CHECK_COUNTRY=1 AND reward.SELECTION_COUNT>0)
	     OR (reward.IS_CHECK_COUNTRY=0 AND reward.ITEM_REWARD_TYPE=1 AND reward.SELECTION_COUNT=0 AND script_reward.ITEM_COUNT>1))
	AND (reward.ACADEMY_POINTS=0 OR EXISTS (
	  SELECT 1 FROM SRO_SHARD.REF_QUEST_SCRIPT_POINT_REWARD academy_point
	  WHERE academy_point.QUEST_ID=quest.ID AND academy_point.VARIANT_INDEX=0
	    AND academy_point.REWARD_KIND='named_point' AND academy_point.RUNTIME_ENABLED=1
	    AND academy_point.AMOUNT=reward.ACADEMY_POINTS
	    AND academy_point.POINT_KEY IN ('TEMPLE_ANUBIS','TEMPLE_HAROERIS')))
  AND reward.ITEM_REWARD_TYPE IN (0,1)
  AND (reward.SELECTION_COUNT=0 OR (reward.IS_ITEM_REWARD=1 AND reward.SELECTION_COUNT<=script_reward.ITEM_COUNT))
	AND reward.PARAM_1=0 AND reward.PARAM_2=0 AND reward.PARAM_3=0
  AND NOT EXISTS (SELECT 1 FROM SRO_SHARD.REF_QUEST_SCRIPT_POINT_REWARD point_reward
                  WHERE point_reward.QUEST_ID=quest.ID AND point_reward.VARIANT_INDEX=0
					AND (point_reward.RUNTIME_ENABLED<>1 OR point_reward.REWARD_KIND NOT IN ('skill_point','named_point')
					  OR (point_reward.REWARD_KIND='named_point' AND point_reward.POINT_KEY NOT IN ('TEMPLE_ANUBIS','TEMPLE_HAROERIS'))))
  AND NOT EXISTS (SELECT 1 FROM SRO_SHARD.REF_QUEST_RACE_START_CONDITION race_condition
				  WHERE race_condition.QUEST_ID=quest.ID AND race_condition.VARIANT_INDEX=0
				    AND (race_condition.RUNTIME_ENABLED<>1 OR character_ref.Country<>CASE race_condition.COUNTRY_KEY
				      WHEN 'COUNTRY_CHINA' THEN 0 WHEN 'COUNTRY_EUROPE' THEN 1 ELSE 255 END))
	AND ((kill_objective.QUEST_ID IS NOT NULL) + (deliver.QUEST_ID IS NOT NULL) +
	       (standard_deliver.QUEST_ID IS NOT NULL) + (dialog.QUEST_ID IS NOT NULL) + (kill_player.QUEST_ID IS NOT NULL) + (collection.QUEST_ID IS NOT NULL) +
	       (extended_kill.QUEST_ID IS NOT NULL) + (talisman.QUEST_ID IS NOT NULL) + (quest.CODE_NAME='QNO_WC_FERRY4_1')) = 1
  AND (current_quest.QUEST_ID IS NULL OR current_quest.STATUS IN (1,4,6,7,8))
  AND (current_quest.STATUS IN (1,6,7,8) OR policy.ACHIEVEMENT_LIMIT IS NULL OR current_quest.QUEST_ID IS NULL OR
       current_quest.ACHIEVEMENT_COUNT < policy.ACHIEVEMENT_LIMIT)
  AND NOT EXISTS (
    SELECT 1 FROM SRO_SHARD.REF_QUEST_DEPENDENCY_GROUP dependency_group
    WHERE dependency_group.QUEST_ID=quest.ID AND dependency_group.VARIANT_INDEX=0 AND (
	  dependency_group.RUNTIME_ENABLED<>1 OR
      (dependency_group.DEPENDENCY_MODE='all_clear' AND EXISTS (
        SELECT 1 FROM SRO_SHARD.REF_QUEST_DEPENDENCY_ENTRY dependency_entry
        LEFT JOIN SRO_SHARD.CHAR_QUEST dependency_state
          ON dependency_state.FK_CHAR=character_state.id
         AND dependency_state.QUEST_ID=dependency_entry.DEPENDENCY_QUEST_ID
         AND dependency_state.STATUS = 4 AND dependency_state.ACHIEVEMENT_COUNT>0
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
         AND dependency_state.STATUS = 4 AND dependency_state.ACHIEVEMENT_COUNT>0
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
			&offer.ObjectiveKey, &offer.RequiredCount, &offer.TaskCount, &offer.SelectionCount, &offer.TimerDuration,
			&offer.DayLimit, &offer.DayOrNight, &offer.SharedProgress, &offer.InstantReady, &offer.DeliveryItems); err != nil {
			return nil, fmt.Errorf("scan available quest offer: %w", err)
		}
		if offer.TaskCount > 1 {
			offer.ObjectiveKeys = strings.Split(offer.ObjectiveKey, string(rune(31)))
			if len(offer.ObjectiveKeys) != int(offer.TaskCount) || len(offer.ObjectiveKeys) > 4 {
				return nil, fmt.Errorf("quest offer %d has invalid classified objectives", offer.QuestID)
			}
			offer.ObjectiveKey = offer.ObjectiveKeys[0]
		}
		if offer.QuestID <= 0 || offer.CodeName == "" || offer.NameKey == "" || (!offer.InstantReady && offer.ObjectiveKey == "") ||
			(offer.InstantReady && (offer.CodeName != "QNO_WC_FERRY4_1" || offer.ObjectiveKey != "" || offer.TaskCount != 0)) || offer.TaskCount > 4 || offer.TimerDuration > 255 ||
			(offer.DayLimit == 0) != (offer.DayOrNight == 0) || offer.DayOrNight > 2 {
			return nil, fmt.Errorf("quest offer %d has invalid protocol metadata", offer.QuestID)
		}
		offers = append(offers, offer)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate available quest offers: %w", err)
	}
	return offers, nil
}
