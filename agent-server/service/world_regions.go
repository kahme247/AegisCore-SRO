package service

import "github.com/Harvi8/AegisCore/agent-server/navmeshv2"

// worldRegions centralizes access to the immutable post-bootstrap terrain
// catalog. It returns pointers only to internal callers and detached snapshots
// to the public WorldService facade.
type worldRegions struct {
	world *WorldService
}

func (w *WorldService) regionStore() worldRegions {
	return worldRegions{world: w}
}

func (r worldRegions) lookup(regionID int16) (*navmeshv2.RtNavmeshTerrain, error) {
	r.world.mutex.Lock()
	defer r.world.mutex.Unlock()
	if region, exists := r.world.regions[regionID]; exists {
		return region, nil
	}
	return nil, regionNotExistingError(regionID)
}

func (r worldRegions) snapshots() map[int16]navmeshv2.RtNavmeshTerrain {
	r.world.mutex.Lock()
	defer r.world.mutex.Unlock()
	regions := make(map[int16]navmeshv2.RtNavmeshTerrain, len(r.world.regions))
	for regionID, region := range r.world.regions {
		regions[regionID] = snapshotTerrain(*region)
	}
	return regions
}

func (r worldRegions) ids() []int16 {
	r.world.mutex.Lock()
	defer r.world.mutex.Unlock()
	regionIDs := make([]int16, 0, len(r.world.regions))
	for regionID := range r.world.regions {
		regionIDs = append(regionIDs, regionID)
	}
	return regionIDs
}
