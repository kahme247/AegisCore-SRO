package character

import (
	"context"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type teleportWorldFake struct {
	*selectEntityWorldFake
	region       navmeshv2.RtNavmeshTerrain
	stops        int
	flushes      int
	resets       int
	lastPosition navmeshv2.RtNavmeshPosition
}

func (f *teleportWorldFake) GetRegion(id int16) (navmeshv2.RtNavmeshTerrain, error) {
	return f.region, nil
}

func (f *teleportWorldFake) StopMovingCharacter(_ model.ICharacter, position navmeshv2.RtNavmeshPosition, _ string, _ bool) bool {
	f.stops++
	f.lastPosition = position
	return true
}

func (f *teleportWorldFake) FlushPlayerPosition(_ uint32) error {
	f.flushes++
	return nil
}

func (f *teleportWorldFake) ResetPlayerVisibilityForTeleport(_ *model.Player) {
	f.resets++
}

type teleportRepositoryFake struct {
	destination model.TeleportDestination
	result      model.TeleportResult
	err         error
	quotes      int
	commits     int
}

func (f *teleportRepositoryFake) Quote(context.Context, uint32, uint32) (model.TeleportDestination, error) {
	f.quotes++
	return f.destination, f.err
}

func (f *teleportRepositoryFake) Commit(context.Context, int64, uint32, uint32, uint32) (model.TeleportResult, error) {
	f.commits++
	return f.result, f.err
}

func TestTeleportUsesValidatedLinkAndCompletesLoadingHandshake(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare an in-world teleport session")
	}
	session.UserContext.UserID = 7
	npc.RefObjectID = 2094
	player.Gold = 10_000
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(26265))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	destination := model.TeleportDestination{ID: 2, Region: 26265, X: 957, Y: -80, Z: 1508, WorldID: 1, Fee: 5000}
	world := &teleportWorldFake{
		selectEntityWorldFake: &selectEntityWorldFake{
			player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange,
		},
		region: region,
	}
	repository := &teleportRepositoryFake{destination: destination, result: model.TeleportResult{Destination: destination, Gold: 5000}}
	handler := NewTeleportHandler(world, repository)
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	bootstraps := 0
	handler.bootstrap = func(gotSession *server.Session, gotPlayer *model.Player) error {
		if gotSession != session || gotPlayer != player {
			t.Fatal("bootstrap did not retain the authenticated session player")
		}
		bootstraps++
		return nil
	}
	npcInteractions.Record(player.GetUniqueID(), npc.GetUniqueID(), npc.GetRefObjectID(), 1)
	t.Cleanup(func() { npcInteractions.Clear(player.GetUniqueID()) })

	request := network.EmptyClientPacket()
	request.WriteUInt32(npc.GetUniqueID())
	request.WriteByte(normalTeleportType)
	request.WriteUInt32(destination.ID)
	if err := handler.handleUse(context.Background(), session, request); err != nil {
		t.Fatalf("handleUse: %v", err)
	}
	if repository.quotes != 1 || repository.commits != 1 || world.stops != 2 || world.flushes != 1 || world.resets != 1 {
		t.Fatalf("transaction path quotes=%d commits=%d stops=%d flushes=%d resets=%d", repository.quotes, repository.commits, world.stops, world.flushes, world.resets)
	}
	if player.Gold != 5000 {
		t.Fatalf("runtime gold = %d, want 5000", player.Gold)
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("teleport lifecycle = %s, want %s", got, want)
	}
	if got, want := player.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("teleport player life state = %d, want %d", got, want)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.TeleportUseResponse || !reflect.DeepEqual(sent[0].Payload(), []byte{1}) || sent[1].MessageID != opcode.TeleportReadyRequest || !reflect.DeepEqual(sent[1].Payload(), []byte{0x99, 0x66}) {
		t.Fatalf("teleport handshake = %#v", sent)
	}

	ready := network.EmptyClientPacket()
	if err := handler.handleReady(context.Background(), session, ready); err != nil {
		t.Fatalf("handleReady: %v", err)
	}
	if bootstraps != 1 {
		t.Fatalf("bootstrap calls = %d, want 1", bootstraps)
	}
	if err := handler.handleReady(context.Background(), session, network.EmptyClientPacket()); err == nil {
		t.Fatal("duplicate teleport ready was accepted")
	}
}

func TestReturnScrollUsesTeleportLoadingHandshakeWithoutGateResponse(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare an in-world return-scroll session")
	}
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(25000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{
		selectEntityWorldFake: &selectEntityWorldFake{player: player},
		region:                region,
	}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	bootstraps := 0
	handler.bootstrap = func(*server.Session, *model.Player) error {
		bootstraps++
		return nil
	}

	if err := handler.PrepareItemReturn(session, player); err != nil {
		t.Fatalf("PrepareItemReturn() error = %v", err)
	}
	if session.LifecyclePhase() != server.SessionCharacterLoading || world.stops != 1 || world.flushes != 1 {
		t.Fatalf("prepared state = %s stops=%d flushes=%d", session.LifecyclePhase(), world.stops, world.flushes)
	}
	destination := model.TeleportDestination{ID: 1, Region: 25000, X: 969, Z: 1369, WorldID: 1}
	if err := handler.CompleteItemReturn(session, player, destination); err != nil {
		t.Fatalf("CompleteItemReturn() error = %v", err)
	}
	if world.resets != 1 || world.stops != 2 || player.GetLifeState() != model.Spawning {
		t.Fatalf("completed world state resets=%d stops=%d life=%d", world.resets, world.stops, player.GetLifeState())
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.TeleportReadyRequest || !reflect.DeepEqual(sent[0].Payload(), []byte{0xA8, 0x61}) {
		t.Fatalf("return-scroll handshake = %#v", sent)
	}
	if err := handler.handleReady(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("handleReady() error = %v", err)
	}
	if bootstraps != 1 {
		t.Fatalf("bootstrap calls = %d, want 1", bootstraps)
	}
}

func TestTeleportRejectsUnlinkedDestinationBeforeMutation(t *testing.T) {
	session, player, npc := selectEntityFixture(t)
	session.UserContext.UserID = 7
	npc.RefObjectID = 2094
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{npc.GetUniqueID(): npc}, distance: selectionKnownRange,
	}}
	repository := &teleportRepositoryFake{err: model.ErrTeleportLinkNotFound}
	handler := NewTeleportHandler(world, repository)
	var sent network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	npcInteractions.Record(player.GetUniqueID(), npc.GetUniqueID(), npc.GetRefObjectID(), 1)
	t.Cleanup(func() { npcInteractions.Clear(player.GetUniqueID()) })
	request := network.EmptyClientPacket()
	request.WriteUInt32(npc.GetUniqueID())
	request.WriteByte(normalTeleportType)
	request.WriteUInt32(999)

	if err := handler.handleUse(context.Background(), session, request); err != nil {
		t.Fatalf("handleUse: %v", err)
	}
	if repository.commits != 0 || world.stops != 0 || sent.MessageID != opcode.TeleportUseResponse || !reflect.DeepEqual(sent.Payload(), []byte{2, 1, 0}) {
		t.Fatalf("rejection commits=%d stops=%d opcode=0x%04x payload=%x", repository.commits, world.stops, sent.MessageID, sent.Payload())
	}
}
