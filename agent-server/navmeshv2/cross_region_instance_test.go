package navmeshv2

import (
	"testing"

	"github.com/g3n/engine/math32"
)

// A neighbour .nvm stores a foreign instance relative to its own region. The
// Jangan-west bridge (owner R24485, local x=160) appears in nv_5fa4 (R24484)
// at x=2080; both copies must resolve to the same global position.
func TestRebaseObjectInstancePositionMatchesOwnerCopy(t *testing.T) {
	owner := NewRegionFromInt16(24485)
	file := NewRegionFromInt16(24484)

	ownerCopy := math32.NewVector3(160, 161.4, 1111.8)
	foreignCopy := math32.NewVector3(2080, 161.4, 1111.8)
	rebaseObjectInstancePosition(foreignCopy, file, owner)

	if !foreignCopy.Equals(ownerCopy) {
		t.Fatalf("rebased foreign copy = %v, want owner copy %v", foreignCopy, ownerCopy)
	}
}

func TestRebaseObjectInstancePositionVerticalNeighbour(t *testing.T) {
	owner := NewRegionFromInt16(24228) // one row below R24484
	file := NewRegionFromInt16(24484)
	position := math32.NewVector3(1823.2, 416.3, -95)
	rebaseObjectInstancePosition(position, file, owner)
	if position.X != 1823.2 || math32.Abs(position.Z-(1920-95)) > 0.001 {
		t.Fatalf("rebased position = %v, want (1823.2, 416.3, 1825)", position)
	}
}

func TestRebaseObjectInstancePositionKeepsOwnRegionAndDungeons(t *testing.T) {
	own := NewRegionFromInt16(24484)
	position := math32.NewVector3(10, 20, 30)
	rebaseObjectInstancePosition(position, own, own)
	if !position.Equals(math32.NewVector3(10, 20, 30)) {
		t.Fatalf("same-region instance moved to %v", position)
	}

	dungeon := NewRegionFromInt16(-32767)
	rebaseObjectInstancePosition(position, own, dungeon)
	if !position.Equals(math32.NewVector3(10, 20, 30)) {
		t.Fatalf("dungeon instance moved to %v", position)
	}
}

func TestRebaseObjectInstancePositionNil(t *testing.T) {
	rebaseObjectInstancePosition(nil, NewRegionFromInt16(24484), NewRegionFromInt16(24485))
}
