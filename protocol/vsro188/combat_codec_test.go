package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCombatCandidateCodecMatchesPinnedClientConstructor(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(1)
	packet.WriteByte(4)
	packet.WriteUInt32(0x12345678)
	packet.WriteByte(0)
	request, err := (CombatCandidateActionRequestCodec{}).Decode(packet)
	if err != nil || request.CandidateID != 0x12345678 {
		t.Fatalf("Decode() = %+v, %v", request, err)
	}
}

func TestCombatActionStateResponseCodecMatchesRecoveredReader(t *testing.T) {
	active, err := (CombatActionStateResponseCodec{}).Encode(CombatActionStateResponse{Active: true})
	if err != nil || !bytes.Equal(active.Payload(), []byte{1, 1}) {
		t.Fatalf("active Encode() = %x, %v", active.Payload(), err)
	}
	inactive, err := (CombatActionStateResponseCodec{}).Encode(CombatActionStateResponse{})
	if err != nil || !bytes.Equal(inactive.Payload(), []byte{2, 0}) {
		t.Fatalf("inactive Encode() = %x, %v", inactive.Payload(), err)
	}
}

func TestCombatSuccessPrefixCodecsMatchRecoveredReaders(t *testing.T) {
	begin, err := (CombatAttackBeginSuccessPrefixCodec{}).Encode(CombatAttackBeginSuccessPrefix{
		SkillID: 1, SourceUniqueID: 0x11223344, CastUniqueID: 0x99AABBCC, TargetUniqueID: 0x55667788,
	})
	wantBegin := []byte{1, 2, 0x30, 1, 0, 0, 0, 0x44, 0x33, 0x22, 0x11, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55}
	if err != nil || !bytes.Equal(begin.Payload(), wantBegin) {
		t.Fatalf("begin Encode() = %x, %v", begin.Payload(), err)
	}
	end, err := (CombatAttackEndSuccessPrefixCodec{}).Encode(CombatAttackEndSuccessPrefix{CastUniqueID: 0x99AABBCC, TargetUniqueID: 0x55667788})
	wantEnd := []byte{1, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55}
	if err != nil || !bytes.Equal(end.Payload(), wantEnd) {
		t.Fatalf("end Encode() = %x, %v", end.Payload(), err)
	}
}

func TestCombatCandidateCodecRejectsNonCanonicalForms(t *testing.T) {
	build := func(phase, action byte, candidate uint32, terminator byte, trailing bool) network.Packet {
		packet := network.EmptyClientPacket()
		packet.WriteByte(phase)
		packet.WriteByte(action)
		packet.WriteUInt32(candidate)
		packet.WriteByte(terminator)
		if trailing {
			packet.WriteByte(0)
		}
		return packet
	}
	for _, packet := range []network.Packet{
		build(0, 4, 1, 0, false),
		build(1, 1, 1, 0, false),
		build(1, 4, 0, 0, false),
		build(1, 4, 1, 1, false),
		build(1, 4, 1, 0, true),
	} {
		if _, err := (CombatCandidateActionRequestCodec{}).Decode(packet); err == nil {
			t.Fatalf("Decode() accepted %x", packet.Payload())
		}
	}
}
