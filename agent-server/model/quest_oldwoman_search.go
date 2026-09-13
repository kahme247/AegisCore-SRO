package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
)

// OldWomanSearchQuestID is the native QNO_RM_OLDWOMAN_5 definition.
const OldWomanSearchQuestID = 140
const OldWomanSearchHandler uint32 = 0x008A0970

const OldWomanSearchResultItemRefID uint32 = 9312
const OldWomanSearchTriggerItemRefID uint32 = 5940

var ErrOldWomanSearchState = errors.New("invalid QNO_RM_OLDWOMAN_5 search state")

const OldWomanSearchFailureTalkKey = "SN_TALK_QNO_RM_OLDWOMAN_5_18"

// OldWomanSearchActionResult is the committed projection of one matching
// world-object action. Added is non-nil only when the countdown resolved to a
// successful result-item grant. FailureTalkKey is populated only for a
// resolved probability failure.
type OldWomanSearchActionResult struct {
	Quest          CharacterQuest
	Inventory      Inventory
	Added          *QuestItemMutation
	Resolved       bool
	Succeeded      bool
	RewardReady    bool
	FailureTalkKey string
}

type OldWomanSearchPosition struct {
	Region int16
	X      float32
	Y      float32
	Z      float32
}

type OldWomanSearchStartResult struct {
	Quest     CharacterQuest
	Stage     byte
	Countdown byte
}

// OldWomanSearchStage is one hardcoded search sphere recovered from
// SR_GameServer.exe 0x008A0AA0.
type OldWomanSearchStage struct {
	Region        int16
	X             float32
	Y             float32
	Z             float32
	Radius        float32
	Countdown     byte
	SuccessChance byte
}

var OldWomanSearchStages = [...]OldWomanSearchStage{
	{Region: 24180, X: 1081, Y: 5775, Z: 1631, Radius: 270, Countdown: 5, SuccessChance: 25},
	{Region: 24427, X: 1282, Y: 5590, Z: 285, Radius: 280, Countdown: 10, SuccessChance: 30},
	{Region: 23664, X: 44, Y: 4767, Z: 1898, Radius: 300, Countdown: 15, SuccessChance: 40},
	{Region: 23153, X: 1825, Y: 4689, Z: 1871, Radius: 270, Countdown: 20, SuccessChance: 50},
	{Region: 22893, X: 1322, Y: 4378, Z: 1761, Radius: 270, Countdown: 25, SuccessChance: 60},
	{Region: 22132, X: 1023, Y: 5785, Z: 1845, Radius: 270, Countdown: 30, SuccessChance: 75},
	{Region: 22379, X: 1013, Y: 5910, Z: 85, Radius: 280, Countdown: 40, SuccessChance: 100},
}

// OldWomanSearchProgress mirrors the three byte fields accessed relative to
// the runtime four-word progress block. That block begins at runtime record
// +0x11 and is folded into CHAR_QUEST.QUEST_DATA_1/2 by save-now.
type OldWomanSearchProgress struct {
	Step      byte // QuestData1 byte 0; native runtime progress +0x00.
	Complete  byte // QuestData1 byte 1; native runtime progress +0x01.
	Stage     byte // QuestData1 byte 2; native runtime progress +0x02.
	Ready     byte // QuestData1 byte 3; native runtime progress +0x03.
	Countdown byte // QuestData1 byte 4; native runtime progress +0x04.
}

func DecodeOldWomanSearchProgress(data1 uint64) OldWomanSearchProgress {
	return OldWomanSearchProgress{
		Step:      byte(data1),
		Complete:  byte(data1 >> 8),
		Stage:     byte(data1 >> 16),
		Ready:     byte(data1 >> 24),
		Countdown: byte(data1 >> 32),
	}
}

// EncodeOldWomanSearchProgress updates only the three native byte fields and
// preserves every opaque neighboring byte in QuestData1.
func EncodeOldWomanSearchProgress(data1 uint64, progress OldWomanSearchProgress) uint64 {
	const fieldMask = uint64(0xffffffffff)
	return data1&^fieldMask |
		uint64(progress.Step) |
		uint64(progress.Complete)<<8 |
		uint64(progress.Stage)<<16 |
		uint64(progress.Ready)<<24 |
		uint64(progress.Countdown)<<32
}

// AdvanceOldWomanSearchAction reproduces SR_GameServer 0x008A0970 and the
// completion helper at 0x008A0E60. roll is the native rand()%101 result and
// must therefore be in the inclusive range 0..100.
func AdvanceOldWomanSearchAction(data1 uint64, roll byte) (next uint64, resolved, succeeded, rewardReady bool, err error) {
	progress := DecodeOldWomanSearchProgress(data1)
	if progress.Stage >= byte(len(OldWomanSearchStages)) || progress.Countdown == 0 || roll > 100 {
		return data1, false, false, false, ErrOldWomanSearchState
	}
	progress.Countdown--
	if progress.Countdown != 0 {
		return EncodeOldWomanSearchProgress(data1, progress), false, false, false, nil
	}
	resolved = true
	if roll > OldWomanSearchStages[progress.Stage].SuccessChance {
		return EncodeOldWomanSearchProgress(data1, progress), true, false, false, nil
	}
	progress.Step++
	succeeded = true
	// Native 0x008A0E60 promotes after stage seven increments step 7 to 8.
	if progress.Step == 8 {
		progress.Complete = 1
		progress.Ready = 1
		rewardReady = true
	}
	return EncodeOldWomanSearchProgress(data1, progress), true, true, rewardReady, nil
}

// AdvanceOldWomanSearchAction commits one event-0x18 callback. Inventory
// admission and quest progress share one transaction so an inventory-full or
// failed insert cannot consume the final action or increment the native step.
func (r *SQLQuestRuntimeRepository) AdvanceOldWomanSearchAction(ctx context.Context, characterID int, roll byte) (OldWomanSearchActionResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 || roll > 100 {
		return OldWomanSearchActionResult{}, ErrOldWomanSearchState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return OldWomanSearchActionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	quest, err := lockCharacterQuest(ctx, tx, characterID, OldWomanSearchQuestID)
	if err != nil || (quest.Status != QuestProgressed && quest.Status != QuestRewardReady) {
		return OldWomanSearchActionResult{}, ErrOldWomanSearchState
	}
	nextData, resolved, succeeded, rewardReady, err := AdvanceOldWomanSearchAction(quest.Data1, roll)
	if err != nil {
		return OldWomanSearchActionResult{}, err
	}
	result := OldWomanSearchActionResult{Quest: quest, Resolved: resolved, Succeeded: succeeded, RewardReady: rewardReady}
	var added *QuestItemMutation
	if succeeded {
		capacity, capacityErr := lockQuestCharacterCapacity(ctx, tx, characterID)
		if capacityErr != nil {
			return OldWomanSearchActionResult{}, capacityErr
		}
		items, inventoryErr := lockQuestInventory(ctx, tx, characterID)
		if inventoryErr != nil {
			return OldWomanSearchActionResult{}, inventoryErr
		}
		slot, ok := freeQuestInventorySlot(items, capacity)
		if !ok {
			return OldWomanSearchActionResult{}, ErrQuestInventoryFull
		}
		item, insertErr := insertQuestItem(ctx, tx, characterID, slot, OldWomanSearchResultItemRefID)
		if insertErr != nil {
			return OldWomanSearchActionResult{}, insertErr
		}
		mutation := QuestItemMutation{Slot: slot, Item: item}
		added = &mutation
	}

	previousStatus := quest.Status
	if rewardReady && quest.Status != QuestRewardReady {
		quest.Status, err = TransitionQuest(quest.Status, QuestTransitionRewardReady)
	} else if succeeded && quest.Status != QuestRewardReady {
		quest.Status, err = TransitionQuest(quest.Status, QuestTransitionProgress)
	}
	if err != nil {
		return OldWomanSearchActionResult{}, err
	}
	quest.Data1 = nextData
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Status, quest.Data1,
		characterID, quest.QuestID, previousStatus, result.Quest.Data1)
	if err != nil {
		return OldWomanSearchActionResult{}, err
	}
	if err = requireQuestRow(updated, "advance QNO_RM_OLDWOMAN_5 action"); err != nil {
		return OldWomanSearchActionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return OldWomanSearchActionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Quest = quest
	result.Added = added
	if resolved && !succeeded {
		result.FailureTalkKey = OldWomanSearchFailureTalkKey
	}
	if added != nil {
		result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
		if err != nil {
			return OldWomanSearchActionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
		}
		if item, exists := result.Inventory.Items[added.Slot]; exists {
			result.Added.Item = item
		}
	}
	return result, nil
}

func ClearOldWomanSearchCountdown(data1 uint64) uint64 {
	return data1 &^ (uint64(0xff) << 32)
}

func OldWomanSearchPositionMatches(stage OldWomanSearchStage, position OldWomanSearchPosition) bool {
	if position.Region != stage.Region {
		return false
	}
	dx, dy, dz := float64(position.X-stage.X), float64(position.Y-stage.Y), float64(position.Z-stage.Z)
	return dx*dx+dy*dy+dz*dz <= math.Pow(float64(stage.Radius), 2)
}

// StartOldWomanSearchAction reproduces the admission portion of
// SR_GameServer 0x008A0AA0 after the caller has proven that the interacted
// ground object is trigger item 5940. It reserves capacity by locking the
// complete inventory; the ground-object lifecycle remains outside this SQL
// transaction.
func (r *SQLQuestRuntimeRepository) StartOldWomanSearchAction(ctx context.Context, characterID int) (OldWomanSearchStartResult, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return OldWomanSearchStartResult{}, ErrOldWomanSearchState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return OldWomanSearchStartResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, OldWomanSearchQuestID)
	if err != nil || (quest.Status != QuestProgressed && quest.Status != QuestRewardReady) {
		return OldWomanSearchStartResult{}, ErrOldWomanSearchState
	}
	progress := DecodeOldWomanSearchProgress(quest.Data1)
	if progress.Step < 1 || progress.Step > byte(len(OldWomanSearchStages)) || progress.Countdown != 0 {
		return OldWomanSearchStartResult{}, ErrOldWomanSearchState
	}
	stageIndex := progress.Step - 1
	stage := OldWomanSearchStages[stageIndex]
	var position OldWomanSearchPosition
	if err = tx.QueryRowContext(ctx, `SELECT FK_REGION,POS_X,POS_Y,POS_Z FROM SRO_SHARD.CHAR
		WHERE ID=? FOR UPDATE`, characterID).Scan(&position.Region, &position.X, &position.Y, &position.Z); err != nil {
		return OldWomanSearchStartResult{}, err
	}
	if !OldWomanSearchPositionMatches(stage, position) {
		return OldWomanSearchStartResult{}, ErrOldWomanSearchState
	}
	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return OldWomanSearchStartResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return OldWomanSearchStartResult{}, err
	}
	resultCount := 0
	for _, item := range items {
		if item.refID == OldWomanSearchResultItemRefID && item.stack > 0 {
			resultCount += item.stack
		}
	}
	if resultCount >= len(OldWomanSearchStages) {
		return OldWomanSearchStartResult{}, ErrOldWomanSearchState
	}
	if _, available := freeQuestInventorySlot(items, capacity); !available {
		return OldWomanSearchStartResult{}, ErrQuestInventoryFull
	}
	previousData := quest.Data1
	progress.Stage, progress.Countdown = stageIndex, stage.Countdown
	quest.Data1 = EncodeOldWomanSearchProgress(quest.Data1, progress)
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Data1,
		characterID, quest.QuestID, quest.Status, previousData)
	if err != nil {
		return OldWomanSearchStartResult{}, err
	}
	if err = requireQuestRow(updated, "start QNO_RM_OLDWOMAN_5 action"); err != nil {
		return OldWomanSearchStartResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return OldWomanSearchStartResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return OldWomanSearchStartResult{Quest: quest, Stage: stageIndex, Countdown: stage.Countdown}, nil
}

// RegisterOldWomanSearchEvents binds the native event-0x17 cancellation
// mutation. SR_GameServer 0x008C8770 clears runtime progress byte +0x04 and
// immediately saves; this is QuestData1 byte 4 after save folding.
func RegisterOldWomanSearchEvents(dispatcher *SQLQuestEventDispatcher) error {
	return dispatcher.Register(OldWomanSearchQuestID, QuestAuthoredLocalAction,
		func(ctx context.Context, tx *sql.Tx, characterID int, quest CharacterQuest) error {
			cleared := ClearOldWomanSearchCountdown(quest.Data1)
			result, err := tx.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR_QUEST SET QUEST_DATA_1=?
				WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`,
				cleared, characterID, quest.QuestID, quest.Status, quest.Data1)
			if err != nil {
				return err
			}
			return requireQuestRow(result, "cancel QNO_RM_OLDWOMAN_5 local action")
		})
}
