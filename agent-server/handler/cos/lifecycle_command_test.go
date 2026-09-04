package cos

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

type cosCommandServiceFake struct {
	petUID, targetUID, skillID uint32
	settings                   uint32
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
func (f *cosCommandServiceFake) MoveTo(_ *model.Player, petUID uint32, _ navmeshv2.RtNavmeshPosition) error {
	f.petUID = petUID
	return nil
}
func (f *cosCommandServiceFake) Follow(_ *model.Player, petUID uint32) error {
	f.petUID = petUID
	return nil
}
func (f *cosCommandServiceFake) Pickup(_ *model.Player, petUID, targetUID uint32) error {
	f.petUID, f.targetUID = petUID, targetUID
	return nil
}
func (f *cosCommandServiceFake) Rename(context.Context, *model.Player, uint32, string) error {
	return nil
}

func (f *cosCommandServiceFake) UpdateSettings(_ context.Context, _ *model.Player, petUID, settings uint32) error {
	f.petUID, f.settings = petUID, settings
	return nil
}

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
	if !bytes.Equal(response.Payload(), []byte{2, 13, 3, 0, 88, 0, 0, 0}) {
		t.Fatalf("failure payload = %x", response.Payload())
	}
}

func TestGrowthPetStanceToggleSucceedsWithoutCommandResponse(t *testing.T) {
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
	packet.WriteByte(11)
	if err := handler.handleCommand(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if response.MessageID != 0 || len(response.Payload()) != 0 {
		t.Fatalf("stance toggle unexpectedly sent response %04x/%x", response.MessageID, response.Payload())
	}
}

func TestGrowthPetSettingsAcceptsStockNineByteStancePayload(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	runtime := &cosCommandServiceFake{}
	handler := NewLifecycleHandler(&playerLookupFake{player: player}, runtime)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		response = packet
		return nil
	}
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(88)
	packet.WriteByte(1)
	packet.WriteUInt32(1)
	if err := handler.handleSettings(context.Background(), session, packet); err != nil {
		t.Fatal(err)
	}
	if runtime.petUID != 88 || runtime.settings != 1 {
		t.Fatalf("settings update = pet %d value %d", runtime.petUID, runtime.settings)
	}
	if response.MessageID != 0 || len(response.Payload()) != 0 {
		t.Fatalf("settings update unexpectedly sent response %04x/%x", response.MessageID, response.Payload())
	}
}
