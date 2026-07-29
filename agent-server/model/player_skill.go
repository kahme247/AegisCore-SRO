package model

import "sort"

func (p *Player) SkillSnapshot() []CharacterSkill {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	result := make([]CharacterSkill, 0, len(p.Skills))
	for id, enabled := range p.Skills {
		result = append(result, CharacterSkill{ID: id, Enabled: enabled})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (p *Player) SetSkills(skills []CharacterSkill) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.Skills = make(map[uint32]byte, len(skills))
	for _, skill := range skills {
		p.Skills[skill.ID] = skill.Enabled
	}
}

func (p *Player) CommitSkill(result SkillLearnResult) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.Skills == nil {
		p.Skills = make(map[uint32]byte)
	}
	if result.ReplacedSkillID != 0 {
		delete(p.Skills, result.ReplacedSkillID)
	}
	p.Skills[result.Skill.ID] = result.Skill.Enabled
	p.SkillPoints = int(result.SkillPoints)
}
