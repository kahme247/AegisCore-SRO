package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestPremiumSilkRequestLayoutsMatchClientBuilders(t *testing.T) {
	pause := network.EmptyClientPacket()
	pause.WriteByte(2)
	pause.WriteByte(1)
	if request, err := DecodePremiumPauseRequest(&pause); err != nil || request != (PremiumPauseRequest{Category: 2, Active: true}) {
		t.Fatalf("pause = %#v, %v", request, err)
	}
	history := network.EmptyClientPacket()
	history.WriteByte(1)
	history.WriteByte(3)
	if request, err := DecodeSilkHistoryRequest(&history); err != nil || request != (SilkHistoryRequest{Mode: 1, Currency: 3}) {
		t.Fatalf("history = %#v, %v", request, err)
	}
	decoration := network.EmptyClientPacket()
	decoration.WriteUInt32(11)
	decoration.WriteUInt32(22)
	if first, second, err := DecodePremiumDecorationUseRequest(&decoration); err != nil || first != 11 || second != 22 {
		t.Fatalf("decoration = %d/%d, %v", first, second, err)
	}
	unknown := network.EmptyClientPacket()
	unknown.WriteUInt16(0x1234)
	if group, err := DecodeSilkUnknownRequest(&unknown); err != nil || group != 0x1234 {
		t.Fatalf("unknown = %x, %v", group, err)
	}
}

func TestPremiumSilkResponseLayoutsMatchClientParsers(t *testing.T) {
	history, err := BuildSilkHistoryResponse(SilkHistoryRequest{Mode: 1, Currency: 2}, []SilkHistoryEntry{{Timestamp: 3, Amount: 4}}, 0)
	if err != nil || hex.EncodeToString(history.Payload()) != "02010001000300000004000000" {
		t.Fatalf("history = %x, %v", history.Payload(), err)
	}
	deco := BuildPremiumDecorationUseResponse(1, 2, 0)
	decoFailure := BuildPremiumDecorationUseResponse(0, 0, 7)
	if hex.EncodeToString(deco.Payload()) != "010100000002000000" || hex.EncodeToString(decoFailure.Payload()) != "0207" {
		t.Fatalf("decoration = %x/%x", deco.Payload(), decoFailure.Payload())
	}
	unknown, err := BuildSilkUnknownResponse(0x1234, []SilkUnknownEntry{{Reference: 5, Amount: 6}}, 0)
	if err != nil || hex.EncodeToString(unknown.Payload()) != "01341201050000000600" {
		t.Fatalf("unknown = %x, %v", unknown.Payload(), err)
	}
}

func TestPremiumUpdateLayoutsMatchClientParsers(t *testing.T) {
	value := PremiumDecoration{Token: 1, Reference: 2, Value: 3}
	decoration := BuildPremiumDecoration(value)
	if got := hex.EncodeToString(decoration.Payload()); got != "010000000200000003000000" {
		t.Fatalf("decoration = %s", got)
	}
	decorationReset := BuildPremiumDecorationReset(value)
	if got := hex.EncodeToString(decorationReset.Payload()); got != "010000000200000003000000" {
		t.Fatalf("decoration reset = %s", got)
	}
	dailyReset := BuildPremiumDailyReset(1, 2)
	if got := hex.EncodeToString(dailyReset.Payload()); got != "0100000002000000" {
		t.Fatalf("daily reset = %s", got)
	}
	unknownUpdate := BuildPremiumUnknownUpdate(PremiumUnknownUpdate{First: 1, Second: 2, Third: 3, State: 4})
	if got := hex.EncodeToString(unknownUpdate.Payload()); got != "01000000020000000300000004" {
		t.Fatalf("unknown update = %s", got)
	}
}
