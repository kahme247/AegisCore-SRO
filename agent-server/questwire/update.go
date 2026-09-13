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
	missionRecords, err := model.ProjectActiveQuestMissionProtocol(quest)
	if err != nil {
		return network.Packet{}, err
	}
	missions := make([]vsro188.QuestMissionDelta, len(missionRecords))
	for index, mission := range missionRecords {
		missions[index] = vsro188.QuestMissionDelta{
			EntryKey: mission.EntryKey, TransitionCode: mission.TransitionCode,
			Text: mission.Text, Values: mission.Values,
		}
	}
	return vsro188.BuildQuestUpdate(vsro188.QuestUpdateMessage{
		Subtype: subtype, QuestID: uint32(quest.QuestID), PackedQuestByte: record.PackedQuestByte,
		QuestDefinitionByte: record.QuestDefinitionByte, Flags: record.Flags, Status: record.Status,
		Missions: missions,
	})
}
