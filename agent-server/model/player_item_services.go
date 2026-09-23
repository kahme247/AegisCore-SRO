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
		if premiumDailyServiceRefItemID(effect) != 0 && !effect.DailyActiveUntil.After(now) {
			continue
		}
		if p.premiumPaused[byte(kind)+1] {
			continue
		}
		experienceBonus += effect.ExperienceBonusPercent
		skillExperienceBonus += effect.SkillExperienceBonusPercent
	}
	return experienceBonus, skillExperienceBonus
}

func (p *Player) ItemServiceEffectValue(kind ItemServiceKind, now time.Time) uint32 {
	if p == nil {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	effect, exists := p.itemServices[kind]
	if !exists || !effect.ExpiresAt.After(now) {
		delete(p.itemServices, kind)
		delete(p.itemServiceTokens, kind)
		return 0
	}
	return effect.EffectValue
}

func ItemServiceAffectsStats(effect ItemServiceEffect) bool {
	return effect.Kind == ItemServiceReinforceWeapon || effect.Kind == ItemServiceReinforceArmor || effect.Kind == ItemServiceReinforceAccessory
}

func (p *Player) PremiumDailyState(now time.Time, serviceRefItemID, token uint32) (ItemServiceEffect, bool) {
	if p == nil || serviceRefItemID == 0 || token == 0 {
		return ItemServiceEffect{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	for kind, effect := range p.itemServices {
		if !effect.ExpiresAt.After(now) {
			delete(p.itemServices, kind)
			delete(p.itemServiceTokens, kind)
			continue
		}
		if p.itemServiceTokens[kind] == token && premiumDailyServiceRefItemID(effect) == serviceRefItemID {
			return effect, true
		}
	}
	return ItemServiceEffect{}, false
}

func (p *Player) ActivateItemServiceDaily(kind ItemServiceKind, token uint32, usedOn, activeUntil time.Time) bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.itemServiceTokens[kind] != token {
		return false
	}
	effect, exists := p.itemServices[kind]
	if !exists {
		return false
	}
	effect.DailyUsedOn = usedOn.UTC()
	effect.DailyActiveUntil = activeUntil.UTC()
	p.itemServices[kind] = effect
	return true
}

func sameUTCDate(left, right time.Time) bool {
	left = left.UTC()
	right = right.UTC()
	leftYear, leftDay := left.Year(), left.YearDay()
	return leftYear == right.Year() && leftDay == right.YearDay()
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

func (p *Player) ClearItemServicesForTargetSlots(slots map[byte]struct{}, now time.Time) []ItemServiceState {
	if p == nil || len(slots) == 0 {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	cleared := make([]ItemServiceState, 0, len(slots))
	for kind, effect := range p.itemServices {
		if !effect.ExpiresAt.After(now) {
			delete(p.itemServices, kind)
			delete(p.itemServiceTokens, kind)
			continue
		}
		if _, matches := slots[effect.TargetSlot]; !effect.TargetSlotSet || !matches {
			continue
		}
		cleared = append(cleared, ItemServiceState{Effect: effect, Token: p.itemServiceTokens[kind]})
		delete(p.itemServices, kind)
		delete(p.itemServiceTokens, kind)
	}
	return cleared
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
