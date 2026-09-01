package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestRepairResponseMatchesMaintainedB03ELayouts(t *testing.T) {
	success := BuildRepairSuccess()
	if got, want := success.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("repair success payload = %X, want %X", got, want)
	}
	failure := BuildRepairFailure(0x183E)
	if got, want := failure.Payload(), []byte{2, 0x3E, 0x18}; !bytes.Equal(got, want) {
		t.Fatalf("repair failure payload = %X, want %X", got, want)
	}

	for _, packet := range []*network.Packet{&success, &failure} {
		decoded, err := DecodeRepairResponse(packet)
		if err != nil {
			t.Fatal(err)
		}
		if packet == &success && !decoded.Success {
			t.Fatalf("decoded success = %+v", decoded)
		}
		if packet == &failure && decoded.Success || packet == &failure && decoded.Code != 0x183E {
			t.Fatalf("decoded failure = %+v", decoded)
		}
	}
}

func TestDecodeRepairResponseRejectsMalformedPayload(t *testing.T) {
	for _, payload := range [][]byte{{}, {1, 0}, {2, 0x3E}, {2, 0x3E, 0x18, 0xFF}, {3}} {
		packet := network.EmptyClientPacket()
		packet.WriteBytes(payload)
		if _, err := DecodeRepairResponse(&packet); err == nil {
			t.Fatalf("0xB03E accepted payload %X", payload)
		}
	}
}
