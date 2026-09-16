package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGuildSiegeAuthorityPacketsMatchBinaryLayouts(t *testing.T) {
	request := network.EmptyClientPacket()
	request.WriteUInt32(0x11223344)
	request.WriteByte(0x20)
	decoded, err := DecodeGuildSiegeAuthorityRequest(&request)
	if err != nil || decoded.CharacterID != 0x11223344 || decoded.Authority != 0x20 {
		t.Fatalf("request = %#v, error = %v", decoded, err)
	}
	update := BuildGuildSiegeAuthorityUpdate(0x11223344, 2)
	if update.MessageID != opcode.EntityGuildUpdateSiegeAuthority || hex.EncodeToString(update.Payload()) != "4433221102" {
		t.Fatalf("update = %04x:%s", update.MessageID, hex.EncodeToString(update.Payload()))
	}
	failure := BuildGuildSiegeAuthorityFailure(0x4c59)
	if failure.MessageID != opcode.GuildUpdateSiegeAuthorityResponse || hex.EncodeToString(failure.Payload()) != "02594c" {
		t.Fatalf("failure = %04x:%s", failure.MessageID, hex.EncodeToString(failure.Payload()))
	}
}
