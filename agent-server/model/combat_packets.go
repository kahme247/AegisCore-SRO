package model

import (
	"sync/atomic"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

var nextActionUID atomic.Uint32

// NextActionUID allocates one process-wide action identifier. Player and NPC
// actions share the same client namespace and must never reuse independent
// per-system counters.
func NextActionUID() uint32 {
	for {
		current := nextActionUID.Load()
		next := current + 1
		if next == 0 {
			next = 1
		}
		if nextActionUID.CompareAndSwap(current, next) {
			return next
		}
	}
}

func BuildAttackBegin(sourceUID, targetUID, castUID, damage uint32, killed bool) network.Packet {
	return BuildAttackBeginWithSkill(1, sourceUID, targetUID, castUID, damage, killed)
}

func BuildAttackBeginWithSkill(skillID, sourceUID, targetUID, castUID, damage uint32, killed bool) network.Packet {
	return BuildAttackBeginWithSkillHits(skillID, sourceUID, targetUID, castUID, []uint32{damage}, killed)
}

func BuildAttackBeginWithSkillHits(skillID, sourceUID, targetUID, castUID uint32, damage []uint32, killed bool) network.Packet {
	p := buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID)
	WriteDamageBlockHits(&p, targetUID, damage, killed)
	return p
}

// BuildDelayedAttackBegin starts a reference skill whose damage is resolved
// later by EntitySkillCastEnd. A stock vSRO client uses the false action flag
// to distinguish this animation-only B070 from an immediate hit.
func BuildDelayedAttackBegin(skillID, sourceUID, targetUID, castUID uint32) network.Packet {
	p := buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID)
	p.WriteBool(false)
	return p
}

func BuildBuffCastBegin(skillID, sourceUID, targetUID, castUID uint32) network.Packet {
	p := buildAttackBeginHeaderWithType(0, skillID, sourceUID, targetUID, castUID)
	p.WriteBool(false)
	return p
}

func BuildBuffCastEnd(targetUID, castUID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastEnd
	p.WriteByte(1)
	p.WriteUInt32(castUID)
	p.WriteUInt32(targetUID)
	p.WriteBool(false)
	return p
}

func BuildBuffAdd(targetUID, skillID, token uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityBuffAdd
	p.WriteUInt32(targetUID)
	p.WriteUInt32(skillID)
	p.WriteUInt32(token)
	return p
}

func BuildTimedBuffAdd(targetUID, skillID, token uint32, remainingMilliseconds uint32) network.Packet {
	p := BuildBuffAdd(targetUID, skillID, token)
	p.WriteUInt32(remainingMilliseconds)
	return p
}

func BuildBuffRemove(tokens ...uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityBuffRemove
	p.WriteByte(byte(len(tokens)))
	for _, token := range tokens {
		p.WriteUInt32(token)
	}
	return p
}

func buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID uint32) network.Packet {
	return buildAttackBeginHeaderWithType(2, skillID, sourceUID, targetUID, castUID)
}

func buildAttackBeginHeaderWithType(actionType byte, skillID, sourceUID, targetUID, castUID uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastBegin
	p.WriteByte(1)
	p.WriteByte(actionType)
	p.WriteByte(0x30)
	p.WriteUInt32(skillID)
	p.WriteUInt32(sourceUID)
	p.WriteUInt32(castUID)
	p.WriteUInt32(targetUID)
	return p
}

func BuildAttackEnd(targetUID, castUID, damage uint32, killed bool) network.Packet {
	return BuildAttackEndHits(targetUID, castUID, []uint32{damage}, killed)
}

func BuildAttackEndHits(targetUID, castUID uint32, damage []uint32, killed bool) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastEnd
	p.WriteByte(1)
	p.WriteUInt32(castUID)
	p.WriteUInt32(targetUID)
	WriteDamageBlockHits(&p, targetUID, damage, killed)
	return p
}

func WriteDamageBlock(p *network.Packet, targetUID, damage uint32, killed bool) {
	WriteDamageBlockHits(p, targetUID, []uint32{damage}, killed)
}

func WriteDamageBlockHits(p *network.Packet, targetUID uint32, damage []uint32, killed bool) {
	p.WriteBool(true)
	p.WriteByte(byte(len(damage))) // Hit count precedes target count.
	p.WriteByte(1)
	p.WriteUInt32(targetUID)
	for index, hitDamage := range damage {
		attackState := byte(0)
		if killed && index == len(damage)-1 {
			attackState = 0x80
		}
		p.WriteByte(attackState)
		p.WriteByte(1) // Normal damage state.
		p.WriteByte(byte(hitDamage))
		p.WriteByte(byte(hitDamage >> 8))
		p.WriteByte(byte(hitDamage >> 16))
		p.WriteUInt32(0) // No effect/status state.
	}
}
