package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestForgottenWorldRequestLayoutsMatchClientBuilders(t *testing.T) {
	solve := network.EmptyPacket()
	solve.WriteUInt32(11)
	solve.WriteUInt32(22)
	solve.WriteByte(1)
	decodedSolve, err := DecodeRecallSolveRequest(&solve)
	if err != nil || decodedSolve != (RecallSolveRequest{ChallengeID: 11, Answer: 22, Mode: 1}) {
		t.Fatalf("solve = %#v, %v", decodedSolve, err)
	}

	response := network.EmptyPacket()
	response.WriteUInt32(33)
	response.WriteUInt32(44)
	response.WriteByte(1)
	decodedResponse, err := DecodeForgottenWorldRecallResponse(&response)
	if err != nil || decodedResponse != (ForgottenWorldRecallResponse{RecallID: 33, MemberID: 44, Accepted: true}) {
		t.Fatalf("response = %#v, %v", decodedResponse, err)
	}

	for name, decode := range map[string]func(*network.Packet) (uint32, error){
		"list":   DecodeForgottenWorldRecallListRequest,
		"member": DecodeForgottenWorldRecallMemberRequest,
		"exit":   DecodeForgottenWorldExitRequest,
	} {
		packet := network.EmptyPacket()
		packet.WriteUInt32(55)
		if value, decodeErr := decode(&packet); decodeErr != nil || value != 55 {
			t.Fatalf("%s = %d, %v", name, value, decodeErr)
		}
	}
}

func TestForgottenWorldPublicationLayoutsMatchClientParsers(t *testing.T) {
	update, err := BuildForgottenWorldUpdate(ForgottenWorldUpdate{MemberID: 7, State: 1})
	if err != nil || update.MessageID != opcode.FGWUpdate || hex.EncodeToString(update.Payload()) != "070000001001" {
		t.Fatalf("update = %04x %x, %v", update.MessageID, update.Payload(), err)
	}
	challenge := BuildRecallChallenge(RecallChallenge{ChallengeID: 1, Left: 2, Right: 3})
	if challenge.MessageID != opcode.RecallChallenge || hex.EncodeToString(challenge.Payload()) != "010000000200000003000000" {
		t.Fatalf("challenge = %04x %x", challenge.MessageID, challenge.Payload())
	}

	invitation, err := BuildForgottenWorldRecallInvitation(1, 2, 3, "Aegis")
	if err != nil || invitation.MessageID != opcode.FGWRecallRequest || hex.EncodeToString(invitation.Payload()) != "01000000020000000300000005004165676973" {
		t.Fatalf("invitation = %04x %x, %v", invitation.MessageID, invitation.Payload(), err)
	}

	list, err := BuildForgottenWorldRecallList([]ForgottenWorldRecallListEntry{{MemberID: 7, State: 1, Level: 90, RegionID: 326}}, 0)
	if err != nil || hex.EncodeToString(list.Payload()) != "010107000000015a4601" {
		t.Fatalf("list = %x, %v", list.Payload(), err)
	}

	for _, messageID := range []uint16{opcode.RecallResponse, opcode.RecallSolveResponse, opcode.FGWRecallMemberResponse, opcode.FGWRecallMemberAcceptedResponse, opcode.FGWExitResponse} {
		success := BuildRecallResult(messageID, 0)
		failure := BuildRecallResult(messageID, 0x1c33)
		if hex.EncodeToString(success.Payload()) != "01" || hex.EncodeToString(failure.Payload()) != "02331c" {
			t.Fatalf("%04x results = %x / %x", messageID, success.Payload(), failure.Payload())
		}
	}
}
