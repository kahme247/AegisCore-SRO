package questwire

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

// BuildUpdate projects committed quest state into the binary-confirmed 0x30D5
// conditional layout. Both character actions and non-character event sources
// use this boundary so publication cannot drift by handler package.
func BuildUpdate(quest model.CharacterQuest, subtype byte) (network.Packet, error) {
	record, err := model.ProjectActiveQuestProtocol(quest)
	if err != nil {
		return network.Packet{}, err
	}
	return vsro188.BuildQuestUpdate(vsro188.QuestUpdateMessage{
		Subtype: subtype, QuestID: uint32(quest.QuestID), PackedQuestByte: record.PackedQuestByte,
		QuestDefinitionByte: record.QuestDefinitionByte, Flags: record.Flags, Status: record.Status,
		Missions: []vsro188.QuestMissionDelta{{
			EntryKey: record.EntryKey, TransitionCode: record.TransitionCode, Text: record.Text, Values: record.Values,
		}},
	})
}
