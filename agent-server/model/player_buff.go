package model

import "time"

type ActiveBuff struct {
	SkillID   uint32
	Token     uint32
	ExpiresAt time.Time
}

func (p *Player) CommitActiveBuff(skillID, token uint32, duration time.Duration) (replacedToken uint32, ok bool) {
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
			delete(p.ActiveBuffs, existingToken)
			replacedToken = existingToken
			break
		}
	}
	p.ActiveBuffs[token] = ActiveBuff{SkillID: skillID, Token: token, ExpiresAt: time.Now().Add(duration)}
	return replacedToken, true
}

func (p *Player) RemoveActiveBuff(token uint32) bool {
	if p == nil || token == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if _, exists := p.ActiveBuffs[token]; !exists {
		return false
	}
	delete(p.ActiveBuffs, token)
	return true
}
