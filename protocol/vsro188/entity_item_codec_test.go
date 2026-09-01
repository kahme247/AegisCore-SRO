package vsro188

import (
	"bytes"
	"testing"
)

func TestEntityItemNotificationsMatchMaintainedLayouts(t *testing.T) {
	equip := BuildEntityEquipItem(0x11223344, 6, 0x55667788, true)
	if got, want := equip.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 6, 0x88, 0x77, 0x66, 0x55, 1}; !bytes.Equal(got, want) {
		t.Fatalf("equip payload = % X, want % X", got, want)
	}
	unequip := BuildEntityUnequipItem(0x11223344, 6, 0x55667788)
	if got, want := unequip.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 6, 0x88, 0x77, 0x66, 0x55}; !bytes.Equal(got, want) {
		t.Fatalf("unequip payload = % X, want % X", got, want)
	}
	pickup := BuildEntityPickupAnimation(0x11223344, 0x7F)
	if got, want := pickup.Payload(), []byte{0x44, 0x33, 0x22, 0x11, 0x7F}; !bytes.Equal(got, want) {
		t.Fatalf("pickup animation payload = % X, want % X", got, want)
	}
}
