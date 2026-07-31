package manager

import (
	"math/rand"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

// admitNPCToPlayerReturnDamage mirrors the RGMD branch before linked-damage
// interception. The player owns admission state; the attacking NPC owns the
// deferred return records.
func (m *MobAIManager) admitNPCToPlayerReturnDamage(attacker *model.NPC, defender *model.Player, physicalHit, magicalHit int, now time.Time) {
	if m == nil || attacker == nil || defender == nil || physicalHit+magicalHit <= 0 {
		return
	}
	m.stateMu.Lock()
	catalog := m.referenceData
	m.stateMu.Unlock()
	if catalog == nil {
		return
	}
	for _, buff := range defender.ActiveBuffSnapshot() {
		if !buff.ExpiresAt.IsZero() && !buff.ExpiresAt.After(now) {
			continue
		}
		skill, ok := catalog.Skill(buff.SkillID)
		if !ok {
			continue
		}
		descriptor := skill.ReturnDamage
		if descriptor.ChancePercent <= 0 || descriptor.MaximumRange <= 0 || navmeshv2.HorizontalDistance(attacker.GetNavmeshPosition(), defender.GetNavmeshPosition()) >= float32(descriptor.MaximumRange) {
			continue
		}
		if !defender.AdmitReturnDamage(descriptor.ChancePercent, rand.Intn(101)) {
			continue
		}
		amount := physicalHit*descriptor.PhysicalPercent/100 + magicalHit*descriptor.MagicalPercent/100
		if amount <= 0 {
			continue
		}
		if accepted, schedule := attacker.QueueReturnDamage(model.PendingReturnDamage{SkillID: skill.ID, SourceUID: defender.GetUniqueID(), Amount: amount}); accepted && schedule {
			m.scheduleAfter(0, func() { m.executeNPCQueuedReturnDamage(attacker) })
		}
	}
}

func (m *MobAIManager) executeNPCQueuedReturnDamage(attacker *model.NPC) {
	if m == nil || attacker == nil {
		return
	}
	for {
		for _, record := range attacker.DrainReturnDamage() {
			source := m.knownPlayer(attacker, record.SourceUID)
			if source == nil {
				continue
			}
			m.stateMu.Lock()
			executor := m.returnDamage
			m.stateMu.Unlock()
			if executor != nil {
				executor(source, attacker, record)
			}
		}
		if !attacker.FinishReturnDamageDrain() {
			return
		}
	}
}
