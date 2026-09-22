package model

import (
	"errors"
	"strings"
	"sync"
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

var (
	dimensionHoleCatalogMu          sync.RWMutex
	dimensionHoleDefinitions        []DimensionHoleDefinition
	dimensionHoleDefinitionsByItem  map[uint32]DimensionHoleDefinition
	dimensionHoleDefinitionsByWorld map[uint16]DimensionHoleDefinition
)

func InstallDimensionHoleCatalog(definitions []DimensionHoleDefinition) error {
	if len(definitions) == 0 {
		return errors.New("Dimension Hole catalog is empty")
	}
	owned := append([]DimensionHoleDefinition(nil), definitions...)
	byItem := make(map[uint32]DimensionHoleDefinition, len(owned))
	byWorld := make(map[uint16]DimensionHoleDefinition, len(owned))
	for _, definition := range owned {
		if definition.ItemRefID == 0 || definition.WorldID == 0 || definition.ItemCodeName == "" || definition.WorldCodeName == "" ||
			definition.MinLevel == 0 || definition.MaxLevel < definition.MinLevel || definition.Capacity == 0 ||
			definition.Difficulty == 0 || definition.Cooldown <= 0 {
			return errors.New("Dimension Hole catalog contains an invalid definition")
		}
		if _, exists := byItem[definition.ItemRefID]; exists {
			return errors.New("Dimension Hole catalog contains a duplicate item")
		}
		if _, exists := byWorld[definition.WorldID]; exists {
			return errors.New("Dimension Hole catalog contains a duplicate world")
		}
		byItem[definition.ItemRefID] = definition
		byWorld[definition.WorldID] = definition
	}
	dimensionHoleCatalogMu.Lock()
	dimensionHoleDefinitions = owned
	dimensionHoleDefinitionsByItem = byItem
	dimensionHoleDefinitionsByWorld = byWorld
	dimensionHoleCatalogMu.Unlock()
	return nil
}

func DimensionHoleCatalog() []DimensionHoleDefinition {
	dimensionHoleCatalogMu.RLock()
	catalog := append([]DimensionHoleDefinition(nil), dimensionHoleDefinitions...)
	dimensionHoleCatalogMu.RUnlock()
	return catalog
}

func DimensionHoleDefinitionForItem(itemRefID uint32) (DimensionHoleDefinition, bool) {
	dimensionHoleCatalogMu.RLock()
	definition, ok := dimensionHoleDefinitionsByItem[itemRefID]
	dimensionHoleCatalogMu.RUnlock()
	return definition, ok
}

func DimensionHoleDefinitionForWorld(worldID uint16) (DimensionHoleDefinition, bool) {
	dimensionHoleCatalogMu.RLock()
	definition, ok := dimensionHoleDefinitionsByWorld[worldID]
	dimensionHoleCatalogMu.RUnlock()
	return definition, ok
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
