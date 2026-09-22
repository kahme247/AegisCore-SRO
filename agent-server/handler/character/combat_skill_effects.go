package character

import (
	"context"
	"math/rand"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func (h *CombatHandler) skillStrikes(skill model.RefSkill) []model.RefSkill {
	strikes := []model.RefSkill{skill}
	seen := map[uint32]bool{skill.ID: true}
	for len(strikes) < 16 {
		nextID := strikes[len(strikes)-1].BasicChainCode
		if nextID == 0 || seen[nextID] {
			break
		}
		next, exists := h.referenceSkill(nextID)
		if !exists {
			break
		}
		seen[nextID] = true
		strikes = append(strikes, next)
	}
	return strikes
}

func (h *CombatHandler) skillComboImpactDuration(player *model.Player, skill model.RefSkill) time.Duration {
	var duration time.Duration
	for _, phase := range h.skillStrikes(skill) {
		// Resolve action time per phase so chained skills can carry distinct
		// speed policy and future phase schedulers do not inherit phase one's
		// stale timing.
		duration += phase.RuntimeImpactDurationAt(h.skillActionTimePercent(player, phase))
	}
	return duration
}

func (h *CombatHandler) skillStrikeDamage(player *model.Player, target *model.NPC, skill model.RefSkill) int {
	if skill.AbsoluteDamage > 0 {
		return skill.AbsoluteDamage
	}
	damage := h.playerVarianceDamage(player, target, skill.IsMagicalAttack(), skill.AttackMin, skill.AttackMax)
	if damage < 1 {
		return 1
	}
	return damage
}

func (h *CombatHandler) weaponImbueDamage(player *model.Player, target *model.NPC, imbue model.RefSkill) int {
	if player == nil || !imbue.IsWeaponImbue() {
		return 0
	}
	return h.skillStrikeDamage(player, target, imbue)
}

func (h *CombatHandler) playerVarianceDamage(player *model.Player, target *model.NPC, magical bool, skillMin, skillMax int) int {
	if player == nil {
		return 0
	}
	stats := player.DerivedStats()
	minimum, maximum := stats.PhyAttackMin, stats.PhyAttackMax
	if magical {
		minimum, maximum = stats.MagAttackMin, stats.MagAttackMax
	}
	minimum += skillMin
	maximum += skillMax
	targetLevel, parry := 0, 0
	varianceAvailable := false
	if target != nil && h != nil && h.referenceData != nil {
		if character, exists := h.referenceData.Character(target.GetRefObjectID()); exists {
			targetLevel, parry = character.Level, character.EvasionRate
			varianceAvailable = true
		}
	}
	if !varianceAvailable {
		return (minimum + maximum) / 2
	}
	hitRate := player.CombatHitRate(stats.HitRate, h.combatNow())
	return model.CombatVarianceDamage(minimum, maximum, hitRate, parry, player.Level, targetLevel, h.combatDamageRolls())
}

func (h *CombatHandler) combatDamageRolls() (rolls [4]int) {
	for index := range rolls {
		if h != nil && h.damageRoll != nil {
			rolls[index] = h.damageRoll()
		}
	}
	return rolls
}

func (h *CombatHandler) activeWeaponImbue(player *model.Player) (model.RefSkill, bool) {
	if h == nil || player == nil {
		return model.RefSkill{}, false
	}
	var selected model.ActiveBuff
	for _, buff := range player.ActiveBuffSnapshot() {
		skill, exists := h.referenceSkill(buff.SkillID)
		if !exists || !skill.IsWeaponImbue() || buff.Token <= selected.Token {
			continue
		}
		selected = buff
	}
	if selected.SkillID == 0 {
		return model.RefSkill{}, false
	}
	return h.referenceSkill(selected.SkillID)
}

func (h *CombatHandler) applyImbueStatuses(session *server.Session, player *model.Player, target *model.NPC, imbue model.RefSkill, now time.Time) uint32 {
	return h.applySkillStatuses(session, player, target, imbue, now)
}

func (h *CombatHandler) applySkillStatuses(session *server.Session, player *model.Player, target *model.NPC, skill model.RefSkill, now time.Time) uint32 {
	if h == nil || player == nil || target == nil {
		return 0
	}
	effects := skill.StatusEffects
	if len(effects) == 0 && skill.ShockPower > 0 {
		effects = []model.SkillStatusEffect{{Kind: model.CombatStatusShock, Power: skill.ShockPower, Payload: skill.ShockPower, Chance: skill.ShockChance, DurationMS: skill.ShockDuration}}
	}
	roll := h.rollPercent
	if roll == nil {
		roll = func(chance int) bool { return chance >= 100 || chance > 0 && rand.Intn(100) < chance }
	}
	appliedBadEffects := uint32(0)
	for _, effect := range effects {
		chance := target.CombatStatusChance(effect.Chance, now)
		if effect.Kind == 0 || chance <= 0 || effect.DurationMS <= 0 || !roll(chance) {
			continue
		}
		status := model.CombatStatus{
			Kind: effect.Kind, Token: model.NextActionUID(), SourceUID: player.GetUniqueID(), SkillID: skill.ID,
			Power: effect.Power, Level: effect.Level, Payload: effect.Payload,
			ExpiresAt: now.Add(time.Duration(effect.DurationMS) * time.Millisecond), Stackable: skill.LinkedSameGroup,
		}
		if !target.ApplyCombatStatusAt(status, now) {
			continue
		}
		if h.world != nil && statusRestrainsMovement(effect.Kind) {
			h.world.StopMovingCharacter(target, target.GetNavmeshPosition(), "combat-status-control", true)
		}
		if (effect.Kind == model.CombatStatusFrostbite || effect.Kind == model.CombatStatusDull) && h.retimeMob != nil {
			h.retimeMob(target, now)
		}
		badEffect := model.CombatStatusBadEffectBit(effect.Kind)
		if badEffect != 0 {
			appliedBadEffects |= badEffect
			h.scheduleBadEffectExpiration(player, target, status)
		} else {
			// Skill-authored stat debuffs (for example Vital Spot defense down)
			// remain B0BD buffs. Elemental abnormal states use 0x3057 below.
			packet := model.BuildTimedBuffAdd(target.GetUniqueID(), skill.ID, status.Token, uint32(effect.DurationMS))
			h.publishCombatPacket(player, &packet)
		}
		if effect.Kind == model.CombatStatusBurn || effect.Kind == model.CombatStatusPoison || effect.Kind == model.CombatStatusBleed {
			h.scheduleStatusDamageTick(session, player, target, status, now.Add(2*time.Second))
		}
	}
	if appliedBadEffects != 0 {
		h.publishBadEffectState(player, target, now)
	}
	return appliedBadEffects
}

func statusRestrainsMovement(kind model.CombatStatusKind) bool {
	switch kind {
	case model.CombatStatusFreeze, model.CombatStatusStun, model.CombatStatusSleep,
		model.CombatStatusBind, model.CombatStatusPetrify:
		return true
	default:
		return false
	}
}

func statusRestrainsAction(kind model.CombatStatusKind) bool {
	return statusRestrainsMovement(kind) || kind == model.CombatStatusFear
}

func (h *CombatHandler) publishCombatPacket(player *model.Player, packet *network.Packet) {
	if h == nil || player == nil || packet == nil {
		return
	}
	if h.broadcast != nil {
		h.broadcast(player, packet)
		return
	}
	player.SendToSelfAndKnown(packet)
}

func (h *CombatHandler) publishBadEffectState(player *model.Player, target *model.NPC, now time.Time) {
	if target == nil {
		return
	}
	mask, levels := target.CombatBadEffectState(now)
	packet := model.BuildBadEffectUpdate(target.GetUniqueID(), mask, levels)
	h.publishCombatPacket(player, &packet)
}

func (h *CombatHandler) scheduleBadEffectExpiration(player *model.Player, target *model.NPC, expected model.CombatStatus) {
	if h == nil || h.schedule == nil || player == nil || target == nil {
		return
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	delay := expected.ExpiresAt.Sub(now)
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		current := time.Now()
		if h.now != nil {
			current = h.now()
		}
		_, active := target.CombatStatusByToken(expected.Kind, expected.Token, current)
		if active {
			return
		}
		h.publishBadEffectState(player, target, current)
		if (expected.Kind == model.CombatStatusFrostbite || expected.Kind == model.CombatStatusDull) && h.retimeMob != nil {
			h.retimeMob(target, current)
		}
	})
}

func (h *CombatHandler) applySkillTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, skill model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, skill, now, h.skillStrikeDamage(player, primary, skill))
}

func (h *CombatHandler) scheduleStatusDamageTick(session *server.Session, player *model.Player, target *model.NPC, expected model.CombatStatus, tickAt time.Time) {
	if h == nil || h.schedule == nil || player == nil || target == nil || !tickAt.Before(expected.ExpiresAt) {
		return
	}
	delay := time.Until(tickAt)
	if h.now != nil {
		delay = tickAt.Sub(h.now())
	}
	if delay < 0 {
		delay = 0
	}
	h.schedule(delay, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		current := time.Now()
		if h.now != nil {
			current = h.now()
		}
		status, active := target.CombatStatusByToken(expected.Kind, expected.Token, current)
		if !active || target.GetLifeState() != model.Alive {
			return
		}
		payload := status.Payload
		if payload <= 0 {
			payload = status.Power
		}
		if status.Kind == model.CombatStatusPoison {
			payload = min(payload, max(0, target.CurrentHitPoints()-1))
			if payload == 0 {
				h.scheduleStatusDamageTick(session, player, target, status, current.Add(2*time.Second))
				return
			}
		}
		result := h.applyPlayerDamageResult(player, target, max(1, payload))
		if result.appliedDamage == 0 {
			return
		}
		castUID := model.NextActionUID()
		// Periodic abnormal damage is target-owned. Reusing the original
		// player's UID makes the stock client replay that player's attack
		// animation on every tick, even after knockback moved the target.
		packet := model.BuildAttackBeginWithSkillHits(status.SkillID, target.GetUniqueID(), target.GetUniqueID(), castUID, []uint32{uint32(result.wireDamage)}, result.killed)
		_ = h.emitAttack(player, &packet)
		if result.killed {
			_ = h.awardPlayerKill(context.Background(), session, player, target, target.GetUniqueID())
			return
		}
		h.scheduleStatusDamageTick(session, player, target, status, current.Add(2*time.Second))
	})
}

func (h *CombatHandler) applyImbueTransfers(ctx context.Context, session *server.Session, player *model.Player, primary *model.NPC, imbue model.RefSkill, now time.Time) error {
	return h.applyTransfers(ctx, session, player, primary, imbue, now, h.weaponImbueDamage(player, primary, imbue))
}
