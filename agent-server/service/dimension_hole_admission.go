package service

import (
	"errors"
	"sort"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

var (
	ErrDimensionHoleAdmissionUnavailable = errors.New("Dimension Hole admission is unavailable")
	ErrDimensionHoleAdmissionInvalid     = errors.New("invalid Dimension Hole admission")
	ErrDimensionHoleAdmissionOwner       = errors.New("Dimension Hole owner is unavailable")
	ErrDimensionHoleAdmissionParty       = errors.New("player is not in the Dimension Hole owner's party")
	ErrDimensionHoleAdmissionLevel       = errors.New("player level is outside the Dimension Hole range")
	ErrDimensionHoleAdmissionState       = errors.New("player state is not eligible for Dimension Hole entry")
	ErrDimensionHoleAdmissionFull        = errors.New("Dimension Hole instance is full")
	ErrDimensionHoleAdmissionConflict    = errors.New("player is already admitted to another Dimension Hole")
	ErrDimensionHoleAdmissionMissing     = errors.New("Dimension Hole admission is missing")
)

// DimensionHoleInstanceLookup is the narrow lifecycle boundary needed by
// admission. It deliberately does not expose quest-instance persistence or
// portal destruction.
type DimensionHoleInstanceLookup interface {
	Instance(uint32) (DimensionHolePortalInstance, bool)
}

// DimensionHolePlayerLookup resolves the portal owner against the live world
// identity before party admission is granted.
type DimensionHolePlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

// DimensionHoleAdmission is a capacity reservation for one player in the
// portal's direct GameWorld layer. World transfer is a separate operation so a
// failed loading exchange can release this reservation without inventing a
// partially-entered player state.
type DimensionHoleAdmission struct {
	PortalUID  uint32
	PlayerUID  uint32
	OwnerUID   uint32
	Scope      model.GameWorldScope
	Definition model.DimensionHoleDefinition
}

type dimensionHoleAdmissionRecord struct {
	instance DimensionHolePortalInstance
	members  map[uint32]DimensionHoleAdmission
}

// DimensionHoleAdmissionService owns only portal admission and capacity. It is
// intentionally separate from QuestInstanceLifecycle because Dimension Hole
// worlds are direct REF_GAME_WORLD layers, not quest/world-map rows.
type DimensionHoleAdmissionService struct {
	mu        sync.RWMutex
	instances DimensionHoleInstanceLookup
	players   DimensionHolePlayerLookup
	byPortal  map[uint32]*dimensionHoleAdmissionRecord
	byPlayer  map[uint32]uint32
}

func NewDimensionHoleAdmissionService(instances DimensionHoleInstanceLookup, players DimensionHolePlayerLookup) (*DimensionHoleAdmissionService, error) {
	if instances == nil || players == nil {
		return nil, ErrDimensionHoleAdmissionUnavailable
	}
	return &DimensionHoleAdmissionService{
		instances: instances,
		players:   players,
		byPortal:  make(map[uint32]*dimensionHoleAdmissionRecord),
		byPlayer:  make(map[uint32]uint32),
	}, nil
}

// Admit reserves one authored capacity slot after revalidating the exact live
// portal instance, source-world identity, owner-led party, and level range.
// Repeating the same admission is idempotent; admission to a different portal
// is rejected until Leave releases the existing reservation.
func (s *DimensionHoleAdmissionService) Admit(portalUID uint32, player *model.Player) (DimensionHoleAdmission, error) {
	admission, _, err := s.admit(portalUID, player)
	return admission, err
}

func (s *DimensionHoleAdmissionService) EntryPlayers(portalUID uint32, initiator *model.Player) ([]*model.Player, error) {
	if s == nil || initiator == nil {
		return nil, ErrDimensionHoleAdmissionUnavailable
	}
	instance, ok := s.instances.Instance(portalUID)
	if !ok || !validDimensionHoleAdmissionInstance(portalUID, instance) {
		return nil, ErrDimensionHoleAdmissionInvalid
	}
	if err := s.authorize(instance, initiator); err != nil {
		return nil, err
	}
	if initiator.GetUniqueID() != instance.Portal.OwnerUniqueID {
		return []*model.Player{initiator}, nil
	}
	party := initiator.GetParty()
	if party == nil {
		return nil, ErrDimensionHoleAdmissionParty
	}
	if party.Mutex != nil {
		party.Mutex.Lock()
	}
	memberUIDs := append([]uint32(nil), party.Members...)
	if party.Mutex != nil {
		party.Mutex.Unlock()
	}
	sort.Slice(memberUIDs, func(i, j int) bool { return memberUIDs[i] < memberUIDs[j] })
	players := make([]*model.Player, 0, min(len(memberUIDs), int(instance.Portal.Definition.Capacity)))
	for _, memberUID := range memberUIDs {
		if len(players) >= int(instance.Portal.Definition.Capacity) {
			break
		}
		member, err := s.players.GetPlayerByUniqueId(memberUID)
		if err != nil || member == nil {
			continue
		}
		if s.authorize(instance, member) == nil {
			players = append(players, member)
		}
	}
	for _, member := range players {
		if member == initiator {
			return players, nil
		}
	}
	return nil, ErrDimensionHoleAdmissionOwner
}

func (s *DimensionHoleAdmissionService) admit(portalUID uint32, player *model.Player) (DimensionHoleAdmission, bool, error) {
	if s == nil || s.instances == nil || s.players == nil {
		return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionUnavailable
	}
	instance, ok := s.instances.Instance(portalUID)
	if !ok || !validDimensionHoleAdmissionInstance(portalUID, instance) {
		return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionInvalid
	}
	if err := s.authorize(instance, player); err != nil {
		return DimensionHoleAdmission{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	playerUID := player.GetUniqueID()
	current, ok := s.instances.Instance(portalUID)
	if !ok || !sameDimensionHolePortalInstance(instance, current) {
		return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionInvalid
	}
	if existingPortal, exists := s.byPlayer[playerUID]; exists {
		record := s.byPortal[existingPortal]
		existing, available := s.instances.Instance(existingPortal)
		if !available || record == nil || !sameDimensionHolePortalInstance(record.instance, existing) {
			s.removeAdmissionLocked(playerUID, existingPortal)
		} else if existingPortal != portalUID {
			return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionConflict
		} else {
			admission, exists := record.members[playerUID]
			if !exists {
				return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionInvalid
			}
			return admission, false, nil
		}
	}

	record := s.byPortal[portalUID]
	if record == nil {
		record = &dimensionHoleAdmissionRecord{instance: current, members: make(map[uint32]DimensionHoleAdmission)}
		s.byPortal[portalUID] = record
	} else if !sameDimensionHolePortalInstance(record.instance, current) {
		return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionInvalid
	}
	if len(record.members) >= int(current.Portal.Definition.Capacity) {
		return DimensionHoleAdmission{}, false, ErrDimensionHoleAdmissionFull
	}
	admission := DimensionHoleAdmission{
		PortalUID:  portalUID,
		PlayerUID:  playerUID,
		OwnerUID:   current.Portal.OwnerUniqueID,
		Scope:      current.Lease.Scope,
		Definition: current.Portal.Definition,
	}
	record.members[playerUID] = admission
	s.byPlayer[playerUID] = portalUID
	return admission, true, nil
}

func (s *DimensionHoleAdmissionService) removeAdmissionLocked(playerUID, portalUID uint32) {
	delete(s.byPlayer, playerUID)
	record := s.byPortal[portalUID]
	if record == nil {
		return
	}
	delete(record.members, playerUID)
	if len(record.members) == 0 {
		delete(s.byPortal, portalUID)
	}
}

func (s *DimensionHoleAdmissionService) authorize(instance DimensionHolePortalInstance, player *model.Player) error {
	portal := instance.Portal
	if player == nil || player.ID <= 0 || player.GetUniqueID() == 0 || player.GetGameWorldID() != portal.GetGameWorldID() {
		return ErrDimensionHoleAdmissionInvalid
	}
	owner, err := s.players.GetPlayerByUniqueId(portal.OwnerUniqueID)
	if err != nil || owner == nil || owner.ID != portal.OwnerCharacterID || owner.GetUniqueID() != portal.OwnerUniqueID || owner.GetName() != portal.OwnerName {
		return ErrDimensionHoleAdmissionOwner
	}
	ownerParty := owner.GetParty()
	playerParty := player.GetParty()
	if ownerParty == nil || playerParty != ownerParty || !dimensionHolePartyAllows(ownerParty, portal.OwnerUniqueID, player.GetUniqueID()) {
		return ErrDimensionHoleAdmissionParty
	}
	player.Mutex.Lock()
	level := player.Level
	player.Mutex.Unlock()
	if level < int(portal.Definition.MinLevel) || level > int(portal.Definition.MaxLevel) {
		return ErrDimensionHoleAdmissionLevel
	}
	// gameworldconfigdata requires job clothes and the free-PVP voucher to be
	// removed and ride/trade transports not to be mounted. Revalidate here
	// because the player's state may change after the owner opens the portal.
	if model.HasActiveJobSuit(player) || player.COSState.Snapshot().Mounted || player.GetPVPCape() != model.PvpCapeNone {
		return ErrDimensionHoleAdmissionState
	}
	return nil
}

func dimensionHolePartyAllows(party *model.Party, ownerUID, playerUID uint32) bool {
	if party == nil || ownerUID == 0 || playerUID == 0 {
		return false
	}
	if party.Mutex != nil {
		party.Mutex.Lock()
		defer party.Mutex.Unlock()
	}
	if party.MasterUniqueID != ownerUID {
		return false
	}
	ownerFound := false
	playerFound := false
	for _, memberUID := range party.Members {
		ownerFound = ownerFound || memberUID == ownerUID
		playerFound = playerFound || memberUID == playerUID
	}
	return ownerFound && playerFound
}

func validDimensionHoleAdmissionInstance(portalUID uint32, instance DimensionHolePortalInstance) bool {
	if instance.Portal == nil || portalUID == 0 || instance.Portal.GetUniqueID() != portalUID || !model.IsDimensionHolePortalObject(instance.Portal) || !instance.Lease.Scope.ValidInstance() || instance.Lease.ReservationKey == 0 {
		return false
	}
	definition := instance.Portal.Definition
	return instance.Lease.ItemRefID == definition.ItemRefID && instance.Lease.Scope.WorldID() == definition.WorldID
}

func sameDimensionHolePortalInstance(left, right DimensionHolePortalInstance) bool {
	return left.Portal == right.Portal && left.Lease == right.Lease
}

func (s *DimensionHoleAdmissionService) Leave(playerUID uint32) error {
	return s.leave(playerUID, 0)
}

// ReleasePortal removes every admission associated with a destroyed portal.
func (s *DimensionHoleAdmissionService) ReleasePortal(portalUID uint32) {
	if s == nil || portalUID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.byPortal[portalUID]
	if record == nil {
		return
	}
	for playerUID := range record.members {
		delete(s.byPlayer, playerUID)
	}
	delete(s.byPortal, portalUID)
}

func (s *DimensionHoleAdmissionService) leave(playerUID, expectedPortalUID uint32) error {
	if s == nil || playerUID == 0 {
		return ErrDimensionHoleAdmissionMissing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	portalUID, ok := s.byPlayer[playerUID]
	if !ok {
		return ErrDimensionHoleAdmissionMissing
	}
	if expectedPortalUID != 0 && portalUID != expectedPortalUID {
		return ErrDimensionHoleAdmissionConflict
	}
	record := s.byPortal[portalUID]
	if record == nil {
		return ErrDimensionHoleAdmissionInvalid
	}
	delete(record.members, playerUID)
	delete(s.byPlayer, playerUID)
	if len(record.members) == 0 {
		delete(s.byPortal, portalUID)
	}
	return nil
}

func (s *DimensionHoleAdmissionService) AdmissionForPlayer(playerUID uint32) (DimensionHoleAdmission, bool) {
	if s == nil || playerUID == 0 {
		return DimensionHoleAdmission{}, false
	}
	s.mu.RLock()
	portalUID, ok := s.byPlayer[playerUID]
	if !ok {
		s.mu.RUnlock()
		return DimensionHoleAdmission{}, false
	}
	record := s.byPortal[portalUID]
	admission, ok := record.members[playerUID]
	s.mu.RUnlock()
	return admission, ok
}

func (s *DimensionHoleAdmissionService) Members(portalUID uint32) []uint32 {
	if s == nil || portalUID == 0 {
		return nil
	}
	s.mu.RLock()
	record := s.byPortal[portalUID]
	members := make([]uint32, 0)
	if record != nil {
		members = make([]uint32, 0, len(record.members))
		for playerUID := range record.members {
			members = append(members, playerUID)
		}
	}
	s.mu.RUnlock()
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	return members
}
