package model

import "testing"

func TestPlayerReturnDamageChanceMatchesNativeActorAccumulator(t *testing.T) {
	player := &Player{}
	// First use is ordinary <= chance admission and stores 2*chance-100.
	if !player.AdmitReturnDamage(50, 50) {
		t.Fatal("first roll at chance boundary was rejected")
	}
	if got := player.returnDamageChanceState[returnDamageChanceCategory]; got != 0 {
		t.Fatalf("state after first success = %d, want 0", got)
	}
	// Existing state is the native threshold; it is not the authored chance.
	if !player.AdmitReturnDamage(50, 0) {
		t.Fatal("actor accumulator threshold roll was rejected")
	}
	if got := player.returnDamageChanceState[returnDamageChanceCategory]; got != -50 {
		t.Fatalf("state after accumulator success = %d, want -50", got)
	}
	if player.AdmitReturnDamage(50, 100) {
		t.Fatal("roll above accumulator threshold was admitted")
	}
	if got := player.returnDamageChanceState[returnDamageChanceCategory]; got != 0 {
		t.Fatalf("state after accumulator failure = %d, want 0", got)
	}
}

func TestPlayerReturnDamageChanceFullPercentDoesNotCreateAccumulatorState(t *testing.T) {
	player := &Player{}
	if !player.AdmitReturnDamage(100, 100) {
		t.Fatal("100 percent rgmd admission rejected")
	}
	if len(player.returnDamageChanceState) != 0 {
		t.Fatalf("100 percent rgmd created state: %#v", player.returnDamageChanceState)
	}
}
