package service

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// DimensionHoleRuntime keeps the direct-world lease, visible portal, and
// admission authorities together. It intentionally excludes destinations and
// teleport packet ordering; startup can construct this boundary only after the
// REF_GAME_WORLD preflight, while entry remains gated on authored coordinates.
type DimensionHoleRuntime struct {
	Worlds       *model.DimensionHoleWorldCatalog
	Regions      *model.DimensionHoleWorldRegionCatalog
	Starts       *model.DimensionHoleStartPositionCatalog
	Scopes       model.DimensionHoleScopeAllocator
	Portals      *DimensionHolePortalService
	Lifecycle    *DimensionHolePortalLifecycle
	Admissions   *DimensionHoleAdmissionService
	Destinations *DimensionHoleEntryDestinationCatalog
	Entries      *DimensionHoleEntryService
}

func NewDimensionHoleRuntime(world dimensionHolePortalWorld, scopes model.DimensionHoleScopeAllocator, players DimensionHolePlayerLookup, nextKey func() uint32) (*DimensionHoleRuntime, error) {
	portals, err := NewDimensionHolePortalService(world)
	if err != nil {
		return nil, fmt.Errorf("configure Dimension Hole portals: %w", err)
	}
	lifecycle, err := NewDimensionHolePortalLifecycle(portals, scopes, nextKey)
	if err != nil {
		return nil, fmt.Errorf("configure Dimension Hole lifecycle: %w", err)
	}
	admissions, err := NewDimensionHoleAdmissionService(lifecycle, players)
	if err != nil {
		return nil, fmt.Errorf("configure Dimension Hole admission: %w", err)
	}
	return &DimensionHoleRuntime{Scopes: scopes, Portals: portals, Lifecycle: lifecycle, Admissions: admissions}, nil
}

// LoadDimensionHoleRuntime performs the read-only REF_GAME_WORLD preflight
// before any portal can reserve a layer or become visible.
func LoadDimensionHoleRuntime(ctx context.Context, repository model.DimensionHoleWorldRepository, regionRepository model.DimensionHoleWorldRegionRepository, startRepository model.DimensionHoleStartPositionRepository, world dimensionHolePortalWorld, players DimensionHolePlayerLookup, nextKey func() uint32) (*DimensionHoleRuntime, error) {
	if repository == nil {
		return nil, fmt.Errorf("load Dimension Hole worlds: %w", model.ErrDimensionHoleWorldRepositoryUnavailable)
	}
	if regionRepository == nil {
		return nil, fmt.Errorf("load Dimension Hole world regions: %w", model.ErrDimensionHoleWorldRegionRepositoryUnavailable)
	}
	if startRepository == nil {
		return nil, fmt.Errorf("load Dimension Hole start positions: %w", model.ErrDimensionHoleStartPositionRepositoryUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	catalog, err := repository.LoadDimensionHoleWorldCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Dimension Hole worlds: %w", err)
	}
	regions, err := regionRepository.LoadDimensionHoleWorldRegionCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Dimension Hole world regions: %w", err)
	}
	starts, err := startRepository.LoadDimensionHoleStartPositionCatalog(ctx, regions)
	if err != nil {
		return nil, fmt.Errorf("load Dimension Hole start positions: %w", err)
	}
	destinations, err := NewDimensionHoleEntryDestinationCatalogFromStarts(regions, starts)
	if err != nil {
		return nil, fmt.Errorf("configure Dimension Hole start positions: %w", err)
	}
	scopes, err := model.NewLocalDimensionHoleScopeAllocator(catalog)
	if err != nil {
		return nil, fmt.Errorf("configure Dimension Hole scopes: %w", err)
	}
	runtime, err := NewDimensionHoleRuntime(world, scopes, players, nextKey)
	if err != nil {
		return nil, err
	}
	runtime.Worlds = catalog
	runtime.Regions = regions
	runtime.Starts = starts
	runtime.Destinations = destinations
	return runtime, nil
}

// ConfigureEntry enables portal interaction only after a complete authored
// destination catalog and concrete teleport coordinator both exist.
func (r *DimensionHoleRuntime) ConfigureEntry(destinations *DimensionHoleEntryDestinationCatalog, coordinator DimensionHoleEntryCoordinator) error {
	if r == nil || r.Admissions == nil || destinations == nil || coordinator == nil {
		return ErrDimensionHoleEntryUnavailable
	}
	entries, err := NewDimensionHoleEntryService(r.Admissions, destinations, coordinator)
	if err != nil {
		return err
	}
	r.Destinations = destinations
	r.Entries = entries
	return nil
}
