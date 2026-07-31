package model

// QueueReturnDamage appends a return record to the attacking NPC. One scheduled
// consumer owns all records until it observes an empty queue.
func (n *NPC) QueueReturnDamage(record PendingReturnDamage) (accepted, schedule bool) {
	if n == nil || record.SkillID == 0 || record.SourceUID == 0 || record.Amount <= 0 {
		return false, false
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	n.combat.pendingReturnDamage = append(n.combat.pendingReturnDamage, record)
	if n.combat.returnDamageScheduled {
		return true, false
	}
	n.combat.returnDamageScheduled = true
	return true, true
}

func (n *NPC) DrainReturnDamage() []PendingReturnDamage {
	if n == nil {
		return nil
	}
	n.combat.mutex.Lock()
	records := n.combat.pendingReturnDamage
	n.combat.pendingReturnDamage = nil
	n.combat.mutex.Unlock()
	return records
}

func (n *NPC) FinishReturnDamageDrain() bool {
	if n == nil {
		return false
	}
	n.combat.mutex.Lock()
	defer n.combat.mutex.Unlock()
	if len(n.combat.pendingReturnDamage) > 0 {
		return true
	}
	n.combat.returnDamageScheduled = false
	return false
}
