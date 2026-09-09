package navmeshv2

import (
	"github.com/g3n/engine/math32"
)

type RtNavmeshObj struct {
	RtNavmeshBase
	Cells         []RtNavmeshCellTri
	GlobalEdges   []RtNavmeshEdgeGlobal
	InternalEdges []RtNavmeshEdgeInternal
	Events        []string
	Grid          RtNavmeshObjGrid
}

func (obj RtNavmeshObj) IsPositionInObjectGrid(vPosLocal *math32.Vector3) bool {
	return obj.Grid.Rectangle.ContainsVec3(vPosLocal)
}

func (obj RtNavmeshObj) IsPositionInObjectCell(vPosLocal *math32.Vector3) bool {
	if !obj.IsPositionInObjectGrid(vPosLocal) {
		return false
	}

	for _, cell := range obj.Cells {
		// Triangle.FindHeight writes the resolved height back into its argument.
		// Containment is an X/Z query and must not change the caller's movement
		// height, which is needed to distinguish overlapping vertical layers.
		candidate := vPosLocal.Clone()
		if inTri, _ := cell.Triangle.FindHeight(candidate); inTri {
			return true
		}
	}

	return false
}

func (obj RtNavmeshObj) FindHeight(vPosLocal *math32.Vector3) (bool, float32) {
	distance := math32.Inf(1)
	var nearestPos *math32.Vector3
	rayOrigin := vPosLocal.Clone()
	rayOrigin.Y += 1000
	for _, cell := range obj.Cells {
		ray := math32.NewRay(rayOrigin, math32.NewVector3(0, -1, 0))
		result := math32.NewVec3()
		intersects := ray.IntersectTriangle(cell.Triangle.A, cell.Triangle.B, cell.Triangle.C, true, result)
		if intersects && vPosLocal.DistanceToSquared(result) <= distance {
			distance = vPosLocal.DistanceToSquared(result)
			nearestPos = result
		}
	}

	if nearestPos == nil {
		return false, 0
	}

	return true, nearestPos.Y

}

func (obj RtNavmeshObj) ResolveCell(vPosLocal *math32.Vector3) (RtNavmeshCellTri, error) {
	if !obj.IsPositionInObjectGrid(vPosLocal) {
		return RtNavmeshCellTri{}, ErrCellNotInObject
	}
	distance := math32.Inf(1)
	var c *RtNavmeshCellTri
	for _, cell := range obj.Cells {
		if inTri, y := cell.Triangle.FindHeight2(vPosLocal); inTri && math32.Abs(vPosLocal.Y-y) < distance {
			distance = math32.Abs(vPosLocal.Y - y)
			c = &cell
		}
	}
	if c == nil {
		return RtNavmeshCellTri{}, ErrNoObjectCellForPosition
	} else {
		return *c, nil
	}
}

func NewNavmeshObj(filename string) RtNavmeshObj {
	obj := RtNavmeshObj{
		RtNavmeshBase: RtNavmeshBase{Filename: filename},
	}
	obj.Grid = NewRtNavmeshObjGrid(&obj)
	return obj
}

func (obj RtNavmeshObj) GetNavmeshType() RtNavmeshType {
	return RtNavmeshTypeObject
}

func (obj RtNavmeshObj) GetCell(index int) RtNavmeshCell {
	return &obj.Cells[index]
}

func (obj RtNavmeshObj) TestOutlineIntersection(line LineSegment) bool {
	if obj.Grid.Width <= 0 || obj.Grid.Height <= 0 || len(obj.Grid.Tiles) == 0 {
		return false
	}

	if !obj.Grid.Rectangle.IntersectsLine(line) {
		return false
	}

	srcTileX := int((line.A.X - obj.Grid.X) / RtNavmeshObjGridTileWidth)
	srcTileZ := int((line.A.Z - obj.Grid.Y) / RtNavmeshObjGridTileHeight)

	dstTileX := int((line.B.X - obj.Grid.X) / RtNavmeshObjGridTileWidth)
	dstTileZ := int((line.B.Z - obj.Grid.Y) / RtNavmeshObjGridTileHeight)

	// TODO stay within grid
	//srcTileX = UnityEngine.Mathf.Clamp(srcTileX, 0, this.Grid.Width - 1);
	//srcTileY = UnityEngine.Mathf.Clamp(srcTileY, 0, this.Grid.Height - 1);
	//
	//dstTileX = UnityEngine.Mathf.Clamp(dstTileX, 0, this.Grid.Width - 1);
	//dstTileY = UnityEngine.Mathf.Clamp(dstTileY, 0, this.Grid.Height - 1);

	// swap if direction is negative
	tileX := srcTileX
	if srcTileX > dstTileX {
		srcTileX = dstTileX
		dstTileX = tileX
	}

	// swap if direction is negative
	tileZ := srcTileZ
	if srcTileZ > dstTileZ {
		srcTileZ = dstTileZ
		dstTileZ = tileZ
	}

	// Limit the scan to actual grid coordinates. Lines outside the grid used to
	// calculate negative or oversized indices and panic in GetTileFromXAndY.
	if dstTileX < 0 || srcTileX >= obj.Grid.Width || dstTileZ < 0 || srcTileZ >= obj.Grid.Height {
		return false
	}
	if srcTileX < 0 {
		srcTileX = 0
	}
	if dstTileX >= obj.Grid.Width {
		dstTileX = obj.Grid.Width - 1
	}
	if srcTileZ < 0 {
		srcTileZ = 0
	}
	if dstTileZ >= obj.Grid.Height {
		dstTileZ = obj.Grid.Height - 1
	}

	vSrc := math32.NewVector2(line.A.X, line.A.Z)

	var intersectionDistanceSquared float32 = 0.0
	var intersectionEdge RtNavmeshEdge = nil

	for tileZ = srcTileZ; tileZ <= dstTileZ; tileZ++ {
		for tileX = srcTileX; tileX <= dstTileX; tileX++ {
			tile, ok := obj.Grid.TryGetTileFromXAndY(tileX, tileZ)
			if !ok {
				continue
			}

			for _, edge := range tile.GlobalEdges {
				if edge.GetFlag().IsBridge() {
					continue
				}

				if intersects, intersPos := edge.GetLine().Intersects(line); intersects {
					vDelta := vSrc.Sub(intersPos)
					if intersectionEdge == nil || intersectionDistanceSquared > vDelta.LengthSq() {
						intersectionDistanceSquared = vDelta.LengthSq()
						intersectionEdge = &edge
					}
				}
			}
		}
	}

	return intersectionEdge != nil
}

/*
GetNavmeshType() byte
	GetFilename() string
	GetCell(index int) RtNavmeshCell
	ResolveCellAndHeight(vPos *math32.Vector3)
	ResolvePosition(pos RtNavmeshPosition)
*/
