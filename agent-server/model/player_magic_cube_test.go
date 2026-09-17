package model

import "testing"

func TestPlayerMagicCubeAuthorityLifecycle(t *testing.T) {
	player := &Player{}
	if _, active := player.ActiveMagicCube(); active {
		t.Fatal("zero-value player has an active magic cube")
	}
	if player.ActivateMagicCube(0) || !player.ActivateMagicCube(55) {
		t.Fatal("magic-cube activation validation failed")
	}
	if itemID, active := player.ActiveMagicCube(); !active || itemID != 55 {
		t.Fatalf("active magic cube = %d/%t", itemID, active)
	}
	player.DeactivateMagicCube()
	if _, active := player.ActiveMagicCube(); active {
		t.Fatal("magic cube remained active after deactivation")
	}
}
