package navmeshv2

import (
	"testing"

	"github.com/g3n/engine/math32"
)

func TestHorizontalDistanceIgnoresHeightAcrossRegions(t *testing.T) {
	from := RtNavmeshPosition{
		Region: NewRegionFromXAndY(10, 20),
		Offset: math32.NewVector3(1915, 100, 500),
	}
	to := RtNavmeshPosition{
		Region: NewRegionFromXAndY(11, 20),
		Offset: math32.NewVector3(5, 900, 500),
	}
	if got := HorizontalDistance(from, to); got != 10 {
		t.Fatalf("HorizontalDistance() = %g, want 10", got)
	}
}
