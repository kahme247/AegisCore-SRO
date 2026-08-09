package service

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestWorldServiceRejectsDuplicateCharacterWithoutOverwritingIndexes(t *testing.T) {
	world := &WorldService{
		visibleObjects:         make(map[uint32]model.ISRObject),
		playersByUniqueId:      make(map[uint32]*model.Player),
		playersByCharName:      make(map[string]*model.Player),
		playersByCharacterID:   make(map[int]*model.Player),
		visibleObjectsByRegion: make(map[int16][]model.ISRObject),
		movingObjects:          make(map[uint32]model.ICharacter),
		mutex:                  &sync.Mutex{},
	}
	first := &model.Player{ID: 7, CharName: "Alice", SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	second := &model.Player{ID: 8, CharName: "Alice", SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}

	if err := world.AddPlayer(first); err != nil {
		t.Fatalf("AddPlayer(first) error = %v", err)
	}
	if first.GetUniqueID() == 0 {
		t.Fatal("first player was assigned reserved UID 0")
	}
	if err := world.AddPlayer(second); !errors.Is(err, ErrPlayerAlreadyConnected) {
		t.Fatalf("AddPlayer(duplicate) error = %v, want ErrPlayerAlreadyConnected", err)
	}
	if got := world.playersByCharName["Alice"]; got != first {
		t.Fatal("duplicate player overwrote the character-name index")
	}
	if got := len(world.playersByUniqueId); got != 1 {
		t.Fatalf("unique-ID index entries = %d, want 1", got)
	}
	if got := world.playersByCharacterID[first.ID]; got != first {
		t.Fatal("first player was not indexed by immutable character ID")
	}
	duplicateID := &model.Player{ID: first.ID, CharName: "Alicia", SRObject: model.SRObject{RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	if err := world.AddPlayer(duplicateID); !errors.Is(err, ErrPlayerAlreadyConnected) {
		t.Fatalf("AddPlayer(duplicate character ID) error = %v, want ErrPlayerAlreadyConnected", err)
	}
	if got := len(world.visibleObjectsByRegion[25000]); got != 1 {
		t.Fatalf("visible region entries = %d, want 1", got)
	}
}

func TestWorldDisconnectUsesResolvedPlayerIdentity(t *testing.T) {
	world := &WorldService{
		visibleObjects:         make(map[uint32]model.ISRObject),
		playersByUniqueId:      make(map[uint32]*model.Player),
		playersByCharName:      make(map[string]*model.Player),
		playersByCharacterID:   make(map[int]*model.Player),
		visibleObjectsByRegion: make(map[int16][]model.ISRObject),
		movingObjects:          make(map[uint32]model.ICharacter),
		mutex:                  &sync.Mutex{},
	}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}
	alice := &model.Player{ID: 7, CharName: "Alice", SRObject: model.SRObject{RtNavmeshPosition: position}}
	bob := &model.Player{ID: 8, CharName: "Bob", SRObject: model.SRObject{RtNavmeshPosition: position}}
	if err := world.AddPlayer(alice); err != nil {
		t.Fatalf("AddPlayer(Alice) error = %v", err)
	}
	if err := world.AddPlayer(bob); err != nil {
		t.Fatalf("AddPlayer(Bob) error = %v", err)
	}

	// Teardown is called with stale name data. It must remove Alice by the UID
	// lookup result, without releasing Bob's active name or character-ID slot.
	world.PlayerDisconnected(alice.GetUniqueID(), bob.CharName)
	if _, exists := world.playersByCharName[alice.CharName]; exists {
		t.Fatal("disconnect retained the resolved player's name index")
	}
	if _, exists := world.playersByCharacterID[alice.ID]; exists {
		t.Fatal("disconnect retained the resolved player's character-ID index")
	}
	if got := world.playersByCharName[bob.CharName]; got != bob {
		t.Fatal("stale teardown input removed another player's name reservation")
	}
	if got := world.playersByCharacterID[bob.ID]; got != bob {
		t.Fatal("stale teardown input removed another player's character reservation")
	}
}

func TestWorldSnapshotsDoNotExposeMutableCollectionBacking(t *testing.T) {
	first := &model.Player{CharName: "Alice", SRObject: model.SRObject{UniqueID: 1, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	second := &model.Player{CharName: "Bob", SRObject: model.SRObject{UniqueID: 2, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}}
	world := &WorldService{
		regions:                map[int16]*navmeshv2.RtNavmeshTerrain{25000: {Region: navmeshv2.Region{ID: 25000}}},
		spawns:                 map[int16][]*model.SpawnArea{25000: {}},
		playersByUniqueId:      map[uint32]*model.Player{2: second, 1: first},
		visibleObjectsByRegion: map[int16][]model.ISRObject{25000: {first}},
		mutex:                  &sync.Mutex{},
	}

	visible := world.GetVisibleObjectsForRegion(25000)
	regions := world.GetRegions()
	world.mutex.Lock()
	world.visibleObjectsByRegion[25000][0] = second
	delete(world.regions, 25000)
	world.mutex.Unlock()
	if got, want := visible[0], model.ISRObject(first); got != want {
		t.Fatal("visible-object snapshot shared its mutable backing array")
	}
	if _, exists := regions[25000]; !exists {
		t.Fatal("regions snapshot shared its mutable map")
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range 2_000 {
			for _, object := range world.GetVisibleObjectsForRegion(25000) {
				_ = object.GetUniqueID()
			}
		}
	}()
	for i := 0; i < 2_000; i++ {
		world.mutex.Lock()
		if i%2 == 0 {
			world.visibleObjectsByRegion[25000] = []model.ISRObject{first}
		} else {
			world.visibleObjectsByRegion[25000] = []model.ISRObject{second}
		}
		world.mutex.Unlock()
	}
	<-finished
}

func TestWorldActivePlayerSnapshotExcludesCharacterLoadingSessions(t *testing.T) {
	loadingClient, loadingServer := net.Pipe()
	defer loadingClient.Close()
	defer loadingServer.Close()
	activeClient, activeServer := net.Pipe()
	defer activeClient.Close()
	defer activeServer.Close()
	loadingSession := server.NewSession(loadingServer, network.EncodingOptions{}, nil, nil, "loading")
	activeSession := server.NewSession(activeServer, network.EncodingOptions{}, nil, nil, "active")
	defer loadingSession.Close()
	defer activeSession.Close()
	loadingSession.MarkAuthenticated()
	activeSession.MarkAuthenticated()
	if !loadingSession.EnterLobby() || !loadingSession.BeginCharacterLoading() {
		t.Fatal("failed to prepare loading session")
	}
	if !activeSession.EnterLobby() || !activeSession.BeginCharacterLoading() || !activeSession.CompleteCharacterLoading() {
		t.Fatal("failed to prepare active session")
	}
	loading := &model.Player{Session: loadingSession, SRObject: model.SRObject{UniqueID: 1}}
	active := &model.Player{Session: activeSession, SRObject: model.SRObject{UniqueID: 2}}
	world := &WorldService{
		playersByUniqueId: map[uint32]*model.Player{2: active, 1: loading},
		mutex:             &sync.Mutex{},
	}

	players := world.GetActivePlayers()
	if len(players) != 1 || players[0] != active {
		t.Fatalf("active player snapshot = %#v, want only in-world player", players)
	}
	players[0] = loading
	if got := world.GetActivePlayers(); len(got) != 1 || got[0] != active {
		t.Fatal("active player snapshot shared its mutable backing array")
	}
}
