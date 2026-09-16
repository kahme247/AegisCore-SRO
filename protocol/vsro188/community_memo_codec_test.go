package vsro188

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCommunityMemoRequestLayoutsMatchClientBuilders(t *testing.T) {
	send := network.EmptyClientPacket()
	send.WriteString("Aegis")
	send.WriteString("Hello")
	decoded, err := DecodeCommunityMemoSendRequest(&send)
	if err != nil || decoded != (CommunityMemoSendRequest{TargetName: "Aegis", Message: "Hello"}) {
		t.Fatalf("send = %+v, %v", decoded, err)
	}
	group := network.EmptyClientPacket()
	group.WriteByte(2)
	group.WriteByte(2)
	group.WriteString("A")
	group.WriteString("B")
	group.WriteString("Hello")
	decodedGroup, err := DecodeCommunityMemoGroupSendRequest(&group)
	if err != nil || decodedGroup.Mode != 2 || len(decodedGroup.Recipients) != 2 || decodedGroup.Message != "Hello" {
		t.Fatalf("group = %+v, %v", decodedGroup, err)
	}
}

func TestCommunityMemoResponseLayoutsMatchClientParsers(t *testing.T) {
	at := time.Unix(0x11223344, 0).UTC()
	list, err := BuildCommunityMemoList([]CommunityMemoListEntry{{SenderName: "Aegis", RefObjectID: 1907, Timestamp: at, Status: 0}}, 0)
	if err != nil || hex.EncodeToString(list.Payload()) != "010105004165676973730700004433221100" {
		t.Fatalf("list = %s, %v", hex.EncodeToString(list.Payload()), err)
	}
	opened, err := BuildCommunityMemoOpen(3, "Hello", 0)
	if err != nil || hex.EncodeToString(opened.Payload()) != "0103050048656c6c6f" {
		t.Fatalf("open = %s, %v", hex.EncodeToString(opened.Payload()), err)
	}
	deleted := BuildCommunityMemoDeleteResult(3, 0)
	group, err := BuildCommunityMemoGroupResult([]CommunityMemoGroupResult{{Recipient: "Aegis", Code: 0}}, 0)
	if err != nil || hex.EncodeToString(deleted.Payload()) != "0103" || hex.EncodeToString(group.Payload()) != "0101050041656769730000" {
		t.Fatalf("delete/group = %s/%s, %v", hex.EncodeToString(deleted.Payload()), hex.EncodeToString(group.Payload()), err)
	}
	notification, err := BuildCommunityMemoNotification(CommunityMemoListEntry{SenderName: "Aegis", RefObjectID: 1907, Timestamp: at})
	if err != nil || hex.EncodeToString(notification.Payload()) != "08050041656769737307000044332211" {
		t.Fatalf("notification = %s, %v", hex.EncodeToString(notification.Payload()), err)
	}
}
