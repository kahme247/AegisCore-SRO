package model

import (
	"context"
	"testing"
)

func TestTeleportGateCatalogContainsAlexandriaStructures(t *testing.T) {
	gates, err := NewTeleportGateRepository().GetTeleportGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[uint32]struct {
		teleportID uint32
		region     int16
	}{
		27753: {teleportID: 175, region: 23088},
		27754: {teleportID: 176, region: 23602},
	}
	for _, gate := range gates {
		if want, ok := wanted[gate.RefObjectID]; ok && gate.TeleportID == want.teleportID && gate.RegionID == want.region {
			delete(wanted, gate.RefObjectID)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("Alexandria teleport structures missing from catalog: %v", wanted)
	}
}

func TestTeleportGateCatalogMatchesEnabledFortressTaxSet(t *testing.T) {
	gates, err := NewTeleportGateRepository().GetTeleportGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantActive := map[uint32]bool{
		2094:  true,  // FORTRESS_JANGAN is enabled in siegefortress.txt.
		2095:  false, // FORTRESS_DONWHANG is absent.
		2096:  true,  // FORTRESS_HOTAN is enabled.
		19554: false,
		19555: false,
		27753: false,
		27754: false,
	}
	for _, gate := range gates {
		want, ok := wantActive[gate.RefObjectID]
		if !ok {
			t.Fatalf("unexpected teleport gate %d", gate.RefObjectID)
		}
		if gate.TaxActive != want {
			t.Errorf("gate %d TaxActive = %t, want %t", gate.RefObjectID, gate.TaxActive, want)
		}
		delete(wantActive, gate.RefObjectID)
	}
	if len(wantActive) != 0 {
		t.Fatalf("teleport gates missing from catalog: %v", wantActive)
	}
}
