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
	QuestWeaponListID   = 3
	QuestChulsanAngerID = 58
)

var (
	ErrQuestRuntimeUnavailable   = errors.New("quest runtime repository is unavailable")
	ErrQuestStateConflict        = errors.New("quest state conflicts with the request")
	ErrQuestInventoryFull        = errors.New("quest inventory is full")
	ErrQuestRequiredItem         = errors.New("required quest item is missing")
	ErrQuestOutcomeUnknown       = errors.New("quest transaction outcome is unknown")
	ErrQuestReferenceMissing     = errors.New("quest reward reference is missing")
	ErrQuestReferenceUnsupported = errors.New("quest reward reference uses an unsupported mechanism")
)

type questRewardReference struct {
	Gold             uint64
	Experience       uint64
	SkillExperience  uint64
	SkillPoints      uint64
	AcademyPoints    uint64
	Hwan             uint8
	InventorySlots   uint8
	IsBasicReward    bool
	IsItemReward     bool
	IsCheckCondition bool
	IsCheckCountry   bool
	IsCheckClass     bool
	IsCheckGender    bool
	ItemRewardType   uint8
	SelectionCount   uint8
	Param1           int64
	Param2           int64
	Param3           int64
	ItemRefID        uint32
	ItemQuantity     int
}

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
	RecordMobKill(context.Context, int, uint32) ([]QuestProgressResult, error)
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
	missionItemRefID, err := lockDeliverItemReference(ctx, tx, questID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return QuestItemGrantResult{}, err
	}
	if _, _, exists := findQuestItem(items, missionItemRefID); exists {
		return QuestItemGrantResult{}, ErrQuestStateConflict
	}
	slot, ok := freeQuestInventorySlot(items, capacity)
	if !ok {
		return QuestItemGrantResult{}, ErrQuestInventoryFull
	}
	item, err := insertQuestItem(ctx, tx, characterID, slot, missionItemRefID)
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

func (r *SQLQuestRuntimeRepository) RecordMobKill(ctx context.Context, characterID int, mobRefObjectID uint32) ([]QuestProgressResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return nil, ErrQuestStateConflict
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
		quest.COMPLETED_AT,quest.QUEST_DATA_1,quest.QUEST_DATA_2,objective.REQUIRED_COUNT
		FROM CHAR_QUEST quest JOIN REF_QUEST_KILL_OBJECTIVE objective ON objective.QUEST_ID=quest.QUEST_ID
		WHERE quest.FK_CHAR=? AND quest.STATUS=? AND objective.RUNTIME_ENABLED=1 AND objective.TARGET_REF_OBJECT_ID=?
		ORDER BY quest.QUEST_ID FOR UPDATE`, characterID, QuestStartedByUser, mobRefObjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pendingProgress struct {
		quest CharacterQuest
		goal  uint64
	}
	pending := make([]pendingProgress, 0)
	for rows.Next() {
		var entry pendingProgress
		if err = rows.Scan(&entry.quest.QuestID, &entry.quest.Status, &entry.quest.AchievementCount, &entry.quest.StartedAt,
			&entry.quest.CompletedAt, &entry.quest.Data1, &entry.quest.Data2, &entry.goal); err != nil {
			return nil, err
		}
		pending = append(pending, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]QuestProgressResult, 0, len(pending))
	for _, entry := range pending {
		if entry.goal == 0 || entry.quest.Data1 >= entry.goal {
			continue
		}
		previous := entry.quest.Data1
		entry.quest.Data1++
		if entry.quest.Data1 >= entry.goal {
			entry.quest.Data1 = entry.goal
			entry.quest.Status = QuestReadyToTurnInByUser
		}
		update, updateErr := tx.ExecContext(ctx, "UPDATE `CHAR_QUEST` SET `STATUS`=?,`QUEST_DATA_1`=? WHERE `FK_CHAR`=? AND `QUEST_ID`=? AND `STATUS`=? AND `QUEST_DATA_1`=?", entry.quest.Status, entry.quest.Data1, characterID, entry.quest.QuestID, QuestStartedByUser, previous)
		if updateErr != nil {
			return nil, updateErr
		}
		if updateErr = requireQuestRow(update, "record mob kill"); updateErr != nil {
			return nil, updateErr
		}
		results = append(results, QuestProgressResult{Quest: entry.quest, Changed: true})
	}
	if err = tx.Commit(); err != nil {
		return nil, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return results, nil
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
	reward, err := lockQuestRewardReference(ctx, tx, questID)
	if err != nil {
		return QuestCompletionResult{}, err
	}
	if err = validateQuestRewardReference(questID, reward); err != nil {
		return QuestCompletionResult{}, err
	}
	result := QuestCompletionResult{Quest: quest, Gold: state.gold}
	switch questID {
	case QuestWeaponListID:
		missionItemRefID, referenceErr := lockDeliverItemReference(ctx, tx, questID)
		if referenceErr != nil {
			return QuestCompletionResult{}, referenceErr
		}
		slot, item, exists := findQuestItem(items, missionItemRefID)
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
		if state.gold > math.MaxUint64-reward.Gold {
			return QuestCompletionResult{}, ErrQuestStateConflict
		}
		state.gold += reward.Gold
	case QuestChulsanAngerID:
		slot, ok := freeQuestInventorySlot(items, state.inventorySize)
		if !ok {
			return QuestCompletionResult{}, ErrQuestInventoryFull
		}
		item, insertErr := insertQuestItem(ctx, tx, characterID, slot, reward.ItemRefID)
		if insertErr != nil {
			return QuestCompletionResult{}, insertErr
		}
		result.Added = &QuestItemMutation{Slot: slot, Item: item}
	}

	progress := ApplyExperience(r.levels, state.level, state.experience, state.skillExperience, reward.Experience, reward.SkillExperience, state.skillPoints, state.statPoints)
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
	result.Experience = reward.Experience
	result.SkillExp = reward.SkillExperience
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

func lockDeliverItemReference(ctx context.Context, tx *sql.Tx, questID int) (uint32, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ITEM_REF_OBJECT_ID FROM REF_QUEST_DELIVER_ITEM_EX
		WHERE QUEST_ID=? AND RUNTIME_ENABLED=1 ORDER BY VARIANT_INDEX,MISSION_INDEX FOR SHARE`, questID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var itemRefID uint32
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&itemRefID); err != nil {
			return 0, err
		}
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if count != 1 || itemRefID == 0 {
		return 0, fmt.Errorf("%w: quest %d resolved %d enabled deliver-item objectives", ErrQuestReferenceUnsupported, questID, count)
	}
	return itemRefID, nil
}

func lockQuestRewardReference(ctx context.Context, tx *sql.Tx, questID int) (questRewardReference, error) {
	var reward questRewardReference
	var basic, item, checkCondition, checkCountry, checkClass, checkGender uint8
	var scriptExperience, scriptSkillExperience, scriptGold uint64
	var scriptItemCount uint16
	err := tx.QueryRowContext(ctx, `SELECT reward.GOLD,reward.EXPERIENCE,reward.SKILL_EXPERIENCE,reward.SKILL_POINTS,
		reward.ACADEMY_POINTS,reward.HWAN,reward.INVENTORY_SLOTS,reward.IS_BASIC_REWARD,reward.IS_ITEM_REWARD,
		reward.IS_CHECK_CONDITION,reward.IS_CHECK_COUNTRY,reward.IS_CHECK_CLASS,reward.IS_CHECK_GENDER,
		reward.ITEM_REWARD_TYPE,reward.SELECTION_COUNT,reward.PARAM_1,reward.PARAM_2,reward.PARAM_3,
		script.EXPERIENCE,script.SKILL_EXPERIENCE,script.GOLD,script.ITEM_COUNT
		FROM REF_QUEST_REWARD reward
		JOIN REF_QUEST_SCRIPT_REWARD script ON script.QUEST_ID=reward.QUEST_ID AND script.RUNTIME_ENABLED=1
		WHERE reward.QUEST_ID=? FOR SHARE`, questID).Scan(
		&reward.Gold, &reward.Experience, &reward.SkillExperience, &reward.SkillPoints, &reward.AcademyPoints,
		&reward.Hwan, &reward.InventorySlots, &basic, &item, &checkCondition, &checkCountry, &checkClass,
		&checkGender, &reward.ItemRewardType, &reward.SelectionCount, &reward.Param1, &reward.Param2, &reward.Param3,
		&scriptExperience, &scriptSkillExperience, &scriptGold, &scriptItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d", ErrQuestReferenceMissing, questID)
	}
	if err != nil {
		return questRewardReference{}, err
	}
	reward.IsBasicReward = basic != 0
	reward.IsItemReward = item != 0
	reward.IsCheckCondition = checkCondition != 0
	reward.IsCheckCountry = checkCountry != 0
	reward.IsCheckClass = checkClass != 0
	reward.IsCheckGender = checkGender != 0
	if reward.Experience != scriptExperience || reward.SkillExperience != scriptSkillExperience || reward.Gold != scriptGold || reward.IsItemReward != (scriptItemCount > 0) {
		return questRewardReference{}, fmt.Errorf("%w: quest %d SQL and script rewards disagree", ErrQuestReferenceUnsupported, questID)
	}
	if !reward.IsItemReward {
		return reward, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT item.ItemID,rewards.ACHIEVE_QUANTITY,scriptItem.ITEM_REF_OBJECT_ID,scriptItem.QUANTITY
		FROM REF_QUEST_REWARD_ITEM rewards
		JOIN ITEMDATA item ON item.CodeName=rewards.ITEM_CODE_NAME
		JOIN REF_QUEST_SCRIPT_REWARD script ON script.QUEST_ID=rewards.QUEST_ID AND script.RUNTIME_ENABLED=1
		JOIN REF_QUEST_SCRIPT_REWARD_ITEM scriptItem ON scriptItem.QUEST_ID=script.QUEST_ID
			AND scriptItem.VARIANT_INDEX=script.VARIANT_INDEX AND scriptItem.ITEM_CODE_NAME=rewards.ITEM_CODE_NAME
		WHERE rewards.QUEST_ID=? ORDER BY rewards.ID FOR SHARE`, questID)
	if err != nil {
		return questRewardReference{}, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var scriptItemRefID uint32
		var scriptQuantity int
		if err = rows.Scan(&reward.ItemRefID, &reward.ItemQuantity, &scriptItemRefID, &scriptQuantity); err != nil {
			return questRewardReference{}, err
		}
		if reward.ItemRefID != scriptItemRefID || reward.ItemQuantity != scriptQuantity {
			return questRewardReference{}, fmt.Errorf("%w: quest %d SQL and script reward items disagree", ErrQuestReferenceUnsupported, questID)
		}
	}
	if err = rows.Err(); err != nil {
		return questRewardReference{}, err
	}
	if count != 1 {
		return questRewardReference{}, fmt.Errorf("%w: quest %d resolved %d reward items", ErrQuestReferenceUnsupported, questID, count)
	}
	return reward, nil
}

func validateQuestRewardReference(questID int, reward questRewardReference) error {
	unsupported := !reward.IsBasicReward || reward.IsCheckCondition || reward.IsCheckCountry || reward.IsCheckClass || reward.IsCheckGender ||
		reward.SkillPoints != 0 || reward.AcademyPoints != 0 || reward.Hwan != 0 || reward.InventorySlots != 0 ||
		reward.ItemRewardType != 0 || reward.SelectionCount != 0 || reward.Param1 != 0 || reward.Param2 != 0 || reward.Param3 != 0
	if unsupported {
		return fmt.Errorf("%w: quest %d", ErrQuestReferenceUnsupported, questID)
	}
	switch questID {
	case QuestWeaponListID:
		if reward.IsItemReward || reward.ItemRefID != 0 || reward.ItemQuantity != 0 {
			return fmt.Errorf("%w: quest %d unexpected item reward", ErrQuestReferenceUnsupported, questID)
		}
	case QuestChulsanAngerID:
		if !reward.IsItemReward || reward.ItemRefID == 0 || reward.ItemQuantity != 1 {
			return fmt.Errorf("%w: quest %d requires one exact item reward", ErrQuestReferenceUnsupported, questID)
		}
	default:
		return fmt.Errorf("%w: quest %d has no verified completion flow", ErrQuestReferenceUnsupported, questID)
	}
	return nil
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
