package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type worldSnapshotNoopPositionRepository struct{}

func (worldSnapshotNoopPositionRepository) UpdateCharacterPosition(int, int16, float32, float32, float32) error {
	return nil
}

type blockingWorldKnownObjectList struct {
	removeStarted chan struct{}
	releaseRemove chan struct{}
	releaseOnce   sync.Once
}

func newBlockingWorldKnownObjectList() *blockingWorldKnownObjectList {
	return &blockingWorldKnownObjectList{
		removeStarted: make(chan struct{}),
		releaseRemove: make(chan struct{}),
	}
}

func (*blockingWorldKnownObjectList) AddObject(model.ISRObject) bool { return false }

func (l *blockingWorldKnownObjectList) RemoveObject(model.ISRObject) bool {
	select {
	case <-l.removeStarted:
	default:
		close(l.removeStarted)
	}
	<-l.releaseRemove
	return true
}

func (*blockingWorldKnownObjectList) GetKnownObjects() map[uint32]model.ISRObject {
	return map[uint32]model.ISRObject{}
}

func (*blockingWorldKnownObjectList) Knows(model.ISRObject) bool { return false }

func (l *blockingWorldKnownObjectList) release() {
	l.releaseOnce.Do(func() { close(l.releaseRemove) })
}

func TestPlayerDisconnectDoesNotHoldWorldLockAcrossKnownObjectCleanup(t *testing.T) {
	regionID := int16(25000)
	player := &model.Player{
		ID:       7,
		CharName: "Alice",
		SRObject: model.SRObject{
			UniqueID: 1,
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.Region{ID: regionID},
				Offset: math32.NewVector3(1, 2, 3),
			},
		},
	}
	player.CharKnownObjectList = model.NewCharKnownObjectList(player)
	knownList := newBlockingWorldKnownObjectList()
	observer := &model.SRObject{KnownObjectList: knownList, UniqueID: 2}
	world := &WorldService{
		visibleObjects: map[uint32]model.ISRObject{
			player.GetUniqueID():   player,
			observer.GetUniqueID(): observer,
		},
		playersByUniqueId: map[uint32]*model.Player{
			player.GetUniqueID(): player,
		},
		playersByCharName: map[string]*model.Player{
			player.CharName: player,
		},
		playersByCharacterID: map[int]*model.Player{
			player.ID: player,
		},
		movingObjects: map[uint32]model.ICharacter{
			player.GetUniqueID(): player,
		},
		visibleObjectsByRegion: map[int16][]model.ISRObject{
			regionID: {player, observer},
		},
		mutex:               &sync.Mutex{},
		positionPersistence: NewPositionPersistenceService(worldSnapshotNoopPositionRepository{}, time.Hour),
	}

	disconnected := make(chan struct{})
	go func() {
		world.PlayerDisconnected(player.GetUniqueID(), player.CharName)
		close(disconnected)
	}()

	select {
	case <-knownList.removeStarted:
	case <-time.After(time.Second):
		knownList.release()
		t.Fatal("disconnect did not enter known-object cleanup")
	}

	lockAcquired := make(chan struct{})
	go func() {
		world.mutex.Lock()
		world.mutex.Unlock()
		close(lockAcquired)
	}()
	select {
	case <-lockAcquired:
		// RemoveObject is still deliberately blocked above, so this proves
		// that disconnect released the world mutex before external cleanup.
	case <-time.After(time.Second):
		knownList.release()
		t.Fatal("disconnect held the world mutex during known-object cleanup")
	}

	knownList.release()
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not finish after known-object cleanup released")
	}

	if _, exists := world.playersByUniqueId[player.GetUniqueID()]; exists {
		t.Fatal("disconnected player remained in the unique-ID index")
	}
	if _, exists := world.playersByCharName[player.CharName]; exists {
		t.Fatal("disconnected player remained in the name index")
	}
	if _, exists := world.playersByCharacterID[player.ID]; exists {
		t.Fatal("disconnected player remained in the character-ID index")
	}
	if _, exists := world.visibleObjects[player.GetUniqueID()]; exists {
		t.Fatal("disconnected player remained in the visible-object index")
	}
	if got := len(world.visibleObjectsByRegion[regionID]); got != 1 {
		t.Fatalf("region membership count = %d, want observer only", got)
	}
}

func TestWorldTerrainSnapshotsDetachCollectionBacking(t *testing.T) {
	regionID := int16(25000)
	terrain := navmeshv2.RtNavmeshTerrain{
		Region: navmeshv2.NewRegionFromInt16(regionID),
		Objects: []navmeshv2.RtNavmeshInstObj{{
			WorldID:   7,
			EdgeLinks: []navmeshv2.RtNavmeshEdgeLink{{EdgeID: 8}},
		}},
		Cells: []navmeshv2.RtNavmeshCellQuad{{
			Objects: []navmeshv2.RtNavmeshInstObj{{WorldID: 9}},
		}},
		GlobalEdges: []navmeshv2.RtNavmeshEdgeGlobal{{
			RtNavmeshEdgeBase: navmeshv2.RtNavmeshEdgeBase{Index: 10},
		}},
		InternalEdges: []navmeshv2.RtNavmeshEdgeInternal{{
			RtNavmeshEdgeBase: navmeshv2.RtNavmeshEdgeBase{Index: 11},
		}},
	}
	world := &WorldService{
		regions: map[int16]*navmeshv2.RtNavmeshTerrain{regionID: &terrain},
		mutex:   &sync.Mutex{},
	}

	regionSnapshot, err := world.GetRegion(regionID)
	if err != nil {
		t.Fatalf("GetRegion: %v", err)
	}
	regionSnapshot.Objects[0].WorldID = 70
	regionSnapshot.Objects[0].EdgeLinks[0].EdgeID = 80
	regionSnapshot.Cells[0].Objects[0].WorldID = 90
	regionSnapshot.GlobalEdges[0].Index = 100
	regionSnapshot.InternalEdges[0].Index = 110

	allSnapshots := world.GetRegions()
	allSnapshot := allSnapshots[regionID]
	allSnapshot.Objects[0].WorldID = 700
	delete(allSnapshots, regionID)

	neighbourSnapshots := world.GetNeighbourRegions(regionID)
	if len(neighbourSnapshots) != 1 {
		t.Fatalf("neighbour snapshot count = %d, want 1", len(neighbourSnapshots))
	}
	neighbourSnapshots[0].Cells[0].Objects[0].WorldID = 900

	actual, err := world.GetRegion(regionID)
	if err != nil {
		t.Fatalf("GetRegion after snapshot mutations: %v", err)
	}
	if actual.Objects[0].WorldID != 7 || actual.Objects[0].EdgeLinks[0].EdgeID != 8 {
		t.Fatalf("object snapshot mutation changed world terrain: %+v", actual.Objects[0])
	}
	if actual.Cells[0].Objects[0].WorldID != 9 {
		t.Fatalf("cell snapshot mutation changed world terrain: %+v", actual.Cells[0].Objects[0])
	}
	if actual.GlobalEdges[0].Index != 10 || actual.InternalEdges[0].Index != 11 {
		t.Fatalf("edge snapshot mutation changed world terrain: global=%d internal=%d", actual.GlobalEdges[0].Index, actual.InternalEdges[0].Index)
	}
}

func TestWorldNeighbourPlanesReturnsDefinedErrors(t *testing.T) {
	regionID := int16(25000)
	world := &WorldService{
		regions: make(map[int16]*navmeshv2.RtNavmeshTerrain),
		mutex:   &sync.Mutex{},
	}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(regionID)}

	if err := world.GetNeighbourPlanes(position); !errors.Is(err, ErrRegionNotExisting) {
		t.Fatalf("missing region error = %v, want ErrRegionNotExisting", err)
	}

	world.regions[regionID] = &navmeshv2.RtNavmeshTerrain{Region: navmeshv2.NewRegionFromInt16(regionID)}
	if err := world.GetNeighbourPlanes(position); !errors.Is(err, ErrNeighbourPlanesUnavailable) {
		t.Fatalf("unimplemented neighbour-plane error = %v, want ErrNeighbourPlanesUnavailable", err)
	}
}
