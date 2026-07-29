package service

import (
	"sync"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
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
