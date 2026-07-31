package character

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

type partyExitInstanceRepository struct {
	state model.QuestInstanceMemberState
}

func (r *partyExitInstanceRepository) SaveQuestInstanceMember(_ context.Context, state model.QuestInstanceMemberState) error {
	r.state = state
	return nil
}
func (r *partyExitInstanceRepository) LoadQuestInstanceMember(context.Context, int, uint32) (model.QuestInstanceMemberState, error) {
	return r.state, nil
}
func (r *partyExitInstanceRepository) LoadQuestInstanceMemberByWorld(context.Context, int, uint16) (model.QuestInstanceMemberState, error) {
	return r.state, nil
}
func (*partyExitInstanceRepository) DeleteQuestInstanceMember(context.Context, int, uint16) error {
	return nil
}

type partyExitInstanceAllocator struct {
	scope    model.GameWorldScope
	released int
}

func (a *partyExitInstanceAllocator) AllocateQuestInstanceScope(context.Context, int, uint32) (model.GameWorldScope, error) {
	return a.scope, nil
}

func (a *partyExitInstanceAllocator) ReleaseQuestInstanceScope(context.Context, model.GameWorldScope) error {
	a.released++
	return nil
}

type teleportWorldFake struct {
	*selectEntityWorldFake
	region       navmeshv2.RtNavmeshTerrain
	stopResults  []bool
	stops        int
	flushes      int
	resets       int
	lastPosition navmeshv2.RtNavmeshPosition
}

type instanceTeleportWorldFake struct {
	*teleportWorldFake
	players map[uint32]*model.Player
}

func (f *instanceTeleportWorldFake) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	player := f.players[uid]
	if player == nil {
		return nil, model.ErrQuestInstanceNotMember
	}
	return player, nil
}

func (f *teleportWorldFake) GetRegion(id int16) (navmeshv2.RtNavmeshTerrain, error) {
	return f.region, nil
}

func (f *teleportWorldFake) StopMovingCharacter(_ model.ICharacter, position navmeshv2.RtNavmeshPosition, _ string, _ bool) bool {
	f.stops++
	f.lastPosition = position
	if f.stops <= len(f.stopResults) {
		return f.stopResults[f.stops-1]
	}
	return true
}

func (f *teleportWorldFake) FlushPlayerPosition(_ uint32) error {
	f.flushes++
	return nil
}

func (f *teleportWorldFake) ResetPlayerVisibilityForTeleport(_ *model.Player) {
	f.resets++
}

func TestNativeQuestInstanceTeleportCoordinatesMembersAndCommitsAfterSubmission(t *testing.T) {
	firstSession, first, _ := selectEntityFixture(t)
	secondSession, second, _ := selectEntityFixture(t)
	second.SetUniqueID(200)
	second.ID = 43
	secondSession.UserContext.UniqueID = 200
	secondSession.UserContext.CharacterID = 43
	for _, session := range []*server.Session{firstSession, secondSession} {
		session.MarkAuthenticated()
		if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
			t.Fatal("could not prepare instance member session")
		}
	}
	scope, _ := model.NewGameWorldScope(41, 2)
	registry := model.NewQuestInstanceRegistry()
	if _, err := registry.Create(scope, 141, first); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Join(scope, second); err != nil {
		t.Fatal(err)
	}
	plan, matched, err := registry.PrepareNativeQuestInstanceDeath(scope, 141, "MOB_QT_01_TAHOMET", 500)
	if err != nil || !matched {
		t.Fatalf("prepare matched=%t error=%v", matched, err)
	}
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(30000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	baseWorld := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{}, region: region}
	world := &instanceTeleportWorldFake{teleportWorldFake: baseWorld, players: map[uint32]*model.Player{100: first, 200: second}}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	handler.roll = func() int { return 0 }
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	if err = handler.SubmitNativeQuestInstanceTeleport(registry, plan, first); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.QuestNotification || sent[1].MessageID != opcode.TeleportReadyRequest || sent[2].MessageID != opcode.TeleportReadyRequest {
		t.Fatalf("packets = %#v", sent)
	}
	if baseWorld.stops != 2 || baseWorld.resets != 2 || first.GetLifeState() != model.Spawning || second.GetLifeState() != model.Spawning {
		t.Fatalf("stops=%d resets=%d life=%d/%d", baseWorld.stops, baseWorld.resets, first.GetLifeState(), second.GetLifeState())
	}
	if firstSession.LifecyclePhase() != server.SessionCharacterLoading || secondSession.LifecyclePhase() != server.SessionCharacterLoading {
		t.Fatal("instance members did not enter loading lifecycle")
	}
	variables, _ := registry.Variables(scope)
	if variables["QNO_RM_OLDWOMAN_6_Step"] != 3 || variables["QNO_RM_OLDWOMAN_6_EVENT_TIMER"] != 60500 {
		t.Fatalf("variables = %#v", variables)
	}
	if baseWorld.lastPosition.Offset.X != 30 || baseWorld.lastPosition.Offset.Y != 0 || baseWorld.lastPosition.Offset.Z != 30 {
		t.Fatalf("destination = %+v", baseWorld.lastPosition)
	}
}

func TestQuestInstanceEntryResetsEveryPersistedMemberAtCurrentPosition(t *testing.T) {
	firstSession, first, _ := selectEntityFixture(t)
	secondSession, second, _ := selectEntityFixture(t)
	second.UniqueID = 200
	secondSession.UserContext.UniqueID = 200
	for _, session := range []*server.Session{firstSession, secondSession} {
		session.MarkAuthenticated()
		if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
			t.Fatal("prepare session")
		}
	}
	scope, _ := model.NewGameWorldScope(13, 1)
	first.SetGameWorldID(uint32(scope))
	second.SetGameWorldID(uint32(scope))
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(30000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	base := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{}, region: region}
	world := &instanceTeleportWorldFake{teleportWorldFake: base, players: map[uint32]*model.Player{100: first, 200: second}}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	instance := model.QuestInstance{Scope: scope, QuestID: 251, OwnerUID: 100, Members: []uint32{100, 200}}
	if err := handler.SubmitQuestInstanceEntry(instance, []*model.Player{first, second}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.TeleportReadyRequest || sent[1].MessageID != opcode.TeleportReadyRequest || base.stops != 2 || base.resets != 2 {
		t.Fatalf("packets=%#v stops=%d resets=%d", sent, base.stops, base.resets)
	}
}

func TestDimensionHoleEntryCommitsAuthoredScopeAndLoadingDestination(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("prepare Dimension Hole session")
	}
	player.SetGameWorldID(1)
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000),
		Offset: math32.NewVector3(100, 20, 200),
	})
	definition := model.DimensionHoleCatalog()[0]
	scope, err := model.NewGameWorldScope(definition.WorldID, 2)
	if err != nil {
		t.Fatal(err)
	}
	admission := service.DimensionHoleAdmission{
		PortalUID: 501, PlayerUID: player.GetUniqueID(), OwnerUID: 100,
		Scope: scope, Definition: definition,
	}
	destination := service.DimensionHoleEntryDestination{
		WorldID: definition.WorldID, Region: 31201,
		X: 30, Y: 4, Z: 40,
	}
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(destination.Region))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{player: player}, region: region}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}

	if err = handler.SubmitDimensionHoleEntry(context.Background(), player, admission, destination); err != nil {
		t.Fatalf("SubmitDimensionHoleEntry() error = %v", err)
	}
	if player.GetGameWorldID() != uint32(scope) || player.GetLifeState() != model.Spawning {
		t.Fatalf("scope=%X life=%d, want %X/spawning", player.GetGameWorldID(), player.GetLifeState(), uint32(scope))
	}
	if session.LifecyclePhase() != server.SessionCharacterLoading || world.stops != 1 || world.resets != 1 {
		t.Fatalf("phase=%s stops=%d resets=%d", session.LifecyclePhase(), world.stops, world.resets)
	}
	if world.lastPosition.Region.ID != destination.Region ||
		world.lastPosition.Offset.X != destination.X || world.lastPosition.Offset.Y != destination.Y || world.lastPosition.Offset.Z != destination.Z {
		t.Fatalf("destination = %+v", world.lastPosition)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.TeleportReadyRequest ||
		!reflect.DeepEqual(sent[0].Payload(), []byte{0xE1, 0x79}) {
		t.Fatalf("Dimension Hole loading packet = %#v", sent)
	}
}

func TestDimensionHoleEntryRollsBackScopePositionAndLifecycleWhenReadySendFails(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("prepare Dimension Hole session")
	}
	player.SetGameWorldID(7)
	source := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000),
		Offset: math32.NewVector3(100, 20, 200),
	}
	player.SetNavmeshPosition(source)
	definition := model.DimensionHoleCatalog()[0]
	scope, _ := model.NewGameWorldScope(definition.WorldID, 3)
	admission := service.DimensionHoleAdmission{
		PortalUID: 501, PlayerUID: player.GetUniqueID(), OwnerUID: 100,
		Scope: scope, Definition: definition,
	}
	destination := service.DimensionHoleEntryDestination{WorldID: definition.WorldID, Region: 31201, X: 30, Y: 4, Z: 40}
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(destination.Region))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{player: player}, region: region}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	sendErr := errors.New("send failed")
	handler.send = func(*server.Session, network.Packet) error { return sendErr }

	if err := handler.SubmitDimensionHoleEntry(context.Background(), player, admission, destination); !errors.Is(err, sendErr) {
		t.Fatalf("SubmitDimensionHoleEntry() error = %v", err)
	}
	if player.GetGameWorldID() != 7 || session.LifecyclePhase() != server.SessionInWorld {
		t.Fatalf("rollback scope=%X phase=%s", player.GetGameWorldID(), session.LifecyclePhase())
	}
	if world.stops != 2 || world.lastPosition.Region.ID != source.Region.ID ||
		world.lastPosition.Offset.X != source.Offset.X || world.lastPosition.Offset.Y != source.Offset.Y || world.lastPosition.Offset.Z != source.Offset.Z {
		t.Fatalf("rollback stops=%d position=%+v", world.stops, world.lastPosition)
	}
	handler.mu.Lock()
	pending := handler.pending[session]
	handler.mu.Unlock()
	if pending != nil {
		t.Fatal("failed Dimension Hole entry retained pending loading state")
	}
}

func TestPartyQuestInstanceExitUsesPersistedOriginAndNativeNotice(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	player.ID = 77
	session.UserContext.CharacterID = 77
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("prepare session")
	}
	scope, _ := model.NewGameWorldScope(41, 2)
	player.SetNavmeshPosition(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(10, 20, 30)})
	repository := &partyExitInstanceRepository{}
	registry := model.NewQuestInstanceRegistry()
	allocator := &partyExitInstanceAllocator{scope: scope}
	lifecycle, _ := model.NewQuestInstanceLifecycle(registry, repository, allocator)
	if _, err := lifecycle.Create(context.Background(), 141, 9001, player, time.Unix(1000, 0), true); err != nil {
		t.Fatal(err)
	}
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(25000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{}, region: region}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	handler.ConfigureQuestInstanceExit(lifecycle)
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	if err := handler.SubmitPartyQuestInstanceExit(context.Background(), player); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.QuestNotification || sent[1].MessageID != opcode.TeleportReadyRequest {
		t.Fatalf("packets=%#v", sent)
	}
	if player.GetGameWorldID() != 0 || world.stops != 1 || world.lastPosition.Region.ID != 25000 || world.lastPosition.Offset.X != 10 || world.lastPosition.Offset.Y != 20 || world.lastPosition.Offset.Z != 30 {
		t.Fatalf("scope=%X stops=%d destination=%+v", player.GetGameWorldID(), world.stops, world.lastPosition)
	}
	if allocator.released != 1 {
		t.Fatalf("released scopes=%d", allocator.released)
	}
	if _, err := registry.Instance(scope); !errors.Is(err, model.ErrQuestInstanceMissing) {
		t.Fatalf("registry instance error=%v", err)
	}
}

type teleportRepositoryFake struct {
	destination model.TeleportDestination
	result      model.TeleportResult
	err         error
	quotes      int
	commits     int
	designates  int
	designated  uint32
}

func (f *teleportRepositoryFake) Quote(context.Context, uint32, uint32) (model.TeleportDestination, error) {
	f.quotes++
	return f.destination, f.err
}

func (f *teleportRepositoryFake) Commit(context.Context, int64, uint32, uint32, uint32) (model.TeleportResult, error) {
	f.commits++
	return f.result, f.err
}

func (f *teleportRepositoryFake) Designate(context.Context, int64, uint32, uint32) (uint32, error) {
	f.designates++
	if f.err != nil {
		return 0, f.err
	}
	f.designated = 1
	return f.designated, nil
}

func TestTeleportDesignationPersistsSelectedBuildingAndReturnsB059Success(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare an in-world teleport session")
	}
	session.UserContext.UserID = 7
	gate := &model.TeleportGate{SRObject: model.SRObject{
		UniqueID: 102, RefObjectID: 2094, Name: "STORE_CH_GATE",
		TypeInfo: model.TypeInfo{TypeID1: 4}, RtNavmeshPosition: selectEntityPosition(30000, 20, 0, 20),
	}}
	if !player.GetKnownObjectList().AddObject(gate) {
		t.Fatal("fixture could not add teleport gate to known-object list")
	}
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{
		player: player, objects: map[uint32]model.ISRObject{gate.GetUniqueID(): gate}, distance: selectionKnownRange,
	}}
	repository := &teleportRepositoryFake{}
	handler := NewTeleportHandler(world, repository)
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	npcInteractions.Record(player.GetUniqueID(), gate.GetUniqueID(), gate.GetRefObjectID(), teleportTaskUse)
	t.Cleanup(func() { npcInteractions.Clear(player.GetUniqueID()) })
	request := network.EmptyClientPacket()
	request.WriteUInt32(gate.GetUniqueID())
	if err := handler.handleDesignate(context.Background(), session, request); err != nil {
		t.Fatalf("handleDesignate: %v", err)
	}
	if repository.designates != 1 || repository.designated != 1 {
		t.Fatalf("designation calls=%d id=%d, want one persisted bind point", repository.designates, repository.designated)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.TeleportDesignateResponse || !reflect.DeepEqual(sent[0].Payload(), []byte{1}) {
		t.Fatalf("designation response = %#v, want B059/01", sent)
	}
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
	if session.LifecyclePhase() != server.SessionInWorld || world.stops != 1 || world.flushes != 1 {
		t.Fatalf("prepared state = %s stops=%d flushes=%d", session.LifecyclePhase(), world.stops, world.flushes)
	}
	destination := model.TeleportDestination{ID: 1, Region: 25000, X: 969, Z: 1369, WorldID: 1}
	if err := handler.CompleteItemReturn(session, player, destination); err != nil {
		t.Fatalf("CompleteItemReturn() error = %v", err)
	}
	if session.LifecyclePhase() != server.SessionCharacterLoading {
		t.Fatalf("completed phase = %s, want character-loading", session.LifecyclePhase())
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

func TestGMRecallUsesTeleportLoadingHandshake(t *testing.T) {
	targetSession, target, _ := selectEntityFixture(t)
	actorSession, actor, _ := selectEntityFixture(t)
	for _, session := range []*server.Session{targetSession, actorSession} {
		session.MarkAuthenticated()
		if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
			t.Fatal("could not prepare an in-world GM recall session")
		}
	}
	target.CharName = "Target"
	targetSession.UserContext.CharName = target.CharName
	actorPosition := target.GetNavmeshPosition()
	actorPosition.Region = target.GetNavmeshPosition().Region
	actorPosition.Offset = math32.NewVector3(42, 0, 84)
	actor.SetNavmeshPosition(actorPosition)
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(25000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{
		selectEntityWorldFake: &selectEntityWorldFake{player: target},
		region:                region,
	}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	var sent []network.Packet
	handler.send = func(session *server.Session, packet network.Packet) error {
		if session == targetSession {
			sent = append(sent, packet)
		}
		return nil
	}
	bootstraps := 0
	handler.bootstrap = func(*server.Session, *model.Player) error {
		bootstraps++
		return nil
	}
	if err := handler.RecallPlayer(actor, target); err != nil {
		t.Fatalf("RecallPlayer() error = %v", err)
	}
	if targetSession.LifecyclePhase() != server.SessionCharacterLoading || world.resets != 1 || world.stops != 2 || target.GetLifeState() != model.Spawning {
		t.Fatalf("recall state phase=%s resets=%d stops=%d life=%d", targetSession.LifecyclePhase(), world.resets, world.stops, target.GetLifeState())
	}
	if world.lastPosition.Offset.X != actor.GetNavmeshPosition().Offset.X ||
		world.lastPosition.Offset.Y != actor.GetNavmeshPosition().Offset.Y ||
		world.lastPosition.Offset.Z != actor.GetNavmeshPosition().Offset.Z {
		t.Fatalf("recall destination = %+v, want actor position %+v", world.lastPosition, actor.GetNavmeshPosition())
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.TeleportReadyRequest {
		t.Fatalf("recall packets = %#v", sent)
	}
	if err := handler.handleReady(context.Background(), targetSession, network.EmptyClientPacket()); err != nil {
		t.Fatalf("handleReady() error = %v", err)
	}
	handler.mu.Lock()
	_, pending := handler.pending[targetSession]
	handler.mu.Unlock()
	if pending || bootstraps != 1 {
		t.Fatalf("recall completion pending=%t bootstraps=%d", pending, bootstraps)
	}
	serverConn, clientConn := net.Pipe()
	targetSession.Conn = serverConn
	go func() { _, _ = io.Copy(io.Discard, clientConn) }()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	gameReady := NewGameReadyHandler(&fakeGameReadyPlayers{player: target})
	if err := gameReady.handleSessionPacket(context.Background(), targetSession, network.EmptyClientPacket()); err != nil {
		t.Fatalf("GameReady after recall: %v", err)
	}
	if targetSession.LifecyclePhase() != server.SessionInWorld {
		t.Fatalf("recall GameReady lifecycle = %s, want in-world", targetSession.LifecyclePhase())
	}
}

func TestGMRecallRejectsUnavailableAndCrossWorldTargetsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name          string
		prepareActor  bool
		actorWorldID  uint32
		targetWorldID uint32
	}{
		{name: "actor outside world", prepareActor: false},
		{name: "cross-world target", prepareActor: true, actorWorldID: 1, targetWorldID: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetSession, target, _ := selectEntityFixture(t)
			actorSession, actor, _ := selectEntityFixture(t)
			targetSession.MarkAuthenticated()
			if !targetSession.EnterLobby() || !targetSession.BeginCharacterLoading() || !targetSession.CompleteCharacterLoading() {
				t.Fatal("could not prepare target session")
			}
			actorSession.MarkAuthenticated()
			if !actorSession.EnterLobby() {
				t.Fatal("could not prepare actor session")
			}
			if test.prepareActor && (!actorSession.BeginCharacterLoading() || !actorSession.CompleteCharacterLoading()) {
				t.Fatal("could not prepare actor world session")
			}
			actor.SetGameWorldID(test.actorWorldID)
			target.SetGameWorldID(test.targetWorldID)
			world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{player: target}}
			if err := NewTeleportHandler(world, &teleportRepositoryFake{}).RecallPlayer(actor, target); err == nil {
				t.Fatal("RecallPlayer() unexpectedly accepted unavailable target")
			}
			if world.stops != 0 || targetSession.LifecyclePhase() != server.SessionInWorld {
				t.Fatalf("rejected recall mutated target stops=%d phase=%s", world.stops, targetSession.LifecyclePhase())
			}
		})
	}
}

func TestTeleportPlayerToAcceptsCatalogWorldIDForPublicTarget(t *testing.T) {
	session, target, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare public target session")
	}
	target.SetGameWorldID(0)
	region := navmeshv2.NewRtNavmeshTerrain("", navmeshv2.NewRegionFromInt16(25000))
	region.Cells = []navmeshv2.RtNavmeshCellQuad{{}}
	world := &teleportWorldFake{
		selectEntityWorldFake: &selectEntityWorldFake{player: target},
		region:                region,
	}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	handler.send = func(*server.Session, network.Packet) error { return nil }
	destination := model.TeleportDestination{ID: 5, Region: 25000, X: 1138, Y: 250, Z: 153, WorldID: 1}
	if target.GetGameWorldID() == uint32(destination.WorldID) {
		t.Fatal("test requires distinct runtime and catalog world IDs")
	}
	if err := handler.TeleportPlayerTo(target, destination); err != nil {
		t.Fatalf("TeleportPlayerTo() error = %v", err)
	}
	if session.LifecyclePhase() != server.SessionCharacterLoading ||
		world.lastPosition.Region.ID != destination.Region ||
		world.lastPosition.Offset.X != destination.X ||
		world.lastPosition.Offset.Y != destination.Y ||
		world.lastPosition.Offset.Z != destination.Z {
		t.Fatalf("catalog-world teleport state=%s destination=%+v", session.LifecyclePhase(), world.lastPosition)
	}
}

func TestTeleportCancelAbortsPreparedReturnScrollCast(t *testing.T) {
	session, player, _ := selectEntityFixture(t)
	session.MarkAuthenticated()
	if !session.EnterLobby() || !session.BeginCharacterLoading() || !session.CompleteCharacterLoading() {
		t.Fatal("could not prepare an in-world session")
	}
	world := &teleportWorldFake{selectEntityWorldFake: &selectEntityWorldFake{player: player}}
	handler := NewTeleportHandler(world, &teleportRepositoryFake{})
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	if err := handler.PrepareItemReturn(session, player); err != nil {
		t.Fatal(err)
	}
	if err := handler.handleCancel(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatalf("handleCancel() error = %v", err)
	}
	if session.LifecyclePhase() != server.SessionInWorld {
		t.Fatalf("cancelled phase = %s, want in-world", session.LifecyclePhase())
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.TeleportCancelResponse || !reflect.DeepEqual(sent[0].Payload(), []byte{1}) {
		t.Fatalf("cancel response packets = %+v", sent)
	}
	if sent[1].MessageID != opcode.EntityUpdateMovementState || !reflect.DeepEqual(sent[1].Payload(), []byte{byte(player.GetUniqueID()), 0, 0, 0, 11, 0}) {
		t.Fatalf("cancel UI teardown opcode=0x%04x payload=%x", sent[1].MessageID, sent[1].Payload())
	}
	if err := handler.CompleteItemReturn(session, player, model.TeleportDestination{}); err == nil {
		t.Fatal("cancelled return-scroll cast completed")
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
