package model

import (
	"sync/atomic"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

var nextActionUID atomic.Uint32

const (
	DamageAttackStateNormal byte = 0x00
	DamageAttackStateBlock  byte = 0x02
	DamageAttackStateDead   byte = 0x80

	DamageStateNormal   byte = 0x01
	DamageStateCritical byte = 0x02
	DamageStateStatus   byte = 0x10
)

// CombatDamageRecord is one stock TestIn per-hit result. A blocked hit carries
// only AttackState on the wire; successful hits also carry DamageState, the
// 24-bit damage value, and the abnormal-effect mask.
type CombatDamageRecord struct {
	AttackState byte
	DamageState byte
	Damage      uint32
	EffectMask  uint32
}

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
	return BuildAttackBeginWithSkillHitsAndEffect(skillID, sourceUID, targetUID, castUID, damage, killed, 0)
}

func BuildAttackBeginWithSkillHitsAndEffect(skillID, sourceUID, targetUID, castUID uint32, damage []uint32, killed bool, effectMask uint32) network.Packet {
	records := normalDamageRecords(damage, killed, effectMask)
	return BuildAttackBeginWithSkillDamageRecords(skillID, sourceUID, targetUID, castUID, records)
}

func BuildAttackBeginWithSkillDamageRecords(skillID, sourceUID, targetUID, castUID uint32, records []CombatDamageRecord) network.Packet {
	p := buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID)
	WriteDamageBlockRecords(&p, targetUID, records)
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

// BuildMovementCastBegin publishes the authoritative endpoint in B070. The
// stock client uses this position while playing Ghost Walk; sending it only in
// B071 leaves the actor stationary during the animation and then snaps it.
func BuildMovementCastBegin(skillID, sourceUID, targetUID, castUID uint32, position navmeshv2.RtNavmeshPosition) network.Packet {
	p := buildAttackBeginHeaderWithType(0, skillID, sourceUID, targetUID, castUID)
	writeActionPosition(&p, position)
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

// BuildTeleportCastEnd completes a movement skill with ActionStateFlag.Teleport
// and the authoritative endpoint. The stock client remains locked in the
// Ghost Walk animation if B071 carries flag 0 or omits this position.
func BuildTeleportCastEnd(targetUID, castUID uint32, position navmeshv2.RtNavmeshPosition) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastEnd
	p.WriteByte(1)
	p.WriteUInt32(castUID)
	p.WriteUInt32(targetUID)
	writeActionPosition(&p, position)
	return p
}

func writeActionPosition(p *network.Packet, position navmeshv2.RtNavmeshPosition) {
	p.WriteByte(0x08)
	p.WriteUInt16(uint16(position.Region.ID))
	p.WriteUInt32(uint32(int32(position.Offset.X)))
	p.WriteUInt32(uint32(int32(position.Offset.Y)))
	p.WriteUInt32(uint32(int32(position.Offset.Z)))
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

// BuildBadEffectUpdate publishes the aggregate abnormal-state mask. Unlike
// B0BD this identifies Frostbite/Freeze/Shock/Burn/Stun themselves instead of
// incorrectly adding the skill that caused them as a buff on the target.
func BuildBadEffectUpdate(targetUID, effectMask uint32, levels []byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityUpdateStatus
	p.WriteUInt32(targetUID)
	p.WriteUInt16(0)
	p.WriteByte(0x04)
	p.WriteUInt32(effectMask)
	for _, level := range levels {
		p.WriteByte(level)
	}
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
	return BuildAttackEndHitsAndEffect(targetUID, castUID, damage, killed, 0)
}

func BuildAttackEndHitsAndEffect(targetUID, castUID uint32, damage []uint32, killed bool, effectMask uint32) network.Packet {
	records := normalDamageRecords(damage, killed, effectMask)
	return BuildAttackEndDamageRecords(targetUID, castUID, records)
}

func BuildAttackEndDamageRecords(targetUID, castUID uint32, records []CombatDamageRecord) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastEnd
	p.WriteByte(1)
	p.WriteUInt32(castUID)
	p.WriteUInt32(targetUID)
	WriteDamageBlockRecords(&p, targetUID, records)
	return p
}

type SkillEffectTarget struct {
	TargetUID  uint32
	EffectMask uint32
}

// BuildStatusAttackEnd completes a status-only cast with zero direct damage
// while retaining the abnormal-state mask in the stock per-target effect
// record. This is how Cold Wave can apply Frostbite without inventing damage.
func BuildStatusAttackEnd(animationTarget, castUID uint32, targets []SkillEffectTarget) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastEnd
	p.WriteByte(1)
	p.WriteUInt32(castUID)
	p.WriteUInt32(animationTarget)
	if len(targets) == 0 {
		p.WriteBool(false)
		return p
	}
	p.WriteBool(true)
	p.WriteByte(1)
	p.WriteByte(byte(len(targets)))
	for _, target := range targets {
		p.WriteUInt32(target.TargetUID)
		writeDamageRecord(&p, CombatDamageRecord{DamageState: DamageStateNormal, EffectMask: target.EffectMask})
	}
	return p
}

func WriteDamageBlock(p *network.Packet, targetUID, damage uint32, killed bool) {
	WriteDamageBlockHits(p, targetUID, []uint32{damage}, killed)
}

func WriteDamageBlockHits(p *network.Packet, targetUID uint32, damage []uint32, killed bool) {
	WriteDamageBlockHitsAndEffect(p, targetUID, damage, killed, 0)
}

func WriteDamageBlockHitsAndEffect(p *network.Packet, targetUID uint32, damage []uint32, killed bool, effectMask uint32) {
	WriteDamageBlockRecords(p, targetUID, normalDamageRecords(damage, killed, effectMask))
}

func normalDamageRecords(damage []uint32, killed bool, effectMask uint32) []CombatDamageRecord {
	records := make([]CombatDamageRecord, len(damage))
	for index, hitDamage := range damage {
		records[index] = CombatDamageRecord{DamageState: DamageStateNormal, Damage: hitDamage}
		if index == len(damage)-1 {
			records[index].EffectMask = effectMask
			if killed {
				records[index].AttackState |= DamageAttackStateDead
			}
		}
	}
	return records
}

func WriteDamageBlockRecords(p *network.Packet, targetUID uint32, records []CombatDamageRecord) {
	p.WriteBool(true)
	p.WriteByte(byte(len(records))) // Hit count precedes target count.
	p.WriteByte(1)
	p.WriteUInt32(targetUID)
	for _, record := range records {
		writeDamageRecord(p, record)
	}
}

func writeDamageRecord(p *network.Packet, record CombatDamageRecord) {
	p.WriteByte(record.AttackState)
	if record.AttackState&DamageAttackStateBlock != 0 {
		return
	}
	damageState := record.DamageState
	if damageState == 0 {
		damageState = DamageStateNormal
	}
	if record.EffectMask != 0 {
		damageState |= DamageStateStatus
	}
	p.WriteByte(damageState)
	p.WriteByte(byte(record.Damage))
	p.WriteByte(byte(record.Damage >> 8))
	p.WriteByte(byte(record.Damage >> 16))
	p.WriteUInt32(record.EffectMask)
}
