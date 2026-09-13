package model

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestWriteQuestLogEncodesCompletedAndWithholdsUnverifiedActiveRecords(t *testing.T) {
	packet := network.EmptyPacket()
	quests := []CharacterQuest{
		{QuestID: 10, Status: QuestClosed},
		{QuestID: 11, Status: QuestInactive, AchievementCount: 1},
		{QuestID: 12, Status: QuestInactive},
		{QuestID: 20, Status: QuestActive, AchievementCount: 3},
		{QuestID: 30, Status: QuestAborted},
	}
	if err := WriteQuestLog(&packet, quests); err != nil {
		t.Fatalf("WriteQuestLog: %v", err)
	}
	want := []byte{
		2, 0, 10, 0, 0, 0, 11, 0, 0, 0, // completed count and IDs
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
	quest := CharacterQuest{QuestID: 58, Status: QuestProgressed, AchievementCount: 1, Data1: 7}
	if err := WriteQuestLog(&packet, []CharacterQuest{quest}); err != nil {
		t.Fatalf("WriteQuestLog: %v", err)
	}
	want := []byte{0, 0, 1, 58, 0, 0, 0, 1, 0, 0x18, 7, 1, 1, 1, 21, 0}
	want = append(want, "SN_CON_QNO_CH_SMITH_2"...)
	want = append(want, 1, 7, 0, 0, 0)
	if got := packet.Payload(); string(got) != string(want) {
		t.Fatalf("quest log = % X, want % X", got, want)
	}
}

func TestWriteActiveQuestRecordKeepsStockV188Prefix(t *testing.T) {
	packet := network.EmptyPacket()
	quest := CharacterQuest{QuestID: 3, Status: QuestActive, AchievementCount: 1}
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatalf("WriteActiveQuestRecord: %v", err)
	}
	payload := packet.Payload()
	if len(payload) < 5 {
		t.Fatalf("active quest payload length = %d", len(payload))
	}
	if payload[0] != 1 || payload[1] != 0 || payload[2] != questRecordStatusFlag|questRecordObjectiveFlag || payload[3] != byte(QuestActive) || payload[4] != 1 {
		t.Fatalf("active quest prefix = % X", payload[:5])
	}
}

func TestWriteActiveQuestRecordAllowsFirstAttemptAchievementZero(t *testing.T) {
	packet := network.EmptyPacket()
	quest := CharacterQuest{QuestID: 3, Status: QuestActive, AchievementCount: 0}
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatalf("first attempt active record: %v", err)
	}
	if payload := packet.Payload(); len(payload) == 0 || payload[0] != 0 {
		t.Fatalf("first attempt packed achievement = % X", payload)
	}
}

func TestWriteActiveQuestRecordMarksReadyObjectiveComplete(t *testing.T) {
	quest := CharacterQuest{
		QuestID: 58, Status: QuestRewardReady, AchievementCount: 1,
		ObjectiveKey: "SN_CON_QNO_CH_SMITH_2", TaskCount: 1, Data1: 140,
	}
	packet := network.EmptyPacket()
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if len(payload) < 7 || payload[3] != byte(QuestRewardReady) || payload[6] != 2 {
		t.Fatalf("ready quest prefix = % X, want status 8 and completed objective state 2", payload)
	}
}

func TestWriteActiveQuestRecordEncodesClassifiedMissionCounters(t *testing.T) {
	quest := CharacterQuest{
		QuestID: 900, Status: QuestProgressed, TaskCount: 3, Data1: 2 | 7<<16 | 1<<32,
		ObjectiveKeys: [4]string{"SN_NORMAL", "SN_CHAMPION", "SN_GIANT"},
	}
	packet := network.EmptyPacket()
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0x18, byte(QuestProgressed), 3}
	for index, entry := range []struct {
		key   string
		value uint32
	}{{"SN_NORMAL", 2}, {"SN_CHAMPION", 7}, {"SN_GIANT", 1}} {
		_ = index
		want = append(want, 1, 1, byte(len(entry.key)), 0)
		want = append(want, entry.key...)
		want = append(want, 1, byte(entry.value), 0, 0, 0)
	}
	if got := packet.Payload(); string(got) != string(want) {
		t.Fatalf("classified active record = % X, want % X", got, want)
	}
}

func TestWriteActiveQuestRecordRepeatsSharedMissionCounter(t *testing.T) {
	quest := CharacterQuest{QuestID: 901, Status: QuestProgressed, TaskCount: 2, Data1: 37, SharedProgress: true,
		ObjectiveKeys: [4]string{"SN_FIRST", "SN_SECOND"}}
	packet := network.EmptyPacket()
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if len(payload) < 8 || bytes.Count(payload, []byte{37, 0, 0, 0}) != 2 {
		t.Fatalf("shared mission payload = % X", payload)
	}
}

func TestWriteActiveQuestRecordEncodesFourCollectionCounters(t *testing.T) {
	quest := CharacterQuest{QuestID: 902, Status: QuestProgressed, TaskCount: 4, Data1: 1 | 2<<16 | 3<<32 | 4<<48,
		ObjectiveKeys: [4]string{"SN_ONE", "SN_TWO", "SN_THREE", "SN_FOUR"}}
	packet := network.EmptyPacket()
	if err := WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	for value := byte(1); value <= 4; value++ {
		if bytes.Count(payload, []byte{value, 0, 0, 0}) != 1 {
			t.Fatalf("four-mission payload does not contain counter %d exactly once: % X", value, payload)
		}
	}
}

func TestProjectActiveQuestProtocolSupportsZeroMissionFerryRecord(t *testing.T) {
	quest := CharacterQuest{QuestID: 88, QuestCodeName: "QNO_WC_FERRY4_1", Status: QuestRewardReady}
	record, err := ProjectActiveQuestProtocol(quest)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != byte(QuestRewardReady) || record.TransitionCode != 2 || record.Text != "" || len(record.Values) != 0 {
		t.Fatalf("ferry record = %#v", record)
	}
	packet := network.EmptyPacket()
	if err = WriteActiveQuestRecord(&packet, quest); err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if len(payload) != 5 || payload[4] != 0 {
		t.Fatalf("zero-mission ferry payload = % X", payload)
	}
}
