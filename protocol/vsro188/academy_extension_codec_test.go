package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestAcademyExtensionRequestsMatchClientBuilders(t *testing.T) {
	invite := network.EmptyClientPacket()
	invite.WriteUInt32(7)
	if target, err := DecodeAcademyInviteRequest(&invite); err != nil || target != 7 {
		t.Fatalf("invite = %d, %v", target, err)
	}
	action := network.EmptyClientPacket()
	action.WriteString("Aegis")
	if name, err := DecodeAcademyMemberActionRequest(&action); err != nil || name != "Aegis" {
		t.Fatalf("action = %q, %v", name, err)
	}
	answer := network.EmptyClientPacket()
	answer.WriteUInt32(1)
	answer.WriteUInt32(2)
	answer.WriteByte(1)
	if response, err := DecodeAcademyInvitationResponse(&answer); err != nil || response != (AcademyInvitationResponse{AcademyID: 1, LeaderID: 2, Accepted: true}) {
		t.Fatalf("answer = %#v, %v", response, err)
	}
}

func TestAcademyExtensionPublicationsMatchClientParsers(t *testing.T) {
	invitation := BuildAcademyInvitation(1, 2)
	if got := hex.EncodeToString(invitation.Payload()); got != "0100000002000000" {
		t.Fatalf("invitation = %s", got)
	}
	timed, err := BuildAcademyTimedOperation([]AcademyTimedUpdate{{DurationMinutes: 3, Reference: 4, Value: 5}}, 0)
	if err != nil || hex.EncodeToString(timed.Payload()) != "0001030000000400000005000000" {
		t.Fatalf("timed = %x, %v", timed.Payload(), err)
	}
	update := BuildAcademyUpdate(3, 7, 1)
	if got := hex.EncodeToString(update.Payload()); got != "030700000001" {
		t.Fatalf("update = %s", got)
	}
	unknown86 := BuildAcademyUnknownUpdate3C86(9)
	unknown87 := BuildAcademyUnknownUpdate3C87(9, 10)
	if hex.EncodeToString(unknown86.Payload()) != "09000000" || hex.EncodeToString(unknown87.Payload()) != "090000000a000000" {
		t.Fatalf("unknown updates = %x/%x", unknown86.Payload(), unknown87.Payload())
	}
}
