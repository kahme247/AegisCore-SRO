package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestInventoryItemUpdateGoldenMatchesClient3040ReadOrder(t *testing.T) {
	u32a, u32b := uint32(0x11223344), uint32(0x8899AABB)
	u8a, u8b := byte(0x55), byte(0xCC)
	u16 := uint16(0x6677)
	u64 := uint64(0x0102030405060708)
	fixture := "0d" + "ff" + "44332211" + "55" + "0807060504030201" + "7766" + "bbaa9988" + "02" + "1111111111111111" + "2222222222222222" + "cc" + "00ffeedd"
	wire, err := hex.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}

	packet := network.EmptyClientPacket()
	if err := packet.WriteBytes(wire); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInventoryItemUpdate(packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ItemKind != 0x0D || decoded.Flags != 0xFF ||
		decoded.Flag01U32 == nil || *decoded.Flag01U32 != u32a ||
		decoded.Flag02U8 == nil || *decoded.Flag02U8 != u8a ||
		decoded.Flag04U64 == nil || *decoded.Flag04U64 != u64 ||
		decoded.Flag08U16 == nil || *decoded.Flag08U16 != u16 ||
		decoded.Flag10U32 == nil || *decoded.Flag10U32 != u32b ||
		len(decoded.MagicOptions) != 2 || decoded.MagicOptions[0] != 0x1111111111111111 || decoded.MagicOptions[1] != 0x2222222222222222 ||
		decoded.Flag40U8 == nil || *decoded.Flag40U8 != u8b ||
		decoded.Flag80U32 == nil || *decoded.Flag80U32 != 0xDDEEFF00 {
		t.Fatalf("decoded 0x3040 = %+v", decoded)
	}

	built, err := BuildInventoryItemUpdate(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(built.Payload()); got != fixture {
		t.Fatalf("encoded 0x3040 = %s, want %s", got, fixture)
	}
}

func TestInventoryItemUpdateAllowsEquippedTimerField(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(0x0C)
	packet.WriteByte(0xFF)
	packet.WriteUInt32(1)
	packet.WriteByte(2)
	packet.WriteUInt64(3)
	packet.WriteUInt16(4)
	packet.WriteUInt32(5)
	packet.WriteByte(0)
	packet.WriteUInt32(0)
	decoded, err := DecodeInventoryItemUpdate(packet)
	if err != nil || decoded.Flag40U8 != nil || decoded.Flag80U32 == nil || *decoded.Flag80U32 != 0 {
		t.Fatalf("equipped timer decode = %+v, %v", decoded, err)
	}
	if _, err := BuildInventoryItemUpdate(decoded); err != nil {
		t.Fatal(err)
	}

	packet.WriteByte(0)
	if _, err := DecodeInventoryItemUpdate(packet); err == nil {
		t.Fatal("0x3040 accepted trailing bytes")
	}
}

func TestInventoryItemUpdateRejectsMagicOptionOverflow(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(0x0D)
	packet.WriteByte(0x20)
	packet.WriteByte(13)
	if _, err := DecodeInventoryItemUpdate(packet); err == nil {
		t.Fatal("0x3040 accepted magic-option count above client maximum")
	}
}
