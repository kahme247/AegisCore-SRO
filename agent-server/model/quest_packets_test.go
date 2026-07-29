package model

import (
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
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
