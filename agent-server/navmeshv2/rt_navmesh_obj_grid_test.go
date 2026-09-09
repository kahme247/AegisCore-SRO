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
	if err := meshFile.loadObjectGrid(&object); err != nil {
		t.Fatalf("loadObjectGrid() error = %v", err)
	}

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

func TestMeshFileLoadObjectGridPersistsSpatialReferences(t *testing.T) {
	content := make([]byte, 24)
	binary.LittleEndian.PutUint32(content[8:12], 1)
	binary.LittleEndian.PutUint32(content[12:16], 1)
	binary.LittleEndian.PutUint32(content[16:20], 1)
	// The only tile has no authored global-edge references.
	binary.LittleEndian.PutUint32(content[20:24], 0)

	meshFile := MeshFile{FileContent: content}
	object := NewNavmeshObj("spatial-references")
	object.Cells = []RtNavmeshCellTri{{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 0},
		Triangle: Triangle{
			A: math32.NewVector3(10, 0, 10),
			B: math32.NewVector3(20, 0, 10),
			C: math32.NewVector3(10, 0, 20),
		},
	}}
	object.InternalEdges = []RtNavmeshEdgeInternal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Index: 0,
		Line: LineSegment{
			A: math32.NewVector3(10, 0, 10),
			B: math32.NewVector3(20, 0, 10),
		},
	}}}

	if err := meshFile.loadObjectGrid(&object); err != nil {
		t.Fatalf("loadObjectGrid() error = %v", err)
	}
	tile := object.Grid.Tiles[0]
	if len(tile.Cells) != 1 || tile.Cells[0].Index != 0 {
		t.Fatalf("grid tile cells = %#v, want cell 0", tile.Cells)
	}
	if len(tile.InternalEdges) != 1 || tile.InternalEdges[0].Index != 0 {
		t.Fatalf("grid tile internal edges = %#v, want edge 0", tile.InternalEdges)
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

func TestRtNavmeshObjTestOutlineIntersectionFindsBlockingEdgeInsideGrid(t *testing.T) {
	object := RtNavmeshObj{Grid: testRtNavmeshObjGrid(2, 1)}
	blocking := RtNavmeshEdgeGlobal{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Line: LineSegment{
			A: math32.NewVector3(100, 0, 0),
			B: math32.NewVector3(100, 0, 100),
		},
		Flag: RtNavmeshEdgeFlag(1),
	}}
	object.Grid.Tiles[0].GlobalEdges = []RtNavmeshEdgeGlobal{blocking}
	object.Grid.Tiles[1].GlobalEdges = []RtNavmeshEdgeGlobal{blocking}

	line := LineSegment{
		A: math32.NewVector3(50, 0, 50),
		B: math32.NewVector3(150, 0, 50),
	}
	if !object.TestOutlineIntersection(line) {
		t.Fatal("line crossing a blocking object edge was not detected")
	}
}

func TestRtNavmeshObjTestOutlineIntersectionIgnoresBridgeEdge(t *testing.T) {
	object := RtNavmeshObj{Grid: testRtNavmeshObjGrid(1, 1)}
	bridge := RtNavmeshEdgeGlobal{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
		Line: LineSegment{
			A: math32.NewVector3(50, 0, 0),
			B: math32.NewVector3(50, 0, 100),
		},
		Flag: RtNavmeshEdgeFlag(1 << 4),
	}}
	object.Grid.Tiles[0].GlobalEdges = []RtNavmeshEdgeGlobal{bridge}

	line := LineSegment{
		A: math32.NewVector3(25, 0, 50),
		B: math32.NewVector3(75, 0, 50),
	}
	if object.TestOutlineIntersection(line) {
		t.Fatal("line crossing only a bridge edge was treated as blocked")
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
