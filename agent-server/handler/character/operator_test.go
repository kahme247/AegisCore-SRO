package character

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type operatorWorldFake struct {
	actor  *model.Player
	target *model.Player
}

type operatorPunishmentRepositoryFake struct {
	actor  int
	target int
	at     time.Time
	result model.OperatorPunishmentResult
}

func (f *operatorPunishmentRepositoryFake) ToggleOperatorPunishment(_ context.Context, actor, target int, at time.Time) (model.OperatorPunishmentResult, error) {
	f.actor, f.target, f.at = actor, target, at
	return f.result, nil
}

func (world operatorWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return world.actor, nil
}
func (world operatorWorldFake) GetPlayerByCharName(name string) (*model.Player, error) {
	if world.target != nil && world.target.CharName == name {
		return world.target, nil
	}
	return nil, model.ErrCharacterNotFound
}
func (operatorWorldFake) OperatorStatistics() (uint32, uint32, uint32) { return 2, 3, 4 }

func TestOperatorHandlerRequiresLiveGMIdentity(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharacterID: 20}}
	actor := &model.Player{ID: 20, Session: session}
	var sent network.Packet
	handler := &OperatorHandler{world: operatorWorldFake{actor: actor}, send: func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }}
	request := network.EmptyClientPacket()
	request.WriteUInt16(operatorStatistics)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if sent.MessageID != opcode.OperatorResponse || hex.EncodeToString(sent.Payload()) != "020114" {
		t.Fatalf("denial = %04x/%x", sent.MessageID, sent.Payload())
	}
}

func TestOperatorStatisticsExecutesForGM(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharacterID: 20}}
	actor := &model.Player{ID: 20, Session: session, IsGm: true}
	var sent network.Packet
	handler := &OperatorHandler{world: operatorWorldFake{actor: actor}, send: func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }}
	request := network.EmptyClientPacket()
	request.WriteUInt16(operatorStatistics)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sent.Payload()); got != "010400020000000300000004000000" {
		t.Fatalf("statistics = %s", got)
	}
}

func TestOperatorPunishmentCommitsBeforeTargetNotification(t *testing.T) {
	gmSession := &server.Session{UserContext: server.UserContext{UniqueID: 10, CharacterID: 20}}
	targetSession := &server.Session{}
	actor := &model.Player{ID: 20, Session: gmSession, IsGm: true}
	target := &model.Player{ID: 30, CharName: "Target", Session: targetSession}
	var sent []network.Packet
	repository := &operatorPunishmentRepositoryFake{result: model.OperatorPunishmentResult{TargetCharacterID: 30, TargetAccountID: 300, Active: true, ChangedAt: time.Date(2026, 7, 26, 12, 34, 0, 0, time.UTC)}}
	handler := &OperatorHandler{
		world:      operatorWorldFake{actor: actor, target: target},
		repository: repository,
		now:        func() time.Time { return time.Date(2026, 7, 26, 12, 34, 0, 0, time.UTC) },
		send: func(session *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
	}
	request := network.EmptyClientPacket()
	request.WriteUInt16(operatorPunishToggle)
	request.WriteString("Target")
	if err := handler.handlePacket(context.Background(), gmSession, request); err != nil {
		t.Fatal(err)
	}
	if repository.actor != 20 || repository.target != 30 || repository.at.IsZero() {
		t.Fatalf("repository call = actor:%d target:%d at:%s", repository.actor, repository.target, repository.at)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.OperatorPunishment || sent[1].MessageID != opcode.OperatorResponse {
		t.Fatalf("deliveries = %#v", sent)
	}
	if got := hex.EncodeToString(sent[1].Payload()); got != "013700" {
		t.Fatalf("acknowledgement = %s", got)
	}
}
