package model

import (
	"errors"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

const (
	DimensionHolePortalRefObjectID uint32 = 19556
	DimensionHolePortalCodeName           = "INS_QUEST_TELEPORT"
)

var ErrDimensionHolePortalInvalid = errors.New("invalid Dimension Hole portal")

// DimensionHolePortal is the owner-bound type-4 structure created by a
// Dimension Hole item. Its state-6 wire body is implemented separately from
// ordinary TeleportGate structures because the stock client reads owner name
// and the source item's public RefObjCommon ID instead of the two normal
// runtime-state dwords.
type DimensionHolePortal struct {
	SRObject
	Definition       DimensionHoleDefinition
	OwnerCharacterID int
	OwnerUniqueID    uint32
	OwnerName        string
}

func (p *DimensionHolePortal) GetType() string { return "DimensionHolePortal" }

func NewDimensionHolePortal(definition DimensionHoleDefinition, owner *Player, position navmeshv2.RtNavmeshPosition) (*DimensionHolePortal, error) {
	pinned, ok := DimensionHoleDefinitionForItem(definition.ItemRefID)
	if !ok || pinned != definition || owner == nil || owner.ID <= 0 || owner.GetUniqueID() == 0 ||
		strings.TrimSpace(owner.GetName()) == "" || position.Region.ID == 0 || position.Offset == nil {
		return nil, ErrDimensionHolePortalInvalid
	}
	object := NewSRObject()
	object.RefObjectID = DimensionHolePortalRefObjectID
	object.Name = DimensionHolePortalCodeName
	object.Type = "DimensionHolePortal"
	object.TypeInfo = TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 2, TypeID4: 0}
	object.GameWorldID = owner.GetGameWorldID()
	object.SetNavmeshPosition(position)
	return &DimensionHolePortal{
		SRObject:         object,
		Definition:       definition,
		OwnerCharacterID: owner.ID,
		OwnerUniqueID:    owner.GetUniqueID(),
		OwnerName:        owner.GetName(),
	}, nil
}

func IsDimensionHolePortalObject(object ISRObject) bool {
	portal, ok := object.(*DimensionHolePortal)
	if !ok || portal == nil || portal.GetRefObjectID() != DimensionHolePortalRefObjectID ||
		portal.OwnerCharacterID <= 0 || portal.OwnerUniqueID == 0 || strings.TrimSpace(portal.OwnerName) == "" {
		return false
	}
	typeInfo := portal.GetTypeInfo()
	if typeInfo.TypeID1 != 4 || typeInfo.TypeID2 != 1 || typeInfo.TypeID3 != 2 || typeInfo.TypeID4 != 0 {
		return false
	}
	pinned, exists := DimensionHoleDefinitionForItem(portal.Definition.ItemRefID)
	return exists && pinned == portal.Definition
}
