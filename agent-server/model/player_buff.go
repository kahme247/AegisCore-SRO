package model

import "time"

type ActiveBuff struct {
	SkillID   uint32
	Token     uint32
	ExpiresAt time.Time
	Modifiers BuffModifiers
}

func (p *Player) ActiveBuffSnapshot() []ActiveBuff {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buffs := make([]ActiveBuff, 0, len(p.ActiveBuffs))
	for _, buff := range p.ActiveBuffs {
		buffs = append(buffs, buff)
	}
	return buffs
}

func (p *Player) CommitActiveBuff(skillID, token uint32, duration time.Duration, modifiers BuffModifiers) (replacedToken uint32, ok bool) {
	if p == nil || skillID == 0 || token == 0 || duration <= 0 {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.ActiveBuffs == nil {
		p.ActiveBuffs = make(map[uint32]ActiveBuff)
	}
	for existingToken, buff := range p.ActiveBuffs {
		if buff.SkillID == skillID {
			p.applyBuffModifiersLocked(buff.Modifiers, -1)
			delete(p.ActiveBuffs, existingToken)
			replacedToken = existingToken
			break
		}
	}
	p.applyBuffModifiersLocked(modifiers, 1)
	p.ActiveBuffs[token] = ActiveBuff{SkillID: skillID, Token: token, ExpiresAt: time.Now().Add(duration), Modifiers: modifiers}
	return replacedToken, true
}

func (p *Player) RemoveActiveBuff(token uint32) bool {
	if p == nil || token == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	buff, exists := p.ActiveBuffs[token]
	if !exists {
		return false
	}
	p.applyBuffModifiersLocked(buff.Modifiers, -1)
	delete(p.ActiveBuffs, token)
	return true
}

func (p *Player) RemoveActiveBuffBySkill(skillID uint32) (uint32, bool) {
	if p == nil || skillID == 0 {
		return 0, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for token, buff := range p.ActiveBuffs {
		if buff.SkillID != skillID {
			continue
		}
		p.applyBuffModifiersLocked(buff.Modifiers, -1)
		delete(p.ActiveBuffs, token)
		return token, true
	}
	return 0, false
}

func (p *Player) applyBuffModifiersLocked(modifiers BuffModifiers, direction int) {
	p.BonusStats.HP += direction * modifiers.HP
	p.BonusStats.HpPercent += direction * modifiers.HPPercent
	p.BonusStats.MP += direction * modifiers.MP
	p.BonusStats.MpPercent += direction * modifiers.MPPercent
	p.BonusStats.Str += direction * modifiers.Strength
	p.BonusStats.Int += direction * modifiers.Intellect
	p.BonusAttackStats.HitRate += direction * modifiers.HitRate
	p.BonusAttackStats.HitRatePercent += direction * modifiers.HitRatePercent
	p.BonusAttackStats.PhyAttackPercent += direction * modifiers.DamagePercent
	p.BonusAttackStats.MagAttackPercent += direction * modifiers.DamagePercent
	p.BonusDefenseStats.PhyDef += direction * modifiers.PhysicalDefense
	p.BonusDefenseStats.MagDef += direction * modifiers.MagicalDefense
	p.BonusDefenseStats.BlockRate += direction * modifiers.BlockRate
	p.BonusDefenseStats.ParryRate += direction * modifiers.ParryRate
	p.BonusDefenseStats.ParryRatePercent += direction * modifiers.ParryRatePercent
}

func (p *Player) RecalculatePassiveSkillModifiers(catalog *ReferenceDataCatalog) {
	if p == nil || catalog == nil {
		return
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.applyBuffModifiersLocked(p.PassiveSkillModifiers, -1)
	p.PassiveSkillModifiers = BuffModifiers{}
	for skillID, enabled := range p.Skills {
		skill, exists := catalog.Skill(skillID)
		if enabled != 1 || !exists || !skill.IsPassive() {
			continue
		}
		p.PassiveSkillModifiers = addBuffModifiers(p.PassiveSkillModifiers, skill.BuffModifiers)
	}
	p.applyBuffModifiersLocked(p.PassiveSkillModifiers, 1)
}

func addBuffModifiers(left, right BuffModifiers) BuffModifiers {
	left.HP += right.HP
	left.HPPercent += right.HPPercent
	left.MP += right.MP
	left.MPPercent += right.MPPercent
	left.Strength += right.Strength
	left.Intellect += right.Intellect
	left.HitRate += right.HitRate
	left.HitRatePercent += right.HitRatePercent
	left.DamagePercent += right.DamagePercent
	left.PhysicalDefense += right.PhysicalDefense
	left.MagicalDefense += right.MagicalDefense
	left.BlockRate += right.BlockRate
	left.ParryRate += right.ParryRate
	left.ParryRatePercent += right.ParryRatePercent
	return left
}
