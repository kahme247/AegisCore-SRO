package model

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

func TestNewDimensionHolePortalBuildsPinnedOwnerBoundStructure(t *testing.T) {
	owner := &Player{ID: 7, CharName: "Owner", SRObject: SRObject{UniqueID: 900}}
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1254, -6, 1374),
	}
	definition := DimensionHoleCatalog()[0]
	portal, err := NewDimensionHolePortal(definition, owner, position)
	if err != nil {
		t.Fatalf("NewDimensionHolePortal() error = %v", err)
	}
	if !IsDimensionHolePortalObject(portal) || portal.GetRefObjectID() != DimensionHolePortalRefObjectID ||
		portal.GetName() != DimensionHolePortalCodeName || portal.OwnerCharacterID != owner.ID ||
		portal.OwnerUniqueID != owner.GetUniqueID() || portal.OwnerName != owner.GetName() || portal.Definition != definition {
		t.Fatalf("portal = %#v", portal)
	}
	if got := portal.GetTypeInfo(); got != (TypeInfo{TypeID1: 4, TypeID2: 1, TypeID3: 2, TypeID4: 0}) {
		t.Fatalf("portal type = %+v", got)
	}
}

func TestNewDimensionHolePortalRejectsUnpinnedOrIncompleteInput(t *testing.T) {
	definition := DimensionHoleCatalog()[0]
	owner := &Player{ID: 7, CharName: "Owner", SRObject: SRObject{UniqueID: 900}}
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1, 2, 3),
	}
	tests := []struct {
		name       string
		definition DimensionHoleDefinition
		owner      *Player
		position   navmeshv2.RtNavmeshPosition
	}{
		{name: "unpinned definition", definition: DimensionHoleDefinition{ItemRefID: 1}, owner: owner, position: position},
		{name: "missing owner", definition: definition, position: position},
		{name: "missing owner UID", definition: definition, owner: &Player{ID: 7, CharName: "Owner"}, position: position},
		{name: "missing owner name", definition: definition, owner: &Player{ID: 7, SRObject: SRObject{UniqueID: 900}}, position: position},
		{name: "missing position", definition: definition, owner: owner},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portal, err := NewDimensionHolePortal(test.definition, test.owner, test.position)
			if portal != nil || !errors.Is(err, ErrDimensionHolePortalInvalid) {
				t.Fatalf("NewDimensionHolePortal() = %#v, %v", portal, err)
			}
		})
	}
}

func TestIsDimensionHolePortalObjectRejectsMutatedWireIdentity(t *testing.T) {
	owner := &Player{ID: 7, CharName: "Owner", SRObject: SRObject{UniqueID: 900}}
	position := navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1, 2, 3),
	}
	portal, _ := NewDimensionHolePortal(DimensionHoleCatalog()[0], owner, position)
	portal.RefObjectID++
	if IsDimensionHolePortalObject(portal) {
		t.Fatal("portal with mutated reference identity was accepted")
	}
}
