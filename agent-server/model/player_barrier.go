package model

type ActiveBarrier struct {
	SkillID      uint32
	Token        uint32
	HitPoints    int
	MaxHitPoints int
	Absorption   int
	Physical     bool
}

func (p *Player) CommitBarrier(barrier ActiveBarrier) bool {
	if p == nil || barrier.SkillID == 0 || barrier.Token == 0 || barrier.HitPoints <= 0 || barrier.Absorption <= 0 {
		return false
	}
	p.Mutex.Lock()
	if barrier.MaxHitPoints <= 0 {
		barrier.MaxHitPoints = barrier.HitPoints
	}
	p.activeBarrier = barrier
	p.Mutex.Unlock()
	return true
}

func (p *Player) BarrierSnapshot() ActiveBarrier {
	if p == nil {
		return ActiveBarrier{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.activeBarrier
}

// IsBarrierRooted reports whether a wall skill currently owns the character's
// position. Wall skills are stationary channels: turning is allowed, but
// destination and forward movement must be rejected until the wall ends.
func (p *Player) IsBarrierRooted() bool {
	return p.BarrierSnapshot().Token != 0
}

func (p *Player) RemoveActiveBarrier() (ActiveBarrier, bool) {
	if p == nil {
		return ActiveBarrier{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	barrier := p.activeBarrier
	if barrier.Token == 0 {
		return ActiveBarrier{}, false
	}
	p.activeBarrier = ActiveBarrier{}
	return barrier, true
}

func (p *Player) RemoveBarrier(token uint32) bool {
	if p == nil || token == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.activeBarrier.Token != token {
		return false
	}
	p.activeBarrier = ActiveBarrier{}
	return true
}

func (p *Player) absorbPhysicalDamageLocked(damage int) (int, int, ActiveBarrier) {
	barrier := &p.activeBarrier
	return absorbBarrierDamage(barrier, damage, true)
}

func (p *Player) absorbMagicalDamageLocked(damage int) (int, int, ActiveBarrier) {
	return absorbBarrierDamage(&p.activeBarrier, damage, false)
}

func absorbBarrierDamage(barrier *ActiveBarrier, damage int, physical bool) (remaining, absorbed int, snapshot ActiveBarrier) {
	if barrier == nil || damage <= 0 || barrier.Physical != physical || barrier.HitPoints <= 0 || barrier.Absorption <= 0 {
		return damage, 0, ActiveBarrier{}
	}
	percent := min(barrier.Absorption, 100)
	absorbed = damage * percent / 100
	if absorbed > barrier.HitPoints {
		absorbed = barrier.HitPoints
	}
	barrier.HitPoints -= absorbed
	return damage - absorbed, absorbed, *barrier
}
