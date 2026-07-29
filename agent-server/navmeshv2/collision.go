package navmeshv2

import (
	"github.com/g3n/engine/math32"
)

const (
	CollisionTerrain = iota
	CollisionObject
)

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
	Flag CollisionFlag
}

func FindTerrainCollisions(currentPosition, nextPosition RtNavmeshPosition,
	currentCell, nextCell RtNavmeshCellQuad) (bool, Collision) {
	collisions := make([]Collision, 0)
	line1 := LineSegment{
		A: currentPosition.Offset,
		B: nextPosition.Offset,
	}
	if currentPosition.Region.ID == nextPosition.Region.ID {

		if currentCell.Index == nextCell.Index {
			return false, Collision{}
		}

		for _, edge := range currentCell.Edges {
			if intersects, vIntersection := line1.Intersects(edge.GetLine()); intersects {
				if edgeBlocksMovementFrom(edge, currentCell, false) {
					collisions = append(collisions, Collision{
						VectorLocal:  math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						ContactEdge:  edge,
						Region:       currentPosition.Region,
						Flag:         CollisionTerrain,
					})
				}
			}
		}

		for _, edge := range nextCell.Edges {
			if intersects, vIntersection := line1.Intersects(edge.GetLine()); intersects {
				if edgeBlocksMovementFrom(edge, currentCell, false) {
					collisions = append(collisions, Collision{
						VectorLocal:  math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						ContactEdge:  edge,
						Region:       nextPosition.Region,
						Flag:         CollisionTerrain,
					})
				}
			}
		}

	} else {
		vCurGlobal := currentPosition.GetGlobalCoordinates()
		vNextGlobal := nextPosition.GetGlobalCoordinates()
		line1 = LineSegment{
			A: vCurGlobal,
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
			if intersects, vIntersection := line1.Intersects(line2); intersects {
				if edge.GetFlag().IsBlocked() {
					collisions = append(collisions, Collision{
						VectorLocal:  global2DToRegionLocal(vIntersection, currentPosition.Region),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						ContactEdge:  edge,
						Region:       currentPosition.Region,
						Flag:         CollisionTerrain,
					})
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
			if intersects, vIntersection := line1.Intersects(line2); intersects {
				if edge.GetFlag().IsBlocked() {
					collisions = append(collisions, Collision{
						VectorLocal:  global2DToRegionLocal(vIntersection, nextPosition.Region),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						ContactEdge:  edge,
						Region:       nextPosition.Region,
						Flag:         CollisionTerrain,
					})
				}
			}
		}
	}

	var collision Collision
	var err error
	if currentPosition.Region.ID == nextPosition.Region.ID {
		collision, err = FindNearestCollision(currentPosition.Offset, collisions)
	} else {
		collision, err = findNearestGlobalCollision(currentPosition.GetGlobalCoordinates(), collisions)
	}
	if err != nil {
		return false, Collision{}
	} else {
		return true, collision
	}
}

// edgeBlocksMovementFrom preserves the directional meaning of the two blocked
// bits in a terrain edge. A one-way portal (common at town gates) must block
// only the prohibited direction; treating either bit as a solid wall traps a
// player on the permitted side.
func edgeBlocksMovementFrom(edge RtNavmeshEdge, currentCell RtNavmeshCellQuad, crossRegion bool) bool {
	flag := edge.GetFlag()
	// A fully linked terrain edge carries concrete cell references. Synthetic
	// test data and malformed/unlinked edges only carry numeric indexes, whose
	// values are not meaningful across regions; retain the safe wall behavior.
	if crossRegion && (edge.GetSrcCell() == nil || edge.GetDstCell() == nil) {
		return flag.IsBlocked()
	}
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

		curInObject := object.Object.IsPositionInObjectCell(vCurLocal)
		nextInObject := object.Object.IsPositionInObjectCell(vNextLocal)
		if !curInObject && !nextInObject {
			// skip
			continue
		}

		line1 := LineSegment{
			A: vCurLocal.Clone(),
			B: vNextLocal.Clone(),
		}

		gCollisions := make([]Collision, 0)
		iCollisions := make([]Collision, 0)

		for _, gEdge := range object.Object.GlobalEdges {
			line2 := gEdge.GetLine()
			if intersects, vIntersection := line1.Intersects(line2); intersects {
				// Bridge edges are the authored transition between an object's
				// walkable surface and the surrounding terrain. They must remain
				// passable in both directions; treating a bridge as blocked while
				// exiting traps players inside structures such as Jangan's gates.
				if gEdge.GetFlag().IsBlocked() {
					gCollisions = append(gCollisions, Collision{
						VectorLocal:  math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y).ApplyMatrix4(object.GetLocalToWorld()),
						ContactEdge:  &gEdge,
						Region:       object.Region,
						Flag:         CollisionObject,
					})
				}
			}
		}

		for _, iEdge := range object.Object.InternalEdges {
			line2 := iEdge.GetLine()
			if intersects, vIntersection := line1.Intersects(line2); intersects {
				if iEdge.GetFlag().IsBlocked() {
					iCollisions = append(iCollisions, Collision{
						VectorLocal:  math32.NewVector3(vIntersection.X, 0, vIntersection.Y),
						VectorGlobal: math32.NewVector3(vIntersection.X, 0, vIntersection.Y).ApplyMatrix4(object.GetLocalToWorld()),
						ContactEdge:  &iEdge,
						Region:       object.Region,
						Flag:         CollisionObject,
					})
				}
			}
		}

		filteredGlobalCollisions := make([]Collision, 0)
		for _, collision := range gCollisions {
			flag := collision.ContactEdge.GetFlag()
			if flag.IsBlocked() {
				filteredGlobalCollisions = append(filteredGlobalCollisions, collision)
			}
		}

		finalCollisions := make([]Collision, 0)
		nearestGlobalCollision, err1 := FindNearestCollision(vCurLocal, filteredGlobalCollisions)
		nearestLocalCollision, err2 := FindNearestCollision(vCurLocal, iCollisions)

		if err1 == nil {
			finalCollisions = append(finalCollisions, nearestGlobalCollision)
		}
		if err2 == nil {
			finalCollisions = append(finalCollisions, nearestLocalCollision)
		}

		finalCollision, err3 := FindNearestCollision(vCurLocal, finalCollisions)

		if nextInObject {
			inObj = true
			_, height := object.Object.FindHeight(vNextLocal)
			objectOffset := vNextLocal.Clone()
			objectOffset.Y = height
			objectOffset.ApplyMatrix4(object.LocalToWorld)
			objPos = &RtNavmeshPosition{Region: object.Region, Offset: objectOffset, Heading: next.Heading}
		}
		if err3 == ErrEmptyCollisionList {
			// no collision!
			continue
		}

		return true, finalCollision, nextInObject, objPos
	}

	return false, Collision{}, inObj, objPos
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
