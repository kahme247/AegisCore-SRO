// Package registry defines evidence-aware identities for SRO application
// packets. An opcode alone is not an identity: the same numeric value can mean
// different things on different services, in opposite directions, or during
// different lifecycle phases.
package registry

import (
	"errors"
	"fmt"
	"sort"
)

type Service string

const (
	ServiceGateway Service = "gateway"
	ServiceAgent   Service = "agent"
)

type Direction string

const (
	ClientToServer Direction = "client-to-server"
	ServerToClient Direction = "server-to-client"
)

type Phase string

const (
	GatewayPreLogin       Phase = "gateway-pre-login"
	AgentAuthentication   Phase = "agent-authentication"
	AgentLobby            Phase = "agent-lobby"
	AgentCharacterLoading Phase = "agent-character-loading"
	AgentInWorld          Phase = "agent-in-world"
)

type EncryptionRule string

const (
	EncryptionRequired  EncryptionRule = "required"
	EncryptionOptional  EncryptionRule = "optional"
	EncryptionForbidden EncryptionRule = "forbidden"
)

type MassiveRule string

const (
	MassiveAllowed   MassiveRule = "allowed"
	MassiveRequired  MassiveRule = "required"
	MassiveForbidden MassiveRule = "forbidden"
)

type ImplementationStatus string

const (
	StatusUnknown     ImplementationStatus = "unknown"
	StatusEvidenced   ImplementationStatus = "evidenced"
	StatusImplemented ImplementationStatus = "implemented"
	StatusVerified    ImplementationStatus = "verified"
)

type EvidenceKind string

const (
	EvidenceBinary          EvidenceKind = "binary"
	EvidenceCapture         EvidenceKind = "capture"
	EvidenceProtocolCorpus  EvidenceKind = "protocol-corpus"
	EvidenceImplementation  EvidenceKind = "implementation"
	EvidenceSecondarySource EvidenceKind = "secondary-source"
)

// Key is the complete identity of an application packet.
type Key struct {
	Service   Service
	Direction Direction
	Phase     Phase
	Opcode    uint16
}

// Evidence points to a reviewable source without embedding proprietary data.
type Evidence struct {
	Kind       EvidenceKind
	Source     string
	Locator    string
	Confidence string
}

// Entry records the operational metadata required before a packet can be
// promoted through the parity gate. Pair is nil for notifications and for
// exchanges whose counterpart is not yet evidenced.
type Entry struct {
	Key           Key
	CanonicalName string
	Pair          *Key
	Encryption    EncryptionRule
	Massive       MassiveRule
	RateLimit     string
	Evidence      []Evidence
	Status        ImplementationStatus
}

// Registry is immutable after construction and safe for concurrent reads.
type Registry struct {
	entries map[Key]Entry
	ordered []Entry
}

func New(entries []Entry) (*Registry, error) {
	if len(entries) == 0 {
		return nil, errors.New("protocol registry is empty")
	}

	result := &Registry{
		entries: make(map[Key]Entry, len(entries)),
		ordered: make([]Entry, 0, len(entries)),
	}
	for index, entry := range entries {
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("entry %d: %w", index, err)
		}
		if _, duplicate := result.entries[entry.Key]; duplicate {
			return nil, fmt.Errorf("entry %d: duplicate packet identity %s", index, formatKey(entry.Key))
		}
		entry = cloneEntry(entry)
		result.entries[entry.Key] = entry
		result.ordered = append(result.ordered, entry)
	}

	for _, entry := range result.ordered {
		if entry.Pair == nil {
			continue
		}
		paired, exists := result.entries[*entry.Pair]
		if !exists {
			return nil, fmt.Errorf("%s pairs with missing identity %s", formatKey(entry.Key), formatKey(*entry.Pair))
		}
		if paired.Pair == nil || *paired.Pair != entry.Key {
			return nil, fmt.Errorf("%s pair is not reciprocal", formatKey(entry.Key))
		}
	}

	sort.Slice(result.ordered, func(i, j int) bool {
		left, right := result.ordered[i].Key, result.ordered[j].Key
		if left.Service != right.Service {
			return left.Service < right.Service
		}
		if left.Phase != right.Phase {
			return left.Phase < right.Phase
		}
		if left.Direction != right.Direction {
			return left.Direction < right.Direction
		}
		return left.Opcode < right.Opcode
	})
	return result, nil
}

func (r *Registry) Lookup(key Key) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	entry, exists := r.entries[key]
	if !exists {
		return Entry{}, false
	}
	return cloneEntry(entry), true
}

func (r *Registry) Entries() []Entry {
	if r == nil {
		return nil
	}
	entries := make([]Entry, len(r.ordered))
	for index, entry := range r.ordered {
		entries[index] = cloneEntry(entry)
	}
	return entries
}

// Opcodes returns a sorted, de-duplicated allowlist for one exact protocol
// context. It deliberately requires all identity dimensions.
func (r *Registry) Opcodes(service Service, direction Direction, phase Phase) []uint16 {
	if r == nil {
		return nil
	}
	opcodes := make([]uint16, 0)
	for _, entry := range r.ordered {
		if entry.Key.Service == service && entry.Key.Direction == direction && entry.Key.Phase == phase {
			opcodes = append(opcodes, entry.Key.Opcode)
		}
	}
	sort.Slice(opcodes, func(i, j int) bool { return opcodes[i] < opcodes[j] })
	return opcodes
}

func validateEntry(entry Entry) error {
	if !validService(entry.Key.Service) || !validDirection(entry.Key.Direction) || !validPhase(entry.Key.Phase) {
		return errors.New("service, direction, or phase is invalid")
	}
	if entry.Key.Opcode == 0 {
		return errors.New("opcode 0 is not an application packet identity")
	}
	if entry.CanonicalName == "" {
		return errors.New("canonical name is required")
	}
	if !validEncryptionRule(entry.Encryption) || !validMassiveRule(entry.Massive) || entry.RateLimit == "" {
		return errors.New("encryption, massive, and rate-limit rules are required")
	}
	if !validStatus(entry.Status) {
		return errors.New("implementation status is required")
	}
	if len(entry.Evidence) == 0 {
		return errors.New("at least one evidence reference is required")
	}
	for index, evidence := range entry.Evidence {
		if !validEvidenceKind(evidence.Kind) || evidence.Source == "" || evidence.Locator == "" || evidence.Confidence == "" {
			return fmt.Errorf("evidence %d is incomplete", index)
		}
	}
	return nil
}

func cloneEntry(entry Entry) Entry {
	entry.Evidence = append([]Evidence(nil), entry.Evidence...)
	if entry.Pair != nil {
		pair := *entry.Pair
		entry.Pair = &pair
	}
	return entry
}

func validService(value Service) bool {
	return value == ServiceGateway || value == ServiceAgent
}

func validDirection(value Direction) bool {
	return value == ClientToServer || value == ServerToClient
}

func validPhase(value Phase) bool {
	switch value {
	case GatewayPreLogin, AgentAuthentication, AgentLobby, AgentCharacterLoading, AgentInWorld:
		return true
	default:
		return false
	}
}

func validEncryptionRule(value EncryptionRule) bool {
	return value == EncryptionRequired || value == EncryptionOptional || value == EncryptionForbidden
}

func validMassiveRule(value MassiveRule) bool {
	return value == MassiveAllowed || value == MassiveRequired || value == MassiveForbidden
}

func validStatus(value ImplementationStatus) bool {
	return value == StatusUnknown || value == StatusEvidenced || value == StatusImplemented || value == StatusVerified
}

func validEvidenceKind(value EvidenceKind) bool {
	switch value {
	case EvidenceBinary, EvidenceCapture, EvidenceProtocolCorpus, EvidenceImplementation, EvidenceSecondarySource:
		return true
	default:
		return false
	}
}

func formatKey(key Key) string {
	return fmt.Sprintf("(%s,%s,%s,0x%04X)", key.Service, key.Direction, key.Phase, key.Opcode)
}
