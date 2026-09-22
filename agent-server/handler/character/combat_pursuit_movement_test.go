package character

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestCombatPursuitRetargetsMovingMonsterBeforeStaleLegArrives(t *testing.T) {
	handler, player, mob, scheduled, destinations, _ := newMovingPursuitTest(t)
	if err := handler.handlePacket(context.Background(), player.Session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if len(*destinations) != 1 || len(*scheduled) != 1 {
		t.Fatalf("initial pursuit = destinations:%d polls:%d", len(*destinations), len(*scheduled))
	}

	mob.SetNavmeshPosition(combatTestPosition(24231, 225, 0, 100))
	handler.now = func() time.Time { return time.Unix(1, 0).Add(attackPursuitRetargetRate) }
	(*scheduled)[0]()
	if len(*destinations) != 2 {
		t.Fatalf("moving target produced %d destinations, want live retarget", len(*destinations))
	}
	if got := (*destinations)[1].GetGlobalCoordinates().X; got <= (*destinations)[0].GetGlobalCoordinates().X {
		t.Fatalf("retarget destination X = %g, want ahead of stale X %g", got, (*destinations)[0].GetGlobalCoordinates().X)
	}
	if movement := player.GetMovementData(); movement == nil || movement.Generation != 2 {
		t.Fatalf("retargeted movement = %#v, want generation 2", movement)
	}
}

func TestCombatPursuitAimsAtTargetSoMovingMonsterDoesNotLeaveAStandOffStop(t *testing.T) {
	handler, player, mob, _, destinations, _ := newMovingPursuitTest(t)
	if err := handler.handlePacket(context.Background(), player.Session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if len(*destinations) != 1 {
		t.Fatalf("initial pursuit destinations = %d, want 1", len(*destinations))
	}
	got := (*destinations)[0].GetGlobalCoordinates()
	want := mob.GetNavmeshPosition().GetGlobalCoordinates()
	if got.X != want.X || got.Z != want.Z {
		t.Fatalf("pursuit destination = (%g, %g), want target position (%g, %g)", got.X, got.Z, want.X, want.Z)
	}
}
func TestCombatPursuitKeepsDungeonTargetCoordinates(t *testing.T) {
	handler, player, mob, _, destinations, _ := newMovingPursuitTest(t)
	player.SetNavmeshPosition(combatTestPosition(-32767, 800, 0, -900))
	mob.SetNavmeshPosition(combatTestPosition(-32767, 900, 0, -900))

	if err := handler.handlePacket(context.Background(), player.Session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	if len(*destinations) != 1 {
		t.Fatalf("dungeon pursuit destinations = %d, want 1", len(*destinations))
	}
	destination := (*destinations)[0]
	if destination.Region.ID != -32767 || destination.Offset == nil || *destination.Offset != *mob.GetNavmeshPosition().Offset {
		t.Fatalf("dungeon pursuit destination = %+v, want target %+v", destination, mob.GetNavmeshPosition())
	}
	if calls := handler.world.(*combatWorldFake).globalResolutions; calls != 0 {
		t.Fatalf("dungeon pursuit used normal-world resolver %d times", calls)
	}
}

func TestCombatPursuitStopsAndHitsWhenMovingMonsterEntersReach(t *testing.T) {
	handler, player, mob, scheduled, _, sent := newMovingPursuitTest(t)
	if err := handler.handlePacket(context.Background(), player.Session, combatAttackPacket(mob.GetUniqueID())); err != nil {
		t.Fatal(err)
	}
	mob.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: combatTestPosition(24231, 240, 0, 100)})
	player.SetNavmeshPosition(combatTestPosition(24231, 190, 0, 100))
	mob.SetNavmeshPosition(combatTestPosition(24231, 200, 0, 100))
	(*scheduled)[0]()

	world := handler.world.(*combatWorldFake)
	if world.stops != 1 {
		t.Fatalf("in-range moving target stops = %d, want 1", world.stops)
	}
	if player.GetMovementData() != nil {
		t.Fatal("pursuit movement remained active after target entered reach")
	}
	if mob.CurrentHitPoints() >= 100 {
		t.Fatal("player did not attack moving monster immediately on entering reach")
	}
	if len(*sent) == 0 {
		t.Fatal("immediate pursuit attack emitted no combat packets")
	}
}

func newMovingPursuitTest(t *testing.T) (*CombatHandler, *model.Player, *model.NPC, *[]func(), *[]navmeshv2.RtNavmeshPosition, *[]network.Packet) {
	t.Helper()
	previous := model.RefItems
	t.Cleanup(func() { model.RefItems = previous })
	model.RefItems = map[uint32]model.RefItem{3632: {
		RefObject: model.RefObject{ID: 3632}, Range: 6,
	}}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 42}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: combatTestPosition(24231, 100, 0, 100)},
		ID:       42, Session: session, BaseStats: model.BaseStats{Str: 20, Int: 20}, Inventory: model.NewInventory(nil),
	}
	player.Inventory.Items[model.SlotPrimaryWeapon] = model.Item{SRObject: model.SRObject{
		RefObjectID: 3632,
		TypeInfo:    model.TypeInfo{TypeID1: model.TypeID1Item, TypeID2: model.TypeID2Wearable, TypeID3: model.TypeID3Weapon, TypeID4: 2},
	}}
	player.SetLifeState(model.Alive)
	player.KnownObjectList = model.NewKnownObjectList(player)
	mob := &model.NPC{SRObject: model.SRObject{
		UniqueID: 200, RefObjectID: 1933,
		TypeInfo:          model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1, TypeID4: 1},
		RtNavmeshPosition: combatTestPosition(24231, 200, 0, 100),
	}, Type: "NPC", Mutex: &sync.Mutex{}}
	mob.ConfigureCombat(100, 10)
	mob.SetLifeState(model.Alive)
	player.KnownObjectList.AddObject(mob)

	scheduled := make([]func(), 0)
	destinations := make([]navmeshv2.RtNavmeshPosition, 0)
	sent := make([]network.Packet, 0)
	world := &combatWorldFake{player: player, objects: map[uint32]model.ISRObject{mob.GetUniqueID(): mob}}
	handler := &CombatHandler{
		world: world, pending: make(map[uint32]*pendingAttack), repeating: make(map[uint32]*pendingAttack),
		nextAttack: make(map[uint32]time.Time), now: func() time.Time { return time.Unix(1, 0) },
		schedule: func(_ time.Duration, fn func()) { scheduled = append(scheduled, fn) },
		move: func(character *model.Player, destination navmeshv2.RtNavmeshPosition) uint64 {
			destinations = append(destinations, destination)
			movement := character.BeginMovement(model.MovementData{TargetPosition: destination, HasDestination: true})
			return movement.Generation
		},
		send:      func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil },
		broadcast: func(_ *model.Player, packet *network.Packet) { sent = append(sent, *packet) },
	}
	return handler, player, mob, &scheduled, &destinations, &sent
}
