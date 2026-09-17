package service

import (
	"fmt"
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
	if navmeshv2.IsDungeonRegion(source.Region.ID) || navmeshv2.IsDungeonRegion(target.Region.ID) {
		return w.canMoveDirectlyInDungeon(source, target)
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

func (w *WorldService) canMoveDirectlyInDungeon(source, target navmeshv2.RtNavmeshPosition) bool {
	if source.Region.ID != target.Region.ID || !navmeshv2.IsDungeonRegion(source.Region.ID) || source.Offset == nil || target.Offset == nil {
		return false
	}
	delta := target.Offset.Clone().Sub(source.Offset.Clone())
	delta.Y = 0
	distance := delta.Length()
	if distance <= 0.0001 {
		return true
	}
	steps := int(math.Ceil(float64(distance / directMotionProbeLength)))
	current := source
	for step := 1; step <= steps; step++ {
		ratio := float32(step) / float32(steps)
		next := navmeshv2.RtNavmeshPosition{
			Region: source.Region,
			Offset: math32.NewVector3(
				source.Offset.X+delta.X*ratio,
				source.Offset.Y+(target.Offset.Y-source.Offset.Y)*ratio,
				source.Offset.Z+delta.Z*ratio,
			),
		}
		if !w.canMoveDirectStep(current, next) {
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

	nextObjects := nextTerrain.Objects
	if nextTerrain == currentTerrain {
		// The same immutable instance slice would otherwise be transformed and
		// tested twice for every direct-motion probe inside one region.
		nextObjects = nil
	}
	hasCollision, _, inObject, _ := navmeshv2.FindObjectCollisions(
		current,
		next,
		currentTerrain.Objects,
		nextObjects,
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

// findTerrainCollisionAlongPath checks every short section of a movement
// segment. Looking only at the starting and ending cells can miss a wall owned
// exclusively by intermediate cells after a long scheduler pause or a high
// movement-speed update.
func (w *WorldService) findTerrainCollisionAlongPath(source, target navmeshv2.RtNavmeshPosition) (bool, navmeshv2.Collision, error) {
	if w == nil || source.Offset == nil || target.Offset == nil {
		return false, navmeshv2.Collision{}, fmt.Errorf("terrain motion path is invalid")
	}
	start := source.GetGlobalCoordinates()
	end := target.GetGlobalCoordinates()
	delta := end.Clone().Sub(start.Clone())
	delta.Y = 0
	distance := delta.Length()
	if distance <= 0.0001 {
		return false, navmeshv2.Collision{}, nil
	}

	steps := int(math.Ceil(float64(distance / directMotionProbeLength)))
	current := source
	for step := 1; step <= steps; step++ {
		ratio := float32(step) / float32(steps)
		probeGlobal := math32.NewVector3(
			start.X+delta.X*ratio,
			start.Y+(end.Y-start.Y)*ratio,
			start.Z+delta.Z*ratio,
		)
		next, err := w.NewPosFromGlobalCoordinates(probeGlobal)
		if err != nil {
			return false, navmeshv2.Collision{}, err
		}
		currentTerrain, err := w.lookupRegionPtr(current.Region.ID)
		if err != nil {
			return false, navmeshv2.Collision{}, err
		}
		currentCell, err := currentTerrain.ResolveCell(current.Offset)
		if err != nil {
			return false, navmeshv2.Collision{}, err
		}
		nextTerrain, err := w.lookupRegionPtr(next.Region.ID)
		if err != nil {
			return false, navmeshv2.Collision{}, err
		}
		nextCell, err := nextTerrain.ResolveCell(next.Offset)
		if err != nil {
			return false, navmeshv2.Collision{}, err
		}
		if current.Region.ID != next.Region.ID || currentCell.Index != nextCell.Index {
			if collides, collision := navmeshv2.FindTerrainCollisions(current, next, currentCell, nextCell); collides {
				return true, collision, nil
			}
		}
		current = next
	}
	return false, navmeshv2.Collision{}, nil
}
