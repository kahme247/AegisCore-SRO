package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCommunityBlockLayoutsMatchClient(t *testing.T) {
	request := network.EmptyClientPacket()
	request.WriteString("Aegis")
	request.WriteByte(CommunityBlockAdd)
	decoded, err := DecodeCommunityBlockRequest(&request)
	if err != nil || decoded != (CommunityBlockRequest{TargetName: "Aegis", Action: CommunityBlockAdd}) {
		t.Fatalf("request = %+v, %v", decoded, err)
	}
	success, err := BuildCommunityBlockResult(CommunityBlockAdd, 0, "Aegis")
	if err != nil || hex.EncodeToString(success.Payload()) != "010005004165676973" {
		t.Fatalf("success = %s, %v", hex.EncodeToString(success.Payload()), err)
	}
	failure, err := BuildCommunityBlockResult(CommunityBlockRemove, 4, "")
	if err != nil || hex.EncodeToString(failure.Payload()) != "0204" {
		t.Fatalf("failure = %s, %v", hex.EncodeToString(failure.Payload()), err)
	}
}
