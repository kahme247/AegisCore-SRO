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

func TestChunkStorageDataUsesStockCompatibleMultipartLimit(t *testing.T) {
	packets := chunkStorageData(make([]byte, storageDataChunkSize*2+1))
	if len(packets) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(packets))
	}
	if len(packets[0].Payload()) != storageDataChunkSize || len(packets[1].Payload()) != storageDataChunkSize || len(packets[2].Payload()) != 1 {
		t.Fatalf("chunk sizes = %d, %d, %d", len(packets[0].Payload()), len(packets[1].Payload()), len(packets[2].Payload()))
	}
	for _, packet := range packets {
		if packet.MessageID != opcode.StorageData {
			t.Fatalf("chunk opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.StorageData)
		}
	}
}

func TestEconomySuccessPacketsMatchVSRO188Layouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		want   string
	}{
		{name: "buy", packet: buildBuySuccess(2, 3, 1, []byte{13}, model.Item{}), want: "01080203010d010000000000"},
		{name: "sell", packet: buildSellSuccess(13, 1, 0x11223344, 1), want: "01090d01004433221101"},
		{name: "buy back", packet: buildBuyBackSuccess(14, 0, 1), want: "01220e000100"},
		{name: "buy back acknowledgement", packet: buildNPCBuyBackResponse(true), want: "01"},
		{name: "storage gold deposit", packet: buildStorageGoldSuccess(DepositStorageGoldOperation, 5, 95), want: "010b5f00000000000000"},
		{name: "storage gold withdrawal", packet: buildStorageGoldSuccess(WithdrawStorageGoldOperation, 5, 105), want: "010c0500000000000000"},
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
	if got, want := hex.EncodeToString(packet.Payload()), "01887766554433221100"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestParseBuyBackRequestUsesNPCThenZeroBasedSoldSlot(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(0x11223344)
	packet.WriteByte(4)
	request, err := parseBuyBackRequest(&packet)
	if err != nil {
		t.Fatalf("parseBuyBackRequest() error = %v", err)
	}
	if request.npcUID != 0x11223344 || request.source != 4 {
		t.Fatalf("request = %+v, want npc 0x11223344 and slot 4", request)
	}
}

func TestParseBuyBackRequestRejectsOutOfRangeSoldSlot(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(0x11223344)
	packet.WriteByte(5)
	if _, err := parseBuyBackRequest(&packet); err == nil {
		t.Fatal("parseBuyBackRequest() accepted slot 5")
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
