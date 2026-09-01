package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestItemEconomySuccessEnvelopesMatchMaintainedLayouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		want   []byte
	}{
		{
			name:   "buy",
			packet: BuildItemBuySuccess(3, 4, 0x1234, []byte{0x0D, 0x0E}, []byte{0x99}),
			want:   []byte{1, 0x08, 3, 4, 2, 0x0D, 0x0E, 0x34, 0x12, 0x99},
		},
		{
			name:   "sell",
			packet: BuildItemSellSuccess(0x0D, 0x1234, 0x11223344, 0x1A),
			want:   []byte{1, 0x09, 0x0D, 0x34, 0x12, 0x44, 0x33, 0x22, 0x11, 0x1A},
		},
		{
			name:   "buyback",
			packet: BuildItemBuyBackSuccess(0x0E, 0x0D, 0x1234),
			want:   []byte{1, 0x22, 0x0E, 0x0D, 0x34, 0x12},
		},
		{
			name:   "npc buyback acknowledgement",
			packet: BuildNPCBuyBackResponse(true),
			want:   []byte{1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.packet.Payload(); !bytes.Equal(got, test.want) {
				t.Fatalf("payload = % X, want % X", got, test.want)
			}
		})
	}
}
