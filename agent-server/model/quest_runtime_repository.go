package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	QuestWeaponListID        = 3
	QuestChulsanAngerID      = 58
	QuestWeaponListItemID    = uint32(2200)
	QuestChulsanRewardItemID = uint32(9228)
	QuestBanditBowmanID      = uint32(1944)
	QuestBanditBowmanGoal    = uint64(140)
)

var (
	ErrQuestRuntimeUnavailable = errors.New("quest runtime repository is unavailable")
	ErrQuestStateConflict      = errors.New("quest state conflicts with the request")
	ErrQuestInventoryFull      = errors.New("quest inventory is full")
	ErrQuestRequiredItem       = errors.New("required quest item is missing")
	ErrQuestOutcomeUnknown     = errors.New("quest transaction outcome is unknown")
)

type QuestItemMutation struct {
	Slot byte
	Item Item
}

type QuestProgressResult struct {
	Quest   CharacterQuest
	Changed bool
}

type QuestItemGrantResult struct {
	Quest     CharacterQuest
	Inventory Inventory
	Added     QuestItemMutation
}

type QuestCompletionResult struct {
	Quest       CharacterQuest
	Inventory   Inventory
	Gold        uint64
	Progress    ProgressionResult
	Experience  uint64
	SkillExp    uint64
	Added       *QuestItemMutation
	RemovedSlot *byte
}

type QuestRuntimeRepository interface {
	GrantMissionItem(context.Context, int, int) (QuestItemGrantResult, error)
	RecordMobKill(context.Context, int, uint32) (QuestProgressResult, error)
	Complete(context.Context, int, int) (QuestCompletionResult, error)
}

type SQLQuestRuntimeRepository struct {
	shard  *sql.DB
	levels *LevelTable
}

func NewQuestRuntimeRepository(shard *sql.DB, levels *LevelTable) (*SQLQuestRuntimeRepository, error) {
	if shard == nil || levels == nil {
		return nil, ErrQuestRuntimeUnavailable
	}
	return &SQLQuestRuntimeRepository{shard: shard, levels: levels}, nil
}

func (r *SQLQuestRuntimeRepository) GrantMissionItem(ctx context.Context, characterID, questID int) (QuestItemGrantResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || questID != QuestWeaponListID {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil || quest.Status != QuestStartedByUser {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	if _, _, exists := findQuestItem(items, QuestWeaponListItemID); exists {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	slot, ok := freeQuestInventorySlot(items, capacity)
	if !ok {
		return QuestItemGrantResult{}, ErrQuestInventoryFull
	}
	item, err := insertQuestItem(ctx, tx, characterID, slot, QuestWeaponListItemID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	quest.Status = QuestReadyToTurnInByUser
	quest.Data1 = 1
	update, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`QUEST_DATA_1`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", quest.Status, quest.Data1, characterID, questID, QuestStartedByUser)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	if err = requireQuestRow(update, "grant mission item"); err != nil {
		return QuestItemGrantResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	inventory, err := loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestItemGrantResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	if persisted, exists := inventory.Items[slot]; exists {
		item = persisted
	}
	return QuestItemGrantResult{Quest: quest, Inventory: inventory, Added: QuestItemMutation{Slot: slot, Item: item}}, nil
}

func (r *SQLQuestRuntimeRepository) RecordMobKill(ctx context.Context, characterID int, mobRefObjectID uint32) (QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return QuestProgressResult{}, ErrQuestStateConflict
	}
	if mobRefObjectID != QuestBanditBowmanID {
		return QuestProgressResult{}, nil
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestProgressResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, QuestChulsanAngerID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && quest.Status != QuestStartedByUser) {
		_ = tx.Rollback()
		committed = true
		return QuestProgressResult{}, nil
	}
	if err != nil {
		return QuestProgressResult{}, err
	}
	if quest.Data1 >= QuestBanditBowmanGoal {
		_ = tx.Rollback()
		committed = true
		return QuestProgressResult{}, nil
	}
	quest.Data1++
	if quest.Data1 >= QuestBanditBowmanGoal {
		quest.Data1 = QuestBanditBowmanGoal
		quest.Status = QuestReadyToTurnInByUser
	}
	update, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`QUEST_DATA_1`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", quest.Status, quest.Data1, characterID, quest.QuestID, QuestStartedByUser)
	if err != nil {
		return QuestProgressResult{}, err
	}
	if err = requireQuestRow(update, "record mob kill"); err != nil {
		return QuestProgressResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestProgressResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return QuestProgressResult{Quest: quest, Changed: true}, nil
}

func (r *SQLQuestRuntimeRepository) Complete(ctx context.Context, characterID, questID int) (QuestCompletionResult, error) {
	if r == nil || r.shard == nil || r.levels == nil || characterID <= 0 || (questID != QuestWeaponListID && questID != QuestChulsanAngerID) {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	state, err := lockQuestCharacter(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil || quest.Status != QuestReadyToTurnInByUser {
		return QuestCompletionResult{}, ErrQuestStateConflict
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	result := QuestCompletionResult{Quest: quest, Gold: state.gold}
	var rewardExp, rewardSkillExp uint64
	switch questID {
	case QuestWeaponListID:
		slot, item, exists := findQuestItem(items, QuestWeaponListItemID)
		if !exists {
			return QuestCompletionResult{}, ErrQuestRequiredItem
		}
		removed, removeErr := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, item.id)
		if removeErr != nil {
			return QuestCompletionResult{}, removeErr
		}
		if err = requireQuestRow(removed, "remove mission item slot"); err != nil {
			return QuestCompletionResult{}, err
		}
		removed, removeErr = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.id)
		if removeErr != nil {
			return QuestCompletionResult{}, removeErr
		}
		if err = requireQuestRow(removed, "remove mission item"); err != nil {
			return QuestCompletionResult{}, err
		}
		result.RemovedSlot = &slot
		if state.gold > math.MaxUint64-205 {
			return QuestCompletionResult{}, ErrQuestStateConflict
		}
		state.gold += 205
		rewardExp = 270
	case QuestChulsanAngerID:
		slot, ok := freeQuestInventorySlot(items, state.inventorySize)
		if !ok {
			return QuestCompletionResult{}, ErrQuestInventoryFull
		}
		item, insertErr := insertQuestItem(ctx, tx, characterID, slot, QuestChulsanRewardItemID)
		if insertErr != nil {
			return QuestCompletionResult{}, insertErr
		}
		result.Added = &QuestItemMutation{Slot: slot, Item: item}
		rewardExp = 68860
		rewardSkillExp = 10000
	}

	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, rewardExp, rewardSkillExp, state.skillPoints, state.statPoints)
	updatedCharacter, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=?,`CURRENT_LEVEL`=?,`EXP`=?,`SKILL_EXP`=?,`SKILL_POINTS`=?,`STAT_POINTS`=?,`MAX_LEVEL`=GREATEST(`MAX_LEVEL`,?) WHERE `id`=?", state.gold, progress.Level, progress.Experience, progress.SkillExperience, progress.SkillPoints, progress.StatPoints, progress.Level, characterID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(updatedCharacter, "apply quest rewards"); err != nil {
		return QuestCompletionResult{}, err
	}
	now := time.Now().UTC()
	completed, err := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`COMPLETED_AT`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=?", QuestCompleted, now, characterID, questID, QuestReadyToTurnInByUser)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = requireQuestRow(completed, "complete quest"); err != nil {
		return QuestCompletionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Quest.Status = QuestCompleted
	result.Quest.CompletedAt = &now
	result.Gold = state.gold
	result.Progress = progress
	result.Experience = rewardExp
	result.SkillExp = rewardSkillExp
	result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
	if err != nil {
		return QuestCompletionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	if result.Added != nil {
		if persisted, exists := result.Inventory.Items[result.Added.Slot]; exists {
			result.Added.Item = persisted
		}
	}
	return result, nil
}

type questCharacterState struct {
	gold            uint64
	level           int
	experience      uint64
	skillExperience uint64
	skillPoints     int
	statPoints      int
	inventorySize   byte
}

func lockQuestCharacter(ctx context.Context, tx *sql.Tx, characterID int) (questCharacterState, error) {
	var state questCharacterState
	err := tx.QueryRowContext(ctx, "SELECT `GOLD`,`CURRENT_LEVEL`,`EXP`,`SKILL_EXP`,`SKILL_POINTS`,`STAT_POINTS`,`INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&state.gold, &state.level, &state.experience, &state.skillExperience, &state.skillPoints, &state.statPoints, &state.inventorySize)
	return state, err
}

func lockQuestCharacterCapacity(ctx context.Context, tx *sql.Tx, characterID int) (byte, error) {
	var capacity byte
	err := tx.QueryRowContext(ctx, "SELECT `INVENTORY_SIZE` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&capacity)
	return capacity, err
}

func lockCharacterQuest(ctx context.Context, tx *sql.Tx, characterID, questID int) (CharacterQuest, error) {
	var quest CharacterQuest
	quest.QuestID = questID
	err := tx.QueryRowContext(ctx, "SELECT `STATUS`,`ACHIEVEMENT_COUNT`,`STARTED_AT`,`COMPLETED_AT`,`QUEST_DATA_1`,`QUEST_DATA_2` FROM `CHAR_QUEST` WHERE `FK_CHAR`=? AND `QUEST_ID`=? FOR UPDATE", characterID, questID).Scan(&quest.Status, &quest.AchievementCount, &quest.StartedAt, &quest.CompletedAt, &quest.Data1, &quest.Data2)
	return quest, err
}

type questInventoryItem struct {
	id    int64
	refID uint32
	stack int
}

func lockQuestInventory(ctx context.Context, tx *sql.Tx, characterID int) (map[byte]questInventoryItem, error) {
	rows, err := tx.QueryContext(ctx, "SELECT i.`SLOT`,i.`FK_ITEM`,it.`FK_REF_ITEM`,it.`STACK_COUNT` FROM `INVENTORY` i JOIN `ITEM` it ON it.`id`=i.`FK_ITEM` WHERE i.`FK_CHAR`=? ORDER BY i.`SLOT` FOR UPDATE", characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[byte]questInventoryItem)
	for rows.Next() {
		var slot byte
		var item questInventoryItem
		if err = rows.Scan(&slot, &item.id, &item.refID, &item.stack); err != nil {
			return nil, err
		}
		items[slot] = item
	}
	return items, rows.Err()
}

func findQuestItem(items map[byte]questInventoryItem, refID uint32) (byte, questInventoryItem, bool) {
	for slot := byte(13); slot < 255; slot++ {
		if item, ok := items[slot]; ok && item.refID == refID && item.stack > 0 {
			return slot, item, true
		}
	}
	return 0, questInventoryItem{}, false
}

func freeQuestInventorySlot(items map[byte]questInventoryItem, capacity byte) (byte, bool) {
	if capacity == 0 {
		capacity = BaseInventorySize
	}
	for slot := byte(13); slot < capacity; slot++ {
		if _, occupied := items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func insertQuestItem(ctx context.Context, tx *sql.Tx, characterID int, slot byte, refID uint32) (Item, error) {
	if _, ok := RefItems[refID]; !ok {
		return Item{}, fmt.Errorf("%w: %d", ErrInventoryReferenceItemNotFound, refID)
	}
	insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`,`RENT_TYPE`,`RENT_CAN_DELETE`,`RENT_CAN_RECHARGE`,`RENT_PERIOD_BEGIN_TIME`,`RENT_PERIOD_END_TIME`,`RENT_METER_RATE_TIME`,`RENT_PACKING_TIME`) VALUES (0,?,1,0,0,0,0,0,0,0,0,0)", refID)
	if err != nil {
		return Item{}, err
	}
	itemID, err := insert.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
		return Item{}, err
	}
	return economyItem(itemID, refID, 0, 1, 0, 0, RentInfo{})
}

func loadQuestInventory(ctx context.Context, shard *sql.DB, characterID int) (Inventory, error) {
	repository, err := NewInventoryRepository(shard)
	if err != nil {
		return Inventory{}, err
	}
	return repository.GetCharacterInventory(ctx, int64(characterID))
}

func requireQuestRow(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrQuestStateConflict, operation, affected)
	}
	return nil
}
