package lobby

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCharacterSkillBootstrapOmitsImplicitBaseAttacks(t *testing.T) {
	player := &model.Player{Skills: map[uint32]byte{70: 1, 1: 1, 40: 0, 2: 1}}
	packet := network.EmptyPacket()
	writeCharacterSkillSection(&packet, player)

	want := []byte{0, 2}
	if !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("skill bootstrap = %x, want %x", packet.Payload(), want)
	}
}

func TestCharacterSkillBootstrapRestoresSortedLearnedSkills(t *testing.T) {
	player := &model.Player{Skills: map[uint32]byte{315: 1, 2: 1, 292: 1, 74: 1}}
	packet := network.EmptyPacket()
	writeCharacterSkillSection(&packet, player)

	want := []byte{
		0,
		1, 74, 0, 0, 0, 1,
		1, 36, 1, 0, 0, 1,
		1, 59, 1, 0, 0, 1,
		2,
	}
	if !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("skill bootstrap = %x, want %x", packet.Payload(), want)
	}
}

func TestCharacterSkillBootstrapPreservesEmptySectionLayout(t *testing.T) {
	packet := network.EmptyPacket()
	writeCharacterSkillSection(&packet, &model.Player{})
	if want := []byte{0, 2}; !bytes.Equal(packet.Payload(), want) {
		t.Fatalf("empty skill bootstrap = %x, want %x", packet.Payload(), want)
	}
}
