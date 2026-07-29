package service

import (
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/ferdoran/go-sro/framework/utils"
	"github.com/g3n/engine/math32"
)

func TestNewPosFromGlobalCoordinatesUsesFloorAtNormalRegionBoundaries(t *testing.T) {
	region00 := utils.XAndZToInt16(0, 0)
	region11 := utils.XAndZToInt16(1, 1)
	maxNormalRegion := utils.XAndZToInt16(255, 127)
	world := newGlobalPositionTestWorld(region00, region11, maxNormalRegion)

	tests := []struct {
		name         string
		global       *math32.Vector3
		wantRegionID int16
		wantOffsetX  float32
		wantOffsetZ  float32
	}{
		{
			name:         "just below the first region boundary remains in the prior region",
			global:       math32.NewVector3(navmeshv2.RegionWidth-0.5, 123, navmeshv2.RegionHeight-0.5),
			wantRegionID: region00,
			wantOffsetX:  navmeshv2.RegionWidth - 0.5,
			wantOffsetZ:  navmeshv2.RegionHeight - 0.5,
		},
		{
			name:         "at the region boundary moves to the next region",
			global:       math32.NewVector3(navmeshv2.RegionWidth, 123, navmeshv2.RegionHeight),
			wantRegionID: region11,
			wantOffsetX:  0,
			wantOffsetZ:  0,
		},
		{
			name:         "maximum normal grid coordinate remains addressable",
			global:       math32.NewVector3(float32(model.WorldWidth)-0.5, 123, float32(model.WorldHeight)-0.5),
			wantRegionID: maxNormalRegion,
			wantOffsetX:  navmeshv2.RegionWidth - 0.5,
			wantOffsetZ:  navmeshv2.RegionHeight - 0.5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position, err := world.NewPosFromGlobalCoordinates(test.global)
			if err != nil {
				t.Fatalf("NewPosFromGlobalCoordinates(%v): %v", test.global, err)
			}
			if position.Region.ID != test.wantRegionID {
				t.Fatalf("region ID = %d, want %d", position.Region.ID, test.wantRegionID)
			}
			if position.Offset == nil {
				t.Fatal("position offset is nil")
			}
			if !float32AlmostEqual(position.Offset.Y, test.global.Y) {
				t.Fatalf("offset Y = %g, want preserved global Y %g", position.Offset.Y, test.global.Y)
			}
			if !float32AlmostEqual(position.Offset.X, test.wantOffsetX) || !float32AlmostEqual(position.Offset.Z, test.wantOffsetZ) {
				t.Fatalf("offset = (%g, %g), want (%g, %g)", position.Offset.X, position.Offset.Z, test.wantOffsetX, test.wantOffsetZ)
			}
		})
	}
}

func TestNewPosFromGlobalCoordinatesRejectsCoordinatesOutsideNormalGridBeforeLookup(t *testing.T) {
	world := newGlobalPositionTestWorld()

	tests := []struct {
		name   string
		global *math32.Vector3
		want   error
	}{
		{name: "nil", global: nil, want: ErrGlobalPositionInvalid},
		{name: "not a number", global: math32.NewVector3(float32(math.NaN()), 0, 0), want: ErrGlobalPositionInvalid},
		{name: "negative x", global: math32.NewVector3(-0.5, 0, 0), want: ErrGlobalPositionOutOfBounds},
		{name: "negative z", global: math32.NewVector3(0, 0, -0.5), want: ErrGlobalPositionOutOfBounds},
		{name: "x at exclusive normal world maximum", global: math32.NewVector3(float32(model.WorldWidth), 0, 0), want: ErrGlobalPositionOutOfBounds},
		{name: "z at exclusive normal world maximum", global: math32.NewVector3(0, 0, float32(model.WorldHeight)), want: ErrGlobalPositionOutOfBounds},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := world.NewPosFromGlobalCoordinates(test.global)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewPosFromGlobalCoordinates(%v) error = %v, want %v", test.global, err, test.want)
			}
			if errors.Is(err, ErrRegionNotExisting) {
				t.Fatalf("invalid global coordinate reached region lookup: %v", err)
			}
		})
	}
}

func newGlobalPositionTestWorld(regionIDs ...int16) *WorldService {
	regions := make(map[int16]*navmeshv2.RtNavmeshTerrain, len(regionIDs))
	for _, regionID := range regionIDs {
		terrain := newGlobalPositionTestTerrain(regionID)
		regions[regionID] = &terrain
	}
	return &WorldService{regions: regions, mutex: &sync.Mutex{}}
}

func newGlobalPositionTestTerrain(regionID int16) navmeshv2.RtNavmeshTerrain {
	terrain := navmeshv2.NewRtNavmeshTerrain("global-position-test", navmeshv2.NewRegionFromInt16(regionID))
	// ResolveCell indexes the fixed-size tile map and then looks up the cell
	// referenced by the tile. Its zero-value tile points at cell zero, which is
	// sufficient for conversion-boundary tests that deliberately avoid collision
	// and terrain topology behavior.
	terrain.Cells = []navmeshv2.RtNavmeshCellQuad{{
		RtNavmeshCellBase: navmeshv2.RtNavmeshCellBase{Index: 0},
	}}
	return terrain
}

func float32AlmostEqual(got, want float32) bool {
	return math.Abs(float64(got-want)) < 0.001
}
