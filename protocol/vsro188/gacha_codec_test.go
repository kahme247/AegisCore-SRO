package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGachaLayoutsMatchClientBuildersAndParsers(t *testing.T) {
	play := network.EmptyClientPacket()
	play.WriteUInt32(1)
	play.WriteUInt32(2)
	play.WriteByte(3)
	if request, err := DecodeGachaPlayRequest(&play); err != nil || request != (GachaPlayRequest{NPCUniqueID: 1, EntryID: 2, CardSlot: 3}) {
		t.Fatalf("play = %#v, %v", request, err)
	}
	exchange := network.EmptyClientPacket()
	exchange.WriteUInt32(4)
	if value, err := DecodeGachaExchangeRequest(&exchange); err != nil || value != 4 {
		t.Fatalf("exchange = %d, %v", value, err)
	}
	playResponse := BuildGachaPlayResponse(1, 5)
	if playResponse.MessageID != opcode.GachaPlayResponse || hex.EncodeToString(playResponse.Payload()) != "0105" {
		t.Fatalf("play response = %04x/%x", playResponse.MessageID, playResponse.Payload())
	}
	exchangeResponse := BuildGachaExchangeResponse(2, 0x1234)
	if hex.EncodeToString(exchangeResponse.Payload()) != "023412" {
		t.Fatalf("exchange response = %x", exchangeResponse.Payload())
	}
	announce, err := BuildGachaAnnouncement(0x12345678, 9)
	if err != nil || hex.EncodeToString(announce.Payload()) != "785634120900" {
		t.Fatalf("announce = %x, %v", announce.Payload(), err)
	}
}
