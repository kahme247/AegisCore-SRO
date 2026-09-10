package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestAlchemyStagedRequestMatches7150And7151Prefix(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(2)
	packet.WriteByte(3)
	packet.WriteByte(3)
	packet.WriteByte(13)
	packet.WriteByte(14)
	packet.WriteByte(15)
	request, err := DecodeAlchemyStagedRequest(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != 2 || request.Type != 3 || len(request.Slots) != 3 || request.Slots[0] != 13 || request.Slots[2] != 15 {
		t.Fatalf("decoded staged request = %+v", request)
	}
}

func TestAlchemyStableResponsePrefixes(t *testing.T) {
	cancel := BuildAlchemyCancel(opcode.AlchemyElixirResponse)
	if got, want := hex.EncodeToString(cancel.Payload()), "0101"; got != want {
		t.Fatalf("cancel = %s, want %s", got, want)
	}
	failure := BuildAlchemyFailure(opcode.AlchemyStoneResponse, 0x5423)
	if got, want := hex.EncodeToString(failure.Payload()), "022354"; got != want {
		t.Fatalf("failure = %s, want %s", got, want)
	}
	material := BuildAlchemyMaterialResult(2, 0x11223344)
	if got, want := hex.EncodeToString(material.Payload()), "01020244332211"; got != want {
		t.Fatalf("material = %s, want %s", got, want)
	}
	dismantle := BuildAlchemyDismantleSuccess()
	if got, want := hex.EncodeToString(dismantle.Payload()), "01"; got != want {
		t.Fatalf("dismantle = %s, want %s", got, want)
	}
}

func TestAlchemyResultEnvelopesStayInProtocolCodec(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		want   string
	}{
		{name: "elixir success", packet: BuildAlchemyElixirResult(true, 13, false, AlchemyTargetPayload{0xAA, 0xBB}), want: "0102010daabb"},
		{name: "elixir non-destructive failure", packet: BuildAlchemyElixirResult(false, 13, false, AlchemyTargetPayload{0xAA, 0xBB}), want: "0102000d00aabb"},
		{name: "elixir destroyed", packet: BuildAlchemyElixirResult(false, 13, true, AlchemyTargetPayload{0xAA, 0xBB}), want: "0102000d01"},
		{name: "stone", packet: BuildAlchemyStoneResult(true, 14, AlchemyTargetPayload{0xCC}), want: "0102010ecc"},
		{name: "socket action", packet: BuildAlchemySocketActionResult(4, 15, AlchemyTargetPayload{0xDD}), want: "01040fdd"},
		{name: "socket short result", packet: BuildAlchemySocketResult(), want: "0102"},
		{name: "socket failure", packet: BuildAlchemySocketFailure(0x1234), want: "02023412"},
		{name: "avatar", packet: BuildAlchemyAvatarResult(true), want: "010201"},
		{name: "avatar grant", packet: BuildAlchemyAvatarGrantResult(false, 0, nil), want: "0100"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hex.EncodeToString(test.packet.Payload()); got != test.want {
				t.Fatalf("payload = %s, want %s", got, test.want)
			}
		})
	}
}
