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
	for index, want := range []uint32{160, 10, 30, 20} {
		got, err := packet.ReadUInt32()
		if err != nil || got != want {
			t.Fatalf("header field %d = %d, want %d: %v", index, got, want, err)
		}
	}
	hasDamage, err := packet.ReadBool()
	if err != nil || hasDamage {
		t.Fatalf("damage flag = %t, want false: %v", hasDamage, err)
	}
	if packet.Remaining() != 0 {
		t.Fatalf("trailing bytes = %d, want 0", packet.Remaining())
	}
}

func TestBuildTeleportCastEndCarriesTeleportFlagAndEndpoint(t *testing.T) {
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(0x5C89),
		Offset: math32.NewVector3(464, 508, 11),
	}
	packet := BuildTeleportCastEnd(0x11223344, 0x55667788, position)
	want := []byte{
		0x01, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x08,
		0x89, 0x5C,
		0xD0, 0x01, 0x00, 0x00,
		0x0B, 0x00, 0x00, 0x00,
		0xFC, 0x01, 0x00, 0x00,
	}
	if packet.MessageID != opcode.EntitySkillCastEnd || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("teleport end = opcode %04X payload % X, want B071 % X", packet.MessageID, packet.Payload(), want)
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

func TestBuildTimedBuffAddAppendsAuthoritativeDuration(t *testing.T) {
	packet := BuildTimedBuffAdd(100, 3977, 55, 5_400_000)
	want := []uint32{100, 3977, 55, 5_400_000}
	for index, expected := range want {
		value, err := packet.ReadUInt32()
		if err != nil || value != expected {
			t.Fatalf("field %d = %d, want %d: %v", index, value, expected, err)
		}
	}
	if packet.Remaining() != 0 {
		t.Fatalf("timed buff trailing bytes = %d", packet.Remaining())
	}
}
