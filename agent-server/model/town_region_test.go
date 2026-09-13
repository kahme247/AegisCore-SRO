package model

import "testing"

func TestIsTownRegionV188(t *testing.T) {
	for _, regionID := range []int16{25000, 26264, 26959, 27243, 24744, 24758} {
		if !IsTownRegionV188(regionID) {
			t.Fatalf("region %d should be a town", regionID)
		}
	}
	for _, regionID := range []int16{0, 25960} {
		if IsTownRegionV188(regionID) {
			t.Fatalf("region %d should not be a town", regionID)
		}
	}
}
