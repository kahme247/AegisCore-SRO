package model

import (
	"context"
	"database/sql"
	"math"
)

func lockNextQuestAchievementCount(ctx context.Context, tx *sql.Tx, quest CharacterQuest) (uint16, error) {
	if tx == nil || quest.QuestID <= 0 || quest.AchievementCount == math.MaxUint16 {
		return 0, ErrQuestStateConflict
	}
	var limit sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT ACHIEVEMENT_LIMIT FROM REF_QUEST_COMPLETION_POLICY
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 FOR SHARE`, quest.QuestID).Scan(&limit); err != nil {
		return 0, err
	}
	return nextQuestAchievementCount(quest.AchievementCount, limit)
}

func nextQuestAchievementCount(current uint16, limit sql.NullInt64) (uint16, error) {
	if current == math.MaxUint16 {
		return 0, ErrQuestStateConflict
	}
	next := uint64(current) + 1
	if limit.Valid && (limit.Int64 <= 0 || next > uint64(limit.Int64)) {
		return 0, ErrQuestStateConflict
	}
	return uint16(next), nil
}
