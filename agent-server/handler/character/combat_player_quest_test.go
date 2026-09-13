package character

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestUniqueDeadGrantPublishesAtomicInventoryAndQuestProjection(t *testing.T) {
	quest := model.CharacterQuest{QuestID: 700, Status: model.QuestRewardReady, Data1: 1, ObjectiveKey: "SN_UNIQUE", TaskCount: 1}
	added := model.Item{SRObject: model.SRObject{RefObjectID: 9002}, StackSize: 1}
	remaining := model.Item{SRObject: model.SRObject{RefObjectID: 9001}, StackSize: 2}
	removedSlot := byte(13)
	inventory := model.NewInventory(map[byte]model.Item{13: remaining, 14: added})
	repository := &questRuntimeStub{uniqueDeadGranted: true, uniqueDeadResult: model.QuestUniqueDeadGrantResult{
		Quest: quest, Inventory: inventory, Added: model.QuestItemMutation{Slot: 14, Item: added}, RemovedSlot: &removedSlot,
	}}
	var packets []network.Packet
	handler := &CombatHandler{quests: repository, send: func(_ *server.Session, packet network.Packet) error {
		packets = append(packets, packet)
		return nil
	}}
	player := &model.Player{ID: 44, Session: &server.Session{}}
	player.SRObject.RtNavmeshPosition = navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32758)}
	if err := handler.recordUniqueDead(context.Background(), player.Session, player, 1933, 8123); err != nil {
		t.Fatal(err)
	}
	if repository.uniqueDeadCalls != 1 || repository.uniqueDeadCharacter != 44 || repository.uniqueDeadMonster != 1933 || repository.uniqueDeadRegion != -32758 {
		t.Fatalf("UNIQUE_DEAD request = calls=%d character=%d monster=%d region=%d", repository.uniqueDeadCalls, repository.uniqueDeadCharacter, repository.uniqueDeadMonster, repository.uniqueDeadRegion)
	}
	if len(player.Quests) != 1 || player.Quests[0] != quest || len(packets) != 4 {
		t.Fatalf("published state quests=%+v packets=%d", player.Quests, len(packets))
	}
	wantRemoved := buildQuestItemRemoved(13)
	if got, want := packets[0].Payload(), wantRemoved.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("precursor removal = % X, want % X", got, want)
	}
	wantRemaining := buildQuestItemAdded(model.QuestItemMutation{Slot: 13, Item: remaining})
	if got, want := packets[1].Payload(), wantRemaining.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("precursor remainder = % X, want % X", got, want)
	}
	wantAdded := buildQuestItemAdded(model.QuestItemMutation{Slot: 14, Item: added})
	if got, want := packets[2].Payload(), wantAdded.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("mission item addition = % X, want % X", got, want)
	}
}

func TestRecordPlayerKillPublishesQualifiedQuestProgress(t *testing.T) {
	quest := model.CharacterQuest{QuestID: 611, Status: model.QuestProgressed, AchievementCount: 1, Data1: 1, ObjectiveKey: "SN_CON_QNO_SD_GU_008_1", TaskCount: 1}
	repository := &questRuntimeStub{progressResult: []model.QuestProgressResult{{Quest: quest, Changed: true}}}
	handler := &CombatHandler{quests: repository}
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		packets = append(packets, packet)
		return nil
	}
	killer := &model.Player{ID: 44, Session: &server.Session{}}
	target := &model.Player{ID: 45, Level: 80, Session: &server.Session{}}
	target.SetJob(model.PlayerJob{Type: model.PlayerJobThief, Level: 7})
	target.SRObject.RtNavmeshPosition = navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32752)}

	if err := handler.recordPlayerKill(context.Background(), killer, target); err != nil {
		t.Fatal(err)
	}
	if repository.playerKillCalls != 1 || repository.playerKillTarget.Level != 80 ||
		repository.playerKillTarget.JobType != model.PlayerJobThief || repository.playerKillTarget.RegionID != -32752 {
		t.Fatalf("player-kill target = %+v calls=%d", repository.playerKillTarget, repository.playerKillCalls)
	}
	if len(killer.Quests) != 1 || killer.Quests[0] != quest || len(packets) != 1 {
		t.Fatalf("killer quests=%+v packets=%d", killer.Quests, len(packets))
	}
}
