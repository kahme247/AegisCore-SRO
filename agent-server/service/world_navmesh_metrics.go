package service

import (
	"runtime"
	"time"

	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	log "github.com/sirupsen/logrus"
)

const bytesPerMiB = 1024 * 1024

type navmeshLoadMeasurement struct {
	started     time.Time
	heapAlloc   uint64
	heapObjects uint64
	totalAlloc  uint64
	gcCycles    uint32
}

func beginNavmeshLoadMeasurement() navmeshLoadMeasurement {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return navmeshLoadMeasurement{
		started:     time.Now(),
		heapAlloc:   memory.HeapAlloc,
		heapObjects: memory.HeapObjects,
		totalAlloc:  memory.TotalAlloc,
		gcCycles:    memory.NumGC,
	}
}

func (m navmeshLoadMeasurement) log(source string, stats navmeshv2.LoaderStats) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	log.WithFields(log.Fields{
		"source":                   source,
		"duration_ms":              time.Since(m.started).Milliseconds(),
		"heap_alloc_mib":           float64(memory.HeapAlloc) / bytesPerMiB,
		"heap_alloc_delta_mib":     float64(int64(memory.HeapAlloc)-int64(m.heapAlloc)) / bytesPerMiB,
		"heap_objects":             memory.HeapObjects,
		"heap_objects_delta":       int64(memory.HeapObjects) - int64(m.heapObjects),
		"allocated_mib":            float64(memory.TotalAlloc-m.totalAlloc) / bytesPerMiB,
		"gc_cycles":                memory.NumGC - m.gcCycles,
		"regions":                  stats.Regions,
		"object_meshes":            stats.ObjectMeshes,
		"terrain_instances":        stats.TerrainInstances,
		"terrain_cells":            stats.TerrainCells,
		"terrain_global_edges":     stats.TerrainGlobalEdges,
		"terrain_internal_edges":   stats.TerrainInternalEdges,
		"terrain_cell_object_refs": stats.TerrainCellObjectReferences,
		"terrain_cell_edge_refs":   stats.TerrainCellEdgeReferences,
		"object_cells":             stats.ObjectCells,
		"object_global_edges":      stats.ObjectGlobalEdges,
		"object_internal_edges":    stats.ObjectInternalEdges,
		"object_grid_tiles":        stats.ObjectGridTiles,
		"object_grid_cell_refs":    stats.ObjectGridCellReferences,
		"object_grid_edge_refs":    stats.ObjectGridEdgeReferences,
	}).Info("navmesh load metrics")
}
