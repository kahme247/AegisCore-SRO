package model

import (
	"errors"
	"testing"
)

func TestDimensionHoleStartPositionCatalogPreservesAllOrderedStarts(t *testing.T) {
	regions := dimensionHoleWorldRegionCatalogForStarts(t)
	rows := dimensionHoleStartPositionTestReferences(regions)
	firstWorld := DimensionHoleCatalog()[0].WorldID
	rows = append(rows, DimensionHoleStartPositionReference{WorldID: firstWorld, StartIndex: 1, RegionID: rows[0].RegionID, X: 9, Y: 8, Z: 7})
	catalog, err := NewDimensionHoleStartPositionCatalog(regions, rows)
	if err != nil {
		t.Fatal(err)
	}
	positions := catalog.Positions(firstWorld)
	if len(positions) != 2 || positions[0].StartIndex != 0 || positions[1].StartIndex != 1 || positions[1].X != 9 {
		t.Fatalf("world %d positions = %+v", firstWorld, positions)
	}
	positions[0].X = 999
	if catalog.Positions(firstWorld)[0].X == 999 {
		t.Fatal("Positions returned mutable catalog storage")
	}
}

func TestDimensionHoleStartPositionCatalogRejectsIncompleteAndInvalidRows(t *testing.T) {
	regions := dimensionHoleWorldRegionCatalogForStarts(t)
	tests := []struct {
		name   string
		mutate func([]DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference
	}{
		{name: "missing world", mutate: func(rows []DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference {
			return rows[:len(rows)-1]
		}},
		{name: "unknown world", mutate: func(rows []DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference {
			rows[0].WorldID = 999
			return rows
		}},
		{name: "region outside world", mutate: func(rows []DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference {
			rows[0].RegionID = 1
			return rows
		}},
		{name: "unsupported Param", mutate: func(rows []DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference {
			rows[0].Param = 1
			return rows
		}},
		{name: "duplicate index", mutate: func(rows []DimensionHoleStartPositionReference) []DimensionHoleStartPositionReference {
			return append(rows, rows[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := NewDimensionHoleStartPositionCatalog(regions, test.mutate(dimensionHoleStartPositionTestReferences(regions)))
			if catalog != nil || !errors.Is(err, ErrDimensionHoleStartPositionCatalogMismatch) {
				t.Fatalf("NewDimensionHoleStartPositionCatalog() = %#v, %v", catalog, err)
			}
		})
	}
	if catalog, err := NewDimensionHoleStartPositionCatalog(nil, nil); catalog != nil || !errors.Is(err, ErrDimensionHoleStartPositionCatalogMismatch) {
		t.Fatalf("NewDimensionHoleStartPositionCatalog(nil) = %#v, %v", catalog, err)
	}
}

func dimensionHoleWorldRegionCatalogForStarts(t *testing.T) *DimensionHoleWorldRegionCatalog {
	t.Helper()
	catalog, err := NewDimensionHoleWorldRegionCatalog(dimensionHoleWorldRegionTestReferences())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func dimensionHoleStartPositionTestReferences(regions *DimensionHoleWorldRegionCatalog) []DimensionHoleStartPositionReference {
	definitions := DimensionHoleCatalog()
	rows := make([]DimensionHoleStartPositionReference, len(definitions))
	for index, definition := range definitions {
		rows[index] = DimensionHoleStartPositionReference{
			WorldID: definition.WorldID, RegionID: regions.Regions(definition.WorldID)[0],
			X: int32(index + 1), Y: int32(index + 2), Z: int32(index + 3),
		}
	}
	return rows
}
