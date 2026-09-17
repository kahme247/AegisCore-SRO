package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/pkg/errors"
)

// ErrWorldPartitionInvalid identifies a region catalog that cannot give every
// enabled region exactly one runtime owner.
var ErrWorldPartitionInvalid = errors.New("world region partition is invalid")

type worldPartitionRegion struct {
	id    int16
	owner string
}

type worldPartition struct {
	regions []worldPartitionRegion
}

// validateWorldPartition converts the runtime-enabled database grouping into
// a deterministic one-owner-per-region plan. The catalog query excludes
// disabled dungeon planes; a negative ID reaching this boundary is therefore
// still rejected as a configuration/programming error.
func validateWorldPartition(catalog []model.GameServerRegion) (worldPartition, error) {
	if len(catalog) == 0 {
		return worldPartition{}, fmt.Errorf("%w: catalog has no enabled regions", ErrWorldPartitionInvalid)
	}

	owners := make(map[int16]string)
	continents := make(map[string]struct{}, len(catalog))
	partition := worldPartition{regions: make([]worldPartitionRegion, 0)}
	for _, entry := range catalog {
		owner := strings.TrimSpace(entry.ContinentName)
		if owner == "" {
			return worldPartition{}, fmt.Errorf("%w: continent name is empty", ErrWorldPartitionInvalid)
		}
		if _, exists := continents[owner]; exists {
			return worldPartition{}, fmt.Errorf("%w: continent %q appears more than once", ErrWorldPartitionInvalid, owner)
		}
		continents[owner] = struct{}{}
		if len(entry.Regions) == 0 {
			return worldPartition{}, fmt.Errorf("%w: continent %q has no enabled regions", ErrWorldPartitionInvalid, owner)
		}

		seen := make(map[int16]struct{}, len(entry.Regions))
		for _, regionID := range entry.Regions {
			if regionID < 0 {
				return worldPartition{}, fmt.Errorf("%w: continent %q enables unsupported dungeon region %d", ErrWorldPartitionInvalid, owner, regionID)
			}
			if _, exists := seen[regionID]; exists {
				return worldPartition{}, fmt.Errorf("%w: continent %q contains region %d more than once", ErrWorldPartitionInvalid, owner, regionID)
			}
			seen[regionID] = struct{}{}
			if existingOwner, exists := owners[regionID]; exists {
				return worldPartition{}, fmt.Errorf("%w: region %d is assigned to both %q and %q", ErrWorldPartitionInvalid, regionID, existingOwner, owner)
			}
			owners[regionID] = owner
			partition.regions = append(partition.regions, worldPartitionRegion{id: regionID, owner: owner})
		}
	}

	sort.Slice(partition.regions, func(i, j int) bool {
		if partition.regions[i].id == partition.regions[j].id {
			return partition.regions[i].owner < partition.regions[j].owner
		}
		return partition.regions[i].id < partition.regions[j].id
	})
	return partition, nil
}

func (w *WorldService) stageWorldPartition(partition worldPartition) (map[int16]*navmeshv2.RtNavmeshTerrain, error) {
	if w == nil || w.mutex == nil {
		return nil, fmt.Errorf("%w: navmesh loader is unavailable", ErrWorldPartitionInvalid)
	}
	w.mutex.Lock()
	loader := w.loader2
	w.mutex.Unlock()
	return stageWorldPartitionFromLoader(loader, partition)
}

func stageWorldPartitionFromLoader(loader *navmeshv2.Loader, partition worldPartition) (map[int16]*navmeshv2.RtNavmeshTerrain, error) {
	if loader == nil {
		return nil, fmt.Errorf("%w: navmesh loader is unavailable", ErrWorldPartitionInvalid)
	}
	loaded := make(map[int16]*navmeshv2.RtNavmeshTerrain, len(partition.regions))
	for _, region := range partition.regions {
		terrain, exists := loader.RegionData[region.id]
		if !exists || terrain == nil {
			return nil, fmt.Errorf("load game server continent %q: navmesh terrain for region %d is unavailable", region.owner, region.id)
		}
		loaded[region.id] = terrain
	}
	return loaded, nil
}

// extendWorldPartitionWithDungeonRegions publishes only dungeon planes that
// were successfully decoded from the active Data.pk2. REGION_REFERENCE also
// contains many event/instance regions whose navmesh is not loaded; deriving
// this set from DungeonData prevents those unrelated rows from breaking boot.
func extendWorldPartitionWithDungeonRegions(partition worldPartition, loader *navmeshv2.Loader) (worldPartition, error) {
	if loader == nil {
		return worldPartition{}, fmt.Errorf("%w: navmesh loader is unavailable", ErrWorldPartitionInvalid)
	}
	extended := worldPartition{regions: append([]worldPartitionRegion(nil), partition.regions...)}
	seen := make(map[int16]struct{}, len(extended.regions)+len(loader.DungeonData))
	for _, region := range extended.regions {
		seen[region.id] = struct{}{}
	}
	for regionID := range loader.DungeonData {
		if regionID >= 0 {
			return worldPartition{}, fmt.Errorf("%w: decoded dungeon region %d is not negative", ErrWorldPartitionInvalid, regionID)
		}
		if _, exists := seen[regionID]; exists {
			return worldPartition{}, fmt.Errorf("%w: dungeon region %d already has a catalog owner", ErrWorldPartitionInvalid, regionID)
		}
		if terrain := loader.RegionData[regionID]; terrain == nil {
			return worldPartition{}, fmt.Errorf("%w: decoded dungeon region %d has no admitted terrain", ErrWorldPartitionInvalid, regionID)
		}
		seen[regionID] = struct{}{}
		extended.regions = append(extended.regions, worldPartitionRegion{id: regionID, owner: "dungeon"})
	}
	sort.Slice(extended.regions, func(i, j int) bool { return extended.regions[i].id < extended.regions[j].id })
	return extended, nil
}

// commitWorldPartition publishes a completely staged partition in one critical
// section. Existing ownership is never overwritten, including by direct test
// or future reload callers of AddRegions.
func (w *WorldService) commitWorldPartition(partition worldPartition, loaded map[int16]*navmeshv2.RtNavmeshTerrain) error {
	if w == nil || w.mutex == nil {
		return ErrWorldRepositoryConfiguration
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.commitWorldPartitionLocked(partition, loaded)
}

func (w *WorldService) commitWorldBootstrap(partition worldPartition, loaded map[int16]*navmeshv2.RtNavmeshTerrain, loader *navmeshv2.Loader) error {
	if w == nil || w.mutex == nil || loader == nil {
		return ErrWorldRepositoryConfiguration
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if err := w.commitWorldPartitionLocked(partition, loaded); err != nil {
		return err
	}
	w.loader2 = loader
	return nil
}

func (w *WorldService) commitWorldPartitionLocked(partition worldPartition, loaded map[int16]*navmeshv2.RtNavmeshTerrain) error {
	for _, region := range partition.regions {
		if existingTerrain, exists := w.regions[region.id]; exists {
			existingOwner := w.regionOwners[region.id]
			if existingOwner == region.owner && existingTerrain == loaded[region.id] {
				continue
			}
			return fmt.Errorf("%w: region %d already has runtime owner %q", ErrWorldPartitionInvalid, region.id, existingOwner)
		}
		if loaded[region.id] == nil {
			return fmt.Errorf("%w: staged terrain for region %d is missing", ErrWorldPartitionInvalid, region.id)
		}
	}

	if w.regions == nil {
		w.regions = make(map[int16]*navmeshv2.RtNavmeshTerrain)
	}
	if w.regionOwners == nil {
		w.regionOwners = make(map[int16]string)
	}
	for _, region := range partition.regions {
		if _, exists := w.regions[region.id]; exists {
			continue
		}
		w.regions[region.id] = loaded[region.id]
		w.regionOwners[region.id] = region.owner
	}
	return nil
}
