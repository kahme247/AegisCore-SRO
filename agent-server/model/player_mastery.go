package model

func (p *Player) MasterySnapshot() map[uint32]byte {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	result := make(map[uint32]byte, len(p.Masteries))
	for id, level := range p.Masteries {
		result[id] = level
	}
	return result
}

func (p *Player) MasteryLevel(id uint32) (byte, bool) {
	if p == nil {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	level, exists := p.Masteries[id]
	return level, exists
}

func (p *Player) CommitMastery(result MasteryIncreaseResult) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	if p.Masteries == nil {
		p.Masteries = make(map[uint32]byte)
	}
	p.Masteries[result.Mastery.ID] = result.Mastery.Level
	p.SkillPoints = int(result.SkillPoints)
	p.Mutex.Unlock()
}
