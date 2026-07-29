package navmeshv2

import (
	"fmt"
	"github.com/g3n/engine/math32"
)

type RtNavmeshPosition struct {
	Cell     RtNavmeshCell
	Instance RtNavmeshInst
	Region   Region
	Offset   *math32.Vector3
	Heading  float32
}

func (pos RtNavmeshPosition) GetMesh() RtNavmesh {
	return pos.Cell.GetMesh()
}

func (pos RtNavmeshPosition) GetGlobalCoordinates() *math32.Vector3 {
	xGlobal := float32(pos.Region.X)*RegionWidth + pos.Offset.X
	yGlobal := pos.Offset.Y
	zGlobal := float32(pos.Region.Y)*RegionHeight + pos.Offset.Z

	return math32.NewVector3(xGlobal, yGlobal, zGlobal)
}

// HorizontalDistance returns world-plane distance and deliberately ignores
// the client-facing vertical coordinate. vSRO movement range, sight, and
// melee reach are calculated from region X/Z plus local X/Z; terrain height
// must not make two visually adjacent entities appear out of range.
func HorizontalDistance(from, to RtNavmeshPosition) float32 {
	fromGlobal := from.GetGlobalCoordinates()
	toGlobal := to.GetGlobalCoordinates()
	dx := fromGlobal.X - toGlobal.X
	dz := fromGlobal.Z - toGlobal.Z
	return math32.Sqrt(dx*dx + dz*dz)
}

func (pos RtNavmeshPosition) String() string {
	return fmt.Sprintf("RtNavmeshPosition R(%d) (%f|%f|%f)", pos.Region.ID, pos.Offset.X, pos.Offset.Y, pos.Offset.Z)
}
