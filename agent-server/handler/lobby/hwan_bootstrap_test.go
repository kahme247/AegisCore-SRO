package lobby

import (
	"encoding/binary"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCharacterBootstrapSerializesPersistentHwanState(t *testing.T) {
	player := &model.Player{
		RemainHwanCount:   4,
		GatheredHwanPoint: 0x11223344,
		HwanLevel:         9,
	}
	packet := network.EmptyPacket()
	WriteCharDataToPacket(&packet, player)
	payload := packet.Payload()
	if len(payload) != 56 {
		t.Fatalf("character data length = %d, want 56", len(payload))
	}
	if payload[33] != 4 || binary.LittleEndian.Uint32(payload[34:38]) != 0x11223344 || payload[54] != 9 {
		t.Fatalf("HWAN bootstrap fields = points %d gathered %#x level %d", payload[33], binary.LittleEndian.Uint32(payload[34:38]), payload[54])
	}
}
