package model

import (
	"reflect"
	"testing"

	"github.com/ferdoran/go-sro/framework/network"
)

func TestWritePlayerTransportStateMatchesVSROSpawnParser(t *testing.T) {
	player := &Player{}
	unmounted := network.EmptyPacket()
	WritePlayerTransportState(&unmounted, player)
	if !reflect.DeepEqual(unmounted.Payload(), []byte{0, 0}) {
		t.Fatalf("unmounted spawn segment = %x", unmounted.Payload())
	}

	if err := player.COSState.ActivateTransport(0x11223344, 99, true); err != nil {
		t.Fatal(err)
	}
	mounted := network.EmptyPacket()
	WritePlayerTransportState(&mounted, player)
	want := []byte{1, 0, 0x44, 0x33, 0x22, 0x11}
	if !reflect.DeepEqual(mounted.Payload(), want) {
		t.Fatalf("mounted spawn segment = %x, want %x", mounted.Payload(), want)
	}
}
