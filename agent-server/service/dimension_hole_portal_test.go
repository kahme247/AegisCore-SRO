package service

import (
	"errors"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

type dimensionHolePortalWorldFake struct {
	mu      sync.Mutex
	nextUID uint32
	added   []model.ISRObject
	removed []model.ISRObject
}

func (w *dimensionHolePortalWorldFake) AddVisibleObject(object model.ISRObject) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextUID++
	object.SetUniqueID(w.nextUID)
	w.added = append(w.added, object)
}

func (w *dimensionHolePortalWorldFake) RemoveVisibleObject(object model.ISRObject) {
	w.mu.Lock()
	w.removed = append(w.removed, object)
	w.mu.Unlock()
}

func dimensionHolePortalTestOwner(id int, uid uint32, name string) *model.Player {
	return &model.Player{ID: id, CharName: name, SRObject: model.SRObject{UniqueID: uid}}
}

func dimensionHolePortalTestPosition() navmeshv2.RtNavmeshPosition {
	return navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(1254, -6, 1374),
	}
}

func TestDimensionHolePortalServicePublishesAndDestroysRegisteredWorldObject(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: 100}
	service, err := NewDimensionHolePortalService(world)
	if err != nil {
		t.Fatalf("NewDimensionHolePortalService() error = %v", err)
	}
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	portal, err := service.Publish(model.DimensionHoleCatalog()[0], owner, dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if portal.GetUniqueID() != 101 || len(world.added) != 1 || world.added[0] != portal {
		t.Fatalf("published portal/world = %#v / %#v", portal, world.added)
	}
	if got, ok := service.Portal(101); !ok || got != portal {
		t.Fatalf("Portal(101) = %#v, %t", got, ok)
	}
	if owned := service.PortalsForOwner(owner.GetUniqueID()); len(owned) != 1 || owned[0] != portal {
		t.Fatalf("PortalsForOwner() = %#v", owned)
	}
	if err = service.Destroy(101); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, ok := service.Portal(101); ok || len(service.PortalsForOwner(owner.GetUniqueID())) != 0 || len(world.removed) != 1 || world.removed[0] != portal {
		t.Fatalf("destroyed registry/world state is inconsistent")
	}
	if err = service.Destroy(101); !errors.Is(err, ErrDimensionHolePortalMissing) {
		t.Fatalf("second Destroy() error = %v", err)
	}
}

func TestDimensionHolePortalServiceRejectsBeforeWorldPublication(t *testing.T) {
	world := &dimensionHolePortalWorldFake{}
	service, _ := NewDimensionHolePortalService(world)
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	if portal, err := service.Publish(model.DimensionHoleDefinition{ItemRefID: 1}, owner, dimensionHolePortalTestPosition()); portal != nil || !errors.Is(err, model.ErrDimensionHolePortalInvalid) {
		t.Fatalf("Publish(invalid) = %#v, %v", portal, err)
	}
	if len(world.added) != 0 || len(world.removed) != 0 {
		t.Fatalf("invalid publish touched world: added=%d removed=%d", len(world.added), len(world.removed))
	}
}

func TestDimensionHolePortalServiceRollsBackWorldPublicationWithoutUID(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: ^uint32(0)}
	service, _ := NewDimensionHolePortalService(world)
	owner := dimensionHolePortalTestOwner(7, 900, "Owner")
	portal, err := service.Publish(model.DimensionHoleCatalog()[0], owner, dimensionHolePortalTestPosition())
	if portal != nil || !errors.Is(err, ErrDimensionHolePortalUnavailable) {
		t.Fatalf("Publish() = %#v, %v", portal, err)
	}
	if len(world.added) != 1 || len(world.removed) != 1 || world.removed[0] != world.added[0] {
		t.Fatalf("failed publication was not rolled back: added=%#v removed=%#v", world.added, world.removed)
	}
	if len(service.byUID) != 0 || len(service.byOwner) != 0 {
		t.Fatal("failed publication reached the semantic registry")
	}
}

func TestDimensionHolePortalServiceRegistryIsConcurrentAndOwnerScoped(t *testing.T) {
	world := &dimensionHolePortalWorldFake{nextUID: 100}
	service, _ := NewDimensionHolePortalService(world)
	definition := model.DimensionHoleCatalog()[0]
	const count = 16
	var wait sync.WaitGroup
	errorsByOwner := make(chan error, count)
	for index := 1; index <= count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			owner := dimensionHolePortalTestOwner(index, uint32(900+index), "Owner")
			_, publishErr := service.Publish(definition, owner, dimensionHolePortalTestPosition())
			errorsByOwner <- publishErr
		}(index)
	}
	wait.Wait()
	close(errorsByOwner)
	for publishErr := range errorsByOwner {
		if publishErr != nil {
			t.Fatalf("concurrent Publish() error = %v", publishErr)
		}
	}
	for index := 1; index <= count; index++ {
		if got := service.PortalsForOwner(uint32(900 + index)); len(got) != 1 {
			t.Fatalf("owner %d portals = %d, want 1", index, len(got))
		}
	}
}
