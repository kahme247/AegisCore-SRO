package service

import (
	"math/rand"
	"sync"
)

// LockedDropRandom provides the selector's two primitive draws without
// sharing the process-global math/rand state across concurrent combat workers.
// Tests continue to inject scripted DropRandom implementations directly.
type LockedDropRandom struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func NewLockedDropRandom(seed int64) *LockedDropRandom {
	return &LockedDropRandom{rng: rand.New(rand.NewSource(seed))}
}

func (r *LockedDropRandom) Intn(maximum int) int {
	if r == nil || r.rng == nil || maximum <= 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rng.Intn(maximum)
}

func (r *LockedDropRandom) Float64() float64 {
	if r == nil || r.rng == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rng.Float64()
}
