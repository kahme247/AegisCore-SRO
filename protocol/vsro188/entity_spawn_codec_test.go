package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestEntityGroupSpawnBeginGoldenMatchesClient3017(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		wire  string
		begin EntityGroupSpawnBegin
	}{
		{name: "spawn", wire: "010300", begin: EntityGroupSpawnBegin{Action: 1, Count: 3}},
		{name: "remove", wire: "020100", begin: EntityGroupSpawnBegin{Action: 2, Count: 1}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			bytes, err := hex.DecodeString(fixture.wire)
			if err != nil {
				t.Fatal(err)
			}
			packet := network.EmptyClientPacket()
			if err := packet.WriteBytes(bytes); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeEntityGroupSpawnBegin(packet)
			if err != nil || decoded != fixture.begin {
				t.Fatalf("decoded 0x3017 = %+v, %v", decoded, err)
			}
			built, err := BuildEntityGroupSpawnBegin(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(built.Payload()); got != fixture.wire {
				t.Fatalf("encoded 0x3017 = %s, want %s", got, fixture.wire)
			}
		})
	}
}

func TestEntityGroupSpawnBeginRejectsInvalidActionAndTrailingBytes(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteBytes([]byte{0, 1, 0})
	if _, err := DecodeEntityGroupSpawnBegin(packet); err == nil {
		t.Fatal("0x3017 accepted invalid action")
	}
	packet = network.EmptyClientPacket()
	packet.WriteBytes([]byte{1, 0, 0, 0})
	if _, err := DecodeEntityGroupSpawnBegin(packet); err == nil {
		t.Fatal("0x3017 accepted trailing bytes")
	}
}
