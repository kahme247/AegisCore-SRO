package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

// QuestStatus uses the values confirmed in the pinned SR_GameServer binary.
// Values 2 and 3 are retained legacy/transient states whose writers and exact
// meanings are not recovered; application code must not invent transitions to
// them.
type QuestStatus byte

const (
	QuestActive      QuestStatus = 1
	QuestTransient2  QuestStatus = 2
	QuestTransient3  QuestStatus = 3
	QuestInactive    QuestStatus = 4
	QuestClosed      QuestStatus = 5
	QuestAborted     QuestStatus = 6
	QuestProgressed  QuestStatus = 7
	QuestRewardReady QuestStatus = 8
)

type CharacterQuest struct {
	QuestID          int
	Status           QuestStatus
	AchievementCount uint16
	StartedAt        time.Time
	CompletedAt      *time.Time
	Data1            uint64
	Data2            uint64
	ObjectiveKey     string
	TaskCount        byte
}

const upsertCharacterQuest = `INSERT INTO ` + "`SRO_SHARD`.`CHAR_QUEST`" + `
    (FK_CHAR, QUEST_ID, STATUS, ACHIEVEMENT_COUNT, COMPLETED_AT, QUEST_DATA_1, QUEST_DATA_2)
    VALUES (?, ?, ?, ?, ?, ?, ?)
    ON DUPLICATE KEY UPDATE
        STATUS=VALUES(STATUS),
        ACHIEVEMENT_COUNT=VALUES(ACHIEVEMENT_COUNT),
        COMPLETED_AT=VALUES(COMPLETED_AT),
        QUEST_DATA_1=VALUES(QUEST_DATA_1),
        QUEST_DATA_2=VALUES(QUEST_DATA_2)`

var ErrCharacterQuestRepositoryUnavailable = errors.New("character quest repository is unavailable")
var ErrCharacterQuestNotActive = errors.New("character quest is not active")
var ErrCharacterQuestNotAvailable = errors.New("character quest is not available")

// CharacterQuestRepository is the persistence boundary used by script actions
// once an NPC conversation accepts, updates, or abandons a quest.
type CharacterQuestRepository interface {
	Save(context.Context, int, CharacterQuest) error
	List(context.Context, int) ([]CharacterQuest, error)
	Activate(context.Context, int, CharacterQuest) (CharacterQuest, error)
	Abandon(context.Context, int, int) error
}

type SQLCharacterQuestRepository struct{ shard frameworkdb.Execer }

func NewCharacterQuestRepository(shard frameworkdb.Execer) (*SQLCharacterQuestRepository, error) {
	if shard == nil {
		return nil, ErrCharacterQuestRepositoryUnavailable
	}
	return &SQLCharacterQuestRepository{shard: shard}, nil
}

func (r *SQLCharacterQuestRepository) Save(ctx context.Context, characterID int, quest CharacterQuest) error {
	if r == nil || r.shard == nil {
		return ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || quest.QuestID <= 0 {
		return fmt.Errorf("invalid character quest identity")
	}
	if !quest.Status.Valid() {
		return fmt.Errorf("invalid quest status %d", quest.Status)
	}
	if quest.Status == QuestClosed && quest.CompletedAt == nil {
		return errors.New("closed quest requires completion time")
	}
	if quest.Status != QuestClosed {
		quest.CompletedAt = nil
	}
	result, err := r.shard.ExecContext(ctx, upsertCharacterQuest,
		characterID, quest.QuestID, quest.Status, quest.AchievementCount,
		quest.CompletedAt, quest.Data1, quest.Data2)
	if err != nil {
		return fmt.Errorf("save character quest: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read saved character quest result: %w", err)
	}
	// MySQL reports zero affected rows when an idempotent upsert writes the
	// values already stored for this character and quest.
	if affected < 0 || affected > 2 {
		return fmt.Errorf("save character quest affected %d rows", affected)
	}
	return nil
}

const activateCharacterQuest = `INSERT INTO ` + "`SRO_SHARD`.`CHAR_QUEST`" + `
    (FK_CHAR, QUEST_ID, STATUS, ACHIEVEMENT_COUNT, STARTED_AT, COMPLETED_AT, QUEST_DATA_1, QUEST_DATA_2)
    VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, NULL, ?, ?)
    ON DUPLICATE KEY UPDATE
        STARTED_AT=IF(STATUS IN (?, ?), CURRENT_TIMESTAMP, STARTED_AT),
        COMPLETED_AT=IF(STATUS IN (?, ?), NULL, COMPLETED_AT),
        ACHIEVEMENT_COUNT=IF(STATUS IN (?, ?), VALUES(ACHIEVEMENT_COUNT), ACHIEVEMENT_COUNT),
        QUEST_DATA_1=IF(STATUS IN (?, ?), VALUES(QUEST_DATA_1), QUEST_DATA_1),
        QUEST_DATA_2=IF(STATUS IN (?, ?), VALUES(QUEST_DATA_2), QUEST_DATA_2),
        STATUS=IF(STATUS IN (?, ?), VALUES(STATUS), STATUS)`

// Activate atomically inserts an active quest or promotes an inactive or
// aborted row. Other persisted states are preserved so duplicate accepts
// cannot reset progress and completed one-time quests cannot regress to active.
func (r *SQLCharacterQuestRepository) Activate(ctx context.Context, characterID int, quest CharacterQuest) (CharacterQuest, error) {
	if r == nil || r.shard == nil {
		return CharacterQuest{}, ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || quest.QuestID <= 0 || quest.Status != QuestActive || quest.AchievementCount == 0 {
		return CharacterQuest{}, fmt.Errorf("invalid character quest activation")
	}
	quest.CompletedAt = nil
	activateSources := QuestTransitionSources(QuestTransitionActivate)
	result, err := r.shard.ExecContext(ctx, activateCharacterQuest,
		characterID, quest.QuestID, quest.Status, quest.AchievementCount, quest.Data1, quest.Data2,
		activateSources[0], activateSources[1], activateSources[0], activateSources[1], activateSources[0], activateSources[1],
		activateSources[0], activateSources[1], activateSources[0], activateSources[1], activateSources[0], activateSources[1])
	if err != nil {
		return CharacterQuest{}, fmt.Errorf("activate character quest: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return CharacterQuest{}, fmt.Errorf("read activated character quest result: %w", err)
	}
	if affected == 0 {
		return CharacterQuest{}, ErrCharacterQuestNotAvailable
	}
	if affected != 1 && affected != 2 {
		return CharacterQuest{}, fmt.Errorf("activate character quest affected %d rows", affected)
	}
	quest.StartedAt = time.Now()
	return quest, nil
}

const selectCharacterQuests = `SELECT quest.QUEST_ID,quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
    quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,
    COALESCE(kill_objective.OBJECTIVE_KEY,deliver.OBJECTIVE_KEY,standard_deliver.OBJECTIVE_KEY,''),
    CASE WHEN kill_objective.QUEST_ID IS NULL THEN 0 ELSE 1 END
    FROM SRO_SHARD.CHAR_QUEST quest
    LEFT JOIN SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_objective
      ON kill_objective.QUEST_ID=quest.QUEST_ID AND kill_objective.VARIANT_INDEX=0 AND kill_objective.MISSION_INDEX=0 AND kill_objective.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM_EX deliver
      ON deliver.QUEST_ID=quest.QUEST_ID AND deliver.VARIANT_INDEX=0 AND deliver.MISSION_INDEX=0 AND deliver.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM standard_deliver
      ON standard_deliver.QUEST_ID=quest.QUEST_ID AND standard_deliver.VARIANT_INDEX=0 AND standard_deliver.MISSION_INDEX=0 AND standard_deliver.RUNTIME_ENABLED=1
    WHERE quest.FK_CHAR=? ORDER BY quest.QUEST_ID`

// List returns a detached, deterministic snapshot for the character bootstrap.
func (r *SQLCharacterQuestRepository) List(ctx context.Context, characterID int) ([]CharacterQuest, error) {
	if r == nil || r.shard == nil {
		return nil, ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, fmt.Errorf("invalid character quest identity")
	}
	queryer, ok := r.shard.(frameworkdb.Queryer)
	if !ok {
		return nil, ErrCharacterQuestRepositoryUnavailable
	}
	rows, err := queryer.QueryContext(ctx, selectCharacterQuests, characterID)
	if err != nil {
		return nil, fmt.Errorf("list character quests: %w", err)
	}
	defer rows.Close()

	quests := make([]CharacterQuest, 0)
	for rows.Next() {
		var quest CharacterQuest
		if err := rows.Scan(&quest.QuestID, &quest.Status, &quest.AchievementCount,
			&quest.StartedAt, &quest.CompletedAt, &quest.Data1, &quest.Data2, &quest.ObjectiveKey, &quest.TaskCount); err != nil {
			return nil, fmt.Errorf("scan character quest: %w", err)
		}
		if !quest.Status.Valid() {
			return nil, fmt.Errorf("character quest %d has invalid status %d", quest.QuestID, quest.Status)
		}
		quests = append(quests, quest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate character quests: %w", err)
	}
	return quests, nil
}

const abandonCharacterQuest = `UPDATE ` + "`SRO_SHARD`.`CHAR_QUEST`" + ` SET STATUS=?, COMPLETED_AT=NULL
    WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS IN (?, ?, ?)`

// Abandon is one compare-and-swap update. Concurrent duplicate requests cannot
// both succeed, and a completed quest can never be moved back to abandoned.
func (r *SQLCharacterQuestRepository) Abandon(ctx context.Context, characterID, questID int) error {
	if r == nil || r.shard == nil {
		return ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || questID <= 0 {
		return fmt.Errorf("invalid character quest identity")
	}
	abortSources := QuestTransitionSources(QuestTransitionAbort)
	result, err := r.shard.ExecContext(ctx, abandonCharacterQuest, QuestAborted,
		characterID, questID, abortSources[0], abortSources[1], abortSources[2])
	if err != nil {
		return fmt.Errorf("abandon character quest: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read abandoned character quest result: %w", err)
	}
	if affected == 0 {
		return ErrCharacterQuestNotActive
	}
	if affected != 1 {
		return fmt.Errorf("abandon character quest affected %d rows, want 1", affected)
	}
	return nil
}
