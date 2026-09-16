package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestDecodeTAPInfoRequestLayouts(t *testing.T) {
	npc := network.EmptyClientPacket()
	npc.WriteByte(0)
	npc.WriteUInt32(0x12345678)
	if request, err := DecodeTAPInfoRequest(&npc); err != nil || request != (TAPInfoRequest{Reference: 0x12345678}) {
		t.Fatalf("NPC request = %#v, %v", request, err)
	}

	icon := network.EmptyClientPacket()
	icon.WriteByte(1)
	if request, err := DecodeTAPInfoRequest(&icon); err != nil || request != (TAPInfoRequest{Icon: true}) {
		t.Fatalf("icon request = %#v, %v", request, err)
	}
}

func TestDecodeTAPRequestsRejectMalformedPayloads(t *testing.T) {
	tests := []network.Packet{network.EmptyClientPacket()}
	invalidFlag := network.EmptyClientPacket()
	invalidFlag.WriteByte(2)
	tests = append(tests, invalidFlag)
	missingReference := network.EmptyClientPacket()
	missingReference.WriteByte(0)
	tests = append(tests, missingReference)
	zeroReference := network.EmptyClientPacket()
	zeroReference.WriteByte(0)
	zeroReference.WriteUInt32(0)
	tests = append(tests, zeroReference)
	iconTail := network.EmptyClientPacket()
	iconTail.WriteByte(1)
	iconTail.WriteByte(0)
	tests = append(tests, iconTail)
	for i := range tests {
		if _, err := DecodeTAPInfoRequest(&tests[i]); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}

	updateTail := network.EmptyClientPacket()
	updateTail.WriteByte(0)
	if err := DecodeTAPUpdateRequest(&updateTail); err == nil {
		t.Fatal("update request with payload accepted")
	}
}

func TestBuildTAPInfoResponseLayout(t *testing.T) {
	response, err := BuildTAPInfoResponse([]TAPInfoEntry{{
		Type:              TAPEntryThiefGuild,
		From:              TAPTime{Month: 1, Day: 2, Hour: 3, Minute: 4},
		To:                TAPTime{Month: 5, Day: 6, Hour: 7, Minute: 8},
		TraderUnionPoints: 0x11223344,
		ThiefUnionPoints:  0x55667788,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(response.Payload()); got != "010201020304050607084433221188776655" {
		t.Fatalf("info response = %s", got)
	}

	empty, err := BuildTAPInfoResponse(nil)
	if err != nil || hex.EncodeToString(empty.Payload()) != "00" {
		t.Fatalf("empty info response = %x, %v", empty.Payload(), err)
	}
}

func TestBuildTAPInfoResponseRejectsUnrepresentableCount(t *testing.T) {
	if _, err := BuildTAPInfoResponse(make([]TAPInfoEntry, 256)); err == nil {
		t.Fatal("256 entries accepted")
	}
}

func TestBuildTAPUpdateAndIconLayouts(t *testing.T) {
	update := BuildTAPUpdateResponse([]TAPUpdateEntry{{TraderUnionPoints: 1, ThiefUnionPoints: 2}, {TraderUnionPoints: 3, ThiefUnionPoints: 4}})
	if got := hex.EncodeToString(update.Payload()); got != "01000000020000000300000004000000" {
		t.Fatalf("update response = %s", got)
	}
	emptyUpdate := BuildTAPUpdateResponse(nil)
	if got := hex.EncodeToString(emptyUpdate.Payload()); got != "" {
		t.Fatalf("empty update response = %s", got)
	}
	icon := BuildTAPIcon(true)
	if got := hex.EncodeToString(icon.Payload()); got != "01" {
		t.Fatalf("icon = %s", got)
	}
}
