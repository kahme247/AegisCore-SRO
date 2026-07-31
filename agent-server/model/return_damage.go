package model

const returnDamageChanceCategory uint32 = 0x46000000

// BloodTrapChanceCategory is the native sub_5A1A20 actor-map category used by
// the `bt` abnormal-record producer at 0x0059345C.
const BloodTrapChanceCategory uint32 = 0x19000000

// PendingReturnDamage is a deferred native `rgmd` application owned by the
// attacker that received the original hit. The defender remains the source of
// the later application packet.
type PendingReturnDamage struct {
	SkillID   uint32
	SourceUID uint32
	Amount    int
}

// AdmitReturnDamage reproduces SR_GameServer 0x00599CC0's actor-owned
// probability accumulator for the `rgmd` category. chancePercent is native
// word 0 in EAX; roll must be in the native rand()%101 domain (0..100).
// The accumulator belongs to the actor rather than an individual buff and is
// intentionally retained when an rgmd actor is removed, as in the native
// actor-side map.
func (p *Player) AdmitReturnDamage(chancePercent, roll int) bool {
	return p.AdmitKeyedChance(returnDamageChanceCategory, chancePercent, roll)
}

// AdmitKeyedChance reproduces sub_599CC0's per-actor probability accumulator.
// category is native actor-map bookkeeping, not a wire or reference field; roll
// must use the native rand()%101 domain (0..100).
func (p *Player) AdmitKeyedChance(category uint32, chancePercent, roll int) bool {
	if p == nil || category == 0 || chancePercent <= 0 || roll < 0 || roll > 100 {
		return false
	}
	if chancePercent >= 100 {
		return true
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.returnDamageChanceState == nil {
		p.returnDamageChanceState = make(map[uint32]int)
	}
	state, exists := p.returnDamageChanceState[category]
	if !exists {
		if roll <= chancePercent {
			p.returnDamageChanceState[category] = 2*chancePercent - 100
			return true
		}
		p.returnDamageChanceState[category] = 2 * chancePercent
		return false
	}
	if roll <= state {
		p.returnDamageChanceState[category] = state + chancePercent - 100
		return true
	}
	p.returnDamageChanceState[category] = state + chancePercent
	return false
}

// QueueReturnDamage appends a return record and reports whether this caller
// must schedule the per-attacker drain. One scheduled drain owns all records
// added until it observes an empty queue.
func (p *Player) QueueReturnDamage(record PendingReturnDamage) (accepted, schedule bool) {
	if p == nil || record.SkillID == 0 || record.SourceUID == 0 || record.Amount <= 0 {
		return false, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.pendingReturnDamage = append(p.pendingReturnDamage, record)
	if p.returnDamageScheduled {
		return true, false
	}
	p.returnDamageScheduled = true
	return true, true
}

// DrainReturnDamage transfers the current queue to its scheduled consumer.
func (p *Player) DrainReturnDamage() []PendingReturnDamage {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	records := p.pendingReturnDamage
	p.pendingReturnDamage = nil
	p.Mutex.Unlock()
	return records
}

// FinishReturnDamageDrain either hands the current scheduled consumer more
// work or releases scheduling ownership after it observes an empty queue.
func (p *Player) FinishReturnDamageDrain() bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if len(p.pendingReturnDamage) > 0 {
		return true
	}
	p.returnDamageScheduled = false
	return false
}
