package model

func (p *Player) CommitSkillWithdrawal(currentSkillID, resultingSkillID uint32, skillPoints uint32, itemSlot byte, itemRemaining uint16) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	delete(p.Skills, currentSkillID)
	if resultingSkillID != 0 {
		p.Skills[resultingSkillID] = 1
	}
	p.SkillPoints = int(skillPoints)
	p.commitRestorationItemLocked(itemSlot, itemRemaining)
}

func (p *Player) CommitMasteryWithdrawal(masteryID uint32, level byte, skillPoints uint32, itemSlot byte, itemRemaining uint16) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.Masteries == nil {
		p.Masteries = make(map[uint32]byte)
	}
	p.Masteries[masteryID] = level
	p.SkillPoints = int(skillPoints)
	p.commitRestorationItemLocked(itemSlot, itemRemaining)
}

func (p *Player) commitRestorationItemLocked(slot byte, remaining uint16) {
	if p.Inventory.Items == nil {
		return
	}
	item, exists := p.Inventory.Items[slot]
	if !exists {
		return
	}
	if remaining == 0 {
		delete(p.Inventory.Items, slot)
		return
	}
	item.StackSize = int(remaining)
	p.Inventory.Items[slot] = item
}
