package character

import (
	"math"
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

// selectPlayerPVPTransferTargets preserves the native transfer geometry and
// deterministic target ordering without applying damage or emitting packets.
func (h *CombatHandler) selectPlayerPVPTransferTargets(source, primary *model.Player, skill model.RefSkill) []*model.Player {
	maximum := skill.TransferCount - 1
	if source == nil || primary == nil || maximum <= 0 || skill.TransferRange <= 0 || skill.TransferType == 5 || skill.TransferType == 7 || source.GetKnownObjectList() == nil {
		return nil
	}
	pool := make([]*model.Player, 0)
	for _, object := range source.GetKnownObjectList().GetKnownObjects() {
		candidate, ok := object.(*model.Player)
		if !ok || candidate == nil || candidate == source || candidate == primary || candidate.GetLifeState() != model.Alive || !h.pvpTargetAdmitted(source, candidate, skill) {
			continue
		}
		pool = append(pool, candidate)
	}
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		ordered := make([]*model.Player, 0, maximum)
		current := primary
		for len(ordered) < maximum {
			bestIndex, bestDistance := -1, float32(math.MaxFloat32)
			currentRadius := h.transferActorRadius(current.GetRefObjectID())
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				admissionRadius := radius + currentRadius + h.transferActorRadius(candidate.GetRefObjectID())
				if distance <= admissionRadius && (distance < bestDistance || distance == bestDistance && (bestIndex < 0 || candidate.GetUniqueID() < pool[bestIndex].GetUniqueID())) {
					bestIndex, bestDistance = index, distance
				}
			}
			if bestIndex < 0 {
				break
			}
			candidate := pool[bestIndex]
			ordered = append(ordered, candidate)
			pool = append(pool[:bestIndex], pool[bestIndex+1:]...)
			current = candidate
		}
		pool = ordered
	} else {
		if skill.TransferType == 3 || skill.TransferType == 4 {
			sourceGlobal := source.GetNavmeshPosition().GetGlobalCoordinates()
			primaryGlobal := primary.GetNavmeshPosition().GetGlobalCoordinates()
			dx, dz := primaryGlobal.X-sourceGlobal.X, primaryGlobal.Z-sourceGlobal.Z
			length := float32(math.Hypot(float64(dx), float64(dz)))
			if length <= 0 {
				return nil
			}
			ux, uz := dx/length, dz/length
			forwardLimit := max(length, max(float32(skill.ActionRange), radius))
			filtered := pool[:0]
			for _, candidate := range pool {
				global := candidate.GetNavmeshPosition().GetGlobalCoordinates()
				cx, cz := global.X-sourceGlobal.X, global.Z-sourceGlobal.Z
				projection := cx*ux + cz*uz
				candidateRadius := h.transferActorRadius(candidate.GetRefObjectID())
				lateral := float32(math.Abs(float64(cx*uz - cz*ux)))
				if projection >= -candidateRadius && projection <= forwardLimit+candidateRadius && lateral <= radius+candidateRadius {
					filtered = append(filtered, candidate)
				}
			}
			pool = filtered
			sort.SliceStable(pool, func(i, j int) bool {
				left := navmeshv2.HorizontalDistance(source.GetNavmeshPosition(), pool[i].GetNavmeshPosition())
				right := navmeshv2.HorizontalDistance(source.GetNavmeshPosition(), pool[j].GetNavmeshPosition())
				return left < right || left == right && pool[i].GetUniqueID() < pool[j].GetUniqueID()
			})
		} else {
			center := primary.GetNavmeshPosition()
			centerRadius := h.transferActorRadius(primary.GetRefObjectID())
			if skill.TransferType == 1 {
				center = source.GetNavmeshPosition()
				centerRadius = h.transferActorRadius(source.GetRefObjectID())
			}
			filtered := pool[:0]
			for _, candidate := range pool {
				admissionRadius := radius + centerRadius + h.transferActorRadius(candidate.GetRefObjectID())
				if navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition()) <= admissionRadius {
					filtered = append(filtered, candidate)
				}
			}
			pool = filtered
			sort.Slice(pool, func(i, j int) bool {
				di := navmeshv2.HorizontalDistance(center, pool[i].GetNavmeshPosition())
				dj := navmeshv2.HorizontalDistance(center, pool[j].GetNavmeshPosition())
				return di < dj || di == dj && pool[i].GetUniqueID() < pool[j].GetUniqueID()
			})
		}
		if len(pool) > maximum {
			pool = pool[:maximum]
		}
	}
	return pool
}
