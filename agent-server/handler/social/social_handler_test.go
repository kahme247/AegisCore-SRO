package social

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestOperationPacketUsesStockResultEnvelope(t *testing.T) {
	success := operationPacket(opcode.GuildLeaveResponse, nil)
	if success.MessageID != opcode.GuildLeaveResponse || len(success.Payload()) != 1 || success.Payload()[0] != 1 {
		t.Fatalf("success packet = opcode 0x%04X payload %v", success.MessageID, success.Payload())
	}
	failure := operationPacket(opcode.GuildLeaveResponse, model.ErrSocialAccessDenied)
	if got := failure.Payload(); len(got) != 3 || got[0] != 2 || got[1] != 0x03 || got[2] != 0x4A {
		t.Fatalf("failure payload = %v, want [2 3 74]", got)
	}
}

func TestSocialErrorCodesDistinguishDomainFailures(t *testing.T) {
	errorsToCodes := map[error]uint16{
		model.ErrSocialInsufficientLevel: 0x4A01,
		model.ErrSocialInsufficientGold:  0x4A02,
		model.ErrSocialAccessDenied:      0x4A03,
		model.ErrSocialCapacity:          0x4A04,
		model.ErrSocialConflict:          0x4A05,
		model.ErrSocialNotFound:          0x4A06,
	}
	for err, want := range errorsToCodes {
		if got := socialErrorCode(errors.Join(errors.New("wrapped"), err)); got != want {
			t.Fatalf("socialErrorCode(%v) = %#x, want %#x", err, got, want)
		}
	}
}

func TestWriteGuildMatchesVSRO188MemberLayout(t *testing.T) {
	p := operationPacket(opcode.GuildCreateResponse, nil)
	writeGuild(&p, model.GuildSnapshot{ID: 7, Name: "Aegis", Level: 2, Members: []model.GuildMember{{CharacterID: 9, Name: "Khaled", Class: model.GuildMemberMaster, Level: 80, Permissions: model.GuildPermissionAll, RefObjectID: 1907, Online: true}}})
	if err := p.WriteError(); err != nil {
		t.Fatal(err)
	}
	if len(p.Payload()) < 40 || p.Payload()[0] != 1 {
		t.Fatalf("guild payload is incomplete: %v", p.Payload())
	}
}

func TestAcademySuccessResponsesDoNotEmbedUnverifiedSnapshot(t *testing.T) {
	for _, messageID := range []uint16{opcode.AcademyCreateResponse, opcode.AcademyMatchingJoinResponse} {
		packet := operationPacket(messageID, nil)
		if got := packet.Payload(); len(got) != 1 || got[0] != 1 {
			t.Fatalf("academy response 0x%04X payload = %v, want result envelope only", messageID, got)
		}
	}
}
