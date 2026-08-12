package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

// QuestStatus keeps the persisted values compatible with the original
// _CharQuest.Status field while making transitions explicit in AegisCore.
type QuestStatus byte

const (
	// These values are the stock client values used by _CharQuest.Status.
	// QuestActive is the initialized/offered state; a player acceptance uses
	// QuestStartedByUser.
	QuestActive                 QuestStatus = 1
	QuestReadyToTurnIn          QuestStatus = 2
	QuestCompleted              QuestStatus = 3
	QuestCompletedMultipleTimes QuestStatus = 4
	QuestUnavailable            QuestStatus = 5
	QuestAbandoned              QuestStatus = 6
	QuestStartedByUser          QuestStatus = 7
	QuestReadyToTurnInByUser    QuestStatus = 8
)

type CharacterQuest struct {
	QuestID          int
	Status           QuestStatus
	AchievementCount uint16
	StartedAt        time.Time
	CompletedAt      *time.Time
	Data1            uint64
	Data2            uint64
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
	if quest.Status < QuestActive || quest.Status > QuestReadyToTurnInByUser {
		return fmt.Errorf("invalid quest status %d", quest.Status)
	}
	if quest.Status == QuestCompleted && quest.CompletedAt == nil {
		return errors.New("completed quest requires completion time")
	}
	if quest.Status != QuestCompleted {
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

// Activate atomically inserts a player-started quest or promotes an initialized
// or abandoned row. Other persisted states are preserved so duplicate accepts
// cannot reset progress and completed one-time quests cannot regress to active.
func (r *SQLCharacterQuestRepository) Activate(ctx context.Context, characterID int, quest CharacterQuest) (CharacterQuest, error) {
	if r == nil || r.shard == nil {
		return CharacterQuest{}, ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || quest.QuestID <= 0 || quest.Status != QuestStartedByUser || quest.AchievementCount == 0 {
		return CharacterQuest{}, fmt.Errorf("invalid character quest activation")
	}
	quest.CompletedAt = nil
	result, err := r.shard.ExecContext(ctx, activateCharacterQuest,
		characterID, quest.QuestID, quest.Status, quest.AchievementCount, quest.Data1, quest.Data2,
		QuestActive, QuestAbandoned, QuestActive, QuestAbandoned, QuestActive, QuestAbandoned,
		QuestActive, QuestAbandoned, QuestActive, QuestAbandoned, QuestActive, QuestAbandoned)
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

const selectCharacterQuests = `SELECT QUEST_ID, STATUS, ACHIEVEMENT_COUNT, STARTED_AT, COMPLETED_AT, QUEST_DATA_1, QUEST_DATA_2
    FROM ` + "`SRO_SHARD`.`CHAR_QUEST`" + ` WHERE FK_CHAR=? ORDER BY QUEST_ID`

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
			&quest.StartedAt, &quest.CompletedAt, &quest.Data1, &quest.Data2); err != nil {
			return nil, fmt.Errorf("scan character quest: %w", err)
		}
		if quest.Status < QuestActive || quest.Status > QuestReadyToTurnInByUser {
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
    WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS IN (?, ?, ?, ?)`

// Abandon is one compare-and-swap update. Concurrent duplicate requests cannot
// both succeed, and a completed quest can never be moved back to abandoned.
func (r *SQLCharacterQuestRepository) Abandon(ctx context.Context, characterID, questID int) error {
	if r == nil || r.shard == nil {
		return ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || questID <= 0 {
		return fmt.Errorf("invalid character quest identity")
	}
	result, err := r.shard.ExecContext(ctx, abandonCharacterQuest, QuestAbandoned,
		characterID, questID, QuestActive, QuestReadyToTurnIn, QuestStartedByUser, QuestReadyToTurnInByUser)
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
