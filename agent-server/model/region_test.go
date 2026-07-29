package model

import (
	"sync"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/navmesh"
)

func TestRegionVisibleObjectsZeroValueInitializesSafely(t *testing.T) {
	var region Region

	const objectCount = 32
	var waitGroup sync.WaitGroup
	for id := uint32(1); id <= objectCount; id++ {
		waitGroup.Add(1)
		go func(id uint32) {
			defer waitGroup.Done()
			region.AddVisibleObject(&SRObject{UniqueID: id})
		}(id)
	}
	waitGroup.Wait()

	if region.VisibleObjects == nil {
		t.Fatal("zero-value Region did not initialize its visible-object map")
	}
	if got := len(region.GetVisibleObjects()); got != objectCount {
		t.Fatalf("visible object count = %d, want %d", got, objectCount)
	}

	region.RemoveVisibleObject(&SRObject{UniqueID: 1})
	if got := len(region.GetVisibleObjects()); got != objectCount-1 {
		t.Fatalf("visible object count after remove = %d, want %d", got, objectCount-1)
	}
}

func TestRegionVisibleObjectsLiteralCopiesShareFallbackLock(t *testing.T) {
	region := Region{VisibleObjects: make(map[uint32]ISRObject)}
	copyOfRegion := region

	if region.visibleObjectsLock() != copyOfRegion.visibleObjectsLock() {
		t.Fatal("literal Region copies did not share a visible-object lock")
	}

	region.AddVisibleObject(&SRObject{UniqueID: 1})
	copyOfRegion.AddVisibleObject(&SRObject{UniqueID: 2})
	if got := len(region.GetVisibleObjects()); got != 2 {
		t.Fatalf("source Region visible object count = %d, want 2", got)
	}
}

func TestNewRegionFromNavMeshDataSharesVisibleObjectLockWithValueCopies(t *testing.T) {
	region := NewRegionFromNavMeshData(25000, navmesh.NavMeshData{
		HeightMap: navmesh.HeightMap{Heights: []float32{0}},
	})
	if region.visibleObjectsMutex == nil {
		t.Fatal("constructed Region did not initialize its visible-object lock")
	}

	copyOfRegion := region
	if copyOfRegion.visibleObjectsMutex != region.visibleObjectsMutex {
		t.Fatal("Region value copy did not retain the visible-object lock")
	}

	copyOfRegion.AddVisibleObject(&SRObject{UniqueID: 1})
	if got := len(region.GetVisibleObjects()); got != 1 {
		t.Fatalf("source Region visible object count = %d, want 1", got)
	}
}
