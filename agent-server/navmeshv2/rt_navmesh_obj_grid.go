package navmeshv2

import (
	"github.com/Harvi8/AegisCore/agent-server/navmesh"
	"github.com/g3n/engine/math32"
)

type RtNavmeshObjGrid struct {
	object    *RtNavmeshObj `json:"-"`
	Tiles     []RtNavmeshObjGridTile
	X         float32
	Y         float32
	Width     int
	Height    int
	Rectangle Rectangle
}

func NewRtNavmeshObjGrid(object *RtNavmeshObj) RtNavmeshObjGrid {
	return RtNavmeshObjGrid{
		object: object,
		Tiles:  make([]RtNavmeshObjGridTile, 0),
	}
}

func (grid *RtNavmeshObjGrid) AddTile(tile RtNavmeshObjGridTile) {
	grid.Tiles = append(grid.Tiles, tile)
}

func (grid *RtNavmeshObjGrid) GetTile(index int) RtNavmeshObjGridTile {
	return grid.Tiles[index]
}

// TryGetTile returns the tile at index when the backing slice contains it.
func (grid *RtNavmeshObjGrid) TryGetTile(index int) (RtNavmeshObjGridTile, bool) {
	if index < 0 || index >= len(grid.Tiles) {
		return RtNavmeshObjGridTile{}, false
	}

	return grid.Tiles[index], true
}

func (grid *RtNavmeshObjGrid) GetTileFromXAndY(x, y int) RtNavmeshObjGridTile {
	return grid.Tiles[y*grid.Width+x]
}

// TryGetTileFromXAndY returns a tile only when its coordinates and backing
// slice index are both within the grid. The non-Try getter intentionally keeps
// its original direct-index behavior for existing callers with valid input.
func (grid *RtNavmeshObjGrid) TryGetTileFromXAndY(x, y int) (RtNavmeshObjGridTile, bool) {
	if x < 0 || x >= grid.Width || y < 0 || y >= grid.Height {
		return RtNavmeshObjGridTile{}, false
	}

	return grid.TryGetTile(y*grid.Width + x)
}

func (grid *RtNavmeshObjGrid) GetTileFromVec(v *math32.Vector3) RtNavmeshObjGridTile {
	tileX := int((v.X - grid.X) / RtNavmeshObjGridTileWidth)
	tileY := int((v.Z - grid.Y) / RtNavmeshObjGridTileHeight)

	return grid.GetTileFromXAndY(tileX, tileY)
}

// TryGetTileFromVec returns the tile containing v. The grid's maximum edge is
// exclusive because it is immediately beyond the last tile.
func (grid *RtNavmeshObjGrid) TryGetTileFromVec(v *math32.Vector3) (RtNavmeshObjGridTile, bool) {
	return grid.tryGetTileFromCoordinates(v.X, v.Z)
}

func (grid *RtNavmeshObjGrid) GetTileFromVec2(v *math32.Vector2) RtNavmeshObjGridTile {
	tileX := int((v.X - grid.X) / RtNavmeshObjGridTileWidth)
	tileY := int((v.Y - grid.Y) / RtNavmeshObjGridTileHeight)

	return grid.GetTileFromXAndY(tileX, tileY)
}

// TryGetTileFromVec2 returns the tile containing v. The grid's maximum edge
// is exclusive because it is immediately beyond the last tile.
func (grid *RtNavmeshObjGrid) TryGetTileFromVec2(v *math32.Vector2) (RtNavmeshObjGridTile, bool) {
	return grid.tryGetTileFromCoordinates(v.X, v.Y)
}

func (grid *RtNavmeshObjGrid) tryGetTileFromCoordinates(x, y float32) (RtNavmeshObjGridTile, bool) {
	if grid.Width <= 0 || grid.Height <= 0 {
		return RtNavmeshObjGridTile{}, false
	}

	localX := x - grid.X
	localY := y - grid.Y
	if localX < 0 || localY < 0 ||
		localX >= float32(grid.Width)*RtNavmeshObjGridTileWidth ||
		localY >= float32(grid.Height)*RtNavmeshObjGridTileHeight {
		return RtNavmeshObjGridTile{}, false
	}

	return grid.TryGetTileFromXAndY(
		int(localX/RtNavmeshObjGridTileWidth),
		int(localY/RtNavmeshObjGridTileHeight),
	)
}

func (grid *RtNavmeshObjGrid) Load(reader navmesh.Loader) error {
	return ErrNavmeshNotImplemented
}
