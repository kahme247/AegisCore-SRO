package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestDimensionHoleEntryDestinationCatalogResolvesExactAdmittedWorld(t *testing.T) {
	destinations := dimensionHoleEntryTestDestinations()
	catalog, err := NewDimensionHoleEntryDestinationCatalog(dimensionHoleEntryTestWorldRegions(t), destinations)
	if err != nil {
		t.Fatal(err)
	}
	definition := model.DimensionHoleCatalog()[7]
	scope, _ := model.NewGameWorldScope(definition.WorldID, 3)
	admission := DimensionHoleAdmission{PortalUID: 501, PlayerUID: 200, OwnerUID: 100, Scope: scope, Definition: definition}
	got, err := catalog.ResolveDimensionHoleEntry(context.Background(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if got != destinations[7] {
		t.Fatalf("destination = %+v, want %+v", got, destinations[7])
	}
}

func TestDimensionHoleEntryDestinationCatalogSelectsAmongAllStockStarts(t *testing.T) {
	destinations := dimensionHoleEntryTestDestinations()
	selected := destinations[0]
	selected.X = 999
	destinations = append(destinations, selected)
	catalog, err := NewDimensionHoleEntryDestinationCatalog(dimensionHoleEntryTestWorldRegions(t), destinations, func(count int) int {
		if count != 2 {
			t.Fatalf("selector count = %d, want 2", count)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := model.DimensionHoleCatalog()[0]
	scope, _ := model.NewGameWorldScope(definition.WorldID, 1)
	got, err := catalog.ResolveDimensionHoleEntry(context.Background(), DimensionHoleAdmission{
		PortalUID: 1, PlayerUID: 2, Scope: scope, Definition: definition,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 999 {
		t.Fatalf("selected destination = %+v, want second start", got)
	}
}

func TestDimensionHoleEntryDestinationCatalogRejectsIncompleteDuplicateAndInvalidRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]DimensionHoleEntryDestination) []DimensionHoleEntryDestination
	}{
		{name: "missing", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination { return rows[:len(rows)-1] }},
		{name: "duplicate", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination {
			rows[len(rows)-1].WorldID = rows[0].WorldID
			return rows
		}},
		{name: "unknown world", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination {
			rows[0].WorldID = 999
			return rows
		}},
		{name: "missing region", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination {
			rows[0].Region = 0
			return rows
		}},
		{name: "region outside world", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination {
			rows[0].Region = 29999
			return rows
		}},
		{name: "non-finite coordinate", mutate: func(rows []DimensionHoleEntryDestination) []DimensionHoleEntryDestination {
			rows[0].X = float32(math.NaN())
			return rows
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := test.mutate(dimensionHoleEntryTestDestinations())
			if catalog, err := NewDimensionHoleEntryDestinationCatalog(dimensionHoleEntryTestWorldRegions(t), rows); catalog != nil || !errors.Is(err, ErrDimensionHoleEntryDestinationInvalid) {
				t.Fatalf("NewDimensionHoleEntryDestinationCatalog() = %#v, %v", catalog, err)
			}
		})
	}
	if catalog, err := NewDimensionHoleEntryDestinationCatalog(nil, dimensionHoleEntryTestDestinations()); catalog != nil || !errors.Is(err, ErrDimensionHoleEntryDestinationInvalid) {
		t.Fatalf("NewDimensionHoleEntryDestinationCatalog(nil regions) = %#v, %v", catalog, err)
	}
}

func TestDimensionHoleEntryDestinationCatalogRejectsCancelledOrMismatchedAdmission(t *testing.T) {
	catalog, err := NewDimensionHoleEntryDestinationCatalog(dimensionHoleEntryTestWorldRegions(t), dimensionHoleEntryTestDestinations())
	if err != nil {
		t.Fatal(err)
	}
	definition := model.DimensionHoleCatalog()[0]
	scope, _ := model.NewGameWorldScope(definition.WorldID, 1)
	admission := DimensionHoleAdmission{PortalUID: 501, PlayerUID: 200, OwnerUID: 100, Scope: scope, Definition: definition}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = catalog.ResolveDimensionHoleEntry(ctx, admission); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ResolveDimensionHoleEntry() error = %v", err)
	}
	admission.Definition = model.DimensionHoleCatalog()[1]
	if _, err = catalog.ResolveDimensionHoleEntry(context.Background(), admission); !errors.Is(err, ErrDimensionHoleEntryDestinationInvalid) {
		t.Fatalf("mismatched ResolveDimensionHoleEntry() error = %v", err)
	}
}

func dimensionHoleEntryTestDestinations() []DimensionHoleEntryDestination {
	definitions := model.DimensionHoleCatalog()
	result := make([]DimensionHoleEntryDestination, len(definitions))
	for index, definition := range definitions {
		result[index] = DimensionHoleEntryDestination{
			WorldID: definition.WorldID,
			Region:  dimensionHoleEntryTestRegionID(definition, 0),
			X:       float32(index + 1),
			Y:       float32(index + 2),
			Z:       float32(index + 3),
		}
	}
	return result
}

func dimensionHoleEntryTestWorldRegions(t *testing.T) *model.DimensionHoleWorldRegionCatalog {
	t.Helper()
	definitions := model.DimensionHoleCatalog()
	references := make([]model.DimensionHoleWorldRegionReference, 0, 2088)
	for _, definition := range definitions {
		count := 224
		switch {
		case strings.HasPrefix(definition.WorldCodeName, "INS_GOD_TOGUI_"):
			count = 48
		case strings.HasPrefix(definition.WorldCodeName, "INS_GOD_FLAME_"):
			count = 1
		}
		for index := 0; index < count; index++ {
			references = append(references, model.DimensionHoleWorldRegionReference{
				WorldID: definition.WorldID, RegionID: dimensionHoleEntryTestRegionID(definition, index),
			})
		}
	}
	catalog, err := model.NewDimensionHoleWorldRegionCatalog(references)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func dimensionHoleEntryTestRegionID(definition model.DimensionHoleDefinition, index int) int16 {
	if strings.HasPrefix(definition.WorldCodeName, "INS_GOD_FLAME_") {
		return -32750
	}
	return int16(int(definition.WorldID)*300 + index)
}
