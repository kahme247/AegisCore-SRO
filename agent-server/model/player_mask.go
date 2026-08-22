package model

import (
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type CharacterMask struct {
	RefCharID uint32
	Scale     byte
	Skills    []uint32
	ExpiresAt time.Time
	Token     uint32
}

func (p *Player) ApplyCharacterMask(mask CharacterMask) uint32 {
	if p == nil || mask.RefCharID == 0 || !mask.ExpiresAt.After(time.Now()) {
		return 0
	}
	p.Mutex.Lock()
	p.characterMask = mask
	p.Mutex.Unlock()
	return mask.Token
}

func (p *Player) CharacterMask(now time.Time) (CharacterMask, bool) {
	if p == nil {
		return CharacterMask{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.characterMask.RefCharID == 0 || !p.characterMask.ExpiresAt.After(now) {
		p.characterMask = CharacterMask{}
		return CharacterMask{}, false
	}
	mask := p.characterMask
	mask.Skills = append([]uint32(nil), mask.Skills...)
	return mask, true
}

func (p *Player) RemoveCharacterMask(token uint32) bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.characterMask.RefCharID == 0 || token != 0 && p.characterMask.Token != token {
		return false
	}
	p.characterMask = CharacterMask{}
	return true
}

func (p *Player) CharacterMaskSkill(skillID uint32, now time.Time) bool {
	mask, active := p.CharacterMask(now)
	if !active {
		return false
	}
	for _, allowed := range mask.Skills {
		if allowed == skillID {
			return true
		}
	}
	return false
}

func (p *Player) CharacterMaskBaseSkill(now time.Time) (uint32, bool) {
	mask, active := p.CharacterMask(now)
	if !active || len(mask.Skills) == 0 {
		return 0, false
	}
	return mask.Skills[0], true
}

func BuildCharacterMaskPacket(uniqueID uint32, mask CharacterMask) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityMask
	packet.WriteUInt32(uniqueID)
	packet.WriteUInt32(mask.RefCharID)
	if mask.RefCharID != 0 {
		packet.WriteByte(mask.Scale)
		count := min(len(mask.Skills), 255)
		packet.WriteByte(byte(count))
		for _, skillID := range mask.Skills[:count] {
			packet.WriteUInt32(skillID)
		}
	}
	return packet
}
