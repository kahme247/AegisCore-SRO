package model

import "fmt"

// GameWorldScope is the packed legacy WorldID/LayerID pair. Scope zero is
// Agent's internal public-world sentinel and projects to stock wire pair 1/1.
type GameWorldScope uint32

func NewGameWorldScope(worldID, layerID uint16) (GameWorldScope, error) {
	if worldID == 0 || layerID == 0 || worldID == 0xffff || layerID == 0xffff {
		return 0, fmt.Errorf("invalid game-world scope %d/%d", worldID, layerID)
	}
	return GameWorldScope(uint32(layerID)<<16 | uint32(worldID)), nil
}

func (scope GameWorldScope) WorldID() uint16 {
	if scope == 0 {
		return 1
	}
	return uint16(scope)
}

func (scope GameWorldScope) LayerID() uint16 {
	if scope == 0 {
		return 1
	}
	return uint16(uint32(scope) >> 16)
}

func (scope GameWorldScope) ValidInstance() bool {
	return scope != 0 && scope.WorldID() != 0 && scope.LayerID() != 0 &&
		scope.WorldID() != 0xffff && scope.LayerID() != 0xffff
}
