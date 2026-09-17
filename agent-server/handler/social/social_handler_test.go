package social

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestAcademyInfoPacketMatchesRecovered3C81HeaderAndEightMemberLimit(t *testing.T) {
	members := make([]model.AcademyMember, 9)
	packet := buildAcademyInfoPacket(model.AcademySnapshot{ID: 0x11223344, Members: members}, nil)
	if packet.MessageID != opcode.AcademyInfo {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.AcademyInfo)
	}
	payload := packet.Payload()
	if len(payload) < 26 {
		t.Fatalf("payload length = %d, want at least 26", len(payload))
	}
	if got, want := payload[:25], []byte{0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("0x3C81 header = % X, want % X", got, want)
	}
	if payload[25] != 8 {
		t.Fatalf("0x3C81 member count = %d, want client limit 8", payload[25])
	}
}

func TestAcademyBuffPacketMatchesRecovered3C82Shape(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	packet := buildAcademyBuffPacket(model.AcademyBuffState{Type: 1, Value: 0x11223344, EndsAt: now.Add(90 * time.Second)}, now)
	if packet.MessageID != opcode.AcademyBuffUpdate {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.AcademyBuffUpdate)
	}
	if got, want := packet.Payload(), []byte{1, 2, 0, 0, 0, 0x44, 0x33, 0x22, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("0x3C82 payload = % X, want % X", got, want)
	}
}

func TestSafeTradeRestoreUpdateMatchesGameServerDowngrade(t *testing.T) {
	active, err := (vsro188.JobSafeTradeCodec{}).Encode(safeTradeRestoreUpdate(model.SafeTradeState{AbleCount: 3, Status: 1}, 8))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := active.Payload(), []byte{1, 0x51, 3, 8}; !bytes.Equal(got, want) {
		t.Fatalf("active restore = % X, want % X", got, want)
	}
	inactive, err := (vsro188.JobSafeTradeCodec{}).Encode(safeTradeRestoreUpdate(model.SafeTradeState{AbleCount: 3}, 8))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := inactive.Payload(), []byte{0, 0x40}; !bytes.Equal(got, want) {
		t.Fatalf("inactive restore = % X, want % X", got, want)
	}
}

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
