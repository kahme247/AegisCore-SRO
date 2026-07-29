package navmeshv2

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/g3n/engine/math32"
)

type testGridNavmesh struct{}

func (testGridNavmesh) GetNavmeshType() RtNavmeshType { return RtNavmeshTypeTerrain }
func (testGridNavmesh) GetFilename() string           { return "test-grid" }
func (testGridNavmesh) GetCell(int) RtNavmeshCell     { return nil }

func TestRtNavmeshEdgeGlobalGetMeshUsesEmbeddedBase(t *testing.T) {
	mesh := testGridNavmesh{}
	edge := &RtNavmeshEdgeGlobal{RtNavmeshEdgeBase: RtNavmeshEdgeBase{Mesh: mesh}}

	var interfaceEdge RtNavmeshEdge = edge
	if got := interfaceEdge.GetMesh(); got != mesh {
		t.Fatalf("GetMesh() = %#v, want embedded mesh %#v", got, mesh)
	}
}

func TestMeshFileLoadObjectGridUsesWidthForRows(t *testing.T) {
	const (
		width  = 3
		height = 2
	)

	content := make([]byte, 20+width*height*4)
	binary.LittleEndian.PutUint32(content[0:4], math.Float32bits(10))
	binary.LittleEndian.PutUint32(content[4:8], math.Float32bits(20))
	binary.LittleEndian.PutUint32(content[8:12], width)
	binary.LittleEndian.PutUint32(content[12:16], height)
	binary.LittleEndian.PutUint32(content[16:20], width*height)

	meshFile := MeshFile{FileContent: content}
	object := NewNavmeshObj("non-square-grid")
	meshFile.loadObjectGrid(&object)

	for index, want := range []struct{ x, y int }{
		{0, 0}, {1, 0}, {2, 0},
		{0, 1}, {1, 1}, {2, 1},
	} {
		tile := object.Grid.Tiles[index]
		if tile.X != want.x || tile.Y != want.y {
			t.Fatalf("tile %d coordinates = (%d, %d), want (%d, %d)", index, tile.X, tile.Y, want.x, want.y)
		}
	}

	lastTile, ok := object.Grid.TryGetTileFromXAndY(2, 1)
	if !ok || lastTile.Index != 5 {
		t.Fatalf("last non-square tile = (%+v, %t), want index 5", lastTile, ok)
	}
	if lastTile.Rectangle.X() != 210 || lastTile.Rectangle.Y() != 120 {
		t.Fatalf("last tile rectangle origin = (%v, %v), want (210, 120)", lastTile.Rectangle.X(), lastTile.Rectangle.Y())
	}
}

func TestRtNavmeshObjGridTryGetTileBounds(t *testing.T) {
	grid := testRtNavmeshObjGrid(3, 2)

	if tile, ok := grid.TryGetTileFromXAndY(2, 1); !ok || tile.Index != 5 {
		t.Fatalf("TryGetTileFromXAndY(2, 1) = (%+v, %t), want index 5", tile, ok)
	}

	for _, coordinates := range [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 2}} {
		if _, ok := grid.TryGetTileFromXAndY(coordinates[0], coordinates[1]); ok {
			t.Fatalf("TryGetTileFromXAndY(%d, %d) unexpectedly succeeded", coordinates[0], coordinates[1])
		}
	}
	for _, index := range []int{-1, len(grid.Tiles)} {
		if _, ok := grid.TryGetTile(index); ok {
			t.Fatalf("TryGetTile(%d) unexpectedly succeeded", index)
		}
	}

	if tile, ok := grid.TryGetTileFromVec(math32.NewVector3(299.9, 0, 199.9)); !ok || tile.Index != 5 {
		t.Fatalf("TryGetTileFromVec(last point) = (%+v, %t), want index 5", tile, ok)
	}
	for _, point := range []*math32.Vector3{
		math32.NewVector3(-0.1, 0, 0),
		math32.NewVector3(300, 0, 0),
		math32.NewVector3(0, 0, 200),
	} {
		if _, ok := grid.TryGetTileFromVec(point); ok {
			t.Fatalf("TryGetTileFromVec(%+v) unexpectedly succeeded", point)
		}
	}
	if _, ok := grid.TryGetTileFromVec2(math32.NewVector2(300, 0)); ok {
		t.Fatal("TryGetTileFromVec2() accepted the exclusive maximum X edge")
	}
}

func TestRtNavmeshObjTestOutlineIntersectionOutsideGridDoesNotPanic(t *testing.T) {
	object := RtNavmeshObj{Grid: testRtNavmeshObjGrid(2, 1)}
	line := LineSegment{
		A: math32.NewVector3(-250, 0, 50),
		B: math32.NewVector3(-150, 0, 50),
	}

	if object.TestOutlineIntersection(line) {
		t.Fatal("line outside the object grid unexpectedly intersected its outline")
	}
}

func testRtNavmeshObjGrid(width, height int) RtNavmeshObjGrid {
	grid := RtNavmeshObjGrid{
		Width:     width,
		Height:    height,
		Rectangle: NewRectangle(0, 0, float32(width)*RtNavmeshObjGridTileWidth, float32(height)*RtNavmeshObjGridTileHeight),
		Tiles:     make([]RtNavmeshObjGridTile, width*height),
	}
	for index := range grid.Tiles {
		grid.Tiles[index] = RtNavmeshObjGridTile{
			Index: index,
			X:     index % width,
			Y:     index / width,
		}
	}

	return grid
}
