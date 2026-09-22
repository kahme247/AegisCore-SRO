package model

import (
	"math"
	"sync/atomic"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

var nextActionUID atomic.Uint32

const (
	DamageAttackStateNormal byte = 0x00
	DamageResultSourceState byte = 0x01
	DamageAttackStateBlock  byte = 0x02
	DamageResultTargetState byte = 0x03
	DamageAttackStateDead   byte = 0x80
	DamageResultKnockOver   byte = 0x04
	DamageResultKnockback   byte = 0x05
	DamageResultBarrier     byte = 0x07
	DamageResultSuppressed  byte = 0x08

	DamageStateNormal   byte = 0x01
	DamageStateCritical byte = 0x02
	DamageStateStatus   byte = 0x10
)

// CombatDamageRecord is one stock TestIn per-hit result. A blocked hit carries
// only AttackState on the wire; successful hits also carry DamageState, the
// 24-bit damage value, and the abnormal-effect mask.
type CombatDamageRecord struct {
	AttackState      byte
	DamageState      byte
	Damage           uint32
	EffectMask       uint32
	BarrierRemaining uint16
	BarrierMaximum   uint16
	EndpointRegion   uint16
	EndpointX        float32
	EndpointY        float32
	EndpointZ        float32
}

type CombatDamageTarget struct {
	TargetUID uint32
	Records   []CombatDamageRecord
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

func BuildAttackBeginWithSkillDamageTargets(skillID, sourceUID, castUID uint32, targets []CombatDamageTarget) network.Packet {
	p := buildAttackBeginHeader(skillID, sourceUID, 0, castUID)
	WriteDamageTargetRecords(&p, targets)
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
	p := buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID)
	p.WriteBool(false)
	return p
}

// BuildMovementCastBegin starts Ghost Walk with its position continuation.
// B070's delegated grammar begins with a four-byte value before the flags;
// omitting it makes the client interpret coordinate bytes as flags and causes
// destination-dependent NetProcessInNavAndSkill.cpp:246 assertion faults.
func BuildMovementCastBegin(skillID, sourceUID, targetUID, castUID uint32, position navmeshv2.RtNavmeshPosition) network.Packet {
	p := buildAttackBeginHeader(skillID, sourceUID, targetUID, castUID)
	writeActionPosition(&p, position)
	return p
}

func BuildBuffCastEnd(targetUID, castUID uint32) network.Packet {
	p := buildAttackEndHeader(targetUID, castUID)
	p.WriteBool(false)
	return p
}

// BuildTeleportCastEnd completes a movement skill with ActionStateFlag.Teleport
// and the authoritative endpoint. The stock client remains locked in the
// Ghost Walk animation if B071 carries flag 0 or omits this position.
func BuildTeleportCastEnd(targetUID, castUID uint32, position navmeshv2.RtNavmeshPosition) network.Packet {
	p := buildAttackEndHeader(targetUID, castUID)
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

func BuildTimedBuffAdd(targetUID, skillID, token uint32, _ uint32) network.Packet {
	// Stock vSRO 1.188 B0BD is the fixed 12-byte UID/skill/token tuple. The
	// client does not accept an appended duration; sending one triggers its
	// deliberate packet-contract access violation. Duration remains an
	// authoritative server concern used to schedule B0BE removal.
	return BuildBuffAdd(targetUID, skillID, token)
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

// BuildPlayerBadEffectSnapshot publishes the local-player abnormal records
// consumed by sro_client 0x008ADD60. Durations and elapsed values use the
// stock server's 100-millisecond wire units.
func BuildPlayerBadEffectSnapshot(effectMask uint32, details []CombatBadEffectDetail) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ActionUnknownUpdate2
	p.WriteUInt32(effectMask)
	for _, detail := range details {
		p.WriteUInt32(detail.DurationCentiseconds)
		p.WriteUInt16(detail.ElapsedCentiseconds)
		p.WriteUInt16(detail.Value)
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
	packet, _ := (vsro188.CombatAttackBeginSuccessPrefixCodec{}).Encode(vsro188.CombatAttackBeginSuccessPrefix{
		SkillID: skillID, SourceUniqueID: sourceUID, CastUniqueID: castUID, TargetUniqueID: targetUID,
	})
	return packet
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
	p := buildAttackEndHeader(targetUID, castUID)
	WriteDamageBlockRecords(&p, targetUID, records)
	return p
}

func BuildAttackEndDamageTargets(castUID uint32, targets []CombatDamageTarget) network.Packet {
	var targetUID uint32
	if len(targets) > 0 {
		targetUID = targets[0].TargetUID
	}
	p := buildAttackEndHeader(targetUID, castUID)
	WriteDamageTargetRecords(&p, targets)
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
	p := buildAttackEndHeader(animationTarget, castUID)
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

func buildAttackEndHeader(targetUID, castUID uint32) network.Packet {
	packet, _ := (vsro188.CombatAttackEndSuccessPrefixCodec{}).Encode(vsro188.CombatAttackEndSuccessPrefix{
		CastUniqueID: castUID, TargetUniqueID: targetUID,
	})
	return packet
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
	WriteDamageTargetRecords(p, []CombatDamageTarget{{TargetUID: targetUID, Records: records}})
}

func WriteDamageTargetRecords(p *network.Packet, targets []CombatDamageTarget) {
	if len(targets) == 0 || len(targets[0].Records) == 0 {
		p.WriteBool(false)
		return
	}
	hitCount := len(targets[0].Records)
	for _, target := range targets {
		if target.TargetUID == 0 || len(target.Records) != hitCount {
			p.WriteBool(false)
			return
		}
	}
	p.WriteBool(true)
	p.WriteByte(byte(hitCount))
	p.WriteByte(byte(len(targets)))
	for _, target := range targets {
		p.WriteUInt32(target.TargetUID)
		for _, record := range target.Records {
			writeDamageRecord(p, record)
		}
	}
}

func writeDamageRecord(p *network.Packet, record CombatDamageRecord) {
	p.WriteByte(record.AttackState)
	kind := record.AttackState &^ DamageAttackStateDead
	if kind != DamageAttackStateNormal && kind != DamageResultKnockOver && kind != DamageResultKnockback && kind != DamageResultBarrier {
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
	if kind == DamageResultBarrier {
		p.WriteUInt16(record.BarrierRemaining)
		p.WriteUInt16(record.BarrierMaximum)
		return
	}
	p.WriteUInt32(record.EffectMask)
	if kind == DamageResultKnockOver || kind == DamageResultKnockback {
		p.WriteUInt16(record.EndpointRegion)
		p.WriteUInt32(math.Float32bits(record.EndpointX))
		p.WriteUInt32(math.Float32bits(record.EndpointY))
		p.WriteUInt32(math.Float32bits(record.EndpointZ))
	}
}
