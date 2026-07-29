package service

import (
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
)

// movementCoordinator owns movement timing and delegates world mutation back
// through the WorldService facade. The production clock preserves current
// behavior; tests may construct the coordinator with a deterministic clock.
type movementCoordinator struct {
	world *WorldService
	now   func() time.Time
}

func newMovementCoordinator(world *WorldService) movementCoordinator {
	return movementCoordinator{world: world, now: time.Now}
}

// UpdatePosition remains the handler/manager-facing compatibility facade.
func (w *WorldService) UpdatePosition(character model.ICharacter) bool {
	return newMovementCoordinator(w).updatePosition(character)
}
