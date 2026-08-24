package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestGatewayPatchRequestCodecExactLayout(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(56)
	packet.WriteString("SR_Client")
	packet.WriteUInt32(188)
	request, err := (GatewayPatchRequestCodec{}).Decode(packet)
	if err != nil || request.ContentID != 56 || request.ModuleName != "SR_Client" || request.Version != 188 {
		t.Fatalf("Decode() = %+v, %v", request, err)
	}
	packet.WriteByte(0)
	if _, err := (GatewayPatchRequestCodec{}).Decode(packet); err == nil {
		t.Fatal("Decode() accepted trailing patch data")
	}
}

func TestGatewayEndpointResponseCodecMatchesRecoveredA106(t *testing.T) {
	packet, err := (GatewayEndpointResponseCodec{}).Encode(GatewayEndpointResponse{Profile: 1, FarmID: 7, IPv4: [4]byte{203, 0, 113, 42}})
	if err != nil || !bytes.Equal(packet.Payload(), []byte{1, 7, 203, 0, 113, 42}) {
		t.Fatalf("Encode() = %x, %v", packet.Payload(), err)
	}
}

func TestGatewayLoginRequestCodecRejectsTruncationAndTrailingData(t *testing.T) {
	valid := network.EmptyClientPacket()
	valid.WriteByte(56)
	valid.WriteString("alice")
	valid.WriteString("secret")
	valid.WriteUInt16(1)
	request, err := (GatewayLoginRequestCodec{}).Decode(valid)
	if err != nil || request.Username != "alice" || request.Password != "secret" || request.ShardID != 1 {
		t.Fatalf("Decode() = %+v, %v", request, err)
	}
	truncated := network.EmptyClientPacket()
	truncated.WriteByte(56)
	truncated.WriteString("alice")
	truncated.WriteString("secret")
	truncated.WriteByte(1)
	if _, err := (GatewayLoginRequestCodec{}).Decode(truncated); err == nil {
		t.Fatal("Decode() accepted truncated shard ID")
	}
	valid.WriteByte(0)
	if _, err := (GatewayLoginRequestCodec{}).Decode(valid); err == nil {
		t.Fatal("Decode() accepted trailing login data")
	}
}

func TestGatewayNoticeAndShardListRequestCodecs(t *testing.T) {
	notice := network.EmptyClientPacket()
	notice.WriteByte(56)
	request, err := (GatewayNoticeRequestCodec{}).Decode(notice)
	if err != nil || request.ContentID != 56 {
		t.Fatalf("notice Decode() = %+v, %v", request, err)
	}
	empty := network.EmptyClientPacket()
	if _, err := (GatewayShardListRequestCodec{}).Decode(empty); err != nil {
		t.Fatal(err)
	}
	empty.WriteByte(0)
	if _, err := (GatewayShardListRequestCodec{}).Decode(empty); err == nil {
		t.Fatal("shard-list Decode() accepted payload")
	}
}
