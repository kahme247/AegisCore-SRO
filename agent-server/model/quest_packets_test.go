package model

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestWriteQuestLogEncodesCompletedAndWithholdsUnverifiedActiveRecords(t *testing.T) {
	packet := network.EmptyPacket()
	quests := []CharacterQuest{
		{QuestID: 10, Status: QuestCompleted},
		{QuestID: 20, Status: QuestActive, AchievementCount: 3},
		{QuestID: 30, Status: QuestAbandoned},
	}
	if err := WriteQuestLog(&packet, quests); err != nil {
		t.Fatalf("WriteQuestLog: %v", err)
	}
	want := []byte{
		1, 0, 10, 0, 0, 0, // completed count and ID
		0, // active count
	}
	got, err := packet.ReadBytes(packet.Remaining())
	if err != nil {
		t.Fatalf("read packet: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("quest log = %v, want %v", got, want)
	}
}

func TestWriteQuestLogRejectsInvalidStatus(t *testing.T) {
	packet := network.EmptyPacket()
	err := WriteQuestLog(&packet, []CharacterQuest{{QuestID: 20, Status: 99}})
	if err == nil {
		t.Fatal("WriteQuestLog accepted an invalid status")
	}
}

func TestWriteQuestLogEncodesRecoveredActiveQuestRecord(t *testing.T) {
	packet := network.EmptyPacket()
	quest := CharacterQuest{QuestID: 58, Status: QuestActive, AchievementCount: 1, Data1: 7}
	if err := WriteQuestLog(&packet, []CharacterQuest{quest}); err != nil {
		t.Fatalf("WriteQuestLog: %v", err)
	}
	want := []byte{0, 0, 1, 58, 0, 0, 0, 1, 0, 0x18, 1, 1, 1, 1, 21, 0}
	want = append(want, "SN_CON_QNO_CH_SMITH_2"...)
	want = append(want, 1, 7, 0, 0, 0)
	if got := packet.Payload(); string(got) != string(want) {
		t.Fatalf("quest log = % X, want % X", got, want)
	}
}
