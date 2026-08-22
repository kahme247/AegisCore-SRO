package model

import (
	"bytes"
	"testing"
)

func TestBuildCharacterMaskPacketMatchesStock3207Layout(t *testing.T) {
	packet := BuildCharacterMaskPacket(0x11223344, CharacterMask{RefCharID: 0x55667788, Scale: 100, Skills: []uint32{10, 20}})
	want := []byte{
		0x44, 0x33, 0x22, 0x11,
		0x88, 0x77, 0x66, 0x55,
		100, 2,
		10, 0, 0, 0,
		20, 0, 0, 0,
	}
	if packet.MessageID != 0x3207 || !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("mask packet = opcode:%#x payload:% X, want 0x3207/% X", packet.MessageID, packet.Payload(), want)
	}
}
