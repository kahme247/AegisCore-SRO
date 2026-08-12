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
	nameKey   string
	taskCount byte
}

func questObjectiveForProtocol(questID int) (questObjectiveProtocol, bool) {
	switch questID {
	case 3:
		return questObjectiveProtocol{nameKey: "SN_CON_QNO_CH_SMITH_1"}, true
	case 58:
		return questObjectiveProtocol{nameKey: "SN_CON_QNO_CH_SMITH_2", taskCount: 1}, true
	default:
		return questObjectiveProtocol{}, false
	}
}

// WriteActiveQuestRecord writes the record consumed by both character
// bootstrap and a 0x30D5 Add notification in the pinned vSRO 1.88 client.
func WriteActiveQuestRecord(packet *network.Packet, quest CharacterQuest) error {
	if packet == nil {
		return fmt.Errorf("active quest packet is required")
	}
	objective, ok := questObjectiveForProtocol(quest.QuestID)
	if !ok {
		return fmt.Errorf("quest %d has no verified active protocol definition", quest.QuestID)
	}
	if quest.Status != QuestStartedByUser && quest.Status != QuestReadyToTurnInByUser {
		return fmt.Errorf("quest %d status %d is not active", quest.QuestID, quest.Status)
	}
	if quest.AchievementCount == 0 || quest.AchievementCount > math.MaxUint8 {
		return fmt.Errorf("quest %d achievement count %d is outside 1..255", quest.QuestID, quest.AchievementCount)
	}
	if objective.taskCount != 0 && quest.Data1 > math.MaxUint32 {
		return fmt.Errorf("quest %d task progress %d exceeds uint32", quest.QuestID, quest.Data1)
	}

	packet.WriteByte(byte(quest.AchievementCount))
	packet.WriteByte(0) // RequiredShareParty
	packet.WriteByte(questRecordStatusFlag | questRecordObjectiveFlag)
	packet.WriteByte(byte(quest.Status))
	packet.WriteByte(1) // Objective count
	packet.WriteByte(1) // Objective ID
	packet.WriteByte(1) // In progress
	if err := packet.WriteString(objective.nameKey); err != nil {
		return fmt.Errorf("write quest %d objective name: %w", quest.QuestID, err)
	}
	packet.WriteByte(objective.taskCount)
	if objective.taskCount != 0 {
		packet.WriteUInt32(uint32(quest.Data1))
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
		case QuestCompleted, QuestCompletedMultipleTimes:
			completed = append(completed, quest)
		case QuestStartedByUser, QuestReadyToTurnInByUser:
			if _, ok := questObjectiveForProtocol(quest.QuestID); ok {
				active = append(active, quest)
			}
		case QuestActive, QuestReadyToTurnIn, QuestAbandoned, QuestUnavailable:
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
