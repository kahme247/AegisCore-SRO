package character

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *CombatHandler) applyTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time, damage int) error {
	targets, err := h.resolveTransfers(ctx, session, player, primary, skill, now, damage)
	if err != nil {
		return err
	}
	for _, target := range targets {
		castUID := model.NextActionUID()
		packet := model.BuildAttackBeginWithSkillDamageRecords(skill.ID, player.GetUniqueID(), target.TargetUID, castUID, target.Records)
		if err := h.emitAttack(player, &packet); err != nil {
			return err
		}
	}
	return nil
}

func (h *CombatHandler) resolveTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time, damage int) ([]model.CombatDamageTarget, error) {
	// REF_SKILL's transfer count is the simultaneous target count, including
	// the primary target. A value of five therefore permits four nearby mobs.
	secondaryCount := skill.TransferCount - 1
	if h == nil || player == nil || primary == nil || secondaryCount <= 0 || skill.TransferRange <= 0 || damage <= 0 {
		return nil, nil
	}
	candidates := h.transferTargets(player, primary, skill, secondaryCount)
	targets := make([]model.CombatDamageTarget, 0, len(candidates))
	falloff := min(max(skill.TransferFalloffPercent, 0), 100)
	// Each native geometry producer initializes its byte accumulator to
	// 100-falloff, accounting for the primary hit, and multiplies it once more
	// immediately before appending the first transferred target.
	factor := 100 - falloff
	for _, candidate := range candidates {
		factor = nextTransferFactor(factor, falloff)
		transferDamage := max(1, int(math.Round(float64(damage)*float64(factor)/100)))
		result := h.resolvePlayerDamageWithAggro(player, candidate, transferDamage, skill.IsMagicalAttack(), skill.AbsoluteDamage > 0, now, skill.AggroIncrease, skill.AggroIncreasePercent, skill.AttackPercent)
		effectMask := uint32(0)
		if result.appliedDamage > 0 {
			effectMask = h.applySkillStatuses(session, player, candidate, skill, now)
			if err := h.restoreDamageVitals(ctx, session, player, skill, result.appliedDamage); err != nil {
				return nil, err
			}
		}
		record := result.combatRecord(effectMask)
		if result.appliedDamage > 0 && !result.killed {
			h.applySkillDisplacement(player, candidate, skill, &record)
		}
		targets = append(targets, model.CombatDamageTarget{TargetUID: candidate.GetUniqueID(), Records: []model.CombatDamageRecord{record}})
		if result.killed {
			if err := h.awardPlayerKill(ctx, session, player, candidate, candidate.GetUniqueID()); err != nil {
				return nil, err
			}
		}
	}
	return targets, nil
}

func mergeCombatDamageTargets(targets, additions []model.CombatDamageTarget) []model.CombatDamageTarget {
	for _, addition := range additions {
		merged := false
		for index := range targets {
			if targets[index].TargetUID == addition.TargetUID {
				targets[index].Records = append(targets[index].Records, addition.Records...)
				merged = true
				break
			}
		}
		if !merged {
			targets = append(targets, addition)
		}
	}
	return targets
}

func nextTransferFactor(current, falloff int) int {
	current = min(max(current, 0), 100)
	falloff = min(max(falloff, 0), 100)
	return int(math.Round(float64(current*(100-falloff)) / 100))
}

func scaleTransferAmount(amount, factor int) int {
	if amount <= 0 || factor <= 0 {
		return 0
	}
	return int(math.Round(float64(amount*min(factor, 100)) / 100))
}

type skillTransferCandidate struct {
	npc    *model.NPC
	metric float32
}

func (h *CombatHandler) transferActorRadius(refObjectID uint32) float32 {
	if h == nil || h.referenceData == nil || refObjectID == 0 {
		return 0
	}
	character, exists := h.referenceData.Character(refObjectID)
	if !exists || character.BCRadius <= 0 {
		return 0
	}
	return float32(character.BCRadius)
}

func (h *CombatHandler) transferTargets(player *model.Player, primary *model.NPC, skill model.RefSkill, maximum int) []*model.NPC {
	if player == nil || primary == nil || maximum <= 0 || player.GetKnownObjectList() == nil {
		return nil
	}
	// Native types 5 and 7 enumerate owner/runtime-group collections. They must
	// not silently fall back to arbitrary visible monsters. A configured owner
	// provider is the only supported source for those candidates.
	if skill.TransferType == 5 || skill.TransferType == 7 {
		if h == nil || h.ownerTargets == nil {
			return nil
		}
		provided := h.ownerTargets(primary, skill, maximum)
		result := make([]*model.NPC, 0, min(maximum, len(provided)))
		seen := make(map[uint32]struct{}, len(provided))
		for _, candidate := range provided {
			if candidate == nil || candidate == primary || candidate.GetLifeState() != model.Alive || !candidate.GetTypeInfo().IsNPCMob() {
				continue
			}
			if _, exists := seen[candidate.GetUniqueID()]; exists {
				continue
			}
			seen[candidate.GetUniqueID()] = struct{}{}
			result = append(result, candidate)
			if len(result) == maximum {
				break
			}
		}
		return result
	}
	pool := make([]*model.NPC, 0)
	for _, object := range player.GetKnownObjectList().GetKnownObjects() {
		npc, ok := object.(*model.NPC)
		if ok && npc != nil && npc != primary && npc.GetLifeState() == model.Alive && npc.GetTypeInfo().IsNPCMob() {
			pool = append(pool, npc)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].GetUniqueID() < pool[j].GetUniqueID() })
	radius := float32(skill.TransferRange)
	if skill.TransferType == 6 {
		result := make([]*model.NPC, 0, maximum)
		current := primary
		for len(result) < maximum {
			selected, selectedIndex, best := (*model.NPC)(nil), -1, float32(math.MaxFloat32)
			currentRadius := h.transferActorRadius(current.GetRefObjectID())
			for index, candidate := range pool {
				distance := navmeshv2.HorizontalDistance(current.GetNavmeshPosition(), candidate.GetNavmeshPosition())
				admissionRadius := radius + currentRadius + h.transferActorRadius(candidate.GetRefObjectID())
				if distance <= admissionRadius && (distance < best || distance == best && (selected == nil || candidate.GetUniqueID() < selected.GetUniqueID())) {
					selected, selectedIndex, best = candidate, index, distance
				}
			}
			if selected == nil {
				break
			}
			result = append(result, selected)
			pool = append(pool[:selectedIndex], pool[selectedIndex+1:]...)
			current = selected
		}
		return result
	}

	center := primary.GetNavmeshPosition()
	centerRadius := h.transferActorRadius(primary.GetRefObjectID())
	if skill.TransferType == 1 {
		center = player.GetNavmeshPosition()
		centerRadius = h.transferActorRadius(player.GetRefObjectID())
	}
	candidates := make([]skillTransferCandidate, 0, len(pool))
	if skill.TransferType == 3 || skill.TransferType == 4 {
		sourceGlobal := player.GetNavmeshPosition().GetGlobalCoordinates()
		primaryGlobal := primary.GetNavmeshPosition().GetGlobalCoordinates()
		dx, dz := primaryGlobal.X-sourceGlobal.X, primaryGlobal.Z-sourceGlobal.Z
		length := float32(math.Hypot(float64(dx), float64(dz)))
		if length <= 0 {
			return nil
		}
		ux, uz := dx/length, dz/length
		forwardLimit := max(length, float32(max(skill.ActionRange, skill.TransferRange)))
		for _, candidate := range pool {
			global := candidate.GetNavmeshPosition().GetGlobalCoordinates()
			cx, cz := global.X-sourceGlobal.X, global.Z-sourceGlobal.Z
			projection := cx*ux + cz*uz
			candidateRadius := h.transferActorRadius(candidate.GetRefObjectID())
			lateral := float32(math.Abs(float64(cx*uz - cz*ux)))
			if projection >= -candidateRadius && projection <= forwardLimit+candidateRadius && lateral <= radius+candidateRadius {
				candidates = append(candidates, skillTransferCandidate{npc: candidate, metric: max(projection, 0)})
			}
		}
	} else {
		for _, candidate := range pool {
			distance := navmeshv2.HorizontalDistance(center, candidate.GetNavmeshPosition())
			if distance <= radius+centerRadius+h.transferActorRadius(candidate.GetRefObjectID()) {
				candidates = append(candidates, skillTransferCandidate{npc: candidate, metric: distance})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].metric < candidates[j].metric || candidates[i].metric == candidates[j].metric && candidates[i].npc.GetUniqueID() < candidates[j].npc.GetUniqueID()
	})
	if len(candidates) > maximum {
		candidates = candidates[:maximum]
	}
	result := make([]*model.NPC, len(candidates))
	for index := range candidates {
		result[index] = candidates[index].npc
	}
	return result
}
