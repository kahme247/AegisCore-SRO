package model

import (
	"context"
	"math"
)

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
