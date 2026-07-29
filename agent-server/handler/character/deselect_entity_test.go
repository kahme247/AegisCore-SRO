package character

import (
	"context"
	"reflect"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func TestDeselectEntityAcknowledgesAndClearsMatchingNPCInteraction(t *testing.T) {
	const playerUID uint32 = 77
	const npcUID uint32 = 88
	session := &server.Session{UserContext: server.UserContext{UniqueID: playerUID, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: playerUID}, ID: 44, Session: session}
	world := &selectEntityWorldFake{player: player}
	handler := NewDeselectEntityHandler(world)
	var sent network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	npcInteractions.Record(playerUID, npcUID, 100, 1)
	t.Cleanup(func() { npcInteractions.Clear(playerUID) })

	request := network.EmptyClientPacket()
	request.WriteUInt32(npcUID)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	if sent.MessageID != opcode.EntityDeselectResponse || !reflect.DeepEqual(sent.Payload(), []byte{1}) {
		t.Fatalf("response = opcode 0x%04X payload %x", sent.MessageID, sent.Payload())
	}
	if _, ok := npcInteractions.Get(playerUID); ok {
		t.Fatal("matching NPC interaction remained after deselect")
	}
}

func TestDeselectEntityDoesNotClearNewerInteraction(t *testing.T) {
	const playerUID uint32 = 77
	session := &server.Session{UserContext: server.UserContext{UniqueID: playerUID, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: playerUID}, ID: 44, Session: session}
	handler := NewDeselectEntityHandler(&selectEntityWorldFake{player: player})
	handler.send = func(_ *server.Session, _ network.Packet) error { return nil }
	npcInteractions.Record(playerUID, 99, 100, 1)
	t.Cleanup(func() { npcInteractions.Clear(playerUID) })

	request := network.EmptyClientPacket()
	request.WriteUInt32(88)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("handlePacket: %v", err)
	}
	interaction, ok := npcInteractions.Get(playerUID)
	if !ok || interaction.NPCUniqueID != 99 {
		t.Fatalf("newer interaction was cleared: %#v, %t", interaction, ok)
	}
}
