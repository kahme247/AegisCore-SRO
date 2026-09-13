package model

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNativeCatchActionState = errors.New("invalid native quest catch-action state")

type NativeCatchRollPredicate byte

const (
	NativeCatchRollGuaranteed NativeCatchRollPredicate = iota
	NativeCatchRollLessEqual
	NativeCatchRollGreaterEqual
)

type NativeCatchSphere struct {
	Region int16
	X      float32
	Y      float32
	Z      float32
	Radius float32
}

// NativeCatchActionDefinition describes the TypeID 3/3/9 ground actions
// whose event-0x18 handlers share the ten-tick QuestData1 byte-4 state.
// Handler and StartHandler are SR_GameServer effective addresses at image
// base 0x00401000.
type NativeCatchActionDefinition struct {
	QuestID        int
	CodeName       string
	Handler        uint32
	StartHandler   uint32
	TriggerRefID   uint32
	ResultRefID    uint32
	ResultCap      int
	Countdown      byte
	Predicate      NativeCatchRollPredicate
	Threshold      byte
	FailureTalkKey string
	SuccessTalkKey string
	CompletesQuest bool
	Spheres        []NativeCatchSphere
}

var NativeCatchActionDefinitions = [...]NativeCatchActionDefinition{
	{
		QuestID: 163, CodeName: "QNO_EU_EASTEU_1", Handler: 0x008AA630, StartHandler: 0x008AA700,
		TriggerRefID: 10417, ResultRefID: 10414, ResultCap: 5, Countdown: 10,
		Predicate: NativeCatchRollLessEqual, Threshold: 50, FailureTalkKey: "SN_TALK_QNO_EU_EASTEU_1_12",
		Spheres: []NativeCatchSphere{{Region: 27211, X: 249, Y: 24, Z: 759, Radius: 200}},
	},
	{
		QuestID: 181, CodeName: "QNO_EU_EASTEU_19", Handler: 0x008B2A10, StartHandler: 0x008B2AB0,
		TriggerRefID: 10441, ResultRefID: 10439, ResultCap: 20, Countdown: 10,
		Predicate: NativeCatchRollGuaranteed,
		Spheres:   []NativeCatchSphere{{Region: 25420, X: 126, Y: -41, Z: 1863, Radius: 400}},
	},
	{
		QuestID: 203, CodeName: "QNO_CA_WAREHOUSE_1", Handler: 0x008C86A0, StartHandler: 0x008C87B0,
		TriggerRefID: 10476, ResultRefID: 10475, ResultCap: 10, Countdown: 10,
		Predicate: NativeCatchRollLessEqual, Threshold: 67, FailureTalkKey: "SN_TALK_QNO_CA_WAREHOUSE_1_09",
		Spheres: []NativeCatchSphere{{Region: 27237, X: 1057, Y: 218, Z: 634, Radius: 1000}},
	},
	{
		QuestID: 207, CodeName: "QNO_EU_IVY_2", Handler: 0x008BB5F0, StartHandler: 0x008BB6C0,
		TriggerRefID: 10484, ResultRefID: 10483, ResultCap: 20, Countdown: 10,
		Predicate: NativeCatchRollGreaterEqual, Threshold: 50, FailureTalkKey: "SN_TALK_QNO_EU_IVY_2_10",
		Spheres: []NativeCatchSphere{{Region: 25950, X: 253, Y: 85, Z: 449, Radius: 600}},
	},
	{
		QuestID: 239, CodeName: "QNO_CA_WAREHOUSE_3", Handler: 0x008CA6E0, StartHandler: 0x008CA7B0,
		TriggerRefID: 10506, ResultRefID: 10507, ResultCap: 20, Countdown: 10,
		Predicate: NativeCatchRollGreaterEqual, Threshold: 50, FailureTalkKey: "SN_TALK_QNO_CA_WAREHOUSE_3_08",
		Spheres: []NativeCatchSphere{{Region: 26219, X: 1525, Y: 152, Z: 1495, Radius: 500}},
	},
	{
		QuestID: 286, CodeName: "QNO_TQ_ORNAMENT_3", Handler: 0x008D6B10, StartHandler: 0x008D6BE0,
		TriggerRefID: 10562, ResultRefID: 10563, ResultCap: 10, Countdown: 10,
		Predicate: NativeCatchRollLessEqual, Threshold: 50, FailureTalkKey: "SN_TALK_QNO_TQ_ORNAMENT_3_13",
		Spheres: []NativeCatchSphere{
			{Region: -32763, X: -16604, Y: 1150, Z: 9113, Radius: 300},
			{Region: -32763, X: 8497, Y: 1171, Z: 16664, Radius: 300},
			{Region: -32763, X: 16754, Y: 1163, Z: -8286, Radius: 300},
			{Region: -32763, X: -8440, Y: 1204, Z: -17074, Radius: 300},
		},
	},
	{
		QuestID: 357, CodeName: "QNO_CH_HWAN_2_4", Handler: 0x009038D0, StartHandler: 0x00903730,
		TriggerRefID: 23367, ResultRefID: 23368, ResultCap: 1, Countdown: 10,
		Predicate: NativeCatchRollLessEqual, Threshold: 10, FailureTalkKey: "SN_TALK_QNO_CH_HWAN_2_4_10",
		SuccessTalkKey: "SN_TALK_QNO_CH_HWAN_2_4_08", CompletesQuest: true,
		Spheres: []NativeCatchSphere{{Region: 25954, X: 1672, Y: 180, Z: 232, Radius: 300}},
	},
	{
		QuestID: 371, CodeName: "QNO_EU_HWAN_2_4", Handler: 0x00909600, StartHandler: 0x00909460,
		TriggerRefID: 23367, ResultRefID: 23368, ResultCap: 1, Countdown: 10,
		Predicate: NativeCatchRollLessEqual, Threshold: 10, FailureTalkKey: "SN_TALK_QNO_EU_HWAN_2_4_10",
		SuccessTalkKey: "SN_TALK_QNO_EU_HWAN_2_4_08", CompletesQuest: true,
		Spheres: []NativeCatchSphere{{Region: 25954, X: 1672, Y: 180, Z: 232, Radius: 300}},
	},
}

func NativeCatchDefinitionsForTrigger(refID uint32) []NativeCatchActionDefinition {
	definitions := make([]NativeCatchActionDefinition, 0, 2)
	for _, definition := range NativeCatchActionDefinitions {
		if definition.TriggerRefID == refID {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

func NativeCatchDefinitionForQuest(questID int) (NativeCatchActionDefinition, bool) {
	for _, definition := range NativeCatchActionDefinitions {
		if definition.QuestID == questID {
			return definition, true
		}
	}
	return NativeCatchActionDefinition{}, false
}

func (definition NativeCatchActionDefinition) RollSucceeds(roll byte) bool {
	if roll > 100 {
		return false
	}
	switch definition.Predicate {
	case NativeCatchRollGuaranteed:
		return true
	case NativeCatchRollLessEqual:
		return roll <= definition.Threshold
	case NativeCatchRollGreaterEqual:
		return roll >= definition.Threshold
	default:
		return false
	}
}

func (definition NativeCatchActionDefinition) PositionMatches(position OldWomanSearchPosition) bool {
	for _, sphere := range definition.Spheres {
		if OldWomanSearchPositionMatches(OldWomanSearchStage{
			Region: sphere.Region, X: sphere.X, Y: sphere.Y, Z: sphere.Z, Radius: sphere.Radius,
		}, position) {
			return true
		}
	}
	return false
}

func StartNativeCatchAction(definition NativeCatchActionDefinition, data1 uint64, position OldWomanSearchPosition, resultCount int, hasFreeSlot bool) (uint64, error) {
	if definition.Countdown == 0 || definition.ResultCap <= 0 || resultCount < 0 || resultCount >= definition.ResultCap || !hasFreeSlot || !definition.PositionMatches(position) {
		return data1, ErrNativeCatchActionState
	}
	if byte(data1>>32) != 0 {
		return data1, ErrNativeCatchActionState
	}
	return data1&^(uint64(0xff)<<32) | uint64(definition.Countdown)<<32, nil
}

type NativeCatchActionTransition struct {
	Data1       uint64
	Resolved    bool
	Succeeded   bool
	RewardReady bool
}

type NativeCatchStartResult struct {
	Quest      CharacterQuest
	Definition NativeCatchActionDefinition
	Countdown  byte
}

type NativeCatchActionResult struct {
	Quest          CharacterQuest
	Definition     NativeCatchActionDefinition
	Inventory      Inventory
	Added          *QuestItemMutation
	Resolved       bool
	Succeeded      bool
	RewardReady    bool
	FailureTalkKey string
	SuccessTalkKey string
}

// AdvanceNativeCatchAction applies one SR_GameServer event-0x18 callback.
// Byte 4 is decremented and saved on every callback. The Hwan pair additionally
// promotes runtime bytes 1 and 3 after a successful final roll.
func AdvanceNativeCatchAction(definition NativeCatchActionDefinition, data1 uint64, roll byte) (NativeCatchActionTransition, error) {
	countdown := byte(data1 >> 32)
	if countdown == 0 || roll > 100 || definition.Countdown == 0 || definition.ResultRefID == 0 {
		return NativeCatchActionTransition{Data1: data1}, ErrNativeCatchActionState
	}
	countdown--
	next := data1&^(uint64(0xff)<<32) | uint64(countdown)<<32
	transition := NativeCatchActionTransition{Data1: next}
	if countdown != 0 {
		return transition, nil
	}
	transition.Resolved = true
	transition.Succeeded = definition.RollSucceeds(roll)
	if transition.Succeeded {
		if definition.CompletesQuest {
			transition.Data1 |= uint64(1)<<8 | uint64(1)<<24
			transition.RewardReady = true
		} else {
			count := uint16(transition.Data1)
			if definition.ResultCap <= 0 || count >= uint16(definition.ResultCap) {
				return NativeCatchActionTransition{Data1: data1}, ErrNativeCatchActionState
			}
			count++
			transition.Data1 = transition.Data1&^uint64(0xffff) | uint64(count)
			transition.RewardReady = int(count) >= definition.ResultCap
		}
	}
	return transition, nil
}

func (r *SQLQuestRuntimeRepository) StartNativeCatchAction(ctx context.Context, characterID, questID int) (NativeCatchStartResult, error) {
	definition, exists := NativeCatchDefinitionForQuest(questID)
	if r == nil || r.shard == nil || characterID <= 0 || !exists {
		return NativeCatchStartResult{}, ErrNativeCatchActionState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return NativeCatchStartResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil || !quest.Status.ClientActive() {
		return NativeCatchStartResult{}, ErrNativeCatchActionState
	}
	var position OldWomanSearchPosition
	if err = tx.QueryRowContext(ctx, `SELECT FK_REGION,POS_X,POS_Y,POS_Z FROM SRO_SHARD.CHAR
		WHERE ID=? FOR UPDATE`, characterID).Scan(&position.Region, &position.X, &position.Y, &position.Z); err != nil {
		return NativeCatchStartResult{}, err
	}
	capacity, err := lockQuestCharacterCapacity(ctx, tx, characterID)
	if err != nil {
		return NativeCatchStartResult{}, err
	}
	items, err := lockQuestInventory(ctx, tx, characterID)
	if err != nil {
		return NativeCatchStartResult{}, err
	}
	resultCount := 0
	for _, item := range items {
		if item.refID == definition.ResultRefID && item.stack > 0 {
			resultCount += item.stack
		}
	}
	_, hasFreeSlot := freeQuestInventorySlot(items, capacity)
	previousData := quest.Data1
	quest.Data1, err = StartNativeCatchAction(definition, quest.Data1, position, resultCount, hasFreeSlot)
	if err != nil {
		return NativeCatchStartResult{}, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Data1,
		characterID, questID, quest.Status, previousData)
	if err != nil {
		return NativeCatchStartResult{}, err
	}
	if err = requireQuestRow(updated, "start native catch action"); err != nil {
		return NativeCatchStartResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return NativeCatchStartResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	return NativeCatchStartResult{Quest: quest, Definition: definition, Countdown: definition.Countdown}, nil
}

func (r *SQLQuestRuntimeRepository) AdvanceNativeCatchAction(ctx context.Context, characterID, questID int, roll byte) (NativeCatchActionResult, error) {
	definition, exists := NativeCatchDefinitionForQuest(questID)
	if r == nil || r.shard == nil || characterID <= 0 || roll > 100 || !exists {
		return NativeCatchActionResult{}, ErrNativeCatchActionState
	}
	ctx = normalizeEconomyContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return NativeCatchActionResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	quest, err := lockCharacterQuest(ctx, tx, characterID, questID)
	if err != nil || !quest.Status.ClientActive() {
		return NativeCatchActionResult{}, ErrNativeCatchActionState
	}
	transition, err := AdvanceNativeCatchAction(definition, quest.Data1, roll)
	if err != nil {
		return NativeCatchActionResult{}, err
	}
	result := NativeCatchActionResult{
		Quest: quest, Definition: definition, Resolved: transition.Resolved,
		Succeeded: transition.Succeeded, RewardReady: transition.RewardReady,
	}
	if transition.Resolved && !transition.Succeeded {
		result.FailureTalkKey = definition.FailureTalkKey
	}
	if transition.Succeeded {
		capacity, capacityErr := lockQuestCharacterCapacity(ctx, tx, characterID)
		if capacityErr != nil {
			return NativeCatchActionResult{}, capacityErr
		}
		items, inventoryErr := lockQuestInventory(ctx, tx, characterID)
		if inventoryErr != nil {
			return NativeCatchActionResult{}, inventoryErr
		}
		slot, available := freeQuestInventorySlot(items, capacity)
		if !available {
			return NativeCatchActionResult{}, ErrQuestInventoryFull
		}
		item, insertErr := insertQuestItem(ctx, tx, characterID, slot, definition.ResultRefID)
		if insertErr != nil {
			return NativeCatchActionResult{}, insertErr
		}
		mutation := QuestItemMutation{Slot: slot, Item: item}
		result.Added = &mutation
		result.SuccessTalkKey = definition.SuccessTalkKey
	}
	previousStatus, previousData := quest.Status, quest.Data1
	if transition.RewardReady && quest.Status != QuestRewardReady {
		quest.Status, err = TransitionQuest(quest.Status, QuestTransitionRewardReady)
	} else if transition.Succeeded && quest.Status != QuestRewardReady {
		quest.Status, err = TransitionQuest(quest.Status, QuestTransitionProgress)
	}
	if err != nil {
		return NativeCatchActionResult{}, err
	}
	quest.Data1 = transition.Data1
	updated, err := tx.ExecContext(ctx, `UPDATE CHAR_QUEST SET STATUS=?,QUEST_DATA_1=?
		WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`, quest.Status, quest.Data1,
		characterID, questID, previousStatus, previousData)
	if err != nil {
		return NativeCatchActionResult{}, err
	}
	if err = requireQuestRow(updated, "advance native catch action"); err != nil {
		return NativeCatchActionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return NativeCatchActionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
	}
	committed = true
	result.Quest = quest
	if result.Added != nil {
		result.Inventory, err = loadQuestInventory(ctx, r.shard, characterID)
		if err != nil {
			return NativeCatchActionResult{}, errors.Join(ErrQuestOutcomeUnknown, err)
		}
		if item, found := result.Inventory.Items[result.Added.Slot]; found {
			result.Added.Item = item
		}
	}
	return result, nil
}

// RegisterNativeCatchEvents binds the shared event-0x17 handler at
// SR_GameServer 0x008C8770. Every recovered ten-tick catch-action vtable uses
// this slot immediately before its quest-specific event-0x18 override.
func RegisterNativeCatchEvents(dispatcher *SQLQuestEventDispatcher) error {
	if dispatcher == nil {
		return ErrQuestRuntimeUnavailable
	}
	for _, definition := range NativeCatchActionDefinitions {
		questID := definition.QuestID
		if err := dispatcher.Register(questID, QuestAuthoredLocalAction,
			func(ctx context.Context, tx *sql.Tx, characterID int, quest CharacterQuest) error {
				cleared := quest.Data1 &^ (uint64(0xff) << 32)
				result, err := tx.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR_QUEST SET QUEST_DATA_1=?
					WHERE FK_CHAR=? AND QUEST_ID=? AND STATUS=? AND QUEST_DATA_1=?`,
					cleared, characterID, questID, quest.Status, quest.Data1)
				if err != nil {
					return err
				}
				return requireQuestRow(result, "cancel native catch action")
			}); err != nil {
			return err
		}
	}
	return nil
}
