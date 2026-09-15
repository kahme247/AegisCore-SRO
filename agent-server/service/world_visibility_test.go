package service

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/g3n/engine/math32"
)

func TestWorldVisibilityActivationAddsStationaryPlayersReciprocally(t *testing.T) {
	regionID := int16(25000)
	alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, alice, bob)
	bob.SetLifeState(model.Alive)

	deltas := world.ActivatePlayer(alice)
	want := []VisibilityDelta{
		{ObserverUID: alice.GetUniqueID(), SubjectUID: bob.GetUniqueID(), Kind: VisibilityAdded},
		{ObserverUID: bob.GetUniqueID(), SubjectUID: alice.GetUniqueID(), Kind: VisibilityAdded},
	}
	if !reflect.DeepEqual(deltas, want) {
		t.Fatalf("activation deltas = %#v, want %#v", deltas, want)
	}
	if !alice.GetKnownObjectList().Knows(bob) || !bob.GetKnownObjectList().Knows(alice) {
		t.Fatal("stationary active players were not added to reciprocal known lists")
	}

	// The returned slice is caller-owned. Mutating it cannot alter the
	// authoritative known-list state or a later reconciliation result.
	deltas[0].SubjectUID = 999
	if !alice.GetKnownObjectList().Knows(bob) {
		t.Fatal("caller mutation of UID-only delta changed world visibility")
	}
}

func TestWorldVisibilityIsolatesGameWorldInstancesInSharedRegion(t *testing.T) {
	regionID := int16(25000)
	public := newVisibilityTestPlayer(1, "Public", visibilityTestPosition(regionID, 100, 100))
	instanceA := newVisibilityTestPlayer(2, "InstanceA", visibilityTestPosition(regionID, 110, 100))
	instanceB := newVisibilityTestPlayer(3, "InstanceB", visibilityTestPosition(regionID, 120, 100))
	instanceA.SetGameWorldID(41)
	instanceB.SetGameWorldID(42)
	world := newVisibilityTestWorld(t, public, instanceA, instanceB)
	public.SetLifeState(model.Alive)
	instanceA.SetLifeState(model.Alive)
	instanceB.SetLifeState(model.Alive)

	world.ActivatePlayer(instanceA)
	if instanceA.GetKnownObjectList().Knows(public) ||
		instanceA.GetKnownObjectList().Knows(instanceB) ||
		public.GetKnownObjectList().Knows(instanceA) ||
		instanceB.GetKnownObjectList().Knows(instanceA) {
		t.Fatal("players in distinct game-world scopes became mutually visible")
	}

	member := newVisibilityTestPlayer(4, "Member", visibilityTestPosition(regionID, 130, 100))
	member.SetGameWorldID(41)
	world.AddVisibleObject(member)
	member.SetLifeState(model.Alive)
	world.ActivatePlayer(member)
	if !member.GetKnownObjectList().Knows(instanceA) || !instanceA.GetKnownObjectList().Knows(member) {
		t.Fatal("members of the same game-world instance were not mutually visible")
	}
}

func TestWorldVisibilityActivationQueuesExistingNPC(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	world.AddVisibleObject(npc)

	world.ActivatePlayer(player)
	if !player.GetKnownObjectList().Knows(npc) || !npc.GetKnownObjectList().Knows(player) {
		t.Fatal("activation did not establish reciprocal player/NPC visibility")
	}
	if queued := player.GetCharKnownObjectList().GetObjectsToSpawn(); queued[npc.GetUniqueID()] != npc {
		t.Fatalf("NPC spawn was not queued for the client: %#v", queued)
	}
}

func TestWorldVisibilityActivationQueuesPortalBeyondGenericObjectRange(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	gate := &model.TeleportGate{SRObject: model.SRObject{
		RefObjectID:       2094,
		TypeInfo:          model.TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 1},
		RtNavmeshPosition: visibilityTestPosition(regionID, 100+visibilityRange+500, 100),
	}}
	world.AddVisibleObject(gate)

	world.ActivatePlayer(player)
	if !player.GetKnownObjectList().Knows(gate) {
		t.Fatal("portal outside generic visibility range was not queued")
	}
	if queued := player.GetCharKnownObjectList().GetObjectsToSpawn(); queued[gate.GetUniqueID()] != gate {
		t.Fatalf("portal spawn was not queued for the client: %#v", queued)
	}
}

func TestWorldNPCsKnownToPlayersOnlyIncludesVisibleNPCsAfterActivation(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	nearby := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	remote := newVisibilityTestNPC(visibilityTestPosition(regionID, 1_400, 100))
	world.AddVisibleObject(nearby)
	world.AddVisibleObject(remote)

	if got := world.GetNPCsKnownToPlayers(); len(got) != 0 {
		t.Fatalf("NPCs before player activation = %d, want 0", len(got))
	}
	world.ActivatePlayer(player)
	got := world.GetNPCsKnownToPlayers()
	if len(got) != 1 || got[0] != nearby {
		t.Fatalf("active NPCs = %#v, want only nearby NPC %d", got, nearby.GetUniqueID())
	}
}

func TestWorldVisibilityIgnoresClientVerticalCoordinateForNPCs(t *testing.T) {
	regionID := int16(25000)
	playerPosition := visibilityTestPosition(regionID, 100, 100)
	playerPosition.Offset.Y = 800
	player := newVisibilityTestPlayer(1, "Alice", playerPosition)
	world := newVisibilityTestWorld(t, player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	world.AddVisibleObject(npc)

	world.ActivatePlayer(player)
	if !player.GetKnownObjectList().Knows(npc) || !npc.GetKnownObjectList().Knows(player) {
		t.Fatal("vertical coordinate removed horizontally nearby player/NPC visibility")
	}
}

func TestWorldVisibilityNewNPCQueuesForStationaryPlayer(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	world.ActivatePlayer(player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))

	world.AddVisibleObject(npc)
	if queued := player.GetCharKnownObjectList().GetObjectsToSpawn(); queued[npc.GetUniqueID()] != npc {
		t.Fatalf("new NPC was not queued for a stationary client: %#v", queued)
	}
}

func TestWorldVisibilityMovementQueuesNPCDespawn(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	world.AddVisibleObject(npc)
	world.ActivatePlayer(player)
	spawned := player.GetCharKnownObjectList().GetObjectsToSpawn()
	player.GetCharKnownObjectList().MarkSpawned(spawned)

	player.SetNavmeshPosition(visibilityTestPosition(regionID, 1_400, 100))
	world.ReconcileCharacterVisibility(player)
	if player.GetKnownObjectList().Knows(npc) || npc.GetKnownObjectList().Knows(player) {
		t.Fatal("out-of-range NPC remained in reciprocal known lists")
	}
	if queued := player.GetCharKnownObjectList().GetObjectsToDespawn(); queued[npc.GetUniqueID()] != npc {
		t.Fatalf("NPC despawn was not queued for the client: %#v", queued)
	}
}

func TestWorldVisibilityNPCMovementReconcilesStationaryPlayer(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	world.positionPersistence = NewPositionPersistenceService(visibilityNoopPositionRepository{}, time.Hour)
	world.ActivatePlayer(player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 1_400, 100))
	world.AddVisibleObject(npc)

	inRange := visibilityTestPosition(regionID, 140, 100)
	movement := npc.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: inRange})
	if !world.commitMovementStep(npc, movement.Generation, inRange, time.Now(), true, "visibility-test", false) {
		t.Fatal("NPC movement into range did not finalize")
	}
	if !player.GetKnownObjectList().Knows(npc) || !npc.GetKnownObjectList().Knows(player) {
		t.Fatal("moving NPC entering range was not added to reciprocal known lists")
	}
	spawned := player.GetCharKnownObjectList().GetObjectsToSpawn()
	player.GetCharKnownObjectList().MarkSpawned(spawned)

	outOfRange := visibilityTestPosition(regionID, 1_400, 100)
	movement = npc.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: outOfRange})
	if !world.commitMovementStep(npc, movement.Generation, outOfRange, time.Now(), true, "visibility-test", false) {
		t.Fatal("NPC movement out of range did not finalize")
	}
	if player.GetKnownObjectList().Knows(npc) || npc.GetKnownObjectList().Knows(player) {
		t.Fatal("moving NPC leaving range remained in reciprocal known lists")
	}
	if queued := player.GetCharKnownObjectList().GetObjectsToDespawn(); queued[npc.GetUniqueID()] != npc {
		t.Fatalf("moving NPC despawn was not queued for the client: %#v", queued)
	}
}

func TestWorldVisibilityHysteresisPreventsBoundaryFlapping(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	world := newVisibilityTestWorld(t, player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	world.AddVisibleObject(npc)
	world.ActivatePlayer(player)
	spawned := player.GetCharKnownObjectList().GetObjectsToSpawn()
	player.GetCharKnownObjectList().MarkSpawned(spawned)

	npc.SetNavmeshPosition(visibilityTestPosition(regionID, 100+visibilityRange+50, 100))
	world.ReconcileCharacterVisibility(npc)
	if !player.GetKnownObjectList().Knows(npc) {
		t.Fatal("known NPC was removed inside the visibility hysteresis band")
	}

	npc.SetNavmeshPosition(visibilityTestPosition(regionID, 100+visibilityExitRange+1, 100))
	world.ReconcileCharacterVisibility(npc)
	if player.GetKnownObjectList().Knows(npc) {
		t.Fatal("NPC remained visible beyond the exit range")
	}
	player.GetCharKnownObjectList().GetObjectsToDespawn()

	npc.SetNavmeshPosition(visibilityTestPosition(regionID, 100+visibilityRange+50, 100))
	world.ReconcileCharacterVisibility(npc)
	if player.GetKnownObjectList().Knows(npc) {
		t.Fatal("unknown NPC re-entered inside the hysteresis band")
	}

	npc.SetNavmeshPosition(visibilityTestPosition(regionID, 100+visibilityRange, 100))
	world.ReconcileCharacterVisibility(npc)
	if !player.GetKnownObjectList().Knows(npc) {
		t.Fatal("NPC did not re-enter at the normal visibility range")
	}
}

func TestWorldVisibilitySpatialCellTracksMovementAndDisconnect(t *testing.T) {
	regionID := int16(25000)
	alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)
	world.ReconcileCharacterVisibility(bob)

	world.mutex.Lock()
	oldCell, indexed := world.visibilityCellByUID[bob.GetUniqueID()]
	world.mutex.Unlock()
	if !indexed {
		t.Fatal("player was not entered into visibility cell index")
	}

	bob.SetNavmeshPosition(visibilityTestPosition(regionID, 900, 100))
	world.ReconcileCharacterVisibility(bob)
	world.mutex.Lock()
	newCell := world.visibilityCellByUID[bob.GetUniqueID()]
	oldObjects := append([]model.ISRObject(nil), world.visibilityObjectsByCell[oldCell]...)
	newObjects := append([]model.ISRObject(nil), world.visibilityObjectsByCell[newCell]...)
	world.mutex.Unlock()
	if newCell == oldCell {
		t.Fatalf("movement retained old visibility cell %+v", oldCell)
	}
	for _, object := range oldObjects {
		if object == bob {
			t.Fatal("moving player remained in old visibility cell")
		}
	}
	found := false
	for _, object := range newObjects {
		found = found || object == bob
	}
	if !found {
		t.Fatal("moving player was not added to new visibility cell")
	}

	world.PlayerDisconnectedAfterSave(bob.GetUniqueID(), bob.CharName)
	world.mutex.Lock()
	_, indexed = world.visibilityCellByUID[bob.GetUniqueID()]
	world.mutex.Unlock()
	if indexed {
		t.Fatal("disconnected player remained in visibility cell index")
	}
}

func TestWorldVisibilityReconcileEmitsNoDuplicateDeltas(t *testing.T) {
	regionID := int16(25000)
	alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)

	if got := world.ReconcileCharacterVisibility(alice); len(got) != 2 {
		t.Fatalf("initial reconcile emitted %d deltas, want 2", len(got))
	}
	if got := world.ReconcileCharacterVisibility(alice); len(got) != 0 {
		t.Fatalf("repeat reconcile emitted duplicate deltas: %#v", got)
	}
}

func TestWorldVisibilityFiltersNonAlivePlayers(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*model.Player)
	}{
		{
			name: "spawning",
			configure: func(player *model.Player) {
				player.SetLifeState(model.Spawning)
			},
		},
		{
			name: "dead",
			configure: func(player *model.Player) {
				player.SetLifeState(model.Dead)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			regionID := int16(25000)
			alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
			bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
			world := newVisibilityTestWorld(t, alice, bob)
			alice.SetLifeState(model.Alive)
			test.configure(bob)

			if got := world.ReconcileCharacterVisibility(alice); len(got) != 0 {
				t.Fatalf("filtered player produced deltas: %#v", got)
			}
			if alice.GetKnownObjectList().Knows(bob) || bob.GetKnownObjectList().Knows(alice) {
				t.Fatal("filtered player entered a reciprocal known list")
			}
		})
	}
}

func TestWorldVisibilityHiddenPlayerStillObservesVisiblePeer(t *testing.T) {
	regionID := int16(25000)
	alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)
	bob.SetBodyState(model.Invisible)

	deltas := world.ReconcileCharacterVisibility(alice)
	want := []VisibilityDelta{{ObserverUID: bob.GetUniqueID(), SubjectUID: alice.GetUniqueID(), Kind: VisibilityAdded}}
	if !reflect.DeepEqual(deltas, want) {
		t.Fatalf("directional hidden deltas = %#v, want %#v", deltas, want)
	}
	if alice.GetKnownObjectList().Knows(bob) || !bob.GetKnownObjectList().Knows(alice) {
		t.Fatal("hidden subject visibility was not directional")
	}
}

func TestWorldVisibilityHiddenPlayerStillObservesNPC(t *testing.T) {
	regionID := int16(25000)
	player := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	player.SetLifeState(model.Alive)
	player.SetBodyState(model.Stealth)
	world := newVisibilityTestWorld(t, player)
	npc := newVisibilityTestNPC(visibilityTestPosition(regionID, 140, 100))
	world.AddVisibleObject(npc)

	world.ReconcileCharacterVisibility(player)
	if !player.GetKnownObjectList().Knows(npc) {
		t.Fatal("hidden player could not observe nearby NPC")
	}
	if npc.GetKnownObjectList().Knows(player) {
		t.Fatal("NPC observed hidden player")
	}
}

func TestWorldVisibilityDetectorRevealsOnlyCompatibleHideMaskAndLevel(t *testing.T) {
	regionID := int16(25000)
	observer := newVisibilityTestPlayer(1, "Observer", visibilityTestPosition(regionID, 100, 100))
	hidden := newVisibilityTestPlayer(2, "Hidden", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, observer, hidden)
	observer.SetLifeState(model.Alive)
	hidden.SetLifeState(model.Alive)
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		10: {ID: 10, HideType: 1, HideLevel: 8},
		20: {ID: 20, DetectionType: 5, DetectionLevel: 8, ReferenceRadius: 100},
	}})
	world.ConfigureReferenceData(catalog)
	if _, ok := hidden.CommitActiveBuff(10, 1001, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("commit hide")
	}
	hidden.SetBodyState(model.Stealth)
	if _, ok := observer.CommitActiveBuff(20, 2001, time.Minute, model.BuffModifiers{}); !ok {
		t.Fatal("commit detection")
	}

	world.ReconcileCharacterVisibility(observer)
	if !observer.GetKnownObjectList().Knows(hidden) {
		t.Fatal("compatible dtt mask and level did not reveal hidden player")
	}
	observer.RemoveActiveBuff(2001)
	world.ReconcileCharacterVisibility(observer)
	if observer.GetKnownObjectList().Knows(hidden) {
		t.Fatal("hidden player remained revealed after detection removal")
	}
}

func TestWorldVisibilityCrossRangeProducesOneReciprocalRemoval(t *testing.T) {
	regionID := int16(25000)
	alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
	bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)
	if got := world.ReconcileCharacterVisibility(alice); len(got) != 2 {
		t.Fatalf("initial reconcile emitted %d deltas, want 2", len(got))
	}

	// Keep the player in the same region so the regression exercises range,
	// not region membership. The old peer remains a known-list candidate even
	// after it is outside the visibility radius.
	alice.SetNavmeshPosition(visibilityTestPosition(regionID, 1_400, 100))
	deltas := world.ReconcileCharacterVisibility(alice)
	want := []VisibilityDelta{
		{ObserverUID: alice.GetUniqueID(), SubjectUID: bob.GetUniqueID(), Kind: VisibilityRemoved},
		{ObserverUID: bob.GetUniqueID(), SubjectUID: alice.GetUniqueID(), Kind: VisibilityRemoved},
	}
	if !reflect.DeepEqual(deltas, want) {
		t.Fatalf("cross-range deltas = %#v, want %#v", deltas, want)
	}
	if alice.GetKnownObjectList().Knows(bob) || bob.GetKnownObjectList().Knows(alice) {
		t.Fatal("cross-range reconciliation retained a reciprocal known pair")
	}
	if got := world.ReconcileCharacterVisibility(alice); len(got) != 0 {
		t.Fatalf("repeat cross-range reconcile emitted duplicate deltas: %#v", got)
	}
}

type visibilityNoopPositionRepository struct{}

func (visibilityNoopPositionRepository) UpdateCharacterPosition(int, int16, float32, float32, float32) error {
	return nil
}

func TestWorldVisibilityMovementAndStopTransitionsReconcile(t *testing.T) {
	for _, test := range []struct {
		name string
		move func(*testing.T, *WorldService, *model.Player, navmeshv2.RtNavmeshPosition)
	}{
		{
			name: "committed movement",
			move: func(t *testing.T, world *WorldService, player *model.Player, target navmeshv2.RtNavmeshPosition) {
				t.Helper()
				movement := player.BeginMovement(model.MovementData{HasDestination: true, TargetPosition: target})
				if !world.commitMovementStep(player, movement.Generation, target, time.Now(), true, "visibility-test", false) {
					t.Fatal("committed movement did not finalize")
				}
			},
		},
		{
			name: "stop",
			move: func(t *testing.T, world *WorldService, player *model.Player, target navmeshv2.RtNavmeshPosition) {
				t.Helper()
				if !world.StopMovingCharacter(player, target, "visibility-test", false) {
					t.Fatal("stop transition failed")
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			regionID := int16(25000)
			alice := newVisibilityTestPlayer(1, "Alice", visibilityTestPosition(regionID, 100, 100))
			bob := newVisibilityTestPlayer(2, "Bob", visibilityTestPosition(regionID, 140, 100))
			world := newVisibilityTestWorld(t, alice, bob)
			world.positionPersistence = NewPositionPersistenceService(visibilityNoopPositionRepository{}, time.Hour)
			alice.SetLifeState(model.Alive)
			bob.SetLifeState(model.Alive)
			if got := world.ReconcileCharacterVisibility(alice); len(got) != 2 {
				t.Fatalf("initial reconcile emitted %d deltas, want 2", len(got))
			}

			test.move(t, world, alice, visibilityTestPosition(regionID, 1_400, 100))
			if alice.GetKnownObjectList().Knows(bob) || bob.GetKnownObjectList().Knows(alice) {
				t.Fatal("movement/stop transition did not reconcile cross-range visibility")
			}
		})
	}
}

func TestWorldVisibilityDoesNotWrapNormalWorldEdges(t *testing.T) {
	origin := utils.XAndZToInt16(0, 0)
	wrappedX := utils.XAndZToInt16(255, 0)
	wrappedZ := utils.XAndZToInt16(0, 255)
	world := &WorldService{
		visibleObjectsByRegion: map[int16][]model.ISRObject{
			origin:   nil,
			wrappedX: nil,
			wrappedZ: nil,
		},
		mutex: &sync.Mutex{},
	}

	world.mutex.Lock()
	regionIDs := world.visibilityNeighbourRegionIDsLocked(origin)
	world.mutex.Unlock()
	if containsVisibilityRegionID(regionIDs, wrappedX) || containsVisibilityRegionID(regionIDs, wrappedZ) {
		t.Fatalf("normal-world edge wrapped to opposite region IDs: %#v", regionIDs)
	}
	if !containsVisibilityRegionID(regionIDs, origin) {
		t.Fatalf("normal-world edge omitted its own region: %#v", regionIDs)
	}
}

type blockingVisibilityKnownObjectList struct {
	addStarted chan struct{}
	releaseAdd chan struct{}
	once       sync.Once
}

func newBlockingVisibilityKnownObjectList() *blockingVisibilityKnownObjectList {
	return &blockingVisibilityKnownObjectList{
		addStarted: make(chan struct{}),
		releaseAdd: make(chan struct{}),
	}
}

func (l *blockingVisibilityKnownObjectList) AddObject(model.ISRObject) bool {
	select {
	case <-l.addStarted:
	default:
		close(l.addStarted)
	}
	<-l.releaseAdd
	return true
}

func (*blockingVisibilityKnownObjectList) RemoveObject(model.ISRObject) bool { return false }
func (*blockingVisibilityKnownObjectList) GetKnownObjects() map[uint32]model.ISRObject {
	return map[uint32]model.ISRObject{}
}
func (*blockingVisibilityKnownObjectList) Knows(model.ISRObject) bool { return false }

func (l *blockingVisibilityKnownObjectList) release() {
	l.once.Do(func() { close(l.releaseAdd) })
}

func TestWorldVisibilityDoesNotHoldWorldLockDuringKnownListUpdate(t *testing.T) {
	regionID := int16(25000)
	blockingList := newBlockingVisibilityKnownObjectList()
	alice := &model.Player{
		ID:       1,
		CharName: "Alice",
		SRObject: model.SRObject{
			KnownObjectList:   blockingList,
			RtNavmeshPosition: visibilityTestPosition(regionID, 100, 100),
		},
	}
	bob := &model.Player{
		ID:       2,
		CharName: "Bob",
		SRObject: model.SRObject{
			RtNavmeshPosition: visibilityTestPosition(regionID, 140, 100),
		},
	}
	bob.KnownObjectList = model.NewKnownObjectList(bob)
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)

	reconciled := make(chan struct{})
	go func() {
		_ = world.ReconcileCharacterVisibility(alice)
		close(reconciled)
	}()
	select {
	case <-blockingList.addStarted:
	case <-time.After(time.Second):
		t.Fatal("visibility reconcile did not reach the blocked known-list update")
	}

	lockAcquired := make(chan struct{})
	go func() {
		world.mutex.Lock()
		world.mutex.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
		// The known-list update remains blocked, proving reconciliation released
		// the world lock before it acquired the per-list lock.
	case <-time.After(time.Second):
		blockingList.release()
		t.Fatal("visibility reconciliation held the world lock during known-list update")
	}

	blockingList.release()
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("visibility reconciliation did not finish after known-list release")
	}
}

func TestWorldVisibilitySerializesReconcileAgainstDisconnect(t *testing.T) {
	regionID := int16(25000)
	blockingList := newBlockingVisibilityKnownObjectList()
	t.Cleanup(blockingList.release)
	alice := &model.Player{
		ID:       1,
		CharName: "Alice",
		SRObject: model.SRObject{
			KnownObjectList:   blockingList,
			RtNavmeshPosition: visibilityTestPosition(regionID, 100, 100),
		},
	}
	bob := &model.Player{
		ID:       2,
		CharName: "Bob",
		SRObject: model.SRObject{
			RtNavmeshPosition: visibilityTestPosition(regionID, 140, 100),
		},
	}
	bob.KnownObjectList = model.NewKnownObjectList(bob)
	world := newVisibilityTestWorld(t, alice, bob)
	alice.SetLifeState(model.Alive)
	bob.SetLifeState(model.Alive)

	reconciled := make(chan struct{})
	go func() {
		_ = world.ReconcileCharacterVisibility(alice)
		close(reconciled)
	}()
	select {
	case <-blockingList.addStarted:
	case <-time.After(time.Second):
		t.Fatal("visibility reconcile did not reach the blocked known-list update")
	}

	disconnected := make(chan struct{})
	go func() {
		world.PlayerDisconnected(alice.GetUniqueID(), alice.CharName)
		close(disconnected)
	}()
	select {
	case <-disconnected:
		t.Fatal("disconnect bypassed the in-flight visibility reconciliation")
	case <-time.After(25 * time.Millisecond):
		// Reconciliation owns the visibility transaction until it finishes its
		// reciprocal updates, so disconnect cannot delete and then be re-added.
	}

	blockingList.release()
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("visibility reconciliation did not finish after known-list release")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not finish after visibility reconciliation")
	}
	if _, err := world.GetPlayerByUniqueId(alice.GetUniqueID()); err == nil {
		t.Fatal("disconnect retained a reconciled player in the world index")
	}
	if bob.GetKnownObjectList().Knows(alice) {
		t.Fatal("disconnect left the reconciled player in the reciprocal known list")
	}
}

func TestResetPlayerVisibilityForTeleportDoesNotQueueOldWorldDespawns(t *testing.T) {
	oldRegionID := int16(25000)
	newRegionID := int16(25010)
	player := newVisibilityTestPlayer(1, "Traveler", visibilityTestPosition(oldRegionID, 100, 100))
	oldNPC := newVisibilityTestNPC(visibilityTestPosition(oldRegionID, 120, 100))
	newNPC := newVisibilityTestNPC(visibilityTestPosition(newRegionID, 120, 100))
	world := newVisibilityTestWorld(t, player)
	world.AddVisibleObject(oldNPC)
	world.AddVisibleObject(newNPC)

	world.ActivatePlayer(player)
	spawned := player.GetCharKnownObjectList().GetObjectsToSpawn()
	player.GetCharKnownObjectList().MarkSpawned(spawned)
	if !player.GetKnownObjectList().Knows(oldNPC) || !oldNPC.GetKnownObjectList().Knows(player) {
		t.Fatal("precondition: old-area visibility was not reciprocal")
	}

	player.SetLifeState(model.Spawning)
	world.ResetPlayerVisibilityForTeleport(player)
	if player.GetKnownObjectList().Knows(oldNPC) || oldNPC.GetKnownObjectList().Knows(player) {
		t.Fatal("teleport reset retained old-area visibility")
	}
	if despawns := player.GetCharKnownObjectList().GetObjectsToDespawn(); len(despawns) != 0 {
		t.Fatalf("teleport reset queued stale despawns: %#v", despawns)
	}
	if player.GetCharKnownObjectList().ClientKnows(oldNPC.GetUniqueID()) {
		t.Fatal("teleport reset retained old client-delivery state")
	}

	player.SetNavmeshPosition(visibilityTestPosition(newRegionID, 100, 100))
	world.ActivatePlayer(player)
	if despawns := player.GetCharKnownObjectList().GetObjectsToDespawn(); len(despawns) != 0 {
		t.Fatalf("destination activation queued stale despawns: %#v", despawns)
	}
	spawns := player.GetCharKnownObjectList().GetObjectsToSpawn()
	if spawns[newNPC.GetUniqueID()] != newNPC {
		t.Fatalf("destination activation spawns = %#v, want new NPC %d", spawns, newNPC.GetUniqueID())
	}
	if _, exists := spawns[oldNPC.GetUniqueID()]; exists {
		t.Fatalf("destination activation respawned old NPC %d", oldNPC.GetUniqueID())
	}
}

func newVisibilityTestWorld(t *testing.T, players ...*model.Player) *WorldService {
	t.Helper()
	world := &WorldService{
		visibleObjects:         make(map[uint32]model.ISRObject),
		playersByUniqueId:      make(map[uint32]*model.Player),
		playersByCharName:      make(map[string]*model.Player),
		playersByCharacterID:   make(map[int]*model.Player),
		npcsByUniqueId:         make(map[uint32]*model.NPC),
		visibleObjectsByRegion: make(map[int16][]model.ISRObject),
		movingObjects:          make(map[uint32]model.ICharacter),
		mutex:                  &sync.Mutex{},
	}
	for _, player := range players {
		if err := world.AddPlayer(player); err != nil {
			t.Fatalf("AddPlayer(%s): %v", player.CharName, err)
		}
	}
	return world
}

func newVisibilityTestPlayer(id int, name string, position navmeshv2.RtNavmeshPosition) *model.Player {
	player := &model.Player{
		ID:       id,
		CharName: name,
		BaseStats: model.BaseStats{
			HP: 100,
			MP: 100,
		},
		SRObject: model.SRObject{RtNavmeshPosition: position},
	}
	player.CharKnownObjectList = model.NewCharKnownObjectList(player)
	player.KnownObjectList = player.CharKnownObjectList
	player.SetBodyState(model.Normal)
	player.SetLifeState(model.Spawning)
	return player
}

func newVisibilityTestNPC(position navmeshv2.RtNavmeshPosition) *model.NPC {
	npc := &model.NPC{
		SRObject: model.NewSRObject(),
		Type:     "NPC",
		Mutex:    &sync.Mutex{},
	}
	npc.SetNavmeshPosition(position)
	npc.KnownObjectList = model.NewKnownObjectList(npc)
	npc.SetLifeState(model.Alive)
	return npc
}

func visibilityTestPosition(regionID int16, x, z float32) navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(regionID),
		Offset: math32.NewVector3(x, 0, z),
	}
}

func containsVisibilityRegionID(regionIDs []int16, want int16) bool {
	for _, regionID := range regionIDs {
		if regionID == want {
			return true
		}
	}
	return false
}
