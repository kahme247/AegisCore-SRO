package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type dimensionHoleWorldRepositoryFake struct {
	catalog *model.DimensionHoleWorldCatalog
	err     error
	calls   int
}

type dimensionHoleWorldRegionRepositoryFake struct {
	catalog *model.DimensionHoleWorldRegionCatalog
	err     error
	calls   int
}

type dimensionHoleStartPositionRepositoryFake struct {
	catalog *model.DimensionHoleStartPositionCatalog
	err     error
	calls   int
}

func (f *dimensionHoleStartPositionRepositoryFake) LoadDimensionHoleStartPositionCatalog(ctx context.Context, regions *model.DimensionHoleWorldRegionCatalog) (*model.DimensionHoleStartPositionCatalog, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.catalog, f.err
}

func (f *dimensionHoleWorldRegionRepositoryFake) LoadDimensionHoleWorldRegionCatalog(ctx context.Context) (*model.DimensionHoleWorldRegionCatalog, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.catalog, f.err
}

func (f *dimensionHoleWorldRepositoryFake) LoadDimensionHoleWorldCatalog(ctx context.Context) (*model.DimensionHoleWorldCatalog, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.catalog, f.err
}

func TestNewDimensionHoleRuntimeComposesLeasePortalAndAdmissionAuthorities(t *testing.T) {
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
	owner.Party = dimensionHoleAdmissionTestParty(owner.GetUniqueID(), owner)
	world := &dimensionHolePortalWorldFake{nextUID: 500}
	players := &dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{owner.GetUniqueID(): owner}}
	runtime, err := NewDimensionHoleRuntime(world, &dimensionHoleScopeAllocatorFake{}, players, func() uint32 { return 700 })
	if err != nil {
		t.Fatalf("NewDimensionHoleRuntime() error = %v", err)
	}
	if runtime.Portals == nil || runtime.Lifecycle == nil || runtime.Admissions == nil {
		t.Fatalf("runtime = %#v", runtime)
	}
	instance, err := runtime.Lifecycle.Open(context.Background(), definition, owner, dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	admission, err := runtime.Admissions.Admit(instance.Portal.GetUniqueID(), owner)
	if err != nil {
		t.Fatalf("Admit(owner) error = %v", err)
	}
	if admission.Scope != instance.Lease.Scope || admission.PortalUID != instance.Portal.GetUniqueID() {
		t.Fatalf("admission = %#v; instance = %#v", admission, instance)
	}
	if err = runtime.Lifecycle.Destroy(context.Background(), instance.Portal.GetUniqueID()); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, admitted := runtime.Admissions.AdmissionForPlayer(owner.GetUniqueID()); admitted {
		t.Fatal("destroyed portal retained owner admission")
	}
	replacement, err := runtime.Lifecycle.Open(context.Background(), definition, owner, dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatalf("Open(replacement) error = %v", err)
	}
	if _, err = runtime.Admissions.Admit(replacement.Portal.GetUniqueID(), owner); err != nil {
		t.Fatalf("Admit(replacement) error = %v", err)
	}
}

func TestDimensionHoleRuntimeConfiguresEntryOnlyWithCompleteDestinationsAndCoordinator(t *testing.T) {
	runtime, err := NewDimensionHoleRuntime(
		&dimensionHolePortalWorldFake{},
		&dimensionHoleScopeAllocatorFake{},
		&dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{}},
		func() uint32 { return 700 },
	)
	if err != nil {
		t.Fatal(err)
	}
	destinations, err := NewDimensionHoleEntryDestinationCatalog(dimensionHoleEntryTestWorldRegions(t), dimensionHoleEntryTestDestinations())
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &dimensionHoleEntryCoordinatorFake{}
	if err = runtime.ConfigureEntry(destinations, coordinator); err != nil {
		t.Fatalf("ConfigureEntry() error = %v", err)
	}
	if runtime.Destinations != destinations || runtime.Entries == nil {
		t.Fatalf("configured runtime = %#v", runtime)
	}
	if err = runtime.ConfigureEntry(nil, coordinator); !errors.Is(err, ErrDimensionHoleEntryUnavailable) {
		t.Fatalf("ConfigureEntry(nil destinations) error = %v", err)
	}
	if err = runtime.ConfigureEntry(destinations, nil); !errors.Is(err, ErrDimensionHoleEntryUnavailable) {
		t.Fatalf("ConfigureEntry(nil coordinator) error = %v", err)
	}
}

func TestNewDimensionHoleRuntimeRejectsMissingDependencies(t *testing.T) {
	world := &dimensionHolePortalWorldFake{}
	scopes := &dimensionHoleScopeAllocatorFake{}
	players := &dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{}}
	tests := []struct {
		name    string
		world   dimensionHolePortalWorld
		scopes  model.DimensionHoleScopeAllocator
		players DimensionHolePlayerLookup
		nextKey func() uint32
	}{
		{name: "world", scopes: scopes, players: players, nextKey: func() uint32 { return 1 }},
		{name: "scopes", world: world, players: players, nextKey: func() uint32 { return 1 }},
		{name: "players", world: world, scopes: scopes, nextKey: func() uint32 { return 1 }},
		{name: "key", world: world, scopes: scopes, players: players},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if runtime, err := NewDimensionHoleRuntime(test.world, test.scopes, test.players, test.nextKey); runtime != nil || err == nil {
				t.Fatalf("NewDimensionHoleRuntime() = %#v, %v", runtime, err)
			}
		})
	}
}

func TestLoadDimensionHoleRuntimePreflightsWorldsBeforeComposition(t *testing.T) {
	catalog := dimensionHoleRuntimeTestWorldCatalog(t)
	repository := &dimensionHoleWorldRepositoryFake{catalog: catalog}
	regionCatalog := dimensionHoleEntryTestWorldRegions(t)
	regionRepository := &dimensionHoleWorldRegionRepositoryFake{catalog: regionCatalog}
	startCatalog := dimensionHoleRuntimeTestStartCatalog(t, regionCatalog)
	startRepository := &dimensionHoleStartPositionRepositoryFake{catalog: startCatalog}
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
	owner.Party = dimensionHoleAdmissionTestParty(owner.GetUniqueID(), owner)
	runtime, err := LoadDimensionHoleRuntime(
		context.Background(), repository, regionRepository, startRepository, &dimensionHolePortalWorldFake{},
		&dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{100: owner}}, func() uint32 { return 700 },
	)
	if err != nil {
		t.Fatalf("LoadDimensionHoleRuntime() error = %v", err)
	}
	if repository.calls != 1 || regionRepository.calls != 1 || startRepository.calls != 1 || runtime.Worlds != catalog || runtime.Regions != regionCatalog || runtime.Starts != startCatalog || runtime.Destinations == nil ||
		runtime.Scopes == nil || runtime.Lifecycle == nil || runtime.Admissions == nil {
		t.Fatalf("runtime = %#v; repository calls = %d/%d/%d", runtime, repository.calls, regionRepository.calls, startRepository.calls)
	}
}

func TestLoadDimensionHoleRuntimeFailsClosedOnPreflightError(t *testing.T) {
	preflightErr := errors.New("world catalog missing")
	repository := &dimensionHoleWorldRepositoryFake{err: preflightErr}
	regionCatalog := dimensionHoleEntryTestWorldRegions(t)
	regionRepository := &dimensionHoleWorldRegionRepositoryFake{catalog: regionCatalog}
	startRepository := &dimensionHoleStartPositionRepositoryFake{catalog: dimensionHoleRuntimeTestStartCatalog(t, regionCatalog)}
	runtime, err := LoadDimensionHoleRuntime(context.Background(), repository, regionRepository, startRepository, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 })
	if runtime != nil || !errors.Is(err, preflightErr) || repository.calls != 1 {
		t.Fatalf("LoadDimensionHoleRuntime() = %#v, %v; calls=%d", runtime, err, repository.calls)
	}
	if runtime, err = LoadDimensionHoleRuntime(context.Background(), nil, regionRepository, startRepository, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 }); runtime != nil || !errors.Is(err, model.ErrDimensionHoleWorldRepositoryUnavailable) {
		t.Fatalf("LoadDimensionHoleRuntime(nil repository) = %#v, %v", runtime, err)
	}
	if runtime, err = LoadDimensionHoleRuntime(context.Background(), &dimensionHoleWorldRepositoryFake{}, nil, startRepository, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 }); runtime != nil || !errors.Is(err, model.ErrDimensionHoleWorldRegionRepositoryUnavailable) {
		t.Fatalf("LoadDimensionHoleRuntime(nil region repository) = %#v, %v", runtime, err)
	}
	if runtime, err = LoadDimensionHoleRuntime(context.Background(), &dimensionHoleWorldRepositoryFake{}, regionRepository, nil, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 }); runtime != nil || !errors.Is(err, model.ErrDimensionHoleStartPositionRepositoryUnavailable) {
		t.Fatalf("LoadDimensionHoleRuntime(nil start repository) = %#v, %v", runtime, err)
	}
	regionErr := errors.New("world regions missing")
	if runtime, err = LoadDimensionHoleRuntime(context.Background(), &dimensionHoleWorldRepositoryFake{catalog: dimensionHoleRuntimeTestWorldCatalog(t)}, &dimensionHoleWorldRegionRepositoryFake{err: regionErr}, startRepository, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 }); runtime != nil || !errors.Is(err, regionErr) {
		t.Fatalf("LoadDimensionHoleRuntime(region error) = %#v, %v", runtime, err)
	}
	startErr := errors.New("start positions missing")
	if runtime, err = LoadDimensionHoleRuntime(context.Background(), &dimensionHoleWorldRepositoryFake{catalog: dimensionHoleRuntimeTestWorldCatalog(t)}, regionRepository, &dimensionHoleStartPositionRepositoryFake{err: startErr}, &dimensionHolePortalWorldFake{}, &dimensionHoleAdmissionPlayersFake{}, func() uint32 { return 1 }); runtime != nil || !errors.Is(err, startErr) {
		t.Fatalf("LoadDimensionHoleRuntime(start error) = %#v, %v", runtime, err)
	}
}

func dimensionHoleRuntimeTestStartCatalog(t *testing.T, regions *model.DimensionHoleWorldRegionCatalog) *model.DimensionHoleStartPositionCatalog {
	t.Helper()
	rows := make([]model.DimensionHoleStartPositionReference, 0, len(model.DimensionHoleCatalog()))
	for _, definition := range model.DimensionHoleCatalog() {
		rows = append(rows, model.DimensionHoleStartPositionReference{
			WorldID: definition.WorldID, RegionID: regions.Regions(definition.WorldID)[0], X: 1, Y: 2, Z: 3,
		})
	}
	catalog, err := model.NewDimensionHoleStartPositionCatalog(regions, rows)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func dimensionHoleRuntimeTestWorldCatalog(t *testing.T) *model.DimensionHoleWorldCatalog {
	t.Helper()
	definitions := model.DimensionHoleCatalog()
	worlds := make([]model.DimensionHoleWorldReference, 0, len(definitions))
	for _, definition := range definitions {
		worlds = append(worlds, model.DimensionHoleWorldReference{
			WorldID: definition.WorldID, CodeName: definition.WorldCodeName, WorldType: model.DimensionHoleWorldType,
			MaxInstanceCount: model.DimensionHoleWorldMaxInstanceCount, MaxUserCount: definition.Capacity,
			WorldEntryType: model.DimensionHoleWorldEntryType, WorldEntranceType: model.DimensionHoleWorldEntranceType,
			WorldLeaveType: model.DimensionHoleWorldLeaveType, WorldDurationMS: model.DimensionHoleWorldDurationMS,
			WorldEmptyRemainMS:  model.DimensionHoleWorldEmptyRemainMS,
			ConfigGroupCodeName: "GROUP_" + strings.TrimPrefix(definition.WorldCodeName, "INS_"),
		})
	}
	catalog, err := model.NewDimensionHoleWorldCatalog(worlds)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
