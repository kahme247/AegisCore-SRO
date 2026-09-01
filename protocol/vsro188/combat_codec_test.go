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
	end, err := (CombatAttackEndSuccessPrefixCodec{}).Encode(CombatAttackEndSuccessPrefix{CastUniqueID: 0x99AABBCC, TargetUniqueID: 0x11223344})
	wantEnd := []byte{1, 0xCC, 0xBB, 0xAA, 0x99, 0x44, 0x33, 0x22, 0x11}
	if err != nil || !bytes.Equal(end.Payload(), wantEnd) {
		t.Fatalf("end Encode() = %x, %v", end.Payload(), err)
	}
}

func TestCombatResponseDecodersMatchRecoveredB070B071Prefixes(t *testing.T) {
	b070 := network.EmptyClientPacket()
	b070.WriteByte(1)
	b070.WriteUInt16(0x3002)
	b070.WriteUInt32(0x11223344)
	b070.WriteUInt32(0x55667788)
	b070.WriteUInt32(0x99AABBCC)
	b070.WriteUInt32(0x01020304)
	b070.WriteBytes([]byte{0xDE, 0xAD})
	begin, err := DecodeCombatAttackBeginResponse(b070)
	if err != nil {
		t.Fatal(err)
	}
	if begin.Result != 1 || begin.ResultFamily != 0x3002 || begin.SkillID != 0x11223344 || begin.SourceUID != 0x55667788 || begin.CastUID != 0x99AABBCC || begin.TargetUID != 0x01020304 || !bytes.Equal(begin.Remainder, []byte{0xDE, 0xAD}) {
		t.Fatalf("B070 success = %+v", begin)
	}

	b070Failure := network.EmptyClientPacket()
	b070Failure.WriteByte(2)
	b070Failure.WriteUInt16(0x3344)
	begin, err = DecodeCombatAttackBeginResponse(b070Failure)
	if err != nil || begin.Result != 2 || begin.FailureCode != 0x3344 || len(begin.Remainder) != 0 {
		t.Fatalf("B070 failure = %+v, %v", begin, err)
	}

	b071 := network.EmptyClientPacket()
	b071.WriteByte(1)
	b071.WriteUInt32(0xAABBCCDD)
	b071.WriteBytes([]byte{0x01, 0x02, 0x03})
	end, err := DecodeCombatAttackEndResponse(b071)
	if err != nil || end.Result != 1 || end.CastUID != 0xAABBCCDD || !bytes.Equal(end.Remainder, []byte{1, 2, 3}) {
		t.Fatalf("B071 success = %+v, %v", end, err)
	}

	b071Failure := network.EmptyClientPacket()
	b071Failure.WriteByte(2)
	b071Failure.WriteUInt16(0x7788)
	b071Failure.WriteUInt32(0x01020304)
	b071Failure.WriteByte(0xFF)
	end, err = DecodeCombatAttackEndResponse(b071Failure)
	if err != nil || end.Result != 2 || end.FailureCode != 0x7788 || end.CastUID != 0x01020304 || !bytes.Equal(end.Remainder, []byte{0xFF}) {
		t.Fatalf("B071 failure = %+v, %v", end, err)
	}
}

func TestCombatResponseDecodersRejectUnconfirmedB070Forms(t *testing.T) {
	b070Family := network.EmptyClientPacket()
	b070Family.WriteByte(1)
	b070Family.WriteUInt16(0x3001)
	for range 3 {
		b070Family.WriteUInt32(0)
	}
	if _, err := DecodeCombatAttackBeginResponse(b070Family); err == nil {
		t.Fatal("B070 accepted an unconfirmed result family")
	}

	b070Failure := network.EmptyClientPacket()
	b070Failure.WriteByte(2)
	b070Failure.WriteUInt16(0x1234)
	b070Failure.WriteByte(0)
	if _, err := DecodeCombatAttackBeginResponse(b070Failure); err == nil {
		t.Fatal("B070 accepted trailing bytes on failure")
	}

	b071Short := network.EmptyClientPacket()
	b071Short.WriteByte(2)
	b071Short.WriteUInt16(0x1234)
	if _, err := DecodeCombatAttackEndResponse(b071Short); err == nil {
		t.Fatal("B071 accepted a failure without action ID")
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
