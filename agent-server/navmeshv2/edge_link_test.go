package navmeshv2

import (
	"errors"
	"testing"
)

func TestValidateTerrainObjectEdgeLinksAcceptsMappedAndUnmappedRecords(t *testing.T) {
	objects := []RtNavmeshInstObj{
		{
			RtNavmeshInstBase: RtNavmeshInstBase{Object: RtNavmeshObj{GlobalEdges: make([]RtNavmeshEdgeGlobal, 2)}},
			EdgeLinks: []RtNavmeshEdgeLink{
				{LinkedObjID: -1, LinkedObjEdgeID: -1, EdgeID: -1},
				{LinkedObjID: 1, LinkedObjEdgeID: 0, EdgeID: 1},
			},
		},
		{
			RtNavmeshInstBase: RtNavmeshInstBase{Object: RtNavmeshObj{GlobalEdges: make([]RtNavmeshEdgeGlobal, 1)}},
			EdgeLinks:         []RtNavmeshEdgeLink{{LinkedObjID: 0, LinkedObjEdgeID: 1, EdgeID: 0}},
		},
	}

	if err := validateTerrainObjectEdgeLinks(objects, "nv_6687.nvm"); err != nil {
		t.Fatalf("validateTerrainObjectEdgeLinks() error = %v", err)
	}
}

func TestValidateTerrainObjectEdgeLinksRejectsInvalidMappedRecords(t *testing.T) {
	tests := []struct {
		name string
		link RtNavmeshEdgeLink
	}{
		{name: "partially negative", link: RtNavmeshEdgeLink{LinkedObjID: -1, LinkedObjEdgeID: 0, EdgeID: 0}},
		{name: "source edge out of range", link: RtNavmeshEdgeLink{LinkedObjID: 1, LinkedObjEdgeID: 0, EdgeID: 2}},
		{name: "linked object out of range", link: RtNavmeshEdgeLink{LinkedObjID: 2, LinkedObjEdgeID: 0, EdgeID: 0}},
		{name: "linked edge out of range", link: RtNavmeshEdgeLink{LinkedObjID: 1, LinkedObjEdgeID: 1, EdgeID: 0}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := []RtNavmeshInstObj{
				{
					RtNavmeshInstBase: RtNavmeshInstBase{Object: RtNavmeshObj{GlobalEdges: make([]RtNavmeshEdgeGlobal, 2)}},
					EdgeLinks:         []RtNavmeshEdgeLink{test.link},
				},
				{RtNavmeshInstBase: RtNavmeshInstBase{Object: RtNavmeshObj{GlobalEdges: make([]RtNavmeshEdgeGlobal, 1)}}},
			}

			err := validateTerrainObjectEdgeLinks(objects, "invalid.nvm")
			if !errors.Is(err, ErrInvalidNavmeshData) {
				t.Fatalf("validateTerrainObjectEdgeLinks() error = %v, want ErrInvalidNavmeshData", err)
			}
			var loadErr *NavmeshLoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("validateTerrainObjectEdgeLinks() error type = %T, want *NavmeshLoadError", err)
			}
			if loadErr.Operation != "validate terrain object edge link" {
				t.Fatalf("operation = %q, want validate terrain object edge link", loadErr.Operation)
			}
		})
	}
}

func TestRtNavmeshEdgeLinkClassification(t *testing.T) {
	unmapped := RtNavmeshEdgeLink{LinkedObjID: -1, LinkedObjEdgeID: -1, EdgeID: -1}
	if !unmapped.IsUnmapped() || !unmapped.HasNegativeField() {
		t.Fatalf("unmapped link classification = IsUnmapped:%t HasNegativeField:%t", unmapped.IsUnmapped(), unmapped.HasNegativeField())
	}
	mapped := RtNavmeshEdgeLink{LinkedObjID: 1, LinkedObjEdgeID: 2, EdgeID: 3}
	if mapped.IsUnmapped() || mapped.HasNegativeField() {
		t.Fatalf("mapped link classification = IsUnmapped:%t HasNegativeField:%t", mapped.IsUnmapped(), mapped.HasNegativeField())
	}
}
