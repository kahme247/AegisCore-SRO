package character

import (
	"context"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestForceKillForGMTransitionsOnceAndEmitsDeath(t *testing.T) {
	player := &model.Player{Session: &server.Session{}, SRObject: model.SRObject{UniqueID: 100}}
	target := &model.NPC{Mutex: &sync.Mutex{}, SRObject: model.SRObject{UniqueID: 200}}
	target.ConfigureCombat(750, 123)
	target.SetLifeState(model.Alive)

	var broadcast network.Packet
	var actionEnd network.Packet
	handler := &CombatHandler{
		broadcast: func(_ *model.Player, packet *network.Packet) { broadcast = *packet },
		send:      func(_ *server.Session, packet network.Packet) error { actionEnd = packet; return nil },
	}
	if err := handler.ForceKillForGM(context.Background(), player, target, GMKillRewardsNone); err != nil {
		t.Fatal(err)
	}
	if target.GetLifeState() != model.Dead || target.CurrentHitPoints() != 0 {
		t.Fatalf("target state=%d hp=%d", target.GetLifeState(), target.CurrentHitPoints())
	}
	if broadcast.MessageID == 0 || actionEnd.MessageID == 0 {
		t.Fatalf("packets = broadcast:%#04x end:%#04x", broadcast.MessageID, actionEnd.MessageID)
	}
	if err := handler.ForceKillForGM(context.Background(), player, target, GMKillRewardsNone); err == nil {
		t.Fatal("duplicate force kill succeeded")
	}
}

func TestForceKillForGMRejectsUnknownRewardMode(t *testing.T) {
	player := &model.Player{Session: &server.Session{}}
	target := &model.NPC{Mutex: &sync.Mutex{}}
	target.ConfigureCombat(1, 0)
	target.SetLifeState(model.Alive)
	if err := (&CombatHandler{}).ForceKillForGM(context.Background(), player, target, GMKillRewardsNone+1); err == nil {
		t.Fatal("unknown reward mode succeeded")
	}
}
