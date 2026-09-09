package navmeshv2

type RtNavmeshEdgeLink struct {
	LinkedObjID     int16
	LinkedObjEdgeID int16
	EdgeID          int16
}

func (l RtNavmeshEdgeLink) IsUnmapped() bool {
	return l.LinkedObjID == -1 && l.LinkedObjEdgeID == -1 && l.EdgeID == -1
}

func (l RtNavmeshEdgeLink) HasNegativeField() bool {
	return l.LinkedObjID < 0 || l.LinkedObjEdgeID < 0 || l.EdgeID < 0
}

type RtNavmeshInstObj struct {
	RtNavmeshInstBase
	Region    Region
	WorldID   int
	EdgeLinks []RtNavmeshEdgeLink
}
