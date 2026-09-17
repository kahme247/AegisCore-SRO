package service

import (
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

func TestCanMoveDirectlyRejectsBlockedTerrainBeforePursuit(t *testing.T) {
	world, source, target := directMotionTestWorld(t, navmeshv2.RtNavmeshEdgeFlag(3))
	if world.CanMoveDirectly(source, target) {
		t.Fatal("blocked straight-line motion was accepted")
	}
}

func TestCanMoveDirectlyAcceptsReachableTerrainBeforePursuit(t *testing.T) {
	world, source, target := directMotionTestWorld(t, navmeshv2.RtNavmeshEdgeFlag(0))
	if !world.CanMoveDirectly(source, target) {
		t.Fatal("reachable straight-line motion was rejected")
	}
}

func TestCanMoveDirectlyAcceptsDungeonLocalMotion(t *testing.T) {
	const regionID = int16(-32762)
	region := navmeshv2.NewRegionFromInt16(regionID)
	terrain := navmeshv2.NewRtNavmeshTerrain("dungeon-direct-motion", region)
	terrain.DungeonMinimum = math32.NewVector3(-20000, -1000, -10000)
	terrain.DungeonMaximum = math32.NewVector3(20000, 1000, 10000)
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}}}
	terrain.Cells[0].Mesh = &terrain
	world := &WorldService{regions: map[int16]*navmeshv2.RtNavmeshTerrain{regionID: &terrain}, mutex: &sync.Mutex{}}
	source := navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(-1000, 0, 500)}
	target := navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(-800, 0, 500)}
	if !world.CanMoveDirectly(source, target) {
		t.Fatal("reachable dungeon-local movement skill path was rejected")
	}
}

func TestFindTerrainCollisionAlongPathCatchesIntermediateCellWall(t *testing.T) {
	const regionID = int16(25000)
	region := navmeshv2.NewRegionFromInt16(regionID)
	edge := &navmeshv2.RtNavmeshEdgeBase{
		Line:         navmeshv2.LineSegment{A: math32.NewVector3(200, 0, 80), B: math32.NewVector3(200, 0, 120)},
		Flag:         navmeshv2.RtNavmeshEdgeFlag(3),
		SrcCellIndex: 1,
		DstCellIndex: 2,
	}
	terrain := navmeshv2.NewRtNavmeshTerrain("intermediate-wall-test", region)
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}},
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 1}, Edges: []navmeshv2.RtNavmeshEdge{edge}},
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 2}, Edges: []navmeshv2.RtNavmeshEdge{edge}},
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 3}},
	}
	for z := 0; z < navmeshv2.TilesY; z++ {
		for x := 0; x < navmeshv2.TilesX; x++ {
			localX := float32(x) * navmeshv2.TileWidth
			cell := 0
			switch {
			case localX >= 300:
				cell = 3
			case localX >= 200:
				cell = 2
			case localX >= 100:
				cell = 1
			}
			terrain.TileMap[z*navmeshv2.TilesY+x].CellIndex = cell
		}
	}
	world := &WorldService{
		regions: map[int16]*navmeshv2.RtNavmeshTerrain{regionID: &terrain},
		mutex:   &sync.Mutex{},
	}
	source, err := world.NewPosFromGlobalCoordinates(math32.NewVector3(
		float32(region.X)*navmeshv2.RegionWidth+80,
		0,
		float32(region.Y)*navmeshv2.RegionHeight+100,
	))
	if err != nil {
		t.Fatal(err)
	}
	target, err := world.NewPosFromGlobalCoordinates(math32.NewVector3(
		float32(region.X)*navmeshv2.RegionWidth+320,
		0,
		float32(region.Y)*navmeshv2.RegionHeight+100,
	))
	if err != nil {
		t.Fatal(err)
	}
	if collides, _ := navmeshv2.FindTerrainCollisions(source, target, terrain.Cells[0], terrain.Cells[3]); collides {
		t.Fatal("endpoint-only fixture unexpectedly observed the intermediate wall")
	}
	collides, collision, err := world.findTerrainCollisionAlongPath(source, target)
	if err != nil {
		t.Fatalf("findTerrainCollisionAlongPath() error = %v", err)
	}
	if !collides || collision.ContactEdge != edge {
		t.Fatalf("path collision = collides:%t edge:%v, want intermediate wall", collides, collision.ContactEdge)
	}
}

func directMotionTestWorld(t *testing.T, flag navmeshv2.RtNavmeshEdgeFlag) (*WorldService, navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) {
	t.Helper()
	const regionID = int16(25000)
	region := navmeshv2.NewRegionFromInt16(regionID)
	edge := &navmeshv2.RtNavmeshEdgeBase{
		Line:         navmeshv2.LineSegment{A: math32.NewVector3(100, 0, 80), B: math32.NewVector3(100, 0, 120)},
		Flag:         flag,
		SrcCellIndex: 0,
		DstCellIndex: 1,
	}
	terrain := navmeshv2.NewRtNavmeshTerrain("direct-motion-test", region)
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0}, Edges: []navmeshv2.RtNavmeshEdge{edge}},
		{RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 1}, Edges: []navmeshv2.RtNavmeshEdge{edge}},
	}
	for z := 0; z < navmeshv2.TilesY; z++ {
		for x := 0; x < navmeshv2.TilesX; x++ {
			cell := 0
			if float32(x)*navmeshv2.TileWidth >= 100 {
				cell = 1
			}
			terrain.TileMap[z*navmeshv2.TilesY+x].CellIndex = cell
		}
	}
	world := &WorldService{
		regions: map[int16]*navmeshv2.RtNavmeshTerrain{regionID: &terrain},
		mutex:   &sync.Mutex{},
	}
	source, err := world.NewPosFromGlobalCoordinates(math32.NewVector3(
		float32(region.X)*navmeshv2.RegionWidth+80,
		0,
		float32(region.Y)*navmeshv2.RegionHeight+100,
	))
	if err != nil {
		t.Fatal(err)
	}
	target, err := world.NewPosFromGlobalCoordinates(math32.NewVector3(
		float32(region.X)*navmeshv2.RegionWidth+120,
		0,
		float32(region.Y)*navmeshv2.RegionHeight+100,
	))
	if err != nil {
		t.Fatal(err)
	}
	return world, source, target
}
