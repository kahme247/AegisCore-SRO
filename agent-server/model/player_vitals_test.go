package model

import (
	"encoding/hex"
	"testing"

	"github.com/ferdoran/go-sro/framework/network/opcode"
)

func TestVitalsAndLifeStatePacketsMatchVSRO188Layouts(t *testing.T) {
	vitals := BuildVitalsUpdatePacket(0x11223344, 0x1234, 0x5678)
	if vitals.MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("vitals opcode = 0x%04X", vitals.MessageID)
	}
	if got, want := hex.EncodeToString(vitals.Payload()), "443322110000033412000078560000"; got != want {
		t.Fatalf("vitals payload = %s, want %s", got, want)
	}

	dead := BuildLifeStateUpdatePacket(0x11223344, Dead)
	if dead.MessageID != opcode.EntityUpdateMovementState {
		t.Fatalf("life-state opcode = 0x%04X", dead.MessageID)
	}
	if got, want := hex.EncodeToString(dead.Payload()), "443322110002"; got != want {
		t.Fatalf("life-state payload = %s, want %s", got, want)
	}
}

func TestPlayerResurrectionRestoresDerivedVitals(t *testing.T) {
	player := &Player{Level: 10, BaseStats: BaseStats{Str: 25, Int: 20}, LifeState: Dead}
	maxHP, maxMP := player.MaxVitals()
	if !player.CommitResurrection(maxHP, maxMP) {
		t.Fatal("dead player was not resurrected")
	}
	if hp, mp := player.CurrentVitals(); hp != maxHP || mp != maxMP {
		t.Fatalf("resurrected vitals = %d/%d, want %d/%d", hp, mp, maxHP, maxMP)
	}
	if player.GetLifeState() != Alive {
		t.Fatalf("resurrected life state = %d, want alive", player.GetLifeState())
	}
}
