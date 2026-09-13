package model

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
)

const (
	questRecordStatusFlag    = byte(0x08)
	questRecordObjectiveFlag = byte(0x10)
)

type questObjectiveProtocol struct {
	nameKeys       []string
	taskCount      byte
	sharedProgress bool
}

// ActiveQuestProtocolRecord is the binary-confirmed conditional tail shared
// by a 0x30D5 create/delta and the character bootstrap quest log. Keeping the
// projection in one place prevents live progress packets and login hydration
// from assigning different meanings to the same persisted quest state.
type ActiveQuestProtocolRecord struct {
	PackedQuestByte     byte
	QuestDefinitionByte byte
	Flags               byte
	Status              byte
	EntryKey            byte
	TransitionCode      byte
	Text                string
	Values              []uint32
}

type ActiveQuestMissionRecord struct {
	EntryKey       byte
	TransitionCode byte
	Text           string
	Values         []uint32
}

func questObjectiveForProtocol(quest CharacterQuest) (questObjectiveProtocol, bool) {
	if quest.QuestCodeName == "QNO_WC_FERRY4_1" && quest.TaskCount == 0 && quest.ObjectiveKey == "" {
		return questObjectiveProtocol{}, true
	}
	if quest.TaskCount > 1 && int(quest.TaskCount) <= len(quest.ObjectiveKeys) {
		keys := quest.ObjectiveKeys[:quest.TaskCount]
		for _, key := range keys {
			if key == "" {
				return questObjectiveProtocol{}, false
			}
		}
		return questObjectiveProtocol{nameKeys: append([]string(nil), keys...), taskCount: quest.TaskCount, sharedProgress: quest.SharedProgress}, true
	}
	if quest.ObjectiveKey != "" && quest.TaskCount <= 1 {
		return questObjectiveProtocol{nameKeys: []string{quest.ObjectiveKey}, taskCount: quest.TaskCount}, true
	}
	switch quest.QuestID {
	case 3:
		return questObjectiveProtocol{nameKeys: []string{"SN_CON_QNO_CH_SMITH_1"}}, true
	case 58:
		return questObjectiveProtocol{nameKeys: []string{"SN_CON_QNO_CH_SMITH_2"}, taskCount: 1}, true
	default:
		return questObjectiveProtocol{}, false
	}
}

func ProjectActiveQuestProtocol(quest CharacterQuest) (ActiveQuestProtocolRecord, error) {
	_, ok := questObjectiveForProtocol(quest)
	if !ok {
		return ActiveQuestProtocolRecord{}, fmt.Errorf("quest %d has no verified active protocol definition", quest.QuestID)
	}
	if !quest.Status.ClientActive() {
		return ActiveQuestProtocolRecord{}, fmt.Errorf("quest %d status %d is not active", quest.QuestID, quest.Status)
	}
	if quest.AchievementCount > math.MaxUint8 {
		return ActiveQuestProtocolRecord{}, fmt.Errorf("quest %d achievement count %d exceeds 255", quest.QuestID, quest.AchievementCount)
	}
	record := ActiveQuestProtocolRecord{
		PackedQuestByte: byte(quest.AchievementCount), Flags: questRecordStatusFlag | questRecordObjectiveFlag,
		Status: byte(quest.Status), EntryKey: 1, TransitionCode: 1,
	}
	if quest.Status == QuestRewardReady {
		record.TransitionCode = 2
	}
	missions, err := ProjectActiveQuestMissionProtocol(quest)
	if err != nil {
		return ActiveQuestProtocolRecord{}, err
	}
	if len(missions) == 1 {
		record.EntryKey = missions[0].EntryKey
		record.TransitionCode = missions[0].TransitionCode
		record.Text = missions[0].Text
		record.Values = missions[0].Values
	}
	return record, nil
}

// ProjectActiveQuestMissionProtocol is shared by login hydration and live
// 0x30D5 deltas. The client localizes Text as an SN_CON_* key and formats the
// single scalar value into that localized mission template.
func ProjectActiveQuestMissionProtocol(quest CharacterQuest) ([]ActiveQuestMissionRecord, error) {
	objective, ok := questObjectiveForProtocol(quest)
	if !ok || !quest.Status.ClientActive() {
		return nil, fmt.Errorf("quest %d has no active mission protocol definition", quest.QuestID)
	}
	transition := byte(1)
	if quest.Status == QuestRewardReady {
		transition = 2
	}
	missions := make([]ActiveQuestMissionRecord, 0, len(objective.nameKeys))
	for index, key := range objective.nameKeys {
		mission := ActiveQuestMissionRecord{EntryKey: 1, TransitionCode: transition, Text: key}
		if objective.taskCount != 0 {
			if len(objective.nameKeys) == 1 && quest.Data1 > math.MaxUint32 {
				return nil, fmt.Errorf("quest %d task progress %d exceeds uint32", quest.QuestID, quest.Data1)
			}
			value := uint32(quest.Data1)
			if len(objective.nameKeys) > 1 && !objective.sharedProgress {
				value = uint32((quest.Data1 >> (16 * index)) & 0xffff)
			}
			mission.Values = []uint32{value}
		}
		missions = append(missions, mission)
	}
	return missions, nil
}

// WriteActiveQuestRecord writes the record consumed by both character
// bootstrap and a 0x30D5 Add notification in the pinned vSRO 1.88 client.
func WriteActiveQuestRecord(packet *network.Packet, quest CharacterQuest) error {
	if packet == nil {
		return fmt.Errorf("active quest packet is required")
	}
	record, err := ProjectActiveQuestProtocol(quest)
	if err != nil {
		return err
	}
	packet.WriteByte(record.PackedQuestByte)
	packet.WriteByte(record.QuestDefinitionByte)
	packet.WriteByte(record.Flags)
	packet.WriteByte(record.Status)
	missions, err := ProjectActiveQuestMissionProtocol(quest)
	if err != nil {
		return err
	}
	packet.WriteByte(byte(len(missions)))
	for _, mission := range missions {
		packet.WriteByte(mission.EntryKey)
		packet.WriteByte(mission.TransitionCode)
		if err := packet.WriteString(mission.Text); err != nil {
			return fmt.Errorf("write quest %d objective name: %w", quest.QuestID, err)
		}
		packet.WriteByte(byte(len(mission.Values)))
		for _, value := range mission.Values {
			packet.WriteUInt32(value)
		}
	}
	return nil
}

// WriteQuestLog writes completed IDs followed by active records. Only active
// quests with recovered protocol definitions are exposed to the client.
func WriteQuestLog(packet *network.Packet, quests []CharacterQuest) error {
	if packet == nil {
		return fmt.Errorf("quest log packet is required")
	}
	completed := make([]CharacterQuest, 0, len(quests))
	active := make([]CharacterQuest, 0, len(quests))
	for _, quest := range quests {
		if quest.QuestID <= 0 {
			return fmt.Errorf("invalid quest ID %d", quest.QuestID)
		}
		switch quest.Status {
		case QuestInactive:
			if quest.AchievementCount != 0 {
				completed = append(completed, quest)
			}
		case QuestClosed:
			completed = append(completed, quest)
		case QuestActive, QuestProgressed, QuestRewardReady:
			if _, ok := questObjectiveForProtocol(quest); ok {
				active = append(active, quest)
			}
		case QuestTransient2, QuestTransient3, QuestAborted:
			// Initialized, historical, and non-player-started rows are absent.
		default:
			return fmt.Errorf("quest %d has invalid status %d", quest.QuestID, quest.Status)
		}
	}
	if len(completed) > 0xffff {
		return fmt.Errorf("completed quest count %d exceeds protocol limit", len(completed))
	}
	packet.WriteUInt16(uint16(len(completed)))
	for _, quest := range completed {
		packet.WriteUInt32(uint32(quest.QuestID))
	}
	if len(active) > math.MaxUint8 {
		return fmt.Errorf("active quest count %d exceeds protocol limit", len(active))
	}
	packet.WriteByte(byte(len(active)))
	for _, quest := range active {
		packet.WriteUInt32(uint32(quest.QuestID))
		if err := WriteActiveQuestRecord(packet, quest); err != nil {
			return err
		}
	}
	return nil
}
