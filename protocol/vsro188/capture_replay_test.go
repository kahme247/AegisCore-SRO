package vsro188

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/captures"
)

func TestLogicalCaptureReplayExercisesModernMallAndItemUseCodecs(t *testing.T) {
	mallBody, err := BuildItemMallModernRequest(ItemMallModernRequest{
		ShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13,
		PackageName: "PACKAGE_ITEM_MALL_TEST", PackageCount: 1,
		OpaqueValue: 0x0102030405060708, PackageID: 40000,
	})
	if err != nil {
		t.Fatal(err)
	}
	mall := network.EmptyClientPacket()
	mall.WriteByte(ItemMallModernOperation)
	mall.WriteBytes(mallBody.Payload())
	mallResponse := BuildItemMallModernPurchaseSuccess(ItemMallModernPurchaseSuccess{
		ShopGroupID: 1051, GroupIndex: 2, TabIndex: 1, SlotIndex: 13,
		ResultBytes: []byte{14}, PackageCount: 1,
	})

	itemUse, err := BuildItemUseRequest(ItemUseRequest{
		InventorySlot: 14, ItemType: 0x19EC, HasOption: true, Option: 7, MapID: 0x11223344,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := captures.Fixture{
		SchemaVersion:  captures.CurrentSchemaVersion,
		FixtureID:      "synthetic-modern-mall-item-use",
		Behavior:       "request codec replay",
		EvidenceStatus: "synthetic",
		Client: captures.ClientIdentity{
			ExecutableName: "synthetic-client.exe", SHA256: strings.Repeat("1", 64),
			FileVersion: "test", Locale: "en", ContentID: "synthetic",
		},
		Data: captures.ArchiveIdentity{
			Path: "synthetic/Data.pk2", Size: 1, SHA256: strings.Repeat("2", 64),
			Format: "synthetic", Profile: "vsro188",
		},
		Connection:                 captures.ConnectionIdentity{Branch: "agent", TargetService: "item-lifecycle"},
		Representation:             "logical-payload",
		PrivateSourceCaptureSHA256: strings.Repeat("3", 64),
		PublicFixtureSHA256:        strings.Repeat("4", 64),
		Sanitizer:                  "synthetic-test",
		SanitizedFields:            []string{"none"},
		Reconstruction:             captures.Reconstruction{Details: "synthetic logical payloads"},
		Frames: []captures.Frame{
			{Index: 0, Direction: "client->server", Opcode: 0x7034, LogicalLength: len(mall.Payload()), PayloadHex: hex.EncodeToString(mall.Payload()), ExpectedPayloadHex: hex.EncodeToString(mall.Payload()), EncryptionState: "none", EDCState: "none", MassiveRole: "none"},
			{Index: 1, Direction: "client->server", Opcode: 0x704C, LogicalLength: len(itemUse.Payload()), PayloadHex: hex.EncodeToString(itemUse.Payload()), ExpectedPayloadHex: hex.EncodeToString(itemUse.Payload()), EncryptionState: "none", EDCState: "none", MassiveRole: "none"},
			{Index: 2, Direction: "server->client", Opcode: 0xB034, LogicalLength: len(mallResponse.Payload()), PayloadHex: hex.EncodeToString(mallResponse.Payload()), ExpectedPayloadHex: hex.EncodeToString(mallResponse.Payload()), EncryptionState: "none", EDCState: "none", MassiveRole: "none"},
		},
		Expected:          []captures.ExpectedOutcome{{StateTransition: "typed request decode and re-encode", Rejected: false}},
		ExternalSources:   []string{"synthetic unit test"},
		ReviewerConfirmed: true,
	}
	if err := captures.Replay(fixture, func(frame captures.Frame, payload []byte) ([]byte, error) {
		input := network.EmptyClientPacket()
		if err := input.WriteBytes(payload); err != nil {
			return nil, err
		}
		switch frame.Opcode {
		case 0x7034:
			operation, err := input.ReadByte()
			if err != nil {
				return nil, err
			}
			decoded, err := DecodeItemMallModernRequest(&input)
			if err != nil || operation != ItemMallModernOperation {
				if err != nil {
					return nil, err
				}
				return nil, errUnexpectedCaptureOperation(operation)
			}
			body, err := BuildItemMallModernRequest(decoded)
			if err != nil {
				return nil, err
			}
			encoded := network.EmptyClientPacket()
			encoded.WriteByte(operation)
			encoded.WriteBytes(body.Payload())
			return encoded.Payload(), nil
		case 0x704C:
			decoded, err := DecodeItemUseRequest(&input)
			if err != nil {
				return nil, err
			}
			encoded, err := BuildItemUseRequest(decoded)
			if err != nil {
				return nil, err
			}
			return encoded.Payload(), nil
		case 0xB034:
			decoded, err := DecodeItemMallModernPurchaseSuccess(&input)
			if err != nil {
				return nil, err
			}
			encoded := BuildItemMallModernPurchaseSuccess(decoded)
			return encoded.Payload(), nil
		default:
			return nil, errUnexpectedCaptureOpcode(frame.Opcode)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

type captureReplayError string

func (e captureReplayError) Error() string { return string(e) }

func errUnexpectedCaptureOperation(operation byte) error {
	return captureReplayError("unexpected item operation: " + hex.EncodeToString([]byte{operation}))
}

func errUnexpectedCaptureOpcode(opcode uint16) error {
	return captureReplayError("unexpected capture opcode: " + hex.EncodeToString([]byte{byte(opcode >> 8), byte(opcode)}))
}
