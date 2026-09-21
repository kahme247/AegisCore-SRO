package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestItemOperationPickupSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationSlotSuccess(ItemOperationPickup, 0x0D)
	if got, want := packet.Payload(), []byte{1, 0x06, 0x0D}; !bytes.Equal(got, want) {
		t.Fatalf("pickup success payload = % X, want % X", got, want)
	}
	if packet.MessageID != 0xB034 {
		t.Fatalf("pickup success opcode = 0x%04X, want 0xB034", packet.MessageID)
	}

	input := network.EmptyClientPacket()
	input.WriteBytes(packet.Payload())
	slot, err := DecodeItemOperationSlotSuccess(&input, ItemOperationPickup)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 0x0D {
		t.Fatalf("decoded pickup slot = 0x%02X, want 0x0D", slot)
	}
}

func TestItemOperationServerAddSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationTwoByteSuccess(ItemOperationServerAdd, 0x0D, 0x3C)
	if got, want := packet.Payload(), []byte{1, 0x0E, 0x0D, 0x3C}; !bytes.Equal(got, want) {
		t.Fatalf("server-add success payload = % X, want % X", got, want)
	}

	input := network.EmptyClientPacket()
	input.WriteBytes(packet.Payload())
	first, second, err := DecodeItemOperationTwoByteSuccess(&input, ItemOperationServerAdd)
	if err != nil {
		t.Fatal(err)
	}
	if first != 0x0D || second != 0x3C {
		t.Fatalf("decoded server-add bytes = (0x%02X, 0x%02X), want (0x0D, 0x3C)", first, second)
	}
}

func TestDecodeItemOperationTwoByteSuccessRejectsTrailingItemBody(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteBytes([]byte{1, ItemOperationServerAdd, 0x0D, 0x3C, 0xAA})
	if _, _, err := DecodeItemOperationTwoByteSuccess(&packet, ItemOperationServerAdd); err == nil {
		t.Fatal("two-byte success decoder accepted a trailing item body")
	}
}

func TestDecodeItemOperationSlotSuccessRejectsTrailingItemBody(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(1)
	packet.WriteByte(ItemOperationPickup)
	packet.WriteByte(0x0D)
	packet.WriteByte(0x3C)
	if _, err := DecodeItemOperationSlotSuccess(&packet, ItemOperationPickup); err == nil {
		t.Fatal("slot success decoder accepted a trailing item body")
	}
}

func TestItemOperationCOSTransferSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationObjectSlotSuccess(ItemOperationCOSTransfer, 0x11223344, 0x1A)
	if got, want := packet.Payload(), []byte{1, 0x17, 0x44, 0x33, 0x22, 0x11, 0x1A}; !bytes.Equal(got, want) {
		t.Fatalf("COS transfer success payload = % X, want % X", got, want)
	}
}

func TestItemOperationPlayerCOSTransferSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationObjectSlotPairSuccess(0x1A, 0x11223344, 0x03, 0x0E)
	if got, want := packet.Payload(), []byte{1, 0x1A, 0x44, 0x33, 0x22, 0x11, 0x03, 0x0E}; !bytes.Equal(got, want) {
		t.Fatalf("player/COS transfer success payload = % X, want % X", got, want)
	}
}

func TestItemOperationCOSMoveSuccessMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationObjectSlotPairQuantitySuccess(0x10, 0x11223344, 0x03, 0x04, 0x1234)
	want := []byte{1, 0x10, 0x44, 0x33, 0x22, 0x11, 0x03, 0x04, 0x34, 0x12}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("COS move success payload = % X, want % X", got, want)
	}
}

func TestItemOperationStorageSuccessFormsMatchMaintainedB034Layouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		want   []byte
	}{
		{name: "slot pair", packet: BuildItemOperationSlotPairSuccess(0x02, 0x0D, 0x00), want: []byte{1, 0x02, 0x0D, 0x00}},
		{name: "slot pair quantity", packet: BuildItemOperationSlotPairQuantitySuccess(0x01, 0x0D, 0x00, 0x1234), want: []byte{1, 0x01, 0x0D, 0x00, 0x34, 0x12}},
		{name: "u64", packet: BuildItemOperationUInt64Success(0x0B, 0x1122334455667788), want: []byte{1, 0x0B, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.packet.Payload(); !bytes.Equal(got, test.want) {
				t.Fatalf("payload = % X, want % X", got, test.want)
			}
		})
	}
}

func TestItemOperationServerDeleteMatchesMaintainedB034Layout(t *testing.T) {
	packet := BuildItemOperationServerDeleteSuccess(0x2A)
	if got, want := packet.Payload(), []byte{1, 0x0F, 0x2A, 0}; !bytes.Equal(got, want) {
		t.Fatalf("server-delete payload = % X, want % X", got, want)
	}
}

func TestItemOperationRelocationMatchesStockB034Layout(t *testing.T) {
	packet := BuildItemOperationRelocationSuccess(0x0D, 0x0E, 0x1234)
	if got, want := packet.Payload(), []byte{1, 0x00, 0x0D, 0x0E, 0x34, 0x12, 0}; !bytes.Equal(got, want) {
		t.Fatalf("relocation payload = % X, want % X", got, want)
	}
}

func TestItemOperationAvatarTransferMatchesMaintainedB034Layout(t *testing.T) {
	packet := BuildItemOperationAvatarTransferSuccess(0x24, 35, 4)
	if got, want := packet.Payload(), []byte{1, 0x24, 35, 4, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("avatar transfer payload = % X, want % X", got, want)
	}
}

func TestGroundDropRequestAndResponseMatchStockLayouts(t *testing.T) {
	request := BuildGroundDropRequest(GroundDropRequest{Slot: 0x0D})
	if got, want := request.Payload(), []byte{0x07, 0x0D}; !bytes.Equal(got, want) {
		t.Fatalf("ground-drop request = % X, want % X", got, want)
	}
	decoded, err := DecodeGroundDropRequest(&request)
	if err != nil || decoded.Slot != 0x0D {
		t.Fatalf("decoded ground-drop request = %+v, %v", decoded, err)
	}
	response := BuildGroundDropSuccess(decoded.Slot)
	if got, want := response.Payload(), []byte{1, 0x07, 0x0D}; !bytes.Equal(got, want) {
		t.Fatalf("ground-drop response = % X, want % X", got, want)
	}
}

func TestDecodeGroundDropRequestRejectsTrailingAndWrongOperation(t *testing.T) {
	for _, payload := range [][]byte{{0x07, 0x0D, 0xFF}, {0x06, 0x0D}} {
		packet := network.EmptyClientPacket()
		packet.WriteBytes(payload)
		if _, err := DecodeGroundDropRequest(&packet); err == nil {
			t.Fatalf("ground-drop decoder accepted %X", payload)
		}
	}
}

func TestPackageItemRequestMatchesRecoveredOverloadedZeroEightLayout(t *testing.T) {
	packet := BuildPackageItemRequest(PackageItemRequest{
		SelectorA: 0x12,
		SelectorB: 0x34,
		Quantity:  0x5678,
	})
	if got, want := packet.Payload(), []byte{ItemOperationPackageItem, 0x12, 0x34, 0x78, 0x56}; !bytes.Equal(got, want) {
		t.Fatalf("package-item request = % X, want % X", got, want)
	}
	decoded, err := DecodePackageItemRequest(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SelectorA != 0x12 || decoded.SelectorB != 0x34 || decoded.Quantity != 0x5678 {
		t.Fatalf("decoded package-item request = %+v", decoded)
	}
}

func TestMagicCubeRequestsMatchStock7034Parser(t *testing.T) {
	push, err := BuildMagicCubeMoveRequest(ItemOperationMagicCubePush, MagicCubeMoveRequest{Source: 0x0D, Destination: 2, Amount: 0x1234})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := push.Payload(), []byte{0x27, 0x0D, 2, 0x34, 0x12}; !bytes.Equal(got, want) {
		t.Fatalf("cube push = % X, want % X", got, want)
	}
	operation, decoded, err := DecodeMagicCubeMoveRequest(&push)
	if err != nil || operation != 0x27 || decoded.Source != 0x0D || decoded.Destination != 2 || decoded.Amount != 0x1234 {
		t.Fatalf("decoded cube push = 0x%02X %+v, %v", operation, decoded, err)
	}

	pop, err := BuildMagicCubeMoveRequest(ItemOperationMagicCubePop, MagicCubeMoveRequest{Context: 0x11223344, Source: 2, Destination: 0x0E, Amount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pop.Payload(), []byte{0x28, 0x44, 0x33, 0x22, 0x11, 2, 0x0E, 3, 0}; !bytes.Equal(got, want) {
		t.Fatalf("cube pop = % X, want % X", got, want)
	}
	operation, decoded, err = DecodeMagicCubeMoveRequest(&pop)
	if err != nil || operation != 0x28 || decoded.Context != 0x11223344 || decoded.Source != 2 || decoded.Destination != 0x0E || decoded.Amount != 3 {
		t.Fatalf("decoded cube pop = 0x%02X %+v, %v", operation, decoded, err)
	}

}

func TestMagicCubeRequestDecodersRejectWrongSizes(t *testing.T) {
	for _, payload := range [][]byte{{0x27, 1, 2, 3}, {0x28, 1, 2, 3, 4, 5, 6, 7}, {0x29}} {
		packet := network.EmptyClientPacket()
		packet.WriteBytes(payload)
		_, _, err := DecodeMagicCubeMoveRequest(&packet)
		if err == nil {
			t.Fatalf("magic-cube decoder accepted % X", payload)
		}
	}
}

func TestMagicCubeSuccessBodiesMatchStockB034Decoder(t *testing.T) {
	tests := []struct {
		name    string
		success MagicCubeSuccess
		want    []byte
	}{
		{name: "push", success: MagicCubeSuccess{Operation: 0x27, Source: 0x0D, Destination: 2, Amount: 0x1234}, want: []byte{1, 0x27, 0x0D, 2, 0x34, 0x12}},
		{name: "pop", success: MagicCubeSuccess{Operation: 0x28, Context: 0x11223344, Source: 2, Destination: 0x0E, Amount: 3}, want: []byte{1, 0x28, 0x44, 0x33, 0x22, 0x11, 2, 0x0E, 3, 0}},
		{name: "delete", success: MagicCubeSuccess{Operation: 0x29, DeleteScalar: 7, Context: 0x11223344, Source: 2, Destination: 0x0E, Amount: 3}, want: []byte{1, 0x29, 7, 0x44, 0x33, 0x22, 0x11, 2, 0x0E, 3, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet, err := BuildMagicCubeSuccess(test.success)
			if err != nil {
				t.Fatal(err)
			}
			if got := packet.Payload(); !bytes.Equal(got, test.want) {
				t.Fatalf("magic-cube success = % X, want % X", got, test.want)
			}
		})
	}
}

func TestPackageItemRequestRejectsTrailingBytes(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteBytes([]byte{ItemOperationPackageItem, 0x12, 0x34, 0x78, 0x56, 0xAA})
	if _, err := DecodePackageItemRequest(&packet); err == nil {
		t.Fatal("package-item decoder accepted trailing bytes")
	}
}

func TestPackageItemSuccessMatchesRecoveredB034Layout(t *testing.T) {
	packet := BuildPackageItemSuccess(PackageItemSuccess{
		SelectorA:   0x12,
		SelectorB:   0x34,
		ResultBytes: []byte{0x56, 0x78},
		Quantity:    0x9ABC,
	})
	if got, want := packet.Payload(), []byte{1, ItemOperationPackageItem, 0x12, 0x34, 2, 0x56, 0x78, 0xBC, 0x9A}; !bytes.Equal(got, want) {
		t.Fatalf("package-item success = % X, want % X", got, want)
	}
	input := network.EmptyClientPacket()
	input.WriteBytes(packet.Payload())
	decoded, err := DecodePackageItemSuccess(&input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SelectorA != 0x12 || decoded.SelectorB != 0x34 || !bytes.Equal(decoded.ResultBytes, []byte{0x56, 0x78}) || decoded.Quantity != 0x9ABC {
		t.Fatalf("decoded package-item success = %+v", decoded)
	}
}

func TestPackageItemSuccessRejectsRentTail(t *testing.T) {
	packet := BuildPackageItemSuccess(PackageItemSuccess{SelectorA: 1, SelectorB: 2, ResultBytes: []byte{3}, Quantity: 4})
	packet.WriteBytes([]byte{0, 0, 0, 0})
	if _, err := DecodePackageItemSuccess(&packet); err == nil {
		t.Fatal("package-item success accepted a trailing rent/item body")
	}
}

func TestExtendedPackageItemSuccessMatchesRecoveredContinuationVector(t *testing.T) {
	packet := BuildPackageItemSuccess(PackageItemSuccess{
		Operation:   ItemOperationPackageItemExtended,
		SelectorA:   0x12,
		SelectorB:   0x34,
		ResultBytes: []byte{0x56, 0x78},
		Quantity:    0x9ABC,
		Continuations: []PackageItemContinuation{
			{Selector: 0x0D, Quantity: 3},
			{Selector: 0x0E, Quantity: 0x1234},
		},
	})
	want := []byte{1, 0x2B, 0x12, 0x34, 2, 0x56, 0x78, 0xBC, 0x9A, 2, 0x0D, 3, 0, 0x0E, 0x34, 0x12}
	if got := packet.Payload(); !bytes.Equal(got, want) {
		t.Fatalf("0x2B success payload = % X, want % X", got, want)
	}
	input := packet
	decoded, err := DecodePackageItemSuccess(&input)
	if err != nil {
		t.Fatalf("DecodePackageItemSuccess(0x2B): %v", err)
	}
	if decoded.Operation != ItemOperationPackageItemExtended || decoded.SelectorA != 0x12 || decoded.SelectorB != 0x34 || decoded.Quantity != 0x9ABC || !bytes.Equal(decoded.ResultBytes, []byte{0x56, 0x78}) || len(decoded.Continuations) != 2 || decoded.Continuations[0].Selector != 0x0D || decoded.Continuations[0].Quantity != 3 || decoded.Continuations[1].Selector != 0x0E || decoded.Continuations[1].Quantity != 0x1234 {
		t.Fatalf("decoded 0x2B success = %+v", decoded)
	}
}

func TestExtendedPackageItemSuccessRejectsTruncatedContinuation(t *testing.T) {
	packet := network.EmptyPacket()
	packet.WriteBytes([]byte{1, 0x2B, 1, 2, 0, 4, 0, 1, 3, 0xAA})
	if _, err := DecodePackageItemSuccess(&packet); err == nil {
		t.Fatal("DecodePackageItemSuccess accepted truncated 0x2B continuation")
	}
}
