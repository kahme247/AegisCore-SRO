package inventory

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestBuildEmptyStorageResponsesUsesVSROChunkSequence(t *testing.T) {
	packets := buildEmptyStorageResponses()
	if len(packets) != 3 {
		t.Fatalf("response count = %d, want 3", len(packets))
	}
	if packets[0].MessageID != opcode.StorageDataBegin || !reflect.DeepEqual(packets[0].Payload(), make([]byte, 8)) {
		t.Fatalf("begin = opcode 0x%04X payload %x", packets[0].MessageID, packets[0].Payload())
	}
	if packets[1].MessageID != opcode.StorageData || !reflect.DeepEqual(packets[1].Payload(), []byte{baseStorageSize, 0}) {
		t.Fatalf("data = opcode 0x%04X payload %x", packets[1].MessageID, packets[1].Payload())
	}
	if packets[2].MessageID != opcode.StorageDataEnd || len(packets[2].Payload()) != 0 {
		t.Fatalf("end = opcode 0x%04X payload %x", packets[2].MessageID, packets[2].Payload())
	}
}

func TestEconomySuccessPacketsMatchVSRO188Layouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		want   string
	}{
		{name: "buy", packet: buildBuySuccess(2, 3, 1, []byte{13}, model.Item{}), want: "01080203010d010000000000"},
		{name: "sell", packet: buildSellSuccess(13, 1, 0x11223344, 0xFF), want: "01090d010044332211ff"},
		{name: "deposit", packet: buildStorageTransferSuccess(PutItemIntoStorageOperation, 13, 0), want: "01020d00"},
		{name: "withdraw", packet: buildStorageTransferSuccess(TakeItemFromStorageOperation, 0, 13), want: "0103000d"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hex.EncodeToString(test.packet.Payload()); got != test.want {
				t.Fatalf("payload = %s, want %s", got, test.want)
			}
		})
	}
}

func TestBuildGoldUpdateUsesCurrentBalance(t *testing.T) {
	packet := buildGoldUpdate(0x1122334455667788)
	if packet.MessageID != opcode.EntityUpdatePoints {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.EntityUpdatePoints)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "018877665544332211"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestUnsupportedItemOperationResponseCompletesPendingClientAction(t *testing.T) {
	packet := buildUnsupportedItemOperationResponse()
	if packet.MessageID != opcode.ItemOperationResponse {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.ItemOperationResponse)
	}
	if got, want := packet.Payload(), []byte{2, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %x, want %x", got, want)
	}
}
