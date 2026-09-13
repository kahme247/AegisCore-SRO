package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	QuestCodeName    string
	Status           QuestStatus
	AchievementCount uint16
	StartedAt        time.Time
	CompletedAt      *time.Time
	Data1            uint64
	Data2            uint64
	ObjectiveKey     string
	ObjectiveKeys    [4]string
	TaskCount        byte
	TimerDuration    uint32
	TimerRemaining   uint32
	SharedProgress   bool
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

// QuestAbandonCleanupRepository exposes the concrete event-8 cleanup result
// needed to keep the client's inventory synchronized after quest-owned items
// are removed.
type QuestAbandonCleanupRepository interface {
	AbandonWithCleanup(context.Context, int, int) (QuestAbandonResult, error)
}

type QuestAbandonResult struct {
	RemovedSlots  []byte
	ResetQuestIDs []int
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
    (FK_CHAR, QUEST_ID, STATUS, ACHIEVEMENT_COUNT, STARTED_AT, COMPLETED_AT, QUEST_DATA_1, QUEST_DATA_2, TIMER_REMAINING)
    VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, NULL, ?, ?, ?)
    ON DUPLICATE KEY UPDATE
        STARTED_AT=IF(STATUS IN (?, ?), CURRENT_TIMESTAMP, STARTED_AT),
        COMPLETED_AT=IF(STATUS IN (?, ?), NULL, COMPLETED_AT),
        ACHIEVEMENT_COUNT=IF(STATUS IN (?, ?), VALUES(ACHIEVEMENT_COUNT), ACHIEVEMENT_COUNT),
        QUEST_DATA_1=IF(STATUS IN (?, ?), VALUES(QUEST_DATA_1), QUEST_DATA_1),
        QUEST_DATA_2=IF(STATUS IN (?, ?), VALUES(QUEST_DATA_2), QUEST_DATA_2),
		TIMER_REMAINING=IF(STATUS IN (?, ?), VALUES(TIMER_REMAINING), TIMER_REMAINING),
        STATUS=IF(STATUS IN (?, ?), VALUES(STATUS), STATUS)`

// Activate atomically inserts an active quest or promotes an inactive or
// aborted row. Other persisted states are preserved so duplicate accepts
// cannot reset progress and completed one-time quests cannot regress to active.
func (r *SQLCharacterQuestRepository) Activate(ctx context.Context, characterID int, quest CharacterQuest) (CharacterQuest, error) {
	if r == nil || r.shard == nil {
		return CharacterQuest{}, ErrCharacterQuestRepositoryUnavailable
	}
	instantFerry := quest.QuestCodeName == "QNO_WC_FERRY4_1" && quest.Status == QuestRewardReady && quest.TaskCount == 0 && quest.ObjectiveKey == ""
	if characterID <= 0 || quest.QuestID <= 0 || (quest.Status != QuestActive && !instantFerry) {
		return CharacterQuest{}, fmt.Errorf("invalid character quest activation")
	}
	quest.CompletedAt = nil
	activateSources := QuestTransitionSources(QuestTransitionActivate)
	result, err := r.shard.ExecContext(ctx, activateCharacterQuest,
		characterID, quest.QuestID, quest.Status, quest.AchievementCount, quest.Data1, quest.Data2, quest.TimerDuration,
		activateSources[0], activateSources[1], activateSources[0], activateSources[1], activateSources[0], activateSources[1],
		activateSources[0], activateSources[1], activateSources[0], activateSources[1], activateSources[0], activateSources[1],
		activateSources[0], activateSources[1])
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
	quest.TimerRemaining = quest.TimerDuration
	return quest, nil
}

const selectCharacterQuests = `SELECT quest.QUEST_ID,COALESCE(definition.CODE_NAME,''),quest.STATUS,quest.ACHIEVEMENT_COUNT,quest.STARTED_AT,
    quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,
    COALESCE(CASE WHEN kill_objective.QUEST_ID IS NOT NULL AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.QUEST_ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(kill_all.OBJECTIVE_KEY ORDER BY kill_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.QUEST_ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1) ELSE kill_objective.OBJECTIVE_KEY END,deliver.OBJECTIVE_KEY,standard_deliver.OBJECTIVE_KEY,dialog.OBJECTIVE_KEY,kill_player.OBJECTIVE_KEY,
	  CASE WHEN collection.QUEST_ID IS NOT NULL AND collection.DECLARATION_KIND='standard' AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.QUEST_ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(collection_all.OBJECTIVE_KEY ORDER BY collection_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.QUEST_ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1) ELSE collection.OBJECTIVE_KEY END,
	  CASE WHEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.QUEST_ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1)>1 THEN (SELECT GROUP_CONCAT(extended_all.OBJECTIVE_KEY ORDER BY extended_all.MISSION_INDEX SEPARATOR 0x1F) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.QUEST_ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1) ELSE extended_kill.OBJECTIVE_KEY END,
	  talisman.OBJECTIVE_KEY,''),
	CASE WHEN kill_objective.QUEST_ID IS NOT NULL THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.QUEST_ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)
	     WHEN collection.QUEST_ID IS NOT NULL AND collection.DECLARATION_KIND='standard' THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection_all WHERE collection_all.QUEST_ID=quest.QUEST_ID AND collection_all.VARIANT_INDEX=0 AND collection_all.RUNTIME_ENABLED=1)
	     WHEN extended_kill.QUEST_ID IS NOT NULL THEN (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_all WHERE extended_all.QUEST_ID=quest.QUEST_ID AND extended_all.VARIANT_INDEX=0 AND extended_all.RUNTIME_ENABLED=1)
	     WHEN kill_objective.QUEST_ID IS NULL AND dialog.QUEST_ID IS NULL AND kill_player.QUEST_ID IS NULL AND collection.QUEST_ID IS NULL AND extended_kill.QUEST_ID IS NULL AND talisman.QUEST_ID IS NULL THEN 0 ELSE 1 END,
	COALESCE(policy.LIMIT_TIME,0),quest.TIMER_REMAINING,
	CASE WHEN kill_objective.QUEST_ID IS NOT NULL AND (SELECT COUNT(*) FROM SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_all WHERE kill_all.QUEST_ID=quest.QUEST_ID AND kill_all.VARIANT_INDEX=0 AND kill_all.RUNTIME_ENABLED=1)>1 THEN 1 ELSE 0 END
    FROM SRO_SHARD.CHAR_QUEST quest
	LEFT JOIN SRO_SHARD.REF_QUEST definition ON definition.ID=quest.QUEST_ID
	LEFT JOIN SRO_SHARD.REF_QUEST_COMPLETION_POLICY policy
	  ON policy.QUEST_ID=quest.QUEST_ID AND policy.VARIANT_INDEX=0 AND policy.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_KILL_OBJECTIVE kill_objective
      ON kill_objective.QUEST_ID=quest.QUEST_ID AND kill_objective.VARIANT_INDEX=0 AND kill_objective.MISSION_INDEX=0 AND kill_objective.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM_EX deliver
      ON deliver.QUEST_ID=quest.QUEST_ID AND deliver.VARIANT_INDEX=0 AND deliver.MISSION_INDEX=0 AND deliver.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_DELIVER_ITEM standard_deliver
      ON standard_deliver.QUEST_ID=quest.QUEST_ID AND standard_deliver.VARIANT_INDEX=0 AND standard_deliver.MISSION_INDEX=0 AND standard_deliver.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_DIALOG_OBJECTIVE dialog
      ON dialog.QUEST_ID=quest.QUEST_ID AND dialog.VARIANT_INDEX=0 AND dialog.MISSION_INDEX=0 AND dialog.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_KILL_PLAYER_OBJECTIVE kill_player
      ON kill_player.QUEST_ID=quest.QUEST_ID AND kill_player.VARIANT_INDEX=0 AND kill_player.MISSION_INDEX=0 AND kill_player.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE collection
      ON collection.QUEST_ID=quest.QUEST_ID AND collection.VARIANT_INDEX=0 AND collection.MISSION_INDEX=0 AND collection.RUNTIME_ENABLED=1
    LEFT JOIN SRO_SHARD.REF_QUEST_EXTENDED_KILL_OBJECTIVE extended_kill
      ON extended_kill.QUEST_ID=quest.QUEST_ID AND extended_kill.VARIANT_INDEX=0 AND extended_kill.MISSION_INDEX=0
      AND extended_kill.RUNTIME_ENABLED=1 AND extended_kill.DECLARATION_KIND IN ('multi_target','classified')
    LEFT JOIN SRO_SHARD.REF_QUEST_TALISMAN_OBJECTIVE talisman
      ON talisman.QUEST_ID=quest.QUEST_ID AND talisman.VARIANT_INDEX=0 AND talisman.MISSION_INDEX=0 AND talisman.RUNTIME_ENABLED=1
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
		if err := rows.Scan(&quest.QuestID, &quest.QuestCodeName, &quest.Status, &quest.AchievementCount,
			&quest.StartedAt, &quest.CompletedAt, &quest.Data1, &quest.Data2, &quest.ObjectiveKey, &quest.TaskCount, &quest.TimerDuration, &quest.TimerRemaining, &quest.SharedProgress); err != nil {
			return nil, fmt.Errorf("scan character quest: %w", err)
		}
		if !quest.Status.Valid() {
			return nil, fmt.Errorf("character quest %d has invalid status %d", quest.QuestID, quest.Status)
		}
		if quest.TaskCount > 1 {
			keys := strings.Split(quest.ObjectiveKey, string(rune(31)))
			if len(keys) != int(quest.TaskCount) || len(keys) > len(quest.ObjectiveKeys) {
				return nil, fmt.Errorf("character quest %d has invalid classified objectives", quest.QuestID)
			}
			copy(quest.ObjectiveKeys[:], keys)
			quest.ObjectiveKey = keys[0]
		}
		quests = append(quests, quest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate character quests: %w", err)
	}
	return quests, nil
}

const abandonCharacterQuest = `UPDATE ` + "`SRO_SHARD`.`CHAR_QUEST`" + ` SET STATUS=?, ACHIEVEMENT_COUNT=0, COMPLETED_AT=NULL,
    QUEST_DATA_1=0, QUEST_DATA_2=0, TIMER_REMAINING=0
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
	if beginner, ok := r.shard.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		_, err := r.abandonWithCleanup(ctx, beginner, characterID, questID)
		return err
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

func (r *SQLCharacterQuestRepository) AbandonWithCleanup(ctx context.Context, characterID, questID int) (QuestAbandonResult, error) {
	if r == nil || r.shard == nil {
		return QuestAbandonResult{}, ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || questID <= 0 {
		return QuestAbandonResult{}, fmt.Errorf("invalid character quest identity")
	}
	beginner, ok := r.shard.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return QuestAbandonResult{}, ErrCharacterQuestRepositoryUnavailable
	}
	return r.abandonWithCleanup(ctx, beginner, characterID, questID)
}

func (r *SQLCharacterQuestRepository) abandonWithCleanup(ctx context.Context, beginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, characterID, questID int) (QuestAbandonResult, error) {
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return QuestAbandonResult{}, fmt.Errorf("begin quest abandon: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var status QuestStatus
	if err = tx.QueryRowContext(ctx, `SELECT STATUS FROM SRO_SHARD.CHAR_QUEST
		WHERE FK_CHAR=? AND QUEST_ID=? FOR UPDATE`, characterID, questID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return QuestAbandonResult{}, ErrCharacterQuestNotActive
		}
		return QuestAbandonResult{}, err
	}
	abortSources := QuestTransitionSources(QuestTransitionAbort)
	if status != abortSources[0] && status != abortSources[1] && status != abortSources[2] {
		return QuestAbandonResult{}, ErrCharacterQuestNotActive
	}

	rows, err := tx.QueryContext(ctx, `SELECT ITEM_REF_OBJECT_ID FROM SRO_SHARD.REF_QUEST_COLLECTION_ITEM_OBJECTIVE
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND RUNTIME_ENABLED=1
		UNION SELECT ITEM_REF_OBJECT_ID FROM SRO_SHARD.REF_QUEST_DELIVER_ITEM_EX
		WHERE QUEST_ID=? AND VARIANT_INDEX=0 AND RUNTIME_ENABLED=1
		UNION SELECT ITEM_REF_OBJECT_ID FROM SRO_SHARD.REF_QUEST_DELIVER_ITEM_ENTRY
		WHERE QUEST_ID=? AND VARIANT_INDEX=0`, questID, questID, questID)
	if err != nil {
		return QuestAbandonResult{}, err
	}
	owned := make(map[uint32]struct{})
	for rows.Next() {
		var refID uint32
		if err = rows.Scan(&refID); err != nil {
			_ = rows.Close()
			return QuestAbandonResult{}, err
		}
		if refID != 0 {
			owned[refID] = struct{}{}
		}
	}
	if err = rows.Close(); err != nil {
		return QuestAbandonResult{}, err
	}
	if err = rows.Err(); err != nil {
		return QuestAbandonResult{}, err
	}
	nativeCleanup := nativeQuestAbandonDefinition(questID)
	for _, refID := range nativeCleanup.ItemRefIDs {
		owned[refID] = struct{}{}
	}

	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestAbandonResult{}, err
	}
	result := QuestAbandonResult{
		RemovedSlots:  make([]byte, 0),
		ResetQuestIDs: append([]int(nil), nativeCleanup.RelatedQuestIDs...),
	}
	for slot := byte(13); slot < 255; slot++ {
		item, exists := items[slot]
		if _, remove := owned[item.refID]; !exists || !remove {
			continue
		}
		deleted, deleteErr := tx.ExecContext(ctx, `DELETE FROM SRO_SHARD.INVENTORY
			WHERE FK_CHAR=? AND FK_ITEM=? AND SLOT=?`, characterID, item.id, slot)
		if deleteErr != nil {
			return QuestAbandonResult{}, deleteErr
		}
		if deleteErr = requireQuestRow(deleted, "unlink abandoned quest item"); deleteErr != nil {
			return QuestAbandonResult{}, deleteErr
		}
		deleted, deleteErr = tx.ExecContext(ctx, `DELETE FROM SRO_SHARD.ITEM WHERE id=?`, item.id)
		if deleteErr != nil {
			return QuestAbandonResult{}, deleteErr
		}
		if deleteErr = requireQuestRow(deleted, "delete abandoned quest item"); deleteErr != nil {
			return QuestAbandonResult{}, deleteErr
		}
		result.RemovedSlots = append(result.RemovedSlots, slot)
	}
	updated, err := tx.ExecContext(ctx, abandonCharacterQuest, QuestAborted,
		characterID, questID, abortSources[0], abortSources[1], abortSources[2])
	if err != nil {
		return QuestAbandonResult{}, err
	}
	if err = requireQuestRow(updated, "abandon character quest"); err != nil {
		return QuestAbandonResult{}, ErrCharacterQuestNotActive
	}
	for _, relatedQuestID := range nativeCleanup.RelatedQuestIDs {
		if _, err = tx.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR_QUEST SET STATUS=?, ACHIEVEMENT_COUNT=0,
			COMPLETED_AT=NULL, QUEST_DATA_1=0, QUEST_DATA_2=0, TIMER_REMAINING=0
			WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS IN (?, ?, ?)`,
			QuestAborted, characterID, relatedQuestID,
			QuestActive, QuestProgressed, QuestRewardReady); err != nil {
			return QuestAbandonResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return QuestAbandonResult{}, err
	}
	return result, nil
}

func (r *SQLCharacterQuestRepository) SaveCountdown(ctx context.Context, characterID, questID int, remaining uint32) error {
	if r == nil || r.shard == nil {
		return ErrCharacterQuestRepositoryUnavailable
	}
	if characterID <= 0 || questID <= 0 || remaining > 255 {
		return fmt.Errorf("invalid quest countdown")
	}
	result, err := r.shard.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR_QUEST SET TIMER_REMAINING=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS IN (1,7,8)`, remaining, characterID, questID)
	if err != nil {
		return fmt.Errorf("save quest countdown: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read saved quest countdown result: %w", err)
	}
	if affected > 1 {
		return fmt.Errorf("save quest countdown affected %d rows", affected)
	}
	return nil
}
