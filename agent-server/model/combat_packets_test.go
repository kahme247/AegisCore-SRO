package model

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/g3n/engine/math32"
)

func TestCombatPacketGoldenFixtures(t *testing.T) {
	begin := BuildAttackBegin(0x11223344, 0x55667788, 0x99AABBCC, 0x01020304, true)
	if begin.MessageID != opcode.EntitySkillCastBegin {
		t.Fatalf("attack-begin opcode = 0x%04X, want 0x%04X", begin.MessageID, opcode.EntitySkillCastBegin)
	}
	wantBegin := []byte{
		1, 2, 0x30,
		1, 0, 0, 0,
		0x44, 0x33, 0x22, 0x11,
		0xCC, 0xBB, 0xAA, 0x99,
		0x88, 0x77, 0x66, 0x55,
		1, 1, 1,
		0x88, 0x77, 0x66, 0x55,
		0x80, 1,
		4, 3, 2,
		0, 0, 0, 0,
	}
	if got := begin.Payload(); !bytes.Equal(got, wantBegin) {
		t.Fatalf("attack-begin payload = % X, want % X", got, wantBegin)
	}

	end := BuildAttackEnd(0x55667788, 0x99AABBCC, 0x00010203, false)
	if end.MessageID != opcode.EntitySkillCastEnd {
		t.Fatalf("attack-end opcode = 0x%04X, want 0x%04X", end.MessageID, opcode.EntitySkillCastEnd)
	}
	wantEnd := []byte{
		1,
		0xCC, 0xBB, 0xAA, 0x99,
		0x88, 0x77, 0x66, 0x55,
		1, 1, 1,
		0x88, 0x77, 0x66, 0x55,
		0, 1,
		3, 2, 1, 0,
		0, 0, 0,
	}
	if got := end.Payload(); !bytes.Equal(got, wantEnd) {
		t.Fatalf("attack-end payload = % X, want % X", got, wantEnd)
	}
}

func TestCombatDamageRecordsEncodeCriticalStatusAndBlock(t *testing.T) {
	packet := BuildAttackEndDamageRecords(20, 30, []CombatDamageRecord{
		{DamageState: DamageStateCritical, Damage: 0x010203, EffectMask: 2},
		{AttackState: DamageAttackStateBlock},
	})
	want := []byte{
		1,
		30, 0, 0, 0,
		20, 0, 0, 0,
		1, 2, 1,
		20, 0, 0, 0,
		DamageAttackStateNormal, DamageStateCritical | DamageStateStatus,
		3, 2, 1,
		2, 0, 0, 0,
		DamageAttackStateBlock,
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("critical/status/block payload = % X, want % X", got, want)
	}
}

func TestCombatDamageRecordEncodesSourceStateWithoutPayload(t *testing.T) {
	packet := BuildAttackEndDamageRecords(20, 30, []CombatDamageRecord{{AttackState: DamageResultSourceState}})
	wantTail := []byte{20, 0, 0, 0, DamageResultSourceState}
	got := packet.Payload()
	if len(got) < len(wantTail) || !bytes.Equal(got[len(got)-len(wantTail):], wantTail) {
		t.Fatalf("source-state tail = % X, want % X", got, wantTail)
	}
}

func TestCombatDamageRecordEncodesPWBarrierVariant(t *testing.T) {
	packet := BuildAttackEndDamageRecords(20, 30, []CombatDamageRecord{{
		AttackState: DamageResultBarrier | DamageAttackStateDead,
		DamageState: DamageStateNormal,
		Damage:      0x010203, BarrierRemaining: 0, BarrierMaximum: 824,
	}})
	want := []byte{
		1, 30, 0, 0, 0,
		20, 0, 0, 0,
		1, 1, 1,
		20, 0, 0, 0,
		DamageResultBarrier | DamageAttackStateDead,
		DamageStateNormal, 3, 2, 1,
		0, 0, 0x38, 0x03,
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("pw barrier payload = % X, want % X", got, want)
	}
}

func TestCombatDamageRecordEncodesKnockbackEndpoint(t *testing.T) {
	packet := BuildAttackEndDamageRecords(20, 30, []CombatDamageRecord{{
		AttackState: DamageResultKnockback, DamageState: DamageStateNormal,
		Damage: 0x010203, EffectMask: 0x11223344,
		EndpointRegion: 0x5C89, EndpointX: 12.5, EndpointY: 3, EndpointZ: -7.25,
	}})
	wantTail := []byte{
		DamageResultKnockback, DamageStateNormal | DamageStateStatus, 3, 2, 1,
		0x44, 0x33, 0x22, 0x11,
		0x89, 0x5C,
		0x00, 0x00, 0x48, 0x41,
		0x00, 0x00, 0x40, 0x40,
		0x00, 0x00, 0xE8, 0xC0,
	}
	got := packet.Payload()
	if len(got) < len(wantTail) || !bytes.Equal(got[len(got)-len(wantTail):], wantTail) {
		t.Fatalf("knockback tail = % X, want % X", got, wantTail)
	}
}

func TestCombatDamageTargetsEncodeOneHitAcrossLinkedTargets(t *testing.T) {
	packet := BuildAttackEndDamageTargets(30, []CombatDamageTarget{
		{TargetUID: 20, Records: []CombatDamageRecord{{Damage: 130}}},
		{TargetUID: 21, Records: []CombatDamageRecord{{Damage: 70}}},
	})
	payload := packet.Payload()
	if len(payload) < 12 || payload[9] != 1 || payload[10] != 1 || payload[11] != 2 {
		t.Fatalf("linked target framing = % X, want active/one-hit/two-targets", payload)
	}
}

func TestAttackBeginCarriesSelectedBaseSkill(t *testing.T) {
	packet := BuildAttackBeginWithSkill(8419, 10, 20, 30, 40, false)
	for range 3 {
		if _, err := packet.ReadByte(); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := packet.ReadUInt32(); err != nil || got != 8419 {
		t.Fatalf("skill ID = %d, want 8419: %v", got, err)
	}
}

func TestBuildDelayedAttackBeginCarriesNoDamageUntilB071(t *testing.T) {
	packet := BuildDelayedAttackBegin(160, 10, 20, 30)
	if packet.MessageID != opcode.EntitySkillCastBegin {
		t.Fatalf("opcode = 0x%04X, want B070", packet.MessageID)
	}
	for range 3 {
		if _, err := packet.ReadByte(); err != nil {
			t.Fatal(err)
		}
	}
	for index, want := range []uint32{160, 10, 30} {
		got, err := packet.ReadUInt32()
		if err != nil || got != want {
			t.Fatalf("header field %d = %d, want %d: %v", index, got, want, err)
		}
	}
	delegatedValue, err := packet.ReadUInt32()
	if err != nil || delegatedValue != 20 {
		t.Fatalf("target UID = %d, want 20: %v", delegatedValue, err)
	}
	hasDamage, err := packet.ReadBool()
	if err != nil || hasDamage {
		t.Fatalf("damage flag = %t, want false: %v", hasDamage, err)
	}
	if packet.Remaining() != 0 {
		t.Fatalf("trailing bytes = %d, want 0", packet.Remaining())
	}
}

func TestBuildBuffCastBeginIncludesDelegatedValueBeforeFlags(t *testing.T) {
	packet := BuildBuffCastBegin(10033, 0x11223344, 0x55667788, 0x99AABBCC)
	if packet.MessageID != opcode.EntitySkillCastBegin {
		t.Fatalf("opcode = 0x%04X, want B070", packet.MessageID)
	}
	for range 3 {
		if _, err := packet.ReadByte(); err != nil {
			t.Fatal(err)
		}
	}
	for index, want := range []uint32{10033, 0x11223344, 0x99AABBCC} {
		got, err := packet.ReadUInt32()
		if err != nil || got != want {
			t.Fatalf("header field %d = %d, want %d: %v", index, got, want, err)
		}
	}
	delegatedValue, err := packet.ReadUInt32()
	if err != nil || delegatedValue != 0x55667788 {
		t.Fatalf("target UID = %d, want %d: %v", delegatedValue, uint32(0x55667788), err)
	}
	flags, err := packet.ReadByte()
	if err != nil || flags != 0 || packet.Remaining() != 0 {
		t.Fatalf("flags/trailing = %d/%d, want 0/0: %v", flags, packet.Remaining(), err)
	}
}

func TestBuildTeleportCastEndCarriesTeleportFlagAndEndpoint(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(0x5C89),
		Offset: math32.NewVector3(464, 508, 11),
	}
	packet := BuildTeleportCastEnd(0x11223344, 0x55667788, position)
	want := []byte{0x01, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x08,
		0x89, 0x5C,
		0xD0, 0x01, 0x00, 0x00,
		0xFC, 0x01, 0x00, 0x00,
		0x0B, 0x00, 0x00, 0x00}
	if packet.MessageID != opcode.EntitySkillCastEnd || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("teleport end = opcode %04X payload % X, want B071 % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestBuildMovementCastBeginIncludesDelegatedValueBeforePositionFlags(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(0x5C89),
		Offset: math32.NewVector3(464, 508, 11),
	}
	packet := BuildMovementCastBegin(8142, 0x11223344, 0x11223344, 0x55667788, position)
	want := []byte{
		0x01, 0x02, 0x30,
		0xCE, 0x1F, 0x00, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x88, 0x77, 0x66, 0x55,
		0x44, 0x33, 0x22, 0x11,
		0x08,
		0x89, 0x5C,
		0xD0, 0x01, 0x00, 0x00,
		0xFC, 0x01, 0x00, 0x00,
		0x0B, 0x00, 0x00, 0x00,
	}
	if packet.MessageID != opcode.EntitySkillCastBegin || len(packet.Payload()) != 34 || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("movement begin = opcode %04X payload % X, want % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestBuildStatusAttackEndCarriesFrostbiteWithoutDamage(t *testing.T) {
	packet := BuildStatusAttackEnd(200, 300, []SkillEffectTarget{{TargetUID: 200, EffectMask: 2}})
	payload := packet.Payload()
	if len(payload) != 25 || payload[9] != 1 || payload[10] != 1 || payload[11] != 1 {
		t.Fatalf("status cast end = % X", payload)
	}
	if got := payload[18:21]; !bytes.Equal(got, []byte{0, 0, 0}) {
		t.Fatalf("status-only damage = % X, want zero", got)
	}
	if got := payload[21:25]; !bytes.Equal(got, []byte{2, 0, 0, 0}) {
		t.Fatalf("status effect = % X, want Frostbite bit 2", got)
	}
}

func TestBuildBadEffectUpdateCarriesMaskAndHighEffectLevel(t *testing.T) {
	packet := BuildBadEffectUpdate(200, 0x4002, []byte{7})
	want := []byte{0xC8, 0, 0, 0, 0, 0, 4, 2, 0x40, 0, 0, 7}
	if packet.MessageID != opcode.EntityUpdateStatus || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("bad-effect update = opcode %04X payload % X, want 3057 % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestBuildPlayerBadEffectSnapshotCarriesStockTimerUnits(t *testing.T) {
	packet := BuildPlayerBadEffectSnapshot(0x20, []CombatBadEffectDetail{{
		DurationCentiseconds: 158,
		ElapsedCentiseconds:  3,
		Value:                69,
	}})
	want := []byte{0x20, 0, 0, 0, 158, 0, 0, 0, 3, 0, 69, 0}
	if packet.MessageID != opcode.ActionUnknownUpdate2 || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("bad-effect snapshot = opcode %04X payload % X, want 30D2 % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestAttackEndSerializesTwoHitsBeforeOneTarget(t *testing.T) {
	packet := BuildAttackEndHits(0x55667788, 0x99AABBCC, []uint32{0x008101, 0x008102}, true)
	want := []byte{
		1,
		0xCC, 0xBB, 0xAA, 0x99,
		0x88, 0x77, 0x66, 0x55,
		1, 2, 1,
		0x88, 0x77, 0x66, 0x55,
		0, 1, 0x01, 0x81, 0, 0, 0, 0, 0,
		0x80, 1, 0x02, 0x81, 0, 0, 0, 0, 0,
	}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("two-hit attack-end payload = % X, want % X", got, want)
	}
}

func TestActionUIDsAreProcessWideAndNonZero(t *testing.T) {
	first := NextActionUID()
	second := NextActionUID()
	if first == 0 || second == 0 || first == second {
		t.Fatalf("action UIDs = %d, %d; want distinct non-zero values", first, second)
	}
}

func TestBuildTimedBuffAddUsesStockFixedLayout(t *testing.T) {
	packet := BuildTimedBuffAdd(100, 3977, 55, 5_400_000)
	want := []uint32{100, 3977, 55}
	for index, expected := range want {
		value, err := packet.ReadUInt32()
		if err != nil || value != expected {
			t.Fatalf("field %d = %d, want %d: %v", index, value, expected, err)
		}
	}
	if packet.Remaining() != 0 {
		t.Fatalf("stock B0BD trailing bytes = %d", packet.Remaining())
	}
}

func TestBuildSkillApplicationPacketsMatchB0BCSubtypesOneAndFour(t *testing.T) {
	records := [][3]byte{{1, 2, 3}, {4, 5, 6}}
	one := BuildSkillApplicationRecords(0x11223344, 0x80, 0x55667788, records)
	wantOne := []byte{1, 0x44, 0x33, 0x22, 0x11, 0x80, 0x88, 0x77, 0x66, 0x55, 2, 1, 2, 3, 4, 5, 6}
	if one.MessageID != opcode.EntitySkillApplicationResponse || !bytes.Equal(one.Payload(), wantOne) {
		t.Fatalf("subtype-1 = opcode %04X payload % X, want B0BC % X", one.MessageID, one.Payload(), wantOne)
	}
	four := BuildSkillApplicationDepletion(0x80, 0x55667788, 0x99AA)
	wantFour := []byte{4, 0x80, 0x88, 0x77, 0x66, 0x55, 0xAA, 0x99}
	if four.MessageID != opcode.EntitySkillApplicationResponse || !bytes.Equal(four.Payload(), wantFour) {
		t.Fatalf("subtype-4 = opcode %04X payload % X, want B0BC % X", four.MessageID, four.Payload(), wantFour)
	}
}
