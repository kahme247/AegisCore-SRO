package navmeshv2

import (
	"github.com/g3n/engine/math32"
)

const (
	CollisionTerrain = iota
	CollisionObject
)

const objectCollisionVerticalTolerance = float32(50)

type CollisionFlag byte

func (f CollisionFlag) IsTerrain() bool {
	return f == CollisionTerrain
}

func (f CollisionFlag) IsObject() bool {
	return f == CollisionObject
}

type Collision struct {
	VectorLocal  *math32.Vector3
	VectorGlobal *math32.Vector3
	ContactEdge  RtNavmeshEdge
	Region
	Flag          CollisionFlag
	ObjectWorldID int
	ObjectID      int16
}

func FindTerrainCollisions(currentPosition, nextPosition RtNavmeshPosition,
	currentCell, nextCell RtNavmeshCellQuad) (bool, Collision) {
	sameRegion := currentPosition.Region.ID == nextPosition.Region.ID
	line1 := LineSegment{
		A: currentPosition.Offset,
		B: nextPosition.Offset,
	}
	var currentGlobal *math32.Vector3
	var nearestIntersection math32.Vector2
	var nearestCollision Collision
	nearestDistance := math32.Inf(1)
	hasCollision := false
	consider := func(intersection math32.Vector2, edge RtNavmeshEdge, region Region) {
		var distance float32
		if sameRegion {
			dx := currentPosition.Offset.X - intersection.X
			dy := currentPosition.Offset.Y
			dz := currentPosition.Offset.Z - intersection.Y
			distance = dx*dx + dy*dy + dz*dz
		} else {
			dx := currentGlobal.X - intersection.X
			dy := currentGlobal.Y
			dz := currentGlobal.Z - intersection.Y
			distance = dx*dx + dy*dy + dz*dz
		}
		if distance <= nearestDistance {
			nearestDistance = distance
			nearestIntersection = intersection
			nearestCollision = Collision{
				ContactEdge: edge,
				Region:      region,
				Flag:        CollisionTerrain,
			}
			hasCollision = true
		}
	}

	if sameRegion {

		if currentCell.Index == nextCell.Index {
			return false, Collision{}
		}

		for _, edge := range currentCell.Edges {
			if intersects, vIntersection := line1.IntersectsValue(edge.GetLine()); intersects {
				if edgeBlocksMovementFrom(edge, currentCell) {
					consider(vIntersection, edge, currentPosition.Region)
				}
			}
		}

		for _, edge := range nextCell.Edges {
			if intersects, vIntersection := line1.IntersectsValue(edge.GetLine()); intersects {
				if edgeBlocksMovementFrom(edge, currentCell) {
					consider(vIntersection, edge, nextPosition.Region)
				}
			}
		}

	} else {
		currentGlobal = currentPosition.GetGlobalCoordinates()
		vNextGlobal := nextPosition.GetGlobalCoordinates()
		line1 = LineSegment{
			A: currentGlobal,
			B: vNextGlobal,
		}

		for _, edge := range currentCell.Edges {
			a := RtNavmeshPosition{
				Cell:     &currentCell,
				Instance: nil,
				Region:   currentPosition.Region,
				Offset:   edge.GetLine().A,
			}
			b := RtNavmeshPosition{
				Cell:     &currentCell,
				Instance: nil,
				Region:   currentPosition.Region,
				Offset:   edge.GetLine().B,
			}

			line2 := LineSegment{
				A: a.GetGlobalCoordinates(),
				B: b.GetGlobalCoordinates(),
			}
			if intersects, vIntersection := line1.IntersectsValue(line2); intersects {
				if edgeBlocksCrossRegionMovement(edge, currentCell, false) {
					consider(vIntersection, edge, currentPosition.Region)
				}
			}
		}

		for _, edge := range nextCell.Edges {
			a := RtNavmeshPosition{
				Cell:     &nextCell,
				Instance: nil,
				Region:   nextPosition.Region,
				Offset:   edge.GetLine().A,
			}
			b := RtNavmeshPosition{
				Cell:     &nextCell,
				Instance: nil,
				Region:   nextPosition.Region,
				Offset:   edge.GetLine().B,
			}

			line2 := LineSegment{
				A: a.GetGlobalCoordinates(),
				B: b.GetGlobalCoordinates(),
			}
			if intersects, vIntersection := line1.IntersectsValue(line2); intersects {
				if edgeBlocksCrossRegionMovement(edge, nextCell, true) {
					consider(vIntersection, edge, nextPosition.Region)
				}
			}
		}
	}

	if !hasCollision {
		return false, Collision{}
	}
	if sameRegion {
		nearestCollision.VectorLocal = math32.NewVector3(nearestIntersection.X, 0, nearestIntersection.Y)
		nearestCollision.VectorGlobal = math32.NewVector3(nearestIntersection.X, 0, nearestIntersection.Y)
	} else {
		nearestCollision.VectorLocal = global2DToRegionLocal(&nearestIntersection, nearestCollision.Region)
		nearestCollision.VectorGlobal = math32.NewVector3(nearestIntersection.X, 0, nearestIntersection.Y)
	}
	return true, nearestCollision
}

// edgeBlocksMovementFrom preserves the directional meaning of the two blocked
// bits in a terrain edge. A one-way portal (common at town gates) must block
// only the prohibited direction; treating either bit as a solid wall traps a
// player on the permitted side.
func edgeBlocksMovementFrom(edge RtNavmeshEdge, currentCell RtNavmeshCellQuad) bool {
	flag := edge.GetFlag()
	if currentCell.Index == edge.GetSrcCellIndex() {
		return flag.IsBlockedSrcToDst()
	}
	if currentCell.Index == edge.GetDstCellIndex() {
		return flag.IsBlockedDstToSrc()
	}

	// Terrain edges are normally linked to the source cell, including blocked
	// outer boundaries. Retain the conservative behaviour for malformed or
	// synthetic edges that do not identify either endpoint.
	return flag.IsBlocked()
}

// edgeBlocksCrossRegionMovement applies directional flags from the side of
// the region seam where the edge is stored. A blocked global edge is linked
// only to its source cell, so its destination pointer is normally unavailable.
// The current-region scan leaves its attached cell; the destination-region
// scan enters its attached cell from the opposite side.
func edgeBlocksCrossRegionMovement(edge RtNavmeshEdge, attachedCell RtNavmeshCellQuad, entering bool) bool {
	flag := edge.GetFlag()
	isSource := edgeCellMatches(edge.GetSrcCell(), attachedCell)
	isDestination := edgeCellMatches(edge.GetDstCell(), attachedCell)
	if !isSource && !isDestination {
		// Blocked global edges do not link their destination cell. Recover the
		// attached side from the authored indexes when they are unambiguous.
		isSource = edge.GetSrcCellIndex() == attachedCell.Index && edge.GetDstCellIndex() != attachedCell.Index
		isDestination = edge.GetDstCellIndex() == attachedCell.Index && edge.GetSrcCellIndex() != attachedCell.Index
	}

	if isSource {
		if entering {
			return flag.IsBlockedDstToSrc()
		}
		return flag.IsBlockedSrcToDst()
	}
	if isDestination {
		if entering {
			return flag.IsBlockedSrcToDst()
		}
		return flag.IsBlockedDstToSrc()
	}
	return flag.IsBlocked()
}

func edgeCellMatches(candidate RtNavmeshCell, cell RtNavmeshCellQuad) bool {
	if candidate == nil || candidate.GetIndex() != cell.Index {
		return false
	}
	return candidate.GetMesh() == cell.GetMesh()
}

func FindObjectCollisions(currentPosition, nextPosition RtNavmeshPosition,
	currentObjects, nextObjects []RtNavmeshInstObj) (bool, Collision, bool, *RtNavmeshPosition) {
	collides1, coll1, inObject1, objPos1 := FindCollisionsInObjects(currentPosition, nextPosition, currentObjects)
	collides2, coll2, inObject2, objPos2 := FindCollisionsInObjects(currentPosition, nextPosition, nextObjects)

	if collides1 && collides2 {
		currentGlobal := currentPosition.GetGlobalCoordinates()
		distance1 := currentGlobal.DistanceToSquared(regionLocalToGlobal(coll1.VectorGlobal, coll1.Region))
		distance2 := currentGlobal.DistanceToSquared(regionLocalToGlobal(coll2.VectorGlobal, coll2.Region))
		if distance1 <= distance2 {
			return true, coll1, inObject1, nil
		} else {
			return true, coll2, inObject2, nil
		}
	} else if collides1 {
		return true, coll1, inObject1, nil
	} else if collides2 {
		return true, coll2, inObject2, nil
	} else if inObject1 {
		return false, Collision{}, inObject1, objPos1
	} else if inObject2 {
		return false, Collision{}, inObject2, objPos2
	} else {
		return false, Collision{}, false, nil
	}
}

func FindCollisionsInObjects(cur, next RtNavmeshPosition, objects []RtNavmeshInstObj) (bool, Collision, bool, *RtNavmeshPosition) {
	inObj := false
	var objPos *RtNavmeshPosition
	for _, object := range objects {
		// Position offsets are local to their own region. Rebase both endpoints
		// into the object's region before applying the instance transform.
		vCurLocal := positionInRegion(cur, object.Region).ApplyMatrix4(object.GetWorldToLocal())
		vNextLocal := positionInRegion(next, object.Region).ApplyMatrix4(object.GetWorldToLocal())

		curInObject := isPositionInObjectLayer(object.Object, vCurLocal)
		nextInObject := isPositionInObjectLayer(object.Object, vNextLocal)
		if !curInObject && !nextInObject {
			// skip
			continue
		}

		line1 := LineSegment{
			A: vCurLocal.Clone(),
			B: vNextLocal.Clone(),
		}

		var nearestCollision Collision
		var nearestIntersection math32.Vector3
		nearestDistance := math32.Inf(1)
		hasCollision := false

		for edgeIndex := range object.Object.GlobalEdges {
			gEdge := &object.Object.GlobalEdges[edgeIndex]
			line2 := gEdge.GetLine()
			if intersects, vIntersection := line2.Intersects3DValue(line1); intersects {
				// Object meshes can overlap in X/Z while representing a different
				// floor, tunnel, or terrain layer. A collider far above or below
				// the character must not become an invisible wall.
				if math32.Abs(vCurLocal.Y-vIntersection.Y) > objectCollisionVerticalTolerance {
					continue
				}
				// Bridge edges are the authored transition between an object's
				// walkable surface and the surrounding terrain. They must remain
				// passable in both directions; treating a bridge as blocked while
				// exiting traps players inside structures such as Jangan's gates.
				if gEdge.GetFlag().IsBlocked() {
					candidate := Collision{
						ContactEdge:   gEdge,
						Region:        object.Region,
						Flag:          CollisionObject,
						ObjectWorldID: object.WorldID,
						ObjectID:      object.ID,
					}
					if distance := vCurLocal.DistanceToSquared(&vIntersection); distance <= nearestDistance {
						nearestDistance = distance
						nearestCollision = candidate
						nearestIntersection = vIntersection
						hasCollision = true
					}
				}
			}
		}

		for edgeIndex := range object.Object.InternalEdges {
			iEdge := &object.Object.InternalEdges[edgeIndex]
			line2 := iEdge.GetLine()
			if intersects, vIntersection := line2.Intersects3DValue(line1); intersects {
				if vIntersection.Y < vCurLocal.Y && vCurLocal.Y-vIntersection.Y > objectCollisionVerticalTolerance {
					continue
				}
				if iEdge.GetFlag().IsBlocked() {
					candidate := Collision{
						ContactEdge:   iEdge,
						Region:        object.Region,
						Flag:          CollisionObject,
						ObjectWorldID: object.WorldID,
						ObjectID:      object.ID,
					}
					if distance := vCurLocal.DistanceToSquared(&vIntersection); distance <= nearestDistance {
						nearestDistance = distance
						nearestCollision = candidate
						nearestIntersection = vIntersection
						hasCollision = true
					}
				}
			}
		}

		if nextInObject {
			inObj = true
			_, height := object.Object.FindHeight(vNextLocal)
			objectOffset := vNextLocal.Clone()
			objectOffset.Y = height
			objectOffset.ApplyMatrix4(object.LocalToWorld)
			objPos = &RtNavmeshPosition{Region: object.Region, Offset: objectOffset, Heading: next.Heading}
		}
		if !hasCollision {
			// no collision!
			continue
		}
		nearestCollision.VectorLocal = math32.NewVector3(nearestIntersection.X, nearestIntersection.Y, nearestIntersection.Z)
		nearestCollision.VectorGlobal = nearestCollision.VectorLocal.Clone().ApplyMatrix4(object.GetLocalToWorld())

		return true, nearestCollision, nextInObject, objPos
	}

	return false, Collision{}, inObj, objPos
}

func isPositionInObjectLayer(object RtNavmeshObj, position *math32.Vector3) bool {
	if !object.IsPositionInObjectGrid(position) {
		return false
	}
	for _, cell := range object.Cells {
		candidate := position.Clone()
		if inCell, height := cell.Triangle.FindHeight(candidate); inCell &&
			math32.Abs(position.Y-height) <= objectCollisionVerticalTolerance {
			return true
		}
	}
	return false
}

func positionInRegion(position RtNavmeshPosition, region Region) *math32.Vector3 {
	return globalToRegionLocal(position.GetGlobalCoordinates(), region)
}

func globalToRegionLocal(global *math32.Vector3, region Region) *math32.Vector3 {
	return math32.NewVector3(
		global.X-float32(region.X)*RegionWidth,
		global.Y,
		global.Z-float32(region.Y)*RegionHeight,
	)
}

func global2DToRegionLocal(global *math32.Vector2, region Region) *math32.Vector3 {
	return math32.NewVector3(
		global.X-float32(region.X)*RegionWidth,
		0,
		global.Y-float32(region.Y)*RegionHeight,
	)
}

func regionLocalToGlobal(local *math32.Vector3, region Region) *math32.Vector3 {
	return math32.NewVector3(
		float32(region.X)*RegionWidth+local.X,
		local.Y,
		float32(region.Y)*RegionHeight+local.Z,
	)
}

func findNearestGlobalCollision(globalPosition *math32.Vector3, collisions []Collision) (Collision, error) {
	if len(collisions) == 0 {
		return Collision{}, ErrEmptyCollisionList
	}

	delta := math32.Inf(1)
	var nearest Collision
	for _, collision := range collisions {
		collisionGlobal := collision.VectorGlobal
		if collision.Flag.IsObject() {
			collisionGlobal = regionLocalToGlobal(collision.VectorGlobal, collision.Region)
		}
		if distance := globalPosition.DistanceToSquared(collisionGlobal); distance <= delta {
			delta = distance
			nearest = collision
		}
	}
	return nearest, nil
}

func FindNearestCollision(localPosition *math32.Vector3, collisions []Collision) (Collision, error) {
	delta := math32.Inf(1)
	var nearestCollision Collision

	if len(collisions) == 0 {
		return nearestCollision, ErrEmptyCollisionList
	}

	for _, collision := range collisions {
		if distance := localPosition.DistanceToSquared(collision.VectorLocal); distance <= delta {
			delta = distance
			nearestCollision = collision
		}
	}

	return nearestCollision, nil
}
