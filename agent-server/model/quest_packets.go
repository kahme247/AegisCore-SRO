package model

import (
	"fmt"

	"github.com/ferdoran/go-sro/framework/network"
)

// WriteQuestLog writes the vSRO 1.88 character-data quest section. Completed
// quest IDs precede the active quest records. Active records are deliberately
// withheld until the compiled Lua definitions can provide the exact objective
// flags and payloads; a guessed status-only record can desynchronise the stock
// client. Persistence still retains those quests for the future script runtime.
func WriteQuestLog(packet *network.Packet, quests []CharacterQuest) error {
	if packet == nil {
		return fmt.Errorf("quest log packet is required")
	}
	completed := make([]CharacterQuest, 0, len(quests))
	for _, quest := range quests {
		if quest.QuestID <= 0 {
			return fmt.Errorf("invalid quest ID %d", quest.QuestID)
		}
		switch quest.Status {
		case QuestCompleted, QuestCompletedMultipleTimes:
			completed = append(completed, quest)
		case QuestActive, QuestReadyToTurnIn, QuestStartedByUser, QuestReadyToTurnInByUser:
			// Exact objective/type payload is not yet capture-backed.
		case QuestAbandoned, QuestUnavailable:
			// Historical/non-active rows are intentionally absent from the log.
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
	packet.WriteByte(0) // Active quest count (fail closed; see comment above).
	return nil
}
