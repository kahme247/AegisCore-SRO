package service

import (
	"errors"
	"sort"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

var (
	ErrDimensionHolePortalUnavailable = errors.New("Dimension Hole portal runtime is unavailable")
	ErrDimensionHolePortalMissing     = errors.New("Dimension Hole portal is missing")
)

type dimensionHolePortalWorld interface {
	AddVisibleObject(model.ISRObject)
	RemoveVisibleObject(model.ISRObject)
}

// DimensionHolePortalService owns the semantic registry around WorldService's
// generic object index. Lifetime and admission policy are deliberately left to
// later evidence-backed layers; Publish and Destroy provide the exact,
// rollback-safe world-object boundary those layers need.
type DimensionHolePortalService struct {
	mu      sync.RWMutex
	world   dimensionHolePortalWorld
	byUID   map[uint32]*model.DimensionHolePortal
	byOwner map[uint32]map[uint32]*model.DimensionHolePortal
}

func NewDimensionHolePortalService(world dimensionHolePortalWorld) (*DimensionHolePortalService, error) {
	if world == nil {
		return nil, ErrDimensionHolePortalUnavailable
	}
	return &DimensionHolePortalService{
		world: world, byUID: make(map[uint32]*model.DimensionHolePortal),
		byOwner: make(map[uint32]map[uint32]*model.DimensionHolePortal),
	}, nil
}

func (s *DimensionHolePortalService) Publish(definition model.DimensionHoleDefinition, owner *model.Player, position navmeshv2.RtNavmeshPosition) (*model.DimensionHolePortal, error) {
	if s == nil || s.world == nil {
		return nil, ErrDimensionHolePortalUnavailable
	}
	portal, err := model.NewDimensionHolePortal(definition, owner, position)
	if err != nil {
		return nil, err
	}
	s.world.AddVisibleObject(portal)
	uid := portal.GetUniqueID()
	if uid == 0 {
		s.world.RemoveVisibleObject(portal)
		return nil, ErrDimensionHolePortalUnavailable
	}

	s.mu.Lock()
	if _, exists := s.byUID[uid]; exists {
		s.mu.Unlock()
		s.world.RemoveVisibleObject(portal)
		return nil, ErrDimensionHolePortalUnavailable
	}
	s.byUID[uid] = portal
	owned := s.byOwner[portal.OwnerUniqueID]
	if owned == nil {
		owned = make(map[uint32]*model.DimensionHolePortal)
		s.byOwner[portal.OwnerUniqueID] = owned
	}
	owned[uid] = portal
	s.mu.Unlock()
	return portal, nil
}

func (s *DimensionHolePortalService) Portal(uid uint32) (*model.DimensionHolePortal, bool) {
	if s == nil || uid == 0 {
		return nil, false
	}
	s.mu.RLock()
	portal, ok := s.byUID[uid]
	s.mu.RUnlock()
	return portal, ok
}

func (s *DimensionHolePortalService) PortalsForOwner(ownerUID uint32) []*model.DimensionHolePortal {
	if s == nil || ownerUID == 0 {
		return nil
	}
	s.mu.RLock()
	owned := s.byOwner[ownerUID]
	portals := make([]*model.DimensionHolePortal, 0, len(owned))
	for _, portal := range owned {
		portals = append(portals, portal)
	}
	s.mu.RUnlock()
	sort.Slice(portals, func(i, j int) bool { return portals[i].GetUniqueID() < portals[j].GetUniqueID() })
	return portals
}

func (s *DimensionHolePortalService) Destroy(uid uint32) error {
	if s == nil || s.world == nil || uid == 0 {
		return ErrDimensionHolePortalMissing
	}
	s.mu.Lock()
	portal, ok := s.byUID[uid]
	if !ok {
		s.mu.Unlock()
		return ErrDimensionHolePortalMissing
	}
	delete(s.byUID, uid)
	owned := s.byOwner[portal.OwnerUniqueID]
	delete(owned, uid)
	if len(owned) == 0 {
		delete(s.byOwner, portal.OwnerUniqueID)
	}
	s.mu.Unlock()
	s.world.RemoveVisibleObject(portal)
	return nil
}
