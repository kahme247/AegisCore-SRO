package model

import (
	"context"
	"errors"
	"math"
)

func (r *SQLQuestRuntimeRepository) recordClassifiedMobKill(ctx context.Context, characterID int, mobRefObjectID uint32, rarity MonsterRarity) ([]QuestProgressResult, error) {
	classKey := classifiedMonsterClass(rarity)
	if classKey == "" {
		return nil, nil
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
	rows, err := tx.QueryContext(ctx, `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.MISSION_INDEX,
		CAST(JSON_UNQUOTE(JSON_EXTRACT(objective.PARAMETERS,'$[8].Number')) AS UNSIGNED)
		FROM CHAR_QUEST quest
		JOIN REF_QUEST_EXTENDED_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		JOIN REF_QUEST_EXTENDED_KILL_TARGET target ON target.QUEST_ID=objective.QUEST_ID
		  AND target.VARIANT_INDEX=objective.VARIANT_INDEX AND target.MISSION_INDEX=objective.MISSION_INDEX
		WHERE quest.FK_CHAR=? AND quest.STATUS IN (?,?) AND objective.VARIANT_INDEX=0
		  AND objective.RUNTIME_ENABLED=1 AND objective.DECLARATION_KIND='classified'
		  AND objective.TARGET_CLASS=? AND target.TARGET_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID,objective.MISSION_INDEX FOR UPDATE`, characterID, QuestActive, QuestProgressed, classKey, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	type classifiedMatch struct {
		quest        CharacterQuest
		missionIndex int
		goal         uint16
	}
	matches := make([]classifiedMatch, 0)
	for rows.Next() {
		var match classifiedMatch
		var goal uint64
		if err = rows.Scan(&match.quest.QuestID, &match.quest.Status, &match.quest.AchievementCount, &match.quest.StartedAt,
			&match.quest.CompletedAt, &match.quest.Data1, &match.quest.Data2, &match.missionIndex, &goal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if match.missionIndex < 0 || match.missionIndex >= len(match.quest.ObjectiveKeys) || goal == 0 || goal > math.MaxUint16 {
			_ = rows.Close()
			return nil, ErrQuestReferenceUnsupported
		}
		match.goal = uint16(goal)
		matches = append(matches, match)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(matches))
	for _, match := range matches {
		objectiveRows, queryErr := tx.QueryContext(ctx, `SELECT MISSION_INDEX,OBJECTIVE_KEY,
			CAST(JSON_UNQUOTE(JSON_EXTRACT(PARAMETERS,'$[8].Number')) AS UNSIGNED)
			FROM REF_QUEST_EXTENDED_KILL_OBJECTIVE
			WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND RUNTIME_ENABLED=1 AND DECLARATION_KIND='classified'
			ORDER BY MISSION_INDEX FOR SHARE`, match.quest.QuestID)
		if queryErr != nil {
			return nil, queryErr
		}
		goals := make([]uint16, 0, 3)
		keys := make([]string, 0, 3)
		for objectiveRows.Next() {
			var index int
			var key string
			var goal uint64
			if queryErr = objectiveRows.Scan(&index, &key, &goal); queryErr != nil {
				_ = objectiveRows.Close()
				return nil, queryErr
			}
			if index != len(goals) || index > 2 || key == "" || goal == 0 || goal > math.MaxUint16 {
				_ = objectiveRows.Close()
				return nil, ErrQuestReferenceUnsupported
			}
			goals = append(goals, uint16(goal))
			keys = append(keys, key)
		}
		if queryErr = objectiveRows.Err(); queryErr != nil {
			_ = objectiveRows.Close()
			return nil, queryErr
		}
		if queryErr = objectiveRows.Close(); queryErr != nil {
			return nil, queryErr
		}
		if len(goals) < 2 || len(goals) > 3 || match.missionIndex >= len(goals) || goals[match.missionIndex] != match.goal {
			return nil, ErrQuestReferenceUnsupported
		}
		previousData, previousStatus := match.quest.Data1, match.quest.Status
		updated, changed, updateErr := advanceClassifiedQuest(match.quest, match.missionIndex, goals)
		if updateErr != nil {
			return nil, updateErr
		}
		if !changed {
			continue
		}
		updated.TaskCount = byte(len(keys))
		copy(updated.ObjectiveKeys[:], keys)
		updated.ObjectiveKey = keys[0]
		result, updateErr := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, updated.Status, updated.Data1,
			characterID, updated.QuestID, previousStatus, previousData)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(result, "record classified mob kill"); updateErr != nil {
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

func classifiedMonsterClass(rarity MonsterRarity) string {
	switch rarity.BaseClass() {
	case MonsterNormal:
		return "MONSTER_CLASS_NORMAL"
	case MonsterChampion:
		return "MONSTER_CLASS_CHAMPION"
	case MonsterGiant:
		return "MONSTER_CLASS_GIANT"
	default:
		return ""
	}
}

func advanceClassifiedQuest(quest CharacterQuest, missionIndex int, goals []uint16) (CharacterQuest, bool, error) {
	if missionIndex < 0 || missionIndex >= len(goals) || len(goals) < 2 || len(goals) > 3 || goals[missionIndex] == 0 {
		return CharacterQuest{}, false, ErrQuestStateConflict
	}
	shift := uint(missionIndex * 16)
	current := uint16((quest.Data1 >> shift) & math.MaxUint16)
	if current >= goals[missionIndex] {
		return quest, false, nil
	}
	current++
	quest.Data1 = (quest.Data1 & ^(uint64(math.MaxUint16) << shift)) | uint64(current)<<shift
	complete := true
	for index, goal := range goals {
		if goal == 0 || uint16((quest.Data1>>uint(index*16))&math.MaxUint16) < goal {
			complete = false
			break
		}
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
