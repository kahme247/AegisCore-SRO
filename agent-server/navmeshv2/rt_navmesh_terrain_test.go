package navmeshv2

import (
	"testing"

	"github.com/g3n/engine/math32"
)

func TestResolveHeightClampsPointsOutsideTerrain(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})
	terrain.HeightMap[0] = 11
	terrain.HeightMap[VerticesTotal-1] = 22

	if got := terrain.ResolveHeight(math32.NewVector3(-TileWidth, 0, -TileHeight)); got != 11 {
		t.Fatalf("negative boundary height = %v, want 11", got)
	}
	if got := terrain.ResolveHeight(math32.NewVector3(TerrainWidth+TileWidth, 0, TerrainHeight+TileHeight)); got != 22 {
		t.Fatalf("positive boundary height = %v, want 22", got)
	}
}

func TestResolveHeightInterpolatesAdjacentVertices(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})
	terrain.HeightMap[0] = 0
	terrain.HeightMap[1] = 20
	terrain.HeightMap[VerticesY] = 10
	terrain.HeightMap[VerticesY+1] = 30

	got := terrain.ResolveHeight(math32.NewVector3(TileWidth/2, 0, TileHeight/2))
	if got != 15 {
		t.Fatalf("interpolated height = %v, want 15", got)
	}
}

func TestResolveCellRejectsExclusiveUpperBoundary(t *testing.T) {
	terrain := NewRtNavmeshTerrain("test.nvm", Region{})

	for _, pos := range []*math32.Vector3{
		math32.NewVector3(TerrainWidth, 0, 0),
		math32.NewVector3(0, 0, TerrainHeight),
	} {
		if _, err := terrain.ResolveCell(pos); err == nil {
			t.Fatalf("ResolveCell(%v) succeeded at exclusive terrain boundary", pos)
		}
	}
}
