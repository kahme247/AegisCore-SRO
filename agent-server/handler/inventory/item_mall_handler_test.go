package inventory

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type itemMallRepositoryFake struct {
	result model.ItemMallPurchaseResult
	err    error
	calls  int
}

func (f *itemMallRepositoryFake) Balances(context.Context, uint32) (model.ItemMallBalances, error) {
	return f.result.Balances, f.err
}

func (f *itemMallRepositoryFake) Purchase(_ context.Context, _ int64, _ uint32, _ model.ItemMallPosition, _ uint16) (model.ItemMallPurchaseResult, error) {
	f.calls++
	return f.result, f.err
}

func TestItemMallPacketsMatchVSRO188Layouts(t *testing.T) {
	balance := buildItemMallBalanceUpdate(model.ItemMallBalances{Silk: 1, GiftSilk: 2, Point: 3})
	if balance.MessageID != opcode.ItemMallBalanceUpdate {
		t.Fatalf("balance opcode = 0x%04X", balance.MessageID)
	}
	if got, want := hex.EncodeToString(balance.Payload()), "010000000200000003000000"; got != want {
		t.Fatalf("balance payload = %s, want %s", got, want)
	}

	purchase := buildItemMallPurchaseSuccess(itemMallPurchaseRequest{operation: BuyItemFromMallOperation, mallVersion: 0x36, page: 1}, model.ItemMallPurchaseResult{
		Position:         model.ItemMallPosition{RefShopGroupID: 0x1234, GroupIndex: 2, TabIndex: 3, SlotIndex: 4},
		DestinationSlots: []byte{13},
		Quantity:         5,
	})
	if got, want := hex.EncodeToString(purchase.Payload()), "01123601020304010d0500"; got != want {
		t.Fatalf("purchase payload = %s, want %s", got, want)
	}
}

func TestModernItemMallResponseEchoesPackageCount(t *testing.T) {
	purchase := buildItemMallPurchaseSuccess(itemMallPurchaseRequest{
		operation:     BuyItemFromModernMallOperation,
		purchaseCount: 1,
	}, model.ItemMallPurchaseResult{
		Position:         model.ItemMallPosition{RefShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13},
		DestinationSlots: []byte{14},
		Quantity:         11,
	})
	if got, want := hex.EncodeToString(purchase.Payload()), "01181b0402010d010e0100"; got != want {
		t.Fatalf("modern purchase payload = %s, want %s", got, want)
	}
}

func TestItemMallPurchaseRejectsTrailingDataBeforeRepository(t *testing.T) {
	mall := &itemMallRepositoryFake{}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{}, nil)
	handler.mall = mall
	packet := itemMallRequestPacket(t, 1, []byte{0xff})

	if err := handler.handlePacket(context.Background(), &server.Session{}, packet); err == nil {
		t.Fatal("item mall request with trailing data succeeded")
	}
	if mall.calls != 0 {
		t.Fatalf("malformed request reached repository %d times", mall.calls)
	}
}

func TestItemMallPurchasePublishesInventoryAndSendsBalanceThenPurchase(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UserID: 9, UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	nextInventory := model.NewInventory(map[byte]model.Item{13: {ID: 100}})
	mall := &itemMallRepositoryFake{result: model.ItemMallPurchaseResult{
		Balances:         model.ItemMallBalances{Silk: 90, GiftSilk: 20, Point: 10},
		Inventory:        nextInventory,
		Position:         model.ItemMallPosition{RefShopGroupID: 1051, SlotIndex: 7},
		DestinationSlots: []byte{13},
		Quantity:         1,
	}}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, nil)
	handler.mall = mall
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}

	if err := handler.handlePacket(context.Background(), session, itemMallRequestPacket(t, 1, nil)); err != nil {
		t.Fatalf("item mall purchase error = %v", err)
	}
	if mall.calls != 1 {
		t.Fatalf("purchase calls = %d, want 1", mall.calls)
	}
	if got := player.Inventory.Snapshot().Items[13].ID; got != 100 {
		t.Fatalf("runtime inventory item = %d, want 100", got)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.ItemMallBalanceUpdate || sent[1].MessageID != opcode.ItemOperationResponse {
		t.Fatalf("sent packets = %#v, want balance then purchase response", sent)
	}
}

func TestModernItemMallPurchaseSendsOnlyConfirmedOperationCompletion(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UserID: 9, UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	mall := &itemMallRepositoryFake{result: model.ItemMallPurchaseResult{
		Balances:         model.ItemMallBalances{Silk: 90, GiftSilk: 20, Point: 10},
		Inventory:        model.NewInventory(map[byte]model.Item{13: {ID: 100}}),
		Position:         model.ItemMallPosition{RefShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13},
		DestinationSlots: []byte{13},
		Quantity:         1,
	}}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, nil)
	handler.mall = mall
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}

	body, err := vsro188.BuildItemMallModernRequest(vsro188.ItemMallModernRequest{
		ShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13,
		PackageName: "PACKAGE_ITEM_MALL_TEST", PackageCount: 1, PackageID: 40000,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet := network.EmptyClientPacket()
	packet.WriteByte(byte(BuyItemFromModernMallOperation))
	packet.WriteBytes(body.Payload())
	if err = handler.handlePacket(context.Background(), session, packet); err != nil {
		t.Fatalf("modern mall purchase error = %v", err)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.ItemOperationResponse {
		t.Fatalf("modern mall packets = %#v, want one 0xB034 completion", sent)
	}
}

func itemMallRequestPacket(t *testing.T, purchaseCount uint16, trailing []byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	mustWrite := func(err error) {
		if err != nil {
			t.Fatalf("build item mall request: %v", err)
		}
	}
	mustWrite(packet.WriteByte(byte(BuyItemFromMallOperation)))
	mustWrite(packet.WriteByte(0x36))
	mustWrite(packet.WriteByte(1))
	mustWrite(packet.WriteByte(0))
	mustWrite(packet.WriteByte(0))
	mustWrite(packet.WriteByte(7))
	mustWrite(packet.WriteString("PACKAGE_ITEM_MALL_TEST"))
	mustWrite(packet.WriteUInt16(purchaseCount))
	mustWrite(packet.WriteUInt32(999))
	mustWrite(packet.WriteUInt32(888))
	mustWrite(packet.WriteUInt32(40000))
	mustWrite(packet.WriteBytes(trailing))
	return packet
}
