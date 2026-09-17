package model

import (
	"strings"
	"time"
)

// DimensionHoleDefinition is the stock vSRO 1.188 catalog contract shared by
// the Dimension Hole item and its target GameWorld. Cooldown is the Param10
// item-reuse interval; it is not the portal or GameWorld lifetime.
type DimensionHoleDefinition struct {
	ItemRefID     uint32
	ItemCodeName  string
	WorldID       uint16
	WorldCodeName string
	MinLevel      uint16
	MaxLevel      uint16
	Capacity      uint16        // Item Param5 and GameWorld WorldMaxUserCount.
	Difficulty    uint8         // Item Param1 and the displayed star count.
	Cooldown      time.Duration // Item Param10 reuse cooldown, authored in milliseconds.
}

var dimensionHoleDefinitions = [...]DimensionHoleDefinition{
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

// DimensionHoleCatalog returns a copy so runtime callers cannot mutate the
// pinned reference catalog shared by other sessions.
func DimensionHoleCatalog() []DimensionHoleDefinition {
	catalog := make([]DimensionHoleDefinition, len(dimensionHoleDefinitions))
	copy(catalog, dimensionHoleDefinitions[:])
	return catalog
}

func DimensionHoleDefinitionForItem(itemRefID uint32) (DimensionHoleDefinition, bool) {
	for _, definition := range dimensionHoleDefinitions {
		if definition.ItemRefID == itemRefID {
			return definition, true
		}
	}
	return DimensionHoleDefinition{}, false
}

func DimensionHoleDefinitionForWorld(worldID uint16) (DimensionHoleDefinition, bool) {
	for _, definition := range dimensionHoleDefinitions {
		if definition.WorldID == worldID {
			return definition, true
		}
	}
	return DimensionHoleDefinition{}, false
}

// DimensionHoleDefinitionForReference accepts only the exact vSRO 1.188 item
// row. This prevents a reused public ID or altered parameter row from reaching
// the consume boundary.
func DimensionHoleDefinitionForReference(ref RefItem) (DimensionHoleDefinition, bool) {
	definition, ok := DimensionHoleDefinitionForItem(ref.ID)
	if !ok {
		return DimensionHoleDefinition{}, false
	}
	valid := ref.IsUseable && ref.TypeID1 == 3 && ref.TypeID2 == 3 && ref.TypeID3 == 12 && ref.TypeID4 == 7 &&
		strings.EqualFold(strings.TrimSpace(ref.CodeName), definition.ItemCodeName) &&
		ref.Param1 == int(definition.Difficulty) && ref.Param2 == 0 &&
		ref.Param3 == int(definition.MinLevel) && ref.Param4 == int(definition.MaxLevel) &&
		ref.Param5 == int(definition.Capacity) && ref.Param6 == 6 && ref.Param7 == 1 &&
		ref.Param8 == -1 && ref.Param9 == -1 && ref.Param10 == int(definition.Cooldown.Milliseconds())
	if !valid {
		return DimensionHoleDefinition{}, false
	}
	return definition, true
}
