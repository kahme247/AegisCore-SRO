package cos

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

type cosCommandServiceFake struct {
	petUID, targetUID, skillID uint32
}

func (f *cosCommandServiceFake) Owned(*model.Player, uint32) (*model.COS, error) {
	return nil, nil
}
func (f *cosCommandServiceFake) Unsummon(context.Context, *model.Player, uint32) error {
	return nil
}
func (f *cosCommandServiceFake) SetAttackTarget(_ *model.Player, petUID, targetUID, skillID uint32) error {
	f.petUID, f.targetUID, f.skillID = petUID, targetUID, skillID
	return nil
}
func (f *cosCommandServiceFake) ClearAttackTarget(*model.Player, uint32) error { return nil }

func TestGrowthPetAttackCommandUsesDefaultSkillTarget(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	runtime := &cosCommandServiceFake{}
	handler := NewLifecycleHandler(&playerLookupFake{player: player}, runtime)
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(88)
	packet.WriteByte(2)
	packet.WriteUInt32(99)
	if err := handler.handleCommand(context.Background(), session, packet); err != nil {
		t.Fatalf("growth pet attack rejected: %v", err)
	}
	if runtime.petUID != 88 || runtime.targetUID != 99 || runtime.skillID != 0 {
		t.Fatalf("attack command = pet %d target %d skill %d", runtime.petUID, runtime.targetUID, runtime.skillID)
	}
}

func TestGrowthPetCastCommandCarriesAuthoredSkill(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	runtime := &cosCommandServiceFake{}
	handler := NewLifecycleHandler(&playerLookupFake{player: player}, runtime)
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(88)
	packet.WriteByte(13)
	packet.WriteUInt32(3824)
	packet.WriteUInt32(99)
	if err := handler.handleCommand(context.Background(), session, packet); err != nil {
		t.Fatalf("growth pet cast rejected: %v", err)
	}
	if runtime.petUID != 88 || runtime.targetUID != 99 || runtime.skillID != 3824 {
		t.Fatalf("cast command = pet %d target %d skill %d", runtime.petUID, runtime.targetUID, runtime.skillID)
	}
}

func TestMalformedGrowthPetCommandEchoesParsedCommandInFailure(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	handler := NewLifecycleHandler(&playerLookupFake{player: player}, &cosCommandServiceFake{})
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		response = packet
		return nil
	}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(88)
	packet.WriteByte(13)
	packet.WriteUInt32(3824)
	if err := handler.handleCommand(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.Payload(), []byte{2, 13, 3, 0}) {
		t.Fatalf("failure payload = %x", response.Payload())
	}
}
