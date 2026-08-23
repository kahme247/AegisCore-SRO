package model

import "time"

func (p *Player) ApplyCombatStatus(status CombatStatus) bool {
	if p == nil || status.Kind == 0 || status.ExpiresAt.IsZero() {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState == Dead {
		return false
	}
	if p.combatStatuses == nil {
		p.combatStatuses = make(map[CombatStatusKind]CombatStatus)
	}
	p.combatStatuses[status.Kind] = status
	return true
}

func (p *Player) CombatStatus(kind CombatStatusKind, now time.Time) (CombatStatus, bool) {
	if p == nil || kind == 0 {
		return CombatStatus{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	status, exists := p.combatStatuses[kind]
	if !exists {
		return CombatStatus{}, false
	}
	if !status.ExpiresAt.After(now) {
		delete(p.combatStatuses, kind)
		return CombatStatus{}, false
	}
	return status, true
}

func (p *Player) CombatBadEffectState(now time.Time) (uint32, []byte) {
	var mask uint32
	levels := make([]byte, 0)
	for _, kind := range combatBadEffectKinds {
		status, active := p.CombatStatus(kind, now)
		if !active {
			continue
		}
		bit := CombatStatusBadEffectBit(kind)
		mask |= bit
		if bit > 1<<5 {
			level := status.Level
			if level < 1 {
				level = max(1, status.Power)
			}
			levels = append(levels, byte(min(level, 255)))
		}
	}
	return mask, levels
}

func (p *Player) CureCombatStatuses(mask uint32, maxLevel, count int, now time.Time) int {
	return p.CureCombatStatusesWithCurse(mask, maxLevel, false, 0, 100, count, now, nil)
}

func (p *Player) CureCombatStatusesWithCurse(statusMask uint32, statusMaxLevel int, cureCurse bool, curseMaxLevel, curseChance, count int, now time.Time, roll func(int) bool) int {
	removed := 0
	for _, kind := range combatBadEffectKinds {
		bit := CombatStatusBadEffectBit(kind)
		if bit == 0 || count > 0 && removed >= count {
			continue
		}
		status, active := p.CombatStatus(kind, now)
		if !active || !combatCureEligible(bit, status.Level, statusMask, statusMaxLevel, cureCurse, curseMaxLevel) {
			continue
		}
		if bit > 1<<5 && roll != nil && !roll(curseChance) {
			continue
		}
		p.Mutex.Lock()
		current, exists := p.combatStatuses[kind]
		if exists && current.Token == status.Token {
			delete(p.combatStatuses, kind)
			removed++
		}
		p.Mutex.Unlock()
	}
	return removed
}
