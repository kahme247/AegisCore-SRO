package character

import (
	"context"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type fortressSiegeRepositoryFake struct {
	model.FortressSiegeRepository
	fortressID int
	actor      int
	typeValue  model.FortressRequestType
	at         time.Time
	err        error
}

func (f *fortressSiegeRepositoryFake) ResolveFortressApplicationTarget(context.Context) (int, error) {
	return f.fortressID, f.err
}

func (f *fortressSiegeRepositoryFake) ApplyFortressSiege(_ context.Context, fortressID, actor int, requestType model.FortressRequestType, at time.Time) (model.FortressApplication, error) {
	f.fortressID, f.actor, f.typeValue, f.at = fortressID, actor, requestType, at
	return model.FortressApplication{FortressID: fortressID, ActorID: actor, Type: requestType, AppliedAt: at}, f.err
}

func TestFortressSiegeApplicationRequiresSelectedOfficialAndBroadcastsCommit(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77, SelectedNPCUniqueID: 1234, SelectedNPCRefObjID: 19498}}
	player := &model.Player{ID: 44, Session: session}
	player.SetUniqueID(77)
	player.RtNavmeshPosition = selectEntityPosition(30000, 10, 0, 10)
	player.KnownObjectList = model.NewKnownObjectList(player)
	npc := selectEntityNPC(1234, model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}, selectEntityPosition(30000, 20, 0, 20))
	npc.Name = "NPC_CH_FORTRESS_OFFICIAL"
	player.KnownObjectList.AddObject(npc)
	repository := &fortressSiegeRepositoryFake{fortressID: 1}
	var broadcast network.Packet
	handler := &FortressSiegeHandler{
		repository: repository,
		world: gachaWorldFake{player: player, objects: map[uint32]model.ISRObject{1234: npc}, broadcast: func(packet network.Packet) {
			broadcast = packet
		}},
		now: func() time.Time { return time.Unix(100, 0).UTC() },
		send: func(*server.Session, network.Packet) error {
			t.Fatal("successful async siege application sent a direct response")
			return nil
		},
	}
	packet := siegeClientPacket(vsro188.SiegeActionApplicationApply, vsro188.SiegeRequestAttack)
	if err := handler.handleAction(context.Background(), session, packet); err != nil {
		t.Fatalf("handleAction: %v", err)
	}
	if repository.actor != 44 || repository.typeValue != model.FortressRequestAttack || repository.at.Unix() != 100 {
		t.Fatalf("application = actor:%d type:%d at:%s", repository.actor, repository.typeValue, repository.at)
	}
	if broadcast.MessageID != opcode.SiegeUpdate || len(broadcast.Payload()) != 6 || broadcast.Payload()[0] != vsro188.SiegeUpdateApplicationApply {
		t.Fatalf("broadcast = %04x/%x", broadcast.MessageID, broadcast.Payload())
	}
}

func TestFortressSiegeApplicationFailsClosedAwayFromOfficial(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44, UniqueID: 77}}
	var sent network.Packet
	handler := &FortressSiegeHandler{repository: &fortressSiegeRepositoryFake{fortressID: 1}, world: gachaWorldFake{}, send: func(_ *server.Session, packet network.Packet) error {
		sent = packet
		return nil
	}}
	packet := siegeClientPacket(vsro188.SiegeActionApplicationApply, vsro188.SiegeRequestAttack)
	if err := handler.handleAction(context.Background(), session, packet); err != nil {
		t.Fatalf("handleAction: %v", err)
	}
	if sent.MessageID != opcode.SiegeActionResponse || len(sent.Payload()) != 4 || sent.Payload()[0] != 2 {
		t.Fatalf("failure = %04x/%x", sent.MessageID, sent.Payload())
	}
}

func siegeClientPacket(values ...byte) network.Packet {
	packet := network.EmptyClientPacket()
	for _, value := range values {
		packet.WriteByte(value)
	}
	return packet
}
