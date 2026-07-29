package model

import (
	"errors"
	"sync"
)

var (
	ErrNoActiveTransport = errors.New("player has no active transport")
	ErrTransportNotOwned = errors.New("transport is not owned by player")
	ErrInvalidTransport  = errors.New("transport identity is invalid")
)

// PlayerCOSState owns the mutable, session-local state for a player's summoned
// ride transport. It is deliberately independent from the player's economy
// lock: mounting must not serialize unrelated inventory or gold operations.
//
// Persistence and summon-scroll consumption are intentionally outside this
// type. The summon path activates a validated transport, while protocol
// handlers can only transition that already-owned transport's mount state.
type PlayerCOSState struct {
	mu                   sync.Mutex
	transportUniqueID    uint32
	transportRefObjectID uint32
	mounted              bool
}

type PlayerCOSSnapshot struct {
	TransportUniqueID    uint32
	TransportRefObjectID uint32
	Mounted              bool
}

// ActivateTransport installs an already-created, owned ride transport. It is
// the narrow hand-off intended for the later summon implementation.
func (s *PlayerCOSState) ActivateTransport(uniqueID, refObjectID uint32, mounted bool) error {
	if s == nil || uniqueID == 0 || refObjectID == 0 {
		return ErrInvalidTransport
	}
	s.mu.Lock()
	s.transportUniqueID = uniqueID
	s.transportRefObjectID = refObjectID
	s.mounted = mounted
	s.mu.Unlock()
	return nil
}

func (s *PlayerCOSState) Snapshot() PlayerCOSSnapshot {
	if s == nil {
		return PlayerCOSSnapshot{}
	}
	s.mu.Lock()
	snapshot := PlayerCOSSnapshot{
		TransportUniqueID:    s.transportUniqueID,
		TransportRefObjectID: s.transportRefObjectID,
		Mounted:              s.mounted,
	}
	s.mu.Unlock()
	return snapshot
}

// SetMounted atomically validates ownership and applies the requested state.
// Repeating the same request is successful and leaves the state unchanged,
// making retransmits and concurrent duplicate requests idempotent.
func (s *PlayerCOSState) SetMounted(uniqueID uint32, mounted bool) (PlayerCOSSnapshot, error) {
	if s == nil || uniqueID == 0 {
		return PlayerCOSSnapshot{}, ErrInvalidTransport
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transportUniqueID == 0 {
		return PlayerCOSSnapshot{}, ErrNoActiveTransport
	}
	if s.transportUniqueID != uniqueID {
		return PlayerCOSSnapshot{}, ErrTransportNotOwned
	}
	s.mounted = mounted
	return PlayerCOSSnapshot{
		TransportUniqueID:    s.transportUniqueID,
		TransportRefObjectID: s.transportRefObjectID,
		Mounted:              s.mounted,
	}, nil
}

func (s *PlayerCOSState) ClearTransport(uniqueID uint32) error {
	if s == nil || uniqueID == 0 {
		return ErrInvalidTransport
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transportUniqueID == 0 {
		return ErrNoActiveTransport
	}
	if s.transportUniqueID != uniqueID {
		return ErrTransportNotOwned
	}
	s.transportUniqueID = 0
	s.transportRefObjectID = 0
	s.mounted = false
	return nil
}
