package service

import (
	"math"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

func newCOSCompanionAI(pet *model.COS, settings model.COSGameplaySettings) *cosCompanionAI {
	seed := uint32(pet.PersistentID) ^ pet.GetRefObjectID() ^ uint32(pet.Kind)<<24
	if seed == 0 {
		seed = 1
	}
	// Stable follow slots spread companions while the player is travelling.
	// At rest the angle becomes only the starting point of a true orbit.
	angle := float32(seed%360) * (2 * math.Pi / 360)
	followRadius := settings.FollowBaseRadius + float32((seed>>9)%4)*settings.FollowRadiusStep
	direction := float32(1)
	if seed&1 != 0 {
		direction = -1
	}
	orbitMin, orbitVariance := companionOrbitRadius(pet.Kind, settings)
	return &cosCompanionAI{
		seed:           seed,
		followAngle:    angle,
		followRadius:   followRadius,
		orbitAngle:     angle,
		orbitRadius:    orbitMin + float32((seed>>12)%orbitVariance)/100,
		orbitDirection: direction,
	}
}

func companionOrbitRadius(kind model.COSKind, settings model.COSGameplaySettings) (float32, uint32) {
	if kind == model.COSAbilityPet {
		return settings.PickupIdleMinRadius, settings.PickupIdleVariance
	}
	return settings.OrbitMinRadius, settings.OrbitRadiusVariance
}

func companionRelocationRadius(kind model.COSKind, settings model.COSGameplaySettings) float32 {
	if kind == model.COSAbilityPet {
		return settings.PickupPetRelocateRange
	}
	return settings.CompanionRelocateRange
}

func usesCompanionFormation(kind model.COSKind) bool {
	switch kind {
	case model.COSGrowthPet, model.COSAbilityPet, model.COSCaptured, model.COSQuest, model.COSQuest2, model.COSFellowPet:
		return true
	default:
		return false
	}
}

func (s *COSService) companionTarget(world cosFollowerWorld, pet *model.COS, owner *model.Player, now time.Time, requireDirect bool) (navmeshv2.RtNavmeshPosition, bool) {
	if pet == nil || owner == nil {
		return navmeshv2.RtNavmeshPosition{}, false
	}
	if !usesCompanionFormation(pet.Kind) {
		return owner.GetNavmeshPosition(), true
	}
	settings := s.currentPolicy().Settings
	s.mu.Lock()
	ai := s.companionAI[pet.GetUniqueID()]
	if ai == nil {
		ai = newCOSCompanionAI(pet, settings)
		s.companionAI[pet.GetUniqueID()] = ai
	}
	ownerMoving := owner.GetMotionState() != model.NoMotion
	if ownerMoving {
		ai.ownerMoving = true
		ai.nextWaypoint = now.Add(settings.IdleWaypointMinInterval)
	} else if ai.ownerMoving {
		// Resume circling promptly after the owner stops, from the pet's stable
		// follow side rather than jumping to an unrelated point.
		ai.ownerMoving = false
		ai.orbitAngle = ai.followAngle
		ai.nextWaypoint = time.Time{}
	}
	if !ownerMoving && (ai.nextWaypoint.IsZero() || !now.Before(ai.nextWaypoint)) {
		ai.seed = ai.seed*1664525 + 1013904223
		stepDegrees := settings.OrbitMinAngleStep + float32(ai.seed%settings.OrbitAngleStepRange)
		ai.orbitAngle += ai.orbitDirection * stepDegrees * (2 * math.Pi / 360)
		ai.seed = ai.seed*1664525 + 1013904223
		orbitMin, orbitVariance := companionOrbitRadius(pet.Kind, settings)
		ai.orbitRadius = orbitMin + float32(ai.seed%orbitVariance)/100
		waypointDelay := settings.IdleWaypointMinInterval
		if settings.IdleWaypointVariance > 0 {
			waypointDelay += time.Duration(uint64(ai.seed) % uint64(settings.IdleWaypointVariance))
		}
		ai.nextWaypoint = now.Add(waypointDelay)
	}
	targetAngle, targetRadius := ai.orbitAngle, ai.orbitRadius
	if ownerMoving {
		targetAngle, targetRadius = ai.followAngle, ai.followRadius
	}
	followAngle, followRadius := ai.followAngle, ai.followRadius
	s.mu.Unlock()

	ownerGlobal := owner.GetNavmeshPosition().GetGlobalCoordinates()
	targetGlobal := math32.NewVector3(
		ownerGlobal.X+targetRadius*float32(math.Cos(float64(targetAngle))),
		ownerGlobal.Y,
		ownerGlobal.Z+targetRadius*float32(math.Sin(float64(targetAngle))),
	)
	target, err := world.NewPosFromGlobalCoordinates(targetGlobal)
	if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
		// An orbit waypoint may cross a wall while the closer stable follow slot
		// is still valid. Retry that slot before retaining the current position.
		targetGlobal = math32.NewVector3(
			ownerGlobal.X+followRadius*float32(math.Cos(float64(followAngle))),
			ownerGlobal.Y,
			ownerGlobal.Z+followRadius*float32(math.Sin(float64(followAngle))),
		)
		target, err = world.NewPosFromGlobalCoordinates(targetGlobal)
		if err != nil || (requireDirect && !world.CanMoveDirectly(pet.GetNavmeshPosition(), target)) {
			return navmeshv2.RtNavmeshPosition{}, false
		}
	}
	return target, true
}
