package model

import (
	"reflect"
	"testing"
	"time"
)

func TestDimensionHoleCatalogMatchesPinnedVSRO188(t *testing.T) {
	want := []DimensionHoleDefinition{
		{35883, "ITEM_ETC_TELEPORT_HOLE_TOGUI_35_50_LEVEL_1", 63, "INS_GOD_TOGUI_35_50_LEVEL_1", 35, 50, 4, 1, 5 * time.Hour},
		{35884, "ITEM_ETC_TELEPORT_HOLE_TOGUI_35_50_LEVEL_2", 64, "INS_GOD_TOGUI_35_50_LEVEL_2", 35, 50, 4, 2, 5 * time.Hour},
		{35885, "ITEM_ETC_TELEPORT_HOLE_TOGUI_51_60_LEVEL_1", 65, "INS_GOD_TOGUI_51_60_LEVEL_1", 51, 60, 4, 1, 5 * time.Hour},
		{35886, "ITEM_ETC_TELEPORT_HOLE_TOGUI_51_60_LEVEL_2", 66, "INS_GOD_TOGUI_51_60_LEVEL_2", 51, 60, 4, 2, 5 * time.Hour},
		{35887, "ITEM_ETC_TELEPORT_HOLE_TOGUI_61_70_LEVEL_1", 67, "INS_GOD_TOGUI_61_70_LEVEL_1", 61, 70, 4, 1, 5 * time.Hour},
		{35888, "ITEM_ETC_TELEPORT_HOLE_TOGUI_61_70_LEVEL_2", 68, "INS_GOD_TOGUI_61_70_LEVEL_2", 61, 70, 4, 2, 5 * time.Hour},
		{35889, "ITEM_ETC_TELEPORT_HOLE_FLAME_71_80_LEVEL_1", 69, "INS_GOD_FLAME_71_80_LEVEL_1", 71, 80, 4, 1, 5 * time.Hour},
		{35890, "ITEM_ETC_TELEPORT_HOLE_FLAME_71_80_LEVEL_2", 70, "INS_GOD_FLAME_71_80_LEVEL_2", 71, 80, 4, 2, 5 * time.Hour},
		{35891, "ITEM_ETC_TELEPORT_HOLE_FLAME_71_80_LEVEL_3", 71, "INS_GOD_FLAME_71_80_LEVEL_3", 71, 80, 8, 3, 5 * time.Hour},
		{35892, "ITEM_ETC_TELEPORT_HOLE_FLAME_71_80_LEVEL_4", 72, "INS_GOD_FLAME_71_80_LEVEL_4", 71, 80, 8, 4, 5 * time.Hour},
		{35893, "ITEM_ETC_TELEPORT_HOLE_FLAME_81_90_LEVEL_1", 73, "INS_GOD_FLAME_81_90_LEVEL_1", 81, 90, 4, 1, 5 * time.Hour},
		{35894, "ITEM_ETC_TELEPORT_HOLE_FLAME_81_90_LEVEL_2", 74, "INS_GOD_FLAME_81_90_LEVEL_2", 81, 90, 4, 2, 5 * time.Hour},
		{35895, "ITEM_ETC_TELEPORT_HOLE_FLAME_81_90_LEVEL_3", 75, "INS_GOD_FLAME_81_90_LEVEL_3", 81, 90, 8, 3, 5 * time.Hour},
		{35896, "ITEM_ETC_TELEPORT_HOLE_FLAME_81_90_LEVEL_4", 76, "INS_GOD_FLAME_81_90_LEVEL_4", 81, 90, 8, 4, 5 * time.Hour},
		{35897, "ITEM_ETC_TELEPORT_HOLE_WRECK_91_100_LEVEL_1", 77, "INS_GOD_WRECK_IN_91_100_LEVEL_1", 91, 100, 4, 1, 5 * time.Hour},
		{35898, "ITEM_ETC_TELEPORT_HOLE_WRECK_91_100_LEVEL_2", 78, "INS_GOD_WRECK_IN_91_100_LEVEL_2", 91, 100, 4, 2, 5 * time.Hour},
		{35899, "ITEM_ETC_TELEPORT_HOLE_WRECK_91_100_LEVEL_3", 79, "INS_GOD_WRECK_OUT_91_100_LEVEL_3", 91, 100, 8, 3, 5 * time.Hour},
		{35900, "ITEM_ETC_TELEPORT_HOLE_WRECK_91_100_LEVEL_4", 80, "INS_GOD_WRECK_OUT_91_100_LEVEL_4", 91, 100, 8, 4, 5 * time.Hour},
		{35901, "ITEM_ETC_TELEPORT_HOLE_WRECK_100_110_LEVEL_1", 81, "INS_GOD_WRECK_IN_100_110_LEVEL_1", 101, 110, 4, 1, 5 * time.Hour},
		{35902, "ITEM_ETC_TELEPORT_HOLE_WRECK_100_110_LEVEL_2", 82, "INS_GOD_WRECK_IN_100_110_LEVEL_2", 101, 110, 4, 2, 5 * time.Hour},
		{35903, "ITEM_ETC_TELEPORT_HOLE_WRECK_100_110_LEVEL_3", 83, "INS_GOD_WRECK_OUT_100_110_LEVEL_3", 101, 110, 8, 3, 5 * time.Hour},
		{35904, "ITEM_ETC_TELEPORT_HOLE_WRECK_100_110_LEVEL_4", 84, "INS_GOD_WRECK_OUT_100_110_LEVEL_4", 101, 110, 8, 4, 5 * time.Hour},
	}

	got := DimensionHoleCatalog()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DimensionHoleCatalog() = %#v, want %#v", got, want)
	}
	for _, expected := range want {
		gotDefinition, ok := DimensionHoleDefinitionForItem(expected.ItemRefID)
		if !ok || gotDefinition != expected {
			t.Fatalf("DimensionHoleDefinitionForItem(%d) = %#v, %t; want %#v, true", expected.ItemRefID, gotDefinition, ok, expected)
		}
		gotDefinition, ok = DimensionHoleDefinitionForWorld(expected.WorldID)
		if !ok || gotDefinition != expected {
			t.Fatalf("DimensionHoleDefinitionForWorld(%d) = %#v, %t; want %#v, true", expected.WorldID, gotDefinition, ok, expected)
		}
	}
	if gotDefinition, ok := DimensionHoleDefinitionForItem(0); ok || gotDefinition != (DimensionHoleDefinition{}) {
		t.Fatalf("DimensionHoleDefinitionForItem(0) = %#v, %t; want zero, false", gotDefinition, ok)
	}
}

func TestDimensionHoleDefinitionForReferenceRequiresExactItemContract(t *testing.T) {
	for _, definition := range DimensionHoleCatalog() {
		ref := dimensionHoleTestReference(definition)
		if got, ok := DimensionHoleDefinitionForReference(ref); !ok || got != definition {
			t.Fatalf("DimensionHoleDefinitionForReference(%d) = %#v, %t", definition.ItemRefID, got, ok)
		}
	}

	definition := DimensionHoleCatalog()[0]
	tests := []struct {
		name   string
		mutate func(*RefItem)
	}{
		{name: "code", mutate: func(ref *RefItem) { ref.CodeName = "ITEM_WRONG" }},
		{name: "type", mutate: func(ref *RefItem) { ref.TypeID4 = 6 }},
		{name: "difficulty", mutate: func(ref *RefItem) { ref.Param1++ }},
		{name: "minimum level", mutate: func(ref *RefItem) { ref.Param3++ }},
		{name: "maximum level", mutate: func(ref *RefItem) { ref.Param4++ }},
		{name: "capacity", mutate: func(ref *RefItem) { ref.Param5++ }},
		{name: "parameter 6", mutate: func(ref *RefItem) { ref.Param6++ }},
		{name: "portal state", mutate: func(ref *RefItem) { ref.Param7++ }},
		{name: "cooldown", mutate: func(ref *RefItem) { ref.Param10++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref := dimensionHoleTestReference(definition)
			test.mutate(&ref)
			if got, ok := DimensionHoleDefinitionForReference(ref); ok || got != (DimensionHoleDefinition{}) {
				t.Fatalf("altered reference accepted: %#v, %t", got, ok)
			}
		})
	}
}

func dimensionHoleTestReference(definition DimensionHoleDefinition) RefItem {
	return RefItem{
		RefObject: RefObject{
			ID: definition.ItemRefID, CodeName: definition.ItemCodeName, IsUseable: true,
			TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 12, TypeID4: 7},
		},
		Param1: int(definition.Difficulty), Param2: 0,
		Param3: int(definition.MinLevel), Param4: int(definition.MaxLevel), Param5: int(definition.Capacity),
		Param6: 6, Param7: 1, Param8: -1, Param9: -1, Param10: int(definition.Cooldown.Milliseconds()),
	}
}

func TestDimensionHoleCatalogReturnsCopy(t *testing.T) {
	first := DimensionHoleCatalog()
	first[0].WorldID = 0
	second := DimensionHoleCatalog()
	if second[0].WorldID != 63 {
		t.Fatalf("catalog mutation leaked: first world ID = %d", second[0].WorldID)
	}
}
