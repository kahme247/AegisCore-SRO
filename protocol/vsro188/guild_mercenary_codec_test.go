package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGuildMercenaryAttributePacketsMatchBinaryLayouts(t *testing.T) {
	request := network.EmptyClientPacket()
	request.WriteUInt32(0x11223344)
	request.WriteByte(4)
	decoded, err := DecodeGuildMercenaryAttributeRequest(&request)
	if err != nil || decoded.NPCUniqueID != 0x11223344 || decoded.Attribute != 4 {
		t.Fatalf("request = %#v, error = %v", decoded, err)
	}
	update := BuildGuildMercenaryAttributeUpdate(500000, 5)
	if update.MessageID != opcode.GuildUpdate || hex.EncodeToString(update.Payload()) != "054820a1070005" {
		t.Fatalf("update = %04x:%s", update.MessageID, hex.EncodeToString(update.Payload()))
	}
	failure := BuildGuildMercenaryAttributeFailure(0x4c52)
	if failure.MessageID != opcode.GuildMercenaryAttrResponse || hex.EncodeToString(failure.Payload()) != "02524c" {
		t.Fatalf("failure = %04x:%s", failure.MessageID, hex.EncodeToString(failure.Payload()))
	}
}

func TestGuildMercenaryTerminatePacketsMatchBinaryLayouts(t *testing.T) {
	request := network.EmptyClientPacket()
	if err := DecodeGuildMercenaryTerminateRequest(&request); err != nil {
		t.Fatal(err)
	}
	request.WriteByte(1)
	if err := DecodeGuildMercenaryTerminateRequest(&request); err == nil {
		t.Fatal("expected non-empty request rejection")
	}
	failure := BuildGuildMercenaryTerminateFailure(0x4c48)
	if failure.MessageID != opcode.GuildMercenaryTerminateResponse || hex.EncodeToString(failure.Payload()) != "02484c" {
		t.Fatalf("failure = %04x:%s", failure.MessageID, hex.EncodeToString(failure.Payload()))
	}
}
