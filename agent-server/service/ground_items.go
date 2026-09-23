package service

import (
	"context"
	"errors"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
)

var (
	ErrGroundItemWorldUnavailable = errors.New("ground-item world is unavailable")
	ErrGroundItemInvalid          = errors.New("ground item is invalid")
)

type groundItemWorld interface {
	AddVisibleObject(model.ISRObject)
}

// GroundItemService owns the small handoff between a drop producer and the
// live world. Drop selection and ITEM-row creation remain producer-owned;
// this service applies party ownership before the object becomes pickable.
type GroundItemService struct {
	world              groundItemWorld
	parties            *PartyService
	materializer       model.GroundItemMaterializer
	lifecycle          *GroundItemLifecycle
	ownershipAfter     time.Duration
	despawnAfter       time.Duration
	distributionNotify func(*model.Player, network.Packet) error
}

func NewGroundItemService(world groundItemWorld, parties *PartyService) *GroundItemService {
	return &GroundItemService{
		world:   world,
		parties: parties,
		distributionNotify: func(player *model.Player, packet network.Packet) error {
			if player == nil || player.GetSession() == nil {
				return nil
			}
			return player.GetSession().Send(packet)
		},
	}
}

// ConfigureDistributionNotifier injects the Agent transport boundary for
// tests and alternate session transports. A nil notifier restores the default
// direct-session sender.
func (s *GroundItemService) ConfigureDistributionNotifier(notify func(*model.Player, network.Packet) error) {
	if s == nil {
		return
	}
	if notify == nil {
		notify = func(player *model.Player, packet network.Packet) error {
			if player == nil || player.GetSession() == nil {
				return nil
			}
			return player.GetSession().Send(packet)
		}
	}
	s.distributionNotify = notify
}

// ConfigureMaterializer installs the production ITEM-row boundary. Drop
// selection remains a separate producer concern; this setter only wires the
// persistence half of the lifecycle.
func (s *GroundItemService) ConfigureMaterializer(materializer model.GroundItemMaterializer) {
	if s == nil {
		return
	}
	s.materializer = materializer
}

// Materializer returns the configured persistence boundary for composition
// code that creates the drop producer after reference catalogs load.
func (s *GroundItemService) Materializer() model.GroundItemMaterializer {
	if s == nil {
		return nil
	}
	return s.materializer
}

// ConfigureLifecycle installs ownership/despawn scheduling after an item has
// been admitted to the world. The scheduler is optional so focused handlers
// and compatibility tests can continue to exercise materialization alone.
func (s *GroundItemService) ConfigureLifecycle(lifecycle *GroundItemLifecycle, ownershipAfter, despawnAfter time.Duration) {
	if s == nil {
		return
	}
	s.lifecycle = lifecycle
	s.ownershipAfter = ownershipAfter
	s.despawnAfter = despawnAfter
}

// Spawn registers an already-materialized, persisted ground item. A zero
// party number means ordinary ungrouped loot; a non-zero party number applies
// the party's share/distribution policy before visibility is published.
func (s *GroundItemService) Spawn(item *model.Item, partyNumber uint32) error {
	if s == nil || s.world == nil {
		return ErrGroundItemWorldUnavailable
	}
	if item == nil || (item.ID <= 0 && !item.TransientGroundItem) || item.GetRefObjectID() == 0 {
		return ErrGroundItemInvalid
	}
	var distribution network.Packet
	notifyDistribution := false
	if partyNumber != 0 {
		if s.parties == nil {
			return ErrPartyInvalidRequest
		}
		if err := s.parties.AssignItemOwner(partyNumber, item); err != nil {
			return err
		}
		if item.OwnerUniqueID != 0 {
			var err error
			distribution, err = BuildPartyDistributionPacket(item.OwnerUniqueID, item)
			if err != nil {
				return err
			}
			notifyDistribution = true
		}
	}
	if notifyDistribution {
		notify := s.distributionNotify
		if notify == nil {
			return ErrGroundItemWorldUnavailable
		}
		for _, member := range s.parties.ConnectedMembers(partyNumber) {
			if err := notify(member, distribution); err != nil {
				return err
			}
		}
	}
	// Publish only after the ownership notification has been accepted. This
	// keeps the client-visible assignment and world admission ordered when the
	// transport boundary reports an error.
	s.world.AddVisibleObject(item)
	despawnAfter := s.despawnAfter
	if ref, ok := model.RefItems[item.GetRefObjectID()]; ok && ref.DecayTime > 0 {
		despawnAfter = time.Duration(ref.DecayTime) * time.Millisecond
	}
	if s.lifecycle != nil && (s.ownershipAfter > 0 || despawnAfter > 0) {
		// World admission owns UID allocation; Track therefore runs after the
		// add. A failed timer registration must not roll back a visible object,
		// so the scheduler treats this as a diagnostic-only boundary.
		_ = s.lifecycle.Track(item, s.ownershipAfter, despawnAfter)
	}
	return nil
}

// MaterializeAndSpawn is the producer-facing lifecycle boundary. SQL item
// creation completes before party ownership, notification, and world
// admission. A drop selector must supply only a GroundItemSpec; it does not
// need to know how party distribution or visibility is encoded.
func (s *GroundItemService) MaterializeAndSpawn(ctx context.Context, materializer model.GroundItemMaterializer, spec model.GroundItemSpec, partyNumber uint32) (*model.Item, error) {
	if materializer == nil && s != nil {
		materializer = s.materializer
	}
	if materializer == nil {
		return nil, model.ErrGroundItemMaterializerUnavailable
	}
	item, err := materializer.Materialize(ctx, spec)
	if err != nil {
		return nil, err
	}
	if err := s.Spawn(item, partyNumber); err != nil {
		return nil, err
	}
	return item, nil
}
