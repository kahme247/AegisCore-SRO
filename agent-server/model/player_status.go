package model

import "time"

// CombatMovementRestricted reports whether an active combat status prevents
// locomotion. The native execution path stops movement for these control
// effects; callers should still allow an explicit stop/turn request so the
// client can recover cleanly when the effect expires.
func (p *Player) CombatMovementRestricted(now time.Time) bool {
	if p == nil {
		return false
	}
	for _, kind := range []CombatStatusKind{
		CombatStatusFreeze,
		CombatStatusStun,
		CombatStatusSleep,
		CombatStatusBind,
		CombatStatusPetrify,
	} {
		if _, active := p.CombatStatus(kind, now); active {
			return true
		}
	}
	return false
}

// CombatActionRestricted reports control states whose stock abnormal actors
// suppress offensive skill/attack admission while leaving cure/item use
// available.
func (p *Player) CombatActionRestricted(now time.Time) bool {
	if p == nil {
		return false
	}
	for _, kind := range []CombatStatusKind{
		CombatStatusFreeze,
		CombatStatusStun,
		CombatStatusSleep,
		CombatStatusBind,
		CombatStatusPetrify,
		CombatStatusFear,
	} {
		if _, active := p.CombatStatus(kind, now); active {
			return true
		}
	}
	return false
}

// CombatHitRate applies authored absolute Hit Down and Darkness reductions.
func (p *Player) CombatHitRate(base int, now time.Time) int {
	for _, kind := range []CombatStatusKind{CombatStatusHitDown, CombatStatusDarkness} {
		if status, active := p.CombatStatus(kind, now); active {
			base -= status.Power
		}
	}
	return max(0, base)
}

// CombatParryRate applies authored Parry Down followed by Shock's stock
// remaining-parry factor.
func (p *Player) CombatParryRate(base int, now time.Time) int {
	if status, active := p.CombatStatus(CombatStatusParryDown, now); active {
		base -= status.Power
	}
	base = max(0, base)
	if status, active := p.CombatStatus(CombatStatusShock, now); active {
		magnitude := status.Payload
		if magnitude <= 0 {
			magnitude = status.Power
		}
		base = base * (100 - min(max(magnitude, 0), 100)) / 100
	}
	return base
}

func CombatDefenseAfterReduction(base, reduction int) int {
	base = max(0, base)
	maximumReduction := int(float64(base) - 0.3*float64(base))
	return base - min(max(reduction, 0), maximumReduction)
}

func (p *Player) CombatPhysicalDefense(base int, now time.Time) int {
	if status, active := p.CombatStatus(CombatStatusPhysicalDefenseDown, now); active {
		base = CombatDefenseAfterReduction(base, status.Power)
	}
	if status, active := p.CombatStatus(CombatStatusBleed, now); active {
		base -= status.Power
	}
	return max(0, base)
}

func (p *Player) CombatMagicalDefense(base int, now time.Time) int {
	if status, active := p.CombatStatus(CombatStatusMagicalDefenseDown, now); active {
		base = CombatDefenseAfterReduction(base, status.Power)
	}
	if status, active := p.CombatStatus(CombatStatusBleed, now); active {
		base -= status.Power
	}
	return max(0, base)
}

func CombatStatusRefreshesStats(kind CombatStatusKind) bool {
	switch kind {
	case CombatStatusShock, CombatStatusDarkness, CombatStatusHitDown,
		CombatStatusParryDown, CombatStatusPhysicalDefenseDown,
		CombatStatusMagicalDefenseDown, CombatStatusBleed:
		return true
	default:
		return false
	}
}

func (p *Player) CombatStatsAffected(now time.Time) bool {
	for _, kind := range []CombatStatusKind{
		CombatStatusShock, CombatStatusDarkness, CombatStatusHitDown,
		CombatStatusParryDown, CombatStatusPhysicalDefenseDown,
		CombatStatusMagicalDefenseDown, CombatStatusBleed,
	} {
		if _, active := p.CombatStatus(kind, now); active {
			return true
		}
	}
	return false
}

// CombatAttackRange applies Short Sight as an authored percentage reduction.
func (p *Player) CombatAttackRange(base float32, now time.Time) float32 {
	if base <= 0 {
		return 0
	}
	if status, active := p.CombatStatus(CombatStatusShortSight, now); active {
		reduction := min(max(status.Power, 0), 100)
		return base * float32(100-reduction) / 100
	}
	return base
}

// CombatStatusChance applies Disease's authored susceptibility magnitude to a
// subsequent abnormal-state admission roll.
func (p *Player) CombatStatusChance(base int, now time.Time) int {
	if status, active := p.CombatStatus(CombatStatusDisease, now); active {
		base += status.Power
	}
	return min(max(base, 0), 100)
}

func (p *Player) HasZombieStatus(now time.Time) bool {
	_, active := p.CombatStatus(CombatStatusZombie, now)
	return active
}

// HasZombieStatusWhileLocked reports Zombie without reacquiring Player.Mutex.
// The caller must hold Player.Mutex across this query and the associated
// vitality transaction.
func (p *Player) HasZombieStatusWhileLocked(now time.Time) bool {
	if p == nil {
		return false
	}
	status, active := p.combatStatuses[CombatStatusZombie]
	if !active {
		return false
	}
	if !status.ExpiresAt.After(now) {
		delete(p.combatStatuses, CombatStatusZombie)
		return false
	}
	return true
}

func (p *Player) ApplyCombatStatus(status CombatStatus) bool {
	return p.ApplyCombatStatusAt(status, time.Now())
}

// ApplyCombatStatusAt admits an abnormal actor using the supplied execution
// timestamp, keeping replacement precedence deterministic under replay clocks.
func (p *Player) ApplyCombatStatusAt(status CombatStatus, now time.Time) bool {
	if p == nil || status.Kind == 0 || status.ExpiresAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	if status.AppliedAt.IsZero() {
		status.AppliedAt = now
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.LifeState == Dead {
		return false
	}
	if p.combatStatuses == nil {
		p.combatStatuses = make(map[CombatStatusKind]CombatStatus)
	}
	// tisc is abnormal type 0x14 with fixed class 2 and no explicit
	// priority. SR_GameServer 0x004A4270 rejects an equal active record.
	if status.Kind == CombatStatusDamageTakenUp {
		if current, active := p.combatStatuses[status.Kind]; active && current.ExpiresAt.After(now) {
			return false
		}
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

type CombatBadEffectDetail struct {
	DurationCentiseconds uint32
	ElapsedCentiseconds  uint16
	Value                uint16
}

func (p *Player) CombatBadEffectState(now time.Time) (uint32, []byte) {
	mask, levels, _ := p.CombatBadEffectSnapshot(now)
	return mask, levels
}

func (p *Player) CombatBadEffectSnapshot(now time.Time) (uint32, []byte, []CombatBadEffectDetail) {
	var mask uint32
	levels := make([]byte, 0)
	details := make([]CombatBadEffectDetail, 0)
	for _, kind := range combatBadEffectKinds {
		status, active := p.CombatStatus(kind, now)
		if !active {
			continue
		}
		bit := CombatStatusBadEffectBit(kind)
		mask |= bit
		value := status.Level
		if value < 1 {
			value = max(1, status.Power)
		}
		if bit > 1<<5 {
			levels = append(levels, byte(min(value, 255)))
		}
		duration := status.ExpiresAt.Sub(status.AppliedAt)
		elapsed := now.Sub(status.AppliedAt)
		if duration < 0 {
			duration = 0
		}
		if elapsed < 0 {
			elapsed = 0
		} else if elapsed > duration {
			elapsed = duration
		}
		durationCentiseconds := uint64(duration / (100 * time.Millisecond))
		if durationCentiseconds > uint64(^uint32(0)) {
			durationCentiseconds = uint64(^uint32(0))
		}
		elapsedCentiseconds := uint64(elapsed / (100 * time.Millisecond))
		if elapsedCentiseconds > uint64(^uint16(0)) {
			elapsedCentiseconds = uint64(^uint16(0))
		}
		details = append(details, CombatBadEffectDetail{
			DurationCentiseconds: uint32(durationCentiseconds),
			ElapsedCentiseconds:  uint16(elapsedCentiseconds),
			Value:                uint16(min(value, int(^uint16(0)))),
		})
	}
	return mask, levels, details
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
