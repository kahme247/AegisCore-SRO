package model

import "testing"

func TestCommunityPairCanonicalizesRelationship(t *testing.T) {
	low, high, valid := communityPair(9, 3)
	if !valid || low != 3 || high != 9 {
		t.Fatalf("pair = (%d,%d,%t)", low, high, valid)
	}
	for _, pair := range [][2]int{{0, 1}, {1, 0}, {4, 4}} {
		if _, _, valid = communityPair(pair[0], pair[1]); valid {
			t.Fatalf("pair %v should be invalid", pair)
		}
	}
}
