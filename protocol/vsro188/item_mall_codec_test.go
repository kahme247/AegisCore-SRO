package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestItemMallModernRequestMatchesStockFieldOrder(t *testing.T) {
	request := ItemMallModernRequest{
		ShopGroupID:  0x1234,
		GroupIndex:   2,
		TabIndex:     3,
		SlotIndex:    4,
		PackageName:  "PACKAGE_ITEM_MALL_TEST",
		PackageCount: 5,
		OpaqueValue:  0x0102030405060708,
		PackageID:    0xAABBCCDD,
	}
	want := "341202030416005041434b4147455f4954454d5f4d414c4c5f5445535405000807060504030201ddccbbaa"
	built, err := BuildItemMallModernRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(built.Payload()); got != want {
		t.Fatalf("encoded request = %s, want %s", got, want)
	}

	input := network.EmptyClientPacket()
	if err := input.WriteBytes(built.Payload()); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeItemMallModernRequest(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != request {
		t.Fatalf("decoded request = %+v, want %+v", decoded, request)
	}
}

func TestItemMallModernRequestRejectsTrailingBytes(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteUInt16(1051)
	packet.WriteByte(2)
	packet.WriteByte(1)
	packet.WriteByte(13)
	packet.WriteString("PACKAGE_ITEM_MALL_TEST")
	packet.WriteUInt16(1)
	packet.WriteUInt64(0)
	packet.WriteUInt32(40000)
	packet.WriteByte(0xFF)
	if _, err := DecodeItemMallModernRequest(&packet); err == nil {
		t.Fatal("modern item-mall request accepted trailing bytes")
	}
}

func TestBalanceStateUpdateMatchesStock3153Layout(t *testing.T) {
	update := BalanceStateUpdate{First: 1, Second: 2, Third: 3}
	packet := BuildBalanceStateUpdate(update)
	if packet.MessageID != 0x3153 {
		t.Fatalf("balance opcode = 0x%04X, want 0x3153", packet.MessageID)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "010000000200000003000000"; got != want {
		t.Fatalf("balance payload = %s, want %s", got, want)
	}
	decoded, err := DecodeBalanceStateUpdate(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != update {
		t.Fatalf("decoded balance update = %+v, want %+v", decoded, update)
	}
}

func TestCashNotificationMatchesStock3154Layout(t *testing.T) {
	want := CashNotification{Reason: 5, CashType: 2, DisplayedValue: 0x11223344}
	packet := BuildCashNotification(want)
	if packet.MessageID != 0x3154 {
		t.Fatalf("cash notification opcode = 0x%04X, want 0x3154", packet.MessageID)
	}
	if got, expected := hex.EncodeToString(packet.Payload()), "050244332211"; got != expected {
		t.Fatalf("cash notification payload = %s, want %s", got, expected)
	}
	decoded, err := DecodeCashNotification(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != want {
		t.Fatalf("decoded cash notification = %+v, want %+v", decoded, want)
	}
}

func TestItemMallModernPurchaseSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemMallModernPurchaseSuccess(ItemMallModernPurchaseSuccess{
		ShopGroupID:  1051,
		GroupIndex:   2,
		TabIndex:     1,
		SlotIndex:    13,
		ResultBytes:  []byte{14},
		PackageCount: 1,
	})
	if got, want := hex.EncodeToString(packet.Payload()), "01181b0402010d010e010000000000"; got != want {
		t.Fatalf("modern purchase payload = %s, want %s", got, want)
	}
	input := network.EmptyClientPacket()
	input.WriteBytes(packet.Payload())
	decoded, err := DecodeItemMallModernPurchaseSuccess(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ShopGroupID != 1051 || decoded.GroupIndex != 2 || decoded.TabIndex != 1 || decoded.SlotIndex != 13 || len(decoded.ResultBytes) != 1 || decoded.ResultBytes[0] != 14 || decoded.PackageCount != 1 || decoded.TrailingValue != 0 {
		t.Fatalf("decoded modern purchase = %+v", decoded)
	}
}

func TestDecodeItemMallModernPurchaseSuccessRejectsTrailingData(t *testing.T) {
	packet := BuildItemMallModernPurchaseSuccess(ItemMallModernPurchaseSuccess{
		ShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13,
		ResultBytes: []byte{14}, PackageCount: 1,
	})
	packet.WriteByte(0xAA)
	if _, err := DecodeItemMallModernPurchaseSuccess(&packet); err == nil {
		t.Fatal("modern purchase with trailing data decoded successfully")
	}
}

func TestItemMallLegacyPurchaseSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemMallLegacyPurchaseSuccess(ItemMallLegacyPurchaseSuccess{
		Operation:        0x12,
		MallVersion:      3,
		Page:             4,
		GroupIndex:       2,
		TabIndex:         1,
		SlotIndex:        13,
		DestinationSlots: []byte{14, 15},
		Quantity:         0x1234,
	})
	if got, want := hex.EncodeToString(packet.Payload()), "0112030402010d020e0f3412"; got != want {
		t.Fatalf("legacy purchase payload = %s, want %s", got, want)
	}
}

func TestItemOperationFailureMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationFailure(0x1809)
	if got, want := hex.EncodeToString(packet.Payload()), "020918"; got != want {
		t.Fatalf("failure payload = %s, want %s", got, want)
	}
	code, err := DecodeItemOperationFailure(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0x1809 {
		t.Fatalf("failure code = 0x%04X, want 0x1809", code)
	}
}
