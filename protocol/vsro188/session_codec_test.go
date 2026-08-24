package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestAgentPingCodecGoldenAndRejectsInvalidLengths(t *testing.T) {
	request := network.EmptyClientPacket()
	request.WriteUInt32(0x12345678)
	decoded, err := (AgentPingRequestCodec{}).Decode(request)
	if err != nil || decoded.Token != 0x12345678 {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
	response, err := (AgentPingResponseCodec{}).Encode(AgentPingResponse{Token: decoded.Token})
	if err != nil || response.MessageID != opcode.AgentPingResponse || hex.EncodeToString(response.Payload()) != "78563412" {
		t.Fatalf("Encode() opcode=%04x payload=%x error=%v", response.MessageID, response.Payload(), err)
	}
	for _, size := range []int{0, 1, 2, 3, 5, 32} {
		malformed := network.EmptyClientPacket()
		for i := 0; i < size; i++ {
			malformed.WriteByte(0)
		}
		if _, err := (AgentPingRequestCodec{}).Decode(malformed); err == nil {
			t.Fatalf("Decode() accepted payload size %d", size)
		}
	}
}

func TestLoadingAcknowledgementCodecsRequireEmptyPayload(t *testing.T) {
	empty := network.EmptyClientPacket()
	if _, err := (GameReadyRequestCodec{}).Decode(empty); err != nil {
		t.Fatal(err)
	}
	if _, err := (TeleportReadyResponseCodec{}).Decode(empty); err != nil {
		t.Fatal(err)
	}
	trailing := network.EmptyClientPacket()
	trailing.WriteByte(1)
	if _, err := (GameReadyRequestCodec{}).Decode(trailing); err == nil {
		t.Fatal("GameReady codec accepted trailing data")
	}
	if _, err := (TeleportReadyResponseCodec{}).Decode(trailing); err == nil {
		t.Fatal("teleport-ready codec accepted trailing data")
	}
}

func TestCharacterJoinCodecBoundsNameAndRejectsMalformedPayload(t *testing.T) {
	valid := network.EmptyClientPacket()
	valid.WriteString("Character")
	request, err := (CharacterJoinRequestCodec{}).Decode(valid)
	if err != nil || request.Name != "Character" {
		t.Fatalf("Decode(valid) = %+v, %v", request, err)
	}
	for _, name := range []string{"", " padded ", string([]byte{0xff}), "abcdefghijklmnopqrstuvwxyz1234567"} {
		packet := network.EmptyClientPacket()
		packet.WriteString(name)
		if _, err := (CharacterJoinRequestCodec{}).Decode(packet); err == nil {
			t.Fatalf("Decode() accepted name %q", name)
		}
	}
	truncated := network.EmptyClientPacket()
	truncated.WriteUInt16(4)
	truncated.WriteBytes([]byte("abc"))
	if _, err := (CharacterJoinRequestCodec{}).Decode(truncated); err == nil {
		t.Fatal("Decode() accepted truncated name")
	}
	trailing := network.EmptyClientPacket()
	trailing.WriteString("Character")
	trailing.WriteByte(0)
	if _, err := (CharacterJoinRequestCodec{}).Decode(trailing); err == nil {
		t.Fatal("Decode() accepted trailing data")
	}
}

func TestAgentAuthCodecParsesBindingFieldsAndRejectsMalformedPayload(t *testing.T) {
	valid := network.EmptyClientPacket()
	valid.WriteUInt32(0x12345678)
	valid.WriteString(" alice ")
	valid.WriteString("discarded-secret")
	valid.WriteByte(56)
	valid.WriteBytes([]byte{1, 2, 3, 4, 5, 6})
	request, err := (AgentAuthRequestCodec{}).Decode(valid)
	if err != nil || request.QueueID != 0x12345678 || request.Username != " alice " || request.ContentID != 56 || request.MAC != [6]byte{1, 2, 3, 4, 5, 6} {
		t.Fatalf("Decode(valid) = %+v, %v", request, err)
	}
	truncated := network.EmptyClientPacket()
	truncated.WriteUInt32(1)
	truncated.WriteString("alice")
	truncated.WriteString("secret")
	truncated.WriteByte(56)
	truncated.WriteBytes([]byte{1, 2, 3, 4, 5})
	if _, err := (AgentAuthRequestCodec{}).Decode(truncated); err == nil {
		t.Fatal("Decode() accepted truncated MAC")
	}
	trailing := valid
	trailing.WriteByte(0)
	if _, err := (AgentAuthRequestCodec{}).Decode(trailing); err == nil {
		t.Fatal("Decode() accepted trailing data")
	}
}
