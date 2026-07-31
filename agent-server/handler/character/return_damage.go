package character

import (
	"context"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/sirupsen/logrus"
)

// applyPlayerDamageWithReturn is the common authoritative player incoming-hit
// boundary. The direct queued-return executor intentionally bypasses it so a
// reflected hit cannot admit another reflected hit.
func (h *CombatHandler) applyPlayerDamageWithReturn(attacker, defender *model.Player, damage int, magical bool, physicalHit, magicalHit int, now time.Time) model.PlayerDamageApplication {
	application := defender.ApplyCombatDamage(damage, magical)
	// The components come from the producer before linked interception. Do not
	// gate admission on the final direct application: a fully redirected hit
	// remains eligible in the native pre-linked RGMD branch.
	h.admitReturnDamage(attacker, defender, physicalHit, magicalHit, now)
	return application
}

// admitReturnDamage evaluates active `rgmd` actors owned by defender. Native
// admission is keyed by its positive chance word and occurs only after the
// original hit has produced damage; the resulting work is queued on attacker.
func (h *CombatHandler) admitReturnDamage(attacker, defender *model.Player, physicalHit, magicalHit int, now time.Time) {
	if h == nil || attacker == nil || defender == nil || attacker == defender || h.referenceData == nil || physicalHit+magicalHit <= 0 {
		return
	}
	for _, buff := range defender.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, ok := h.referenceData.Skill(buff.SkillID)
		if !ok {
			continue
		}
		descriptor := skill.ReturnDamage
		if descriptor.ChancePercent <= 0 || descriptor.MaximumRange <= 0 {
			continue
		}
		// Native uses a strict greater-than comparison: a hit exactly at the
		// descriptor range is rejected, and must not consume keyed chance state.
		if navmeshv2.HorizontalDistance(attacker.GetNavmeshPosition(), defender.GetNavmeshPosition()) >= float32(descriptor.MaximumRange) {
			continue
		}
		if h.returnDamageRoll == nil || !h.returnDamageRoll(defender, descriptor.ChancePercent) {
			continue
		}
		amount := physicalHit*descriptor.PhysicalPercent/100 + magicalHit*descriptor.MagicalPercent/100
		if amount <= 0 {
			continue
		}
		accepted, schedule := attacker.QueueReturnDamage(model.PendingReturnDamage{SkillID: skill.ID, SourceUID: defender.GetUniqueID(), Amount: amount})
		if accepted && schedule {
			h.scheduleReturnDamage(attacker)
		}
	}
}

func (h *CombatHandler) scheduleReturnDamage(attacker *model.Player) {
	if h == nil || attacker == nil || h.schedule == nil {
		return
	}
	h.schedule(0, func() {
		h.executeQueuedReturnDamage(context.Background(), attacker)
	})
}

// executeQueuedReturnDamage is the deferred effect-update side of `rgmd`.
// It deliberately does not re-admit return damage, preventing return loops.
func (h *CombatHandler) executeQueuedReturnDamage(ctx context.Context, attacker *model.Player) {
	if h == nil || attacker == nil {
		return
	}
	for {
		for _, record := range attacker.DrainReturnDamage() {
			if attacker.GetLifeState() != model.Alive || h.world == nil {
				continue
			}
			source, err := h.world.GetPlayerByUniqueId(record.SourceUID)
			// The native record is queued before subsequent life-state changes;
			// source resolution is required for attribution, but source aliveness is
			// not an execution precondition.
			if err != nil || source == nil {
				continue
			}
			application := attacker.ApplyCombatDamage(record.Amount, false)
			if application.AppliedDamage == 0 && application.BarrierDamage == 0 {
				continue
			}
			if application.AppliedDamage > 0 && h.vitals != nil && attacker.ID > 0 {
				hp, mp := attacker.CurrentVitals()
				if err := h.vitals.SaveVitals(ctx, attacker.ID, hp, mp); err != nil {
					continue
				}
			}
			flags := byte(0)
			if application.Killed {
				flags = 0x80
			}
			packet := model.BuildSkillApplicationRecords(record.SkillID, flags, attacker.GetUniqueID(), nil)
			h.publishCombatPacket(source, &packet)
			attacker.SendVitalsUpdate()
			h.publishPartyStatus(attacker)
			if !application.Killed {
				continue
			}
			life := model.BuildLifeStateUpdatePacket(attacker.GetUniqueID(), model.Dead)
			h.publishCombatPacket(source, &life)
			_ = h.recordPlayerKill(ctx, source, attacker)
		}
		if !attacker.FinishReturnDamageDrain() {
			return
		}
	}
}

// ExecuteNPCReturnDamage applies an NPC-attacker RGMD record through the
// established player-to-NPC lifecycle, then publishes the native subtype-1
// notification. MobAI resolves the defender/source before invoking it.
func (h *CombatHandler) ExecuteNPCReturnDamage(source *model.Player, target *model.NPC, record model.PendingReturnDamage) {
	if h == nil || source == nil || target == nil || record.SkillID == 0 || record.Amount <= 0 {
		return
	}
	result := h.applyPlayerDamageResult(source, target, record.Amount, false, true, false)
	if result.appliedDamage == 0 {
		return
	}
	flags := byte(0)
	if result.killed {
		flags = 0x80
	}
	packet := model.BuildSkillApplicationRecords(record.SkillID, flags, target.GetUniqueID(), nil)
	target.SendToKnown(&packet)
	if result.killed {
		if err := h.awardPlayerKill(context.Background(), source.GetSession(), source, target, target.GetUniqueID()); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"source_uid": source.GetUniqueID(), "target_uid": target.GetUniqueID()}).Warn("award NPC return-damage kill")
		}
	}
}

// Kept separate from admission to make the native one-tick queue boundary
// testable without a wall-clock scheduler.
func (h *CombatHandler) processQueuedReturnDamage(attacker *model.Player) {
	h.executeQueuedReturnDamage(context.Background(), attacker)
}
