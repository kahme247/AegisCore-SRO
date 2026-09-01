package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestDecodeSkillRestorationRequestUsesBinaryConfirmedLayout(t *testing.T) {
	packet := network.EmptyPacket()
	packet.WriteUInt32(0x01020304)
	packet.WriteUInt32(0x11223344)
	packet.WriteByte(7)

	request, err := DecodeSkillRestorationRequest(&packet)
	if err != nil {
		t.Fatalf("DecodeSkillRestorationRequest() error = %v", err)
	}
	if request != (SkillRestorationRequest{ItemRefID: 0x01020304, TargetID: 0x11223344, TargetLevel: 7}) {
		t.Fatalf("request = %+v", request)
	}
}

func TestDecodeSkillRestorationRequestRejectsNonExactPayload(t *testing.T) {
	for _, size := range []int{0, 8, 10} {
		packet := network.EmptyPacket()
		for range size {
			packet.WriteByte(0)
		}
		if _, err := DecodeSkillRestorationRequest(&packet); err == nil {
			t.Fatalf("payload size %d accepted", size)
		}
	}
}

func TestSkillRestorationResponseLayouts(t *testing.T) {
	skill := BuildSkillWithdrawSuccess(0x01020304)
	if skill.MessageID != opcode.SkillWithdrawResponse || !bytes.Equal(skill.Payload(), []byte{1, 4, 3, 2, 1}) {
		t.Fatalf("skill response = %04X/% X", skill.MessageID, skill.Payload())
	}
	skillBody := network.EmptyClientPacket()
	skillBody.WriteBytes(skill.Payload())
	resultingSkillID, err := DecodeSkillWithdrawSuccess(&skillBody)
	if err != nil || resultingSkillID != 0x01020304 {
		t.Fatalf("decoded skill response = 0x%08X/%v", resultingSkillID, err)
	}

	mastery := BuildMasteryWithdrawSuccess(0x01020304, 5)
	if mastery.MessageID != opcode.MasteryWithdrawResponse || !bytes.Equal(mastery.Payload(), []byte{1, 4, 3, 2, 1, 5}) {
		t.Fatalf("mastery response = %04X/% X", mastery.MessageID, mastery.Payload())
	}
	masteryBody := network.EmptyClientPacket()
	masteryBody.WriteBytes(mastery.Payload())
	masteryID, masteryLevel, err := DecodeMasteryWithdrawSuccess(&masteryBody)
	if err != nil || masteryID != 0x01020304 || masteryLevel != 5 {
		t.Fatalf("decoded mastery response = 0x%08X/%d/%v", masteryID, masteryLevel, err)
	}

	failure := BuildSkillRestorationFailure(opcode.SkillWithdrawResponse, 0x7806)
	if failure.MessageID != opcode.SkillWithdrawResponse || !bytes.Equal(failure.Payload(), []byte{2, 6, 0x78}) {
		t.Fatalf("failure response = %04X/% X", failure.MessageID, failure.Payload())
	}
	failureBody := network.EmptyClientPacket()
	failureBody.WriteBytes(failure.Payload())
	code, err := DecodeSkillRestorationFailure(&failureBody)
	if err != nil || code != 0x7806 {
		t.Fatalf("decoded failure response = 0x%04X/%v", code, err)
	}
}

func TestSkillRestorationSuccessDecodersRejectTrailingBytes(t *testing.T) {
	for _, payload := range [][]byte{
		{1, 4, 3, 2, 1, 0xFF},
		{1, 4, 3, 2, 1, 5, 0xFF},
		{2, 6, 0x78, 0xFF},
	} {
		packet := network.EmptyClientPacket()
		packet.WriteBytes(payload)
		switch len(payload) {
		case 6:
			if _, err := DecodeSkillWithdrawSuccess(&packet); err == nil {
				t.Fatalf("skill success accepted trailing payload %X", payload)
			}
		case 7:
			if _, _, err := DecodeMasteryWithdrawSuccess(&packet); err == nil {
				t.Fatalf("mastery success accepted trailing payload %X", payload)
			}
		default:
			if _, err := DecodeSkillRestorationFailure(&packet); err == nil {
				t.Fatalf("failure accepted trailing payload %X", payload)
			}
		}
	}
}
