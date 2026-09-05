package navmeshv2

import (
	"testing"

	"github.com/g3n/engine/math32"
)

func TestBlockedObjectBoundaryStopsMovement(t *testing.T) {
	object := RtNavmeshObj{
		Cells: []RtNavmeshCellTri{
			{Triangle: Triangle{
				A: math32.NewVector3(0, 0, 0),
				B: math32.NewVector3(10, 0, 0),
				C: math32.NewVector3(10, 0, 10),
			}},
			{Triangle: Triangle{
				A: math32.NewVector3(0, 0, 0),
				B: math32.NewVector3(10, 0, 10),
				C: math32.NewVector3(0, 0, 10),
			}},
		},
		GlobalEdges: []RtNavmeshEdgeGlobal{{
			RtNavmeshEdgeBase: RtNavmeshEdgeBase{
				Line: LineSegment{
					A: math32.NewVector3(5, 0, 0),
					B: math32.NewVector3(5, 0, 10),
				},
				Flag: RtNavmeshEdgeFlag(1),
			},
		}},
		Grid: RtNavmeshObjGrid{Rectangle: NewRectangle(0, 0, 10, 10)},
	}
	identity := math32.NewMatrix4()
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{
			Object:       object,
			LocalToWorld: identity,
			WorldToLocal: identity,
		},
		Region: Region{ID: 1},
	}
	current := RtNavmeshPosition{Region: Region{ID: 1}, Offset: math32.NewVector3(4, 0, 5)}
	next := RtNavmeshPosition{Region: Region{ID: 1}, Offset: math32.NewVector3(6, 0, 5)}

	collides, collision, _, _ := FindObjectCollisions(current, next, []RtNavmeshInstObj{instance}, []RtNavmeshInstObj{instance})
	if !collides {
		t.Fatal("blocked object boundary was discarded")
	}
	if collision.VectorGlobal == nil || math32.Abs(collision.VectorGlobal.X-5) > 0.001 {
		t.Fatalf("unexpected collision position: %+v", collision.VectorGlobal)
	}
}

func TestObjectBoundaryOnDifferentVerticalLayerDoesNotStopMovement(t *testing.T) {
	object := rectangularTestObject(0, 0, 10, 10, LineSegment{
		A: math32.NewVector3(5, 0, 0),
		B: math32.NewVector3(5, 0, 10),
	})
	object.GlobalEdges[0].Flag = RtNavmeshEdgeFlag(3)
	identity := math32.NewMatrix4()
	region := NewRegionFromXAndY(100, 100)
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity},
		Region:            region,
	}
	current := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(4, 74, 5)}
	next := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(6, 74, 5)}
	currentLocal := positionInRegion(current, region).ApplyMatrix4(instance.GetWorldToLocal())
	if math32.Abs(currentLocal.Y-74) > 0.001 {
		t.Fatalf("unexpected transformed height: %f", currentLocal.Y)
	}
	intersects, intersection := object.GlobalEdges[0].GetLine().Intersects3D(LineSegment{A: currentLocal, B: positionInRegion(next, region)})
	if !intersects || math32.Abs(currentLocal.Y-intersection.Y) <= objectCollisionVerticalTolerance {
		t.Fatalf("test does not cross a separate vertical layer: current=%v intersection=%v", currentLocal, intersection)
	}

	collides, collision, _, _ := FindObjectCollisions(current, next, []RtNavmeshInstObj{instance}, nil)
	if collides {
		t.Fatalf("object boundary 74 units below movement became an invisible wall: %+v", collision.VectorLocal)
	}
}

func TestDifferentVerticalLayerDoesNotAdoptObjectHeight(t *testing.T) {
	object := rectangularTestObject(0, 0, 10, 10, LineSegment{
		A: math32.NewVector3(10, 0, 0),
		B: math32.NewVector3(10, 0, 10),
	})
	identity := math32.NewMatrix4()
	region := NewRegionFromXAndY(100, 100)
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity},
		Region:            region,
	}
	current := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(4, 74, 5)}
	next := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(6, 74, 5)}

	_, _, inObject, objectPosition := FindObjectCollisions(current, next, []RtNavmeshInstObj{instance}, nil)
	if inObject || objectPosition != nil {
		t.Fatal("movement adopted the height of a distant overlapping object layer")
	}
}

func TestBlockedObjectBoundaryStillStopsFromContactPoint(t *testing.T) {
	object := RtNavmeshObj{
		Cells: []RtNavmeshCellTri{{Triangle: Triangle{
			A: math32.NewVector3(0, 0, 0),
			B: math32.NewVector3(10, 0, 0),
			C: math32.NewVector3(10, 0, 10),
		}}},
		GlobalEdges: []RtNavmeshEdgeGlobal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{
			Line: LineSegment{A: math32.NewVector3(5, 0, 0), B: math32.NewVector3(5, 0, 10)},
			Flag: RtNavmeshEdgeFlag(1),
		}}},
		Grid: RtNavmeshObjGrid{Rectangle: NewRectangle(0, 0, 10, 10)},
	}
	identity := math32.NewMatrix4()
	instance := RtNavmeshInstObj{RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity}, Region: Region{ID: 1}}
	current := RtNavmeshPosition{Region: Region{ID: 1}, Offset: math32.NewVector3(5, 0, 5)}
	next := RtNavmeshPosition{Region: Region{ID: 1}, Offset: math32.NewVector3(6, 0, 5)}

	collides, _, _, _ := FindObjectCollisions(current, next, []RtNavmeshInstObj{instance}, []RtNavmeshInstObj{instance})
	if !collides {
		t.Fatal("movement starting on a blocked boundary passed through the wall")
	}
}

func TestBridgeObjectBoundaryAllowsEntryAndExit(t *testing.T) {
	object := rectangularTestObject(0, 0, 10, 10, LineSegment{
		A: math32.NewVector3(10, 0, 0),
		B: math32.NewVector3(10, 0, 10),
	})
	object.GlobalEdges[0].Flag = RtNavmeshEdgeFlag(1 << 4)
	identity := math32.NewMatrix4()
	region := NewRegionFromXAndY(100, 100)
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity},
		Region:            region,
	}

	tests := []struct {
		name       string
		current    *math32.Vector3
		next       *math32.Vector3
		wantInObj  bool
		wantObjPos bool
	}{
		{name: "entry", current: math32.NewVector3(12, 0, 5), next: math32.NewVector3(5, 0, 5), wantInObj: true, wantObjPos: true},
		{name: "exit", current: math32.NewVector3(5, 0, 5), next: math32.NewVector3(12, 0, 5), wantInObj: false, wantObjPos: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cur := RtNavmeshPosition{Region: region, Offset: test.current}
			next := RtNavmeshPosition{Region: region, Offset: test.next}
			collides, _, inObject, objectPosition := FindObjectCollisions(cur, next, []RtNavmeshInstObj{instance}, nil)
			if collides {
				t.Fatal("bridge portal was treated as a collision")
			}
			if inObject != test.wantInObj {
				t.Fatalf("inObject = %t, want %t", inObject, test.wantInObj)
			}
			if (objectPosition != nil) != test.wantObjPos {
				t.Fatalf("objectPosition present = %t, want %t", objectPosition != nil, test.wantObjPos)
			}
		})
	}
}

func TestCrossRegionObjectDoesNotCreateLongLocalMovement(t *testing.T) {
	identity := math32.NewMatrix4()
	currentRegion := NewRegionFromXAndY(100, 100)
	nextRegion := NewRegionFromXAndY(100, 99)
	object := rectangularTestObject(0, 1880, 10, 30, LineSegment{
		A: math32.NewVector3(0, 0, 1894),
		B: math32.NewVector3(10, 0, 1894),
	})
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity},
		Region:            currentRegion,
	}
	current := RtNavmeshPosition{Region: currentRegion, Offset: math32.NewVector3(5, 0, 0)}
	next := RtNavmeshPosition{Region: nextRegion, Offset: math32.NewVector3(5, 0, 1895)}

	collides, _, _, _ := FindObjectCollisions(current, next, []RtNavmeshInstObj{instance}, nil)
	if collides {
		t.Fatal("short boundary crossing was interpreted as a 1,895-unit local movement")
	}
}

func TestCrossRegionObjectCollisionUsesOwningRegion(t *testing.T) {
	identity := math32.NewMatrix4()
	currentRegion := NewRegionFromXAndY(100, 100)
	nextRegion := NewRegionFromXAndY(100, 101)
	object := rectangularTestObject(0, 0, 10, 20, LineSegment{
		A: math32.NewVector3(0, 0, 5),
		B: math32.NewVector3(10, 0, 5),
	})
	instance := RtNavmeshInstObj{
		RtNavmeshInstBase: RtNavmeshInstBase{Object: object, LocalToWorld: identity, WorldToLocal: identity},
		Region:            nextRegion,
	}
	current := RtNavmeshPosition{Region: currentRegion, Offset: math32.NewVector3(5, 0, 1910)}
	next := RtNavmeshPosition{Region: nextRegion, Offset: math32.NewVector3(5, 0, 10)}

	collides, collision, _, _ := FindObjectCollisions(current, next, nil, []RtNavmeshInstObj{instance})
	if !collides {
		t.Fatal("blocked edge in the destination region was missed")
	}
	if collision.Region.ID != nextRegion.ID {
		t.Fatalf("collision belongs to region %d, got %d", nextRegion.ID, collision.Region.ID)
	}
	if collision.VectorGlobal == nil || math32.Abs(collision.VectorGlobal.Z-5) > 0.001 {
		t.Fatalf("unexpected destination-region collision position: %+v", collision.VectorGlobal)
	}
}

func TestCrossRegionTerrainChecksDestinationCellEdges(t *testing.T) {
	currentRegion := NewRegionFromXAndY(100, 100)
	nextRegion := NewRegionFromXAndY(100, 101)
	current := RtNavmeshPosition{Region: currentRegion, Offset: math32.NewVector3(5, 0, 1910)}
	next := RtNavmeshPosition{Region: nextRegion, Offset: math32.NewVector3(5, 0, 10)}
	blockedEdge := &RtNavmeshEdgeBase{
		Line: LineSegment{A: math32.NewVector3(0, 0, 5), B: math32.NewVector3(10, 0, 5)},
		// Entering destination cell 2 from source cell 1 is prohibited.
		Flag:         RtNavmeshEdgeFlag(1 << 1),
		SrcCellIndex: 1,
		DstCellIndex: 2,
	}
	currentCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 1}}
	nextCell := RtNavmeshCellQuad{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 2},
		Edges:             []RtNavmeshEdge{blockedEdge},
	}

	collides, collision := FindTerrainCollisions(current, next, currentCell, nextCell)
	if !collides {
		t.Fatal("blocked destination-region terrain edge was missed")
	}
	if collision.Region.ID != nextRegion.ID || math32.Abs(collision.VectorLocal.Z-5) > 0.001 {
		t.Fatalf("unexpected terrain collision: region=%d local=%+v", collision.Region.ID, collision.VectorLocal)
	}
}

func TestCrossRegionTerrainOneWayBoundaryAllowsCityExit(t *testing.T) {
	currentRegion := NewRegionFromXAndY(100, 100)
	nextRegion := NewRegionFromXAndY(100, 101)
	current := RtNavmeshPosition{Region: currentRegion, Offset: math32.NewVector3(5, 0, 1910)}
	next := RtNavmeshPosition{Region: nextRegion, Offset: math32.NewVector3(5, 0, 10)}
	edge := &RtNavmeshEdgeBase{
		Line: LineSegment{A: math32.NewVector3(0, 0, 1915), B: math32.NewVector3(10, 0, 1915)},
		// Destination-to-source is blocked, but leaving the source-side city
		// cell toward the neighboring outer region is permitted.
		Flag:         RtNavmeshEdgeFlag(1),
		SrcCellIndex: 1,
		DstCellIndex: 2,
	}
	currentCell := RtNavmeshCellQuad{
		RtNavmeshCellBase: RtNavmeshCellBase{Index: 1},
		Edges:             []RtNavmeshEdge{edge},
	}
	nextCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 2}}

	if collides, _ := FindTerrainCollisions(current, next, currentCell, nextCell); collides {
		t.Fatal("permitted city-exit crossing was treated as a wall at the region seam")
	}
}

func TestTerrainOneWayBoundaryAllowsPermittedDirection(t *testing.T) {
	region := NewRegionFromXAndY(100, 100)
	current := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(4, 0, 5)}
	next := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(6, 0, 5)}
	// The edge blocks destination-to-source (bit 0) only. Leaving source cell
	// 1 through a town gate to cell 2 is therefore valid.
	edge := &RtNavmeshEdgeBase{
		Line:         LineSegment{A: math32.NewVector3(5, 0, 0), B: math32.NewVector3(5, 0, 10)},
		Flag:         RtNavmeshEdgeFlag(1),
		SrcCellIndex: 1,
		DstCellIndex: 2,
	}
	currentCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 1}, Edges: []RtNavmeshEdge{edge}}
	nextCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 2}}

	if collides, _ := FindTerrainCollisions(current, next, currentCell, nextCell); collides {
		t.Fatal("permitted one-way terrain crossing was treated as a wall")
	}
}

func TestTerrainOneWayBoundaryBlocksProhibitedDirection(t *testing.T) {
	region := NewRegionFromXAndY(100, 100)
	current := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(6, 0, 5)}
	next := RtNavmeshPosition{Region: region, Offset: math32.NewVector3(4, 0, 5)}
	edge := &RtNavmeshEdgeBase{
		Line:         LineSegment{A: math32.NewVector3(5, 0, 0), B: math32.NewVector3(5, 0, 10)},
		Flag:         RtNavmeshEdgeFlag(1),
		SrcCellIndex: 1,
		DstCellIndex: 2,
	}
	currentCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 2}, Edges: []RtNavmeshEdge{edge}}
	nextCell := RtNavmeshCellQuad{RtNavmeshCellBase: RtNavmeshCellBase{Index: 1}}

	if collides, _ := FindTerrainCollisions(current, next, currentCell, nextCell); !collides {
		t.Fatal("prohibited one-way terrain crossing was not blocked")
	}
}

func TestPositionRebasingAcrossEveryRegionBoundary(t *testing.T) {
	base := NewRegionFromXAndY(100, 100)
	tests := []struct {
		name     string
		position RtNavmeshPosition
		expectX  float32
		expectZ  float32
	}{
		{name: "west", position: RtNavmeshPosition{Region: NewRegionFromXAndY(99, 100), Offset: math32.NewVector3(1895, 0, 500)}, expectX: -25, expectZ: 500},
		{name: "east", position: RtNavmeshPosition{Region: NewRegionFromXAndY(101, 100), Offset: math32.NewVector3(25, 0, 500)}, expectX: 1945, expectZ: 500},
		{name: "north", position: RtNavmeshPosition{Region: NewRegionFromXAndY(100, 99), Offset: math32.NewVector3(500, 0, 1895)}, expectX: 500, expectZ: -25},
		{name: "south", position: RtNavmeshPosition{Region: NewRegionFromXAndY(100, 101), Offset: math32.NewVector3(500, 0, 25)}, expectX: 500, expectZ: 1945},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rebased := positionInRegion(test.position, base)
			if math32.Abs(rebased.X-test.expectX) > 0.001 || math32.Abs(rebased.Z-test.expectZ) > 0.001 {
				t.Fatalf("unexpected rebased position: %+v", rebased)
			}
		})
	}
}

func rectangularTestObject(x, z, width, height float32, blocked LineSegment) RtNavmeshObj {
	return RtNavmeshObj{
		Cells: []RtNavmeshCellTri{
			{Triangle: Triangle{A: math32.NewVector3(x, 0, z), B: math32.NewVector3(x+width, 0, z), C: math32.NewVector3(x+width, 0, z+height)}},
			{Triangle: Triangle{A: math32.NewVector3(x, 0, z), B: math32.NewVector3(x+width, 0, z+height), C: math32.NewVector3(x, 0, z+height)}},
		},
		GlobalEdges: []RtNavmeshEdgeGlobal{{RtNavmeshEdgeBase: RtNavmeshEdgeBase{Line: blocked, Flag: RtNavmeshEdgeFlag(1)}}},
		Grid:        RtNavmeshObjGrid{Rectangle: NewRectangle(x, z, width, height)},
	}
}
