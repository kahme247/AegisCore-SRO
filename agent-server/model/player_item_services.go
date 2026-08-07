package model

import "time"

type ItemServiceState struct {
	Effect ItemServiceEffect
	Token  uint32
}

func (p *Player) ApplyItemService(effect ItemServiceEffect) (token uint32, replacedToken uint32) {
	if p == nil {
		return 0, 0
	}
	p.Mutex.Lock()
	token, replacedToken = p.applyItemServiceLocked(effect)
	p.Mutex.Unlock()
	return token, replacedToken
}

func (p *Player) applyItemServiceLocked(effect ItemServiceEffect) (token uint32, replacedToken uint32) {
	if p.itemServices == nil {
		p.itemServices = make(map[ItemServiceKind]ItemServiceEffect)
	}
	if p.itemServiceTokens == nil {
		p.itemServiceTokens = make(map[ItemServiceKind]uint32)
	}
	replacedToken = p.itemServiceTokens[effect.Kind]
	token = NextActionUID()
	p.itemServices[effect.Kind] = effect
	p.itemServiceTokens[effect.Kind] = token
	return token, replacedToken
}

func (p *Player) ItemServiceRates(now time.Time) (experienceBonus, skillExperienceBonus uint32) {
	if p == nil {
		return 0, 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for kind, effect := range p.itemServices {
		if !effect.ExpiresAt.After(now) {
			delete(p.itemServices, kind)
			delete(p.itemServiceTokens, kind)
			continue
		}
		experienceBonus += effect.ExperienceBonusPercent
		skillExperienceBonus += effect.SkillExperienceBonusPercent
	}
	return experienceBonus, skillExperienceBonus
}

func (p *Player) ItemServices(now time.Time) []ItemServiceEffect {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	effects := make([]ItemServiceEffect, 0, len(p.itemServices))
	for kind, effect := range p.itemServices {
		if !effect.ExpiresAt.After(now) {
			delete(p.itemServices, kind)
			delete(p.itemServiceTokens, kind)
			continue
		}
		effects = append(effects, effect)
	}
	return effects
}

func (p *Player) ItemServiceStates(now time.Time) []ItemServiceState {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	states := make([]ItemServiceState, 0, len(p.itemServices))
	for kind, effect := range p.itemServices {
		if !effect.ExpiresAt.After(now) {
			delete(p.itemServices, kind)
			delete(p.itemServiceTokens, kind)
			continue
		}
		states = append(states, ItemServiceState{Effect: effect, Token: p.itemServiceTokens[kind]})
	}
	return states
}

func (p *Player) ClearItemService(kind ItemServiceKind, token uint32) bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.itemServiceTokens[kind] != token {
		return false
	}
	delete(p.itemServices, kind)
	delete(p.itemServiceTokens, kind)
	return true
}
