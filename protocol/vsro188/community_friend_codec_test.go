package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestCommunityFriendRequestLayoutsMatchClientBuilders(t *testing.T) {
	add := network.EmptyClientPacket()
	add.WriteString("Aegis")
	name, err := DecodeCommunityFriendAddRequest(&add)
	if err != nil || name != "Aegis" {
		t.Fatalf("add = %q, %v", name, err)
	}
	invitation, err := BuildCommunityFriendInvitation(1, 2, "Aegis")
	if err != nil || invitation.MessageID != opcode.CommunityFriendRequest || hex.EncodeToString(invitation.Payload()) != "010000000200000005004165676973" {
		t.Fatalf("invitation = %04x:%s, %v", invitation.MessageID, hex.EncodeToString(invitation.Payload()), err)
	}
	response := network.EmptyClientPacket()
	response.WriteUInt32(1)
	response.WriteUInt32(2)
	response.WriteByte(1)
	decoded, err := DecodeCommunityFriendResponse(&response)
	if err != nil || decoded != (CommunityFriendResponse{RequesterID: 1, TargetID: 2, Accepted: true}) {
		t.Fatalf("response = %+v, %v", decoded, err)
	}
}

func TestCommunityFriendPublicationLayoutsMatchGameServer(t *testing.T) {
	friend := CommunityFriendRecord{CharacterID: 2, Name: "Aegis", RefObjectID: 1907, Online: true}
	info, err := BuildCommunityFriendInfo([]CommunityFriendRecord{friend})
	if err != nil || info.MessageID != opcode.CommunityFriendInfo || hex.EncodeToString(info.Payload()) != "0102000000050041656769737307000001" {
		t.Fatalf("info = %04x:%s, %v", info.MessageID, hex.EncodeToString(info.Payload()), err)
	}
	added, err := BuildCommunityFriendAdded(friend)
	if err != nil || hex.EncodeToString(added.Payload()) != "02020000000500416567697373070000" {
		t.Fatalf("added = %s, %v", hex.EncodeToString(added.Payload()), err)
	}
	removed := BuildCommunityFriendRemoved(2)
	state := BuildCommunityFriendState(2, true)
	if hex.EncodeToString(removed.Payload()) != "0302000000" || hex.EncodeToString(state.Payload()) != "040200000001" {
		t.Fatalf("removed/state = %s/%s", hex.EncodeToString(removed.Payload()), hex.EncodeToString(state.Payload()))
	}
}

func TestCommunityFriendOperationResultsMatchClientParsers(t *testing.T) {
	rejected := BuildCommunityFriendAddFailure(0x640b, 2)
	deleted := BuildCommunityFriendDeleteResult(7, 0)
	if hex.EncodeToString(rejected.Payload()) != "020b6402" || hex.EncodeToString(deleted.Payload()) != "0107000000" {
		t.Fatalf("rejected/deleted = %s/%s", hex.EncodeToString(rejected.Payload()), hex.EncodeToString(deleted.Payload()))
	}
}
