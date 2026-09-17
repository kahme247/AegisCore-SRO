package model

import (
	"errors"
	"testing"
)

func TestDimensionHoleWorldRegionCatalogValidatesPinnedFamilyCounts(t *testing.T) {
	references := dimensionHoleWorldRegionTestReferences()
	catalog, err := NewDimensionHoleWorldRegionCatalog(references)
	if err != nil {
		t.Fatal(err)
	}
	definition := DimensionHoleCatalog()[0]
	wantFirst := int16(int(definition.WorldID) * 300)
	if !catalog.Contains(definition.WorldID, wantFirst) || catalog.Contains(definition.WorldID, -32750) {
		t.Fatalf("world %d regions = %#v", definition.WorldID, catalog.Regions(definition.WorldID))
	}
	regions := catalog.Regions(definition.WorldID)
	if len(regions) != 48 || regions[0] != wantFirst || regions[len(regions)-1] != wantFirst+47 {
		t.Fatalf("world %d sorted regions = %#v", definition.WorldID, regions)
	}
	flame := DimensionHoleCatalog()[6]
	if regions = catalog.Regions(flame.WorldID); len(regions) != 1 || regions[0] != -32750 {
		t.Fatalf("flame world %d regions = %#v", flame.WorldID, regions)
	}
}

func TestDimensionHoleWorldRegionCatalogRejectsMissingDuplicateUnknownAndZeroRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]DimensionHoleWorldRegionReference) []DimensionHoleWorldRegionReference
	}{
		{name: "missing", mutate: func(rows []DimensionHoleWorldRegionReference) []DimensionHoleWorldRegionReference {
			return rows[:len(rows)-1]
		}},
		{name: "duplicate", mutate: func(rows []DimensionHoleWorldRegionReference) []DimensionHoleWorldRegionReference {
			return append(rows, rows[0])
		}},
		{name: "unknown world", mutate: func(rows []DimensionHoleWorldRegionReference) []DimensionHoleWorldRegionReference {
			rows[0].WorldID = 999
			return rows
		}},
		{name: "zero region", mutate: func(rows []DimensionHoleWorldRegionReference) []DimensionHoleWorldRegionReference {
			rows[0].RegionID = 0
			return rows
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := NewDimensionHoleWorldRegionCatalog(test.mutate(dimensionHoleWorldRegionTestReferences()))
			if catalog != nil || !errors.Is(err, ErrDimensionHoleWorldRegionCatalogMismatch) {
				t.Fatalf("NewDimensionHoleWorldRegionCatalog() = %#v, %v", catalog, err)
			}
		})
	}
}

func dimensionHoleWorldRegionTestReferences() []DimensionHoleWorldRegionReference {
	definitions := DimensionHoleCatalog()
	rows := make([]DimensionHoleWorldRegionReference, 0, 2088)
	for _, definition := range definitions {
		count, _ := dimensionHoleWorldRegionCount(definition.WorldCodeName)
		for index := 0; index < count; index++ {
			regionID := int16(int(definition.WorldID)*300 + index)
			if count == 1 {
				regionID = -32750
			}
			rows = append(rows, DimensionHoleWorldRegionReference{WorldID: definition.WorldID, RegionID: regionID})
		}
	}
	return rows
}
