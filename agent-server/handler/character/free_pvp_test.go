package character

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type freePVPPlayers struct{ player *model.Player }

func (p freePVPPlayers) GetPlayerByUniqueId(uniqueID uint32) (*model.Player, error) {
	if p.player == nil || p.player.GetUniqueID() != uniqueID {
		return nil, errors.New("missing player")
	}
	return p.player, nil
}

func TestFreePVPHandlerUpdatesCapeAndNotifiesKnownPlayers(t *testing.T) {
	session := &server.Session{}
	session.UserContext.UniqueID = 77
	player := &model.Player{Session: session}
	player.UniqueID = 77
	var sent network.Packet
	h := &FreePVPHandler{
		players: freePVPPlayers{player: player},
		notify:  func(_ *model.Player, packet network.Packet) { sent = packet },
	}
	request := network.EmptyClientPacket()
	request.WriteByte(3)
	if err := h.handleUpdate(context.Background(), session, request); err != nil {
		t.Fatalf("handle update: %v", err)
	}
	if player.GetPVPCape() != model.PvpCapeBlue {
		t.Fatalf("cape = %d", player.GetPVPCape())
	}
	if sent.MessageID != opcode.FRPVPUpdateResponse || hex.EncodeToString(sent.Payload()) != "014d00000003" {
		t.Fatalf("response = %04x/%x", sent.MessageID, sent.Payload())
	}
}

func TestFreePVPHandlerRejectsJobMode(t *testing.T) {
	session := &server.Session{}
	session.UserContext.UniqueID = 77
	player := &model.Player{Session: session, Job: model.PlayerJob{Type: model.PlayerJobTrader}}
	player.UniqueID = 77
	var sent network.Packet
	h := &FreePVPHandler{
		players: freePVPPlayers{player: player},
		send:    func(_ *server.Session, packet network.Packet) error { sent = packet; return nil },
	}
	request := network.EmptyClientPacket()
	request.WriteByte(1)
	if err := h.handleUpdate(context.Background(), session, request); err != nil {
		t.Fatalf("handle update: %v", err)
	}
	if player.GetPVPCape() != model.PvpCapeNone || hex.EncodeToString(sent.Payload()) != "020780" {
		t.Fatalf("cape/response = %d/%x", player.GetPVPCape(), sent.Payload())
	}
}

func TestCompetitiveEventRequestsReturnNativeUnavailableResponses(t *testing.T) {
	session := &server.Session{}
	var sent []network.Packet
	h := &FreePVPHandler{send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	flagWar := network.EmptyClientPacket()
	if err := h.handleFlagWarRegister(context.Background(), session, flagWar); err != nil {
		t.Fatalf("Flag War register: %v", err)
	}
	arena := network.EmptyClientPacket()
	arena.WriteByte(1)
	arena.WriteByte(2)
	arena.WriteByte(3)
	if err := h.handleBattleArenaRequest(context.Background(), session, arena); err != nil {
		t.Fatalf("Battle Arena register: %v", err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.FlagWarUpdate || hex.EncodeToString(sent[0].Payload()) != "ff0100" {
		t.Fatalf("Flag War response = %#v", sent)
	}
	if sent[1].MessageID != opcode.BattleArenaOperation || hex.EncodeToString(sent[1].Payload()) != "ff04" {
		t.Fatalf("Battle Arena response = %04x/%x", sent[1].MessageID, sent[1].Payload())
	}

	cancel := network.EmptyClientPacket()
	cancel.WriteByte(2)
	if err := h.handleBattleArenaRequest(context.Background(), session, cancel); err != nil {
		t.Fatalf("Battle Arena cancel: %v", err)
	}
	if len(sent) != 3 || sent[2].MessageID != opcode.BattleArenaOperation || hex.EncodeToString(sent[2].Payload()) != "ff04" {
		t.Fatalf("Battle Arena cancel response = %#v", sent)
	}
}

func TestCompetitiveEventHandlersRejectMalformedRequestsWithoutResponding(t *testing.T) {
	session := &server.Session{}
	sent := 0
	h := &FreePVPHandler{send: func(_ *server.Session, _ network.Packet) error {
		sent++
		return nil
	}}

	badFlagWar := network.EmptyClientPacket()
	badFlagWar.WriteByte(1)
	if err := h.handleFlagWarRegister(context.Background(), session, badFlagWar); err == nil {
		t.Fatal("expected malformed Flag War request error")
	}
	badArena := network.EmptyClientPacket()
	badArena.WriteByte(1)
	badArena.WriteByte(2)
	if err := h.handleBattleArenaRequest(context.Background(), session, badArena); err == nil {
		t.Fatal("expected malformed Battle Arena request error")
	}
	if sent != 0 {
		t.Fatalf("malformed requests sent %d responses, want 0", sent)
	}
}
