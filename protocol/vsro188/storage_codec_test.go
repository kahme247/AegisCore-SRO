package vsro188

import (
	"bytes"
	"testing"
)

func TestStorageDataFramingMatchesStockLayouts(t *testing.T) {
	begin := BuildStorageDataBegin(0x3047, 0x1122334455667788)
	if got, want := begin.Payload(), []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("storage begin payload = %X, want %X", got, want)
	}
	data := BuildStorageDataChunk(0x3049, []byte{1, 2, 3})
	if got := data.Payload(); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("storage data payload = %X", got)
	}
	end := BuildStorageDataEnd(0x3048)
	if got := end.Payload(); len(got) != 0 {
		t.Fatalf("storage end payload = %X, want empty", got)
	}
}

func TestStorageItemDataPayloadMatchesFixedRecordEnvelope(t *testing.T) {
	packet := BuildStorageItemDataPayload(150, []StorageItemRecord{
		{Slot: 13, RentPayload: []byte{1, 2}, RefItemID: 0x11223344, ItemPayload: []byte{0xAA}},
		{Slot: 14, RentPayload: []byte{0}, RefItemID: 0x55667788, ItemPayload: []byte{0xBB, 0xCC}},
	})
	want := []byte{150, 2, 13, 1, 2, 0x44, 0x33, 0x22, 0x11, 0xAA, 14, 0, 0x88, 0x77, 0x66, 0x55, 0xBB, 0xCC}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("storage item payload = % X, want % X", got, want)
	}
}

func TestGuildStorageControlResponsesMatchMaintainedLayouts(t *testing.T) {
	openSuccess := BuildGuildStorageOpenSuccess()
	if got, want := openSuccess.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("guild open success = % X, want % X", got, want)
	}
	openFailure := BuildGuildStorageOpenFailure(7)
	if got, want := openFailure.Payload(), []byte{2, 7, 0x4C}; !bytes.Equal(got, want) {
		t.Fatalf("guild open failure = % X, want % X", got, want)
	}
	closeSuccess := BuildGuildStorageCloseSuccess()
	if got, want := closeSuccess.Payload(), []byte{1}; !bytes.Equal(got, want) {
		t.Fatalf("guild close success = % X, want % X", got, want)
	}
}

func TestChunkStorageDataUsesStockFourKiBBoundaryForBothProfiles(t *testing.T) {
	input := make([]byte, StorageDataChunkSize*2+1)
	for index := range input {
		input[index] = byte(index)
	}
	packets := ChunkStorageData(0x3255, input)
	if len(packets) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(packets))
	}
	if len(packets[0].Payload()) != StorageDataChunkSize || len(packets[1].Payload()) != StorageDataChunkSize || len(packets[2].Payload()) != 1 {
		t.Fatalf("chunk sizes = %d, %d, %d", len(packets[0].Payload()), len(packets[1].Payload()), len(packets[2].Payload()))
	}
	if !bytes.Equal(packets[0].Payload(), input[:StorageDataChunkSize]) || !bytes.Equal(packets[2].Payload(), input[len(input)-1:]) {
		t.Fatal("storage chunker changed payload order")
	}
	for _, packet := range packets {
		if packet.MessageID != 0x3255 {
			t.Fatalf("guild data opcode = 0x%04X, want 0x3255", packet.MessageID)
		}
	}
}
