package service

import (
	"math"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

const directMotionProbeLength = navmeshv2.TileWidth / 2

// CanMoveDirectly characterizes the original server's ResolveMotion guard for
// combat pursuit. It validates the complete straight-line segment before a
// movement is registered, so an obstructed attack cannot repeatedly run a
// player or NPC into the same terrain boundary.
func (w *WorldService) CanMoveDirectly(source, target navmeshv2.RtNavmeshPosition) bool {
	if w == nil {
		return false
	}
	start := source.GetGlobalCoordinates()
	end := target.GetGlobalCoordinates()
	delta := end.Clone().Sub(start.Clone())
	delta.Y = 0
	distance := delta.Length()
	if distance <= 0.0001 {
		return true
	}

	steps := int(math.Ceil(float64(distance / directMotionProbeLength)))
	current, err := w.NewPosFromGlobalCoordinates(start)
	if err != nil {
		return false
	}
	for step := 1; step <= steps; step++ {
		ratio := float32(step) / float32(steps)
		probeGlobal := math32.NewVector3(
			start.X+delta.X*ratio,
			start.Y,
			start.Z+delta.Z*ratio,
		)
		next, err := w.NewPosFromGlobalCoordinates(probeGlobal)
		if err != nil || !w.canMoveDirectStep(current, next) {
			return false
		}
		current = next
	}
	return true
}

func (w *WorldService) canMoveDirectStep(current, next navmeshv2.RtNavmeshPosition) bool {
	currentTerrain, err := w.lookupRegionPtr(current.Region.ID)
	if err != nil {
		return false
	}
	currentCell, err := currentTerrain.ResolveCell(current.Offset)
	if err != nil {
		return false
	}
	nextTerrain, err := w.lookupRegionPtr(next.Region.ID)
	if err != nil {
		return false
	}
	nextCell, err := nextTerrain.ResolveCell(next.Offset)
	if err != nil {
		return false
	}

	hasCollision, _, inObject, _ := navmeshv2.FindObjectCollisions(
		current,
		next,
		currentTerrain.Objects,
		nextTerrain.Objects,
	)
	if hasCollision {
		return false
	}
	if currentCell.Index == nextCell.Index || inObject {
		return true
	}
	hasCollision, _ = navmeshv2.FindTerrainCollisions(current, next, currentCell, nextCell)
	return !hasCollision
}
