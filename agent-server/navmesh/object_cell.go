package navmesh

import "github.com/Harvi8/AegisCore/framework/math"

type ObjectCell struct {
	*math.Triangle
	Index int
	Flag  uint16
}
