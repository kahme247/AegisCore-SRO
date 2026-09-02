// Package auth contains process-local admission controls shared by the
// Gateway and Agent server. It intentionally has no packet dependencies so
// ticket validation remains usable from any transport boundary.
package auth

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// AdmissionTicketBindingVersion identifies the versioned Gateway-to-Agent
	// suffix that carries content and source-IP bindings.
	AdmissionTicketBindingVersion byte = 1

	// DefaultAdmissionTicketTTL is deliberately short: a QueueID is only an
	// admission hand-off, not a reusable session credential.
	DefaultAdmissionTicketTTL = 45 * time.Second

	// DefaultAdmissionTicketCapacity bounds the process-local map when a
	// backend is flooded with otherwise valid ticket installations.
	DefaultAdmissionTicketCapacity = 8192

	maxQueueIDAttempts = 32
)

var (
	ErrAdmissionTicketInvalid   = errors.New("invalid admission ticket")
	ErrAdmissionTicketUnknown   = errors.New("unknown admission ticket")
	ErrAdmissionTicketExpired   = errors.New("expired admission ticket")
	ErrAdmissionTicketMismatch  = errors.New("admission ticket binding mismatch")
	ErrAdmissionTicketCollision = errors.New("admission ticket queue id collision")
	ErrAdmissionTicketStoreFull = errors.New("admission ticket store is full")
	ErrAdmissionTicketPending   = errors.New("admission ticket is not acknowledged")
	// ErrAdmissionTicketAlreadyConnected means Agent has a pending or active
	// admission for the account. Gateway maps only this typed condition to the
	// documented duplicate-login outcome; all other installation failures stay
	// opaque to a public client.
	ErrAdmissionTicketAlreadyConnected = errors.New("admission account is already connected")
)

// AdmissionTicket binds a short-lived Agent admission to the identity that
// Gateway already authenticated. HasContentID and SourceIP are mandatory at
// the issuing and installation boundaries: a valid zero content ID remains
// distinct from an omitted binding, and an unavailable remote address fails
// closed.
//
// Passwords never belong in this type. Gateway authentication is complete
// before a ticket is issued; Agent only parses the stock password field for
// wire compatibility and discards it. TraceID is diagnostic-only correlation
// metadata and is never consulted for ticket identity or admission decisions.
type AdmissionTicket struct {
	QueueID      uint32
	TraceID      string
	AccountID    uint32
	Username     string
	ShardID      uint16
	ContentID    byte
	HasContentID bool
	SourceIP     netip.Addr
	ExpiresAt    time.Time
}

// AdmissionCredentials are the client-controlled fields that can be checked
// while redeeming a ticket. AccountID and ShardID are deliberately supplied by
// the ticket, never trusted from the client packet.
type AdmissionCredentials struct {
	QueueID      uint32
	Username     string
	ContentID    byte
	HasContentID bool
	SourceIP     netip.Addr
}

// AdmissionTicketStore is a bounded, synchronized QueueID-keyed ticket store.
// A ticket is deleted only after a successful binding check, so malformed or
// wrong-credential attempts cannot consume another user's admission.
type AdmissionTicketStore struct {
	mu         sync.Mutex
	tickets    map[uint32]storedAdmissionTicket
	pending    map[uint32]uint32
	active     map[uint32]struct{}
	ttl        time.Duration
	capacity   int
	now        func() time.Time
	randomRead func([]byte) (int, error)
}

type storedAdmissionTicket struct {
	ticket             AdmissionTicket
	normalizedUsername string
	acknowledged       bool
}

// NewAdmissionTicketStore returns the production store configuration.
func NewAdmissionTicketStore() *AdmissionTicketStore {
	return NewAdmissionTicketStoreWithOptions(DefaultAdmissionTicketTTL, DefaultAdmissionTicketCapacity, time.Now)
}

// NewAdmissionTicketStoreWithOptions permits deterministic tests and explicit
// deployment limits. Invalid options fall back to safe defaults.
func NewAdmissionTicketStoreWithOptions(ttl time.Duration, capacity int, now func() time.Time) *AdmissionTicketStore {
	if ttl <= 0 {
		ttl = DefaultAdmissionTicketTTL
	}
	if capacity <= 0 {
		capacity = DefaultAdmissionTicketCapacity
	}
	if now == nil {
		now = time.Now
	}
	return &AdmissionTicketStore{
		tickets:    make(map[uint32]storedAdmissionTicket),
		pending:    make(map[uint32]uint32),
		active:     make(map[uint32]struct{}),
		ttl:        ttl,
		capacity:   capacity,
		now:        now,
		randomRead: cryptorand.Read,
	}
}

// Issue assigns a cryptographically random, nonzero, collision-checked QueueID
// and installs the ticket in this store. Gateway uses this as an issuing
// registry; Agent receives the complete ticket only over its private mTLS
// installation endpoint and installs an equivalent ticket in its own
// process-local store.
func (s *AdmissionTicketStore) Issue(ticket AdmissionTicket) (AdmissionTicket, error) {
	if s == nil {
		return AdmissionTicket{}, ErrAdmissionTicketInvalid
	}
	if err := validateAdmissionTicket(ticket, false); err != nil {
		return AdmissionTicket{}, err
	}

	for attempt := 0; attempt < maxQueueIDAttempts; attempt++ {
		queueID, err := s.nextQueueID()
		if err != nil {
			if errors.Is(err, ErrAdmissionTicketCollision) {
				continue
			}
			return AdmissionTicket{}, err
		}

		s.mu.Lock()
		now := s.now()
		s.expireLocked(now)
		if _, exists := s.tickets[queueID]; exists {
			s.mu.Unlock()
			continue
		}
		if len(s.tickets) >= s.capacity {
			s.mu.Unlock()
			return AdmissionTicket{}, ErrAdmissionTicketStoreFull
		}

		ticket.QueueID = queueID
		ticket.ExpiresAt = now.Add(s.ttl)
		s.tickets[queueID] = storedAdmissionTicket{
			ticket:             cloneTicket(ticket),
			normalizedUsername: NormalizeUsername(ticket.Username),
			acknowledged:       false,
		}
		s.mu.Unlock()
		return ticket, nil
	}

	return AdmissionTicket{}, ErrAdmissionTicketCollision
}

// Install records a ticket received by Agent over the private mTLS endpoint.
// A successful installation is already acknowledged to Agent itself; the
// local expiration is calculated at installation time rather than trusting a
// timestamp carried by Gateway.
func (s *AdmissionTicketStore) Install(ticket AdmissionTicket) error {
	if s == nil {
		return ErrAdmissionTicketInvalid
	}
	if err := validateAdmissionTicket(ticket, true); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.expireLocked(now)
	if _, active := s.active[ticket.AccountID]; active {
		return ErrAdmissionTicketAlreadyConnected
	}
	if _, pending := s.pending[ticket.AccountID]; pending {
		return ErrAdmissionTicketAlreadyConnected
	}
	if len(s.active)+len(s.pending) >= s.capacity {
		return ErrAdmissionTicketStoreFull
	}
	if _, exists := s.tickets[ticket.QueueID]; exists {
		return ErrAdmissionTicketCollision
	}
	if len(s.tickets) >= s.capacity {
		return ErrAdmissionTicketStoreFull
	}
	ticket.ExpiresAt = now.Add(s.ttl)
	s.tickets[ticket.QueueID] = storedAdmissionTicket{
		ticket:             cloneTicket(ticket),
		normalizedUsername: NormalizeUsername(ticket.Username),
		acknowledged:       true,
	}
	s.pending[ticket.AccountID] = ticket.QueueID
	return nil
}

// Acknowledge marks a locally issued ticket as accepted by Agent's private
// mTLS endpoint. Gateway calls this only after it receives Agent's explicit
// installation response and must not serialize a successful A102 beforehand.
func (s *AdmissionTicketStore) Acknowledge(queueID uint32) error {
	if s == nil || queueID == 0 {
		return ErrAdmissionTicketInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, exists := s.tickets[queueID]
	if !exists {
		return ErrAdmissionTicketUnknown
	}
	if !s.now().Before(stored.ticket.ExpiresAt) {
		delete(s.tickets, queueID)
		return ErrAdmissionTicketExpired
	}
	stored.acknowledged = true
	s.tickets[queueID] = stored
	return nil
}

// Redeem atomically validates and consumes one ticket. A missing QueueID or an
// empty username is rejected before map lookup, preventing the old zero-value
// map admission path. Binding failures leave the ticket intact; only a valid
// redemption can consume it.
func (s *AdmissionTicketStore) Redeem(credentials AdmissionCredentials) (AdmissionTicket, error) {
	if s == nil || credentials.QueueID == 0 || NormalizeUsername(credentials.Username) == "" {
		return AdmissionTicket{}, ErrAdmissionTicketInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	stored, exists := s.tickets[credentials.QueueID]
	if !exists {
		return AdmissionTicket{}, ErrAdmissionTicketUnknown
	}
	if !now.Before(stored.ticket.ExpiresAt) {
		s.deleteTicketLocked(credentials.QueueID, stored)
		return AdmissionTicket{}, ErrAdmissionTicketExpired
	}
	if !stored.acknowledged {
		return AdmissionTicket{}, ErrAdmissionTicketPending
	}
	if NormalizeUsername(credentials.Username) != stored.normalizedUsername {
		return AdmissionTicket{}, ErrAdmissionTicketMismatch
	}
	if !credentials.HasContentID || credentials.ContentID != stored.ticket.ContentID {
		return AdmissionTicket{}, ErrAdmissionTicketMismatch
	}
	if !credentials.SourceIP.IsValid() || credentials.SourceIP.Unmap() != stored.ticket.SourceIP {
		return AdmissionTicket{}, ErrAdmissionTicketMismatch
	}

	s.deleteTicketLocked(credentials.QueueID, stored)
	s.active[stored.ticket.AccountID] = struct{}{}
	return cloneTicket(stored.ticket), nil
}

// Expire removes tickets whose TTL has elapsed and returns the number removed.
func (s *AdmissionTicketStore) Expire() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expireLocked(s.now())
}

// Discard removes a locally issued ticket that was never acknowledged by
// Agent. It is deliberately idempotent: an installation timeout can be
// ambiguous, but the Gateway copy must not accumulate while the client was
// never given the QueueID.
func (s *AdmissionTicketStore) Discard(queueID uint32) {
	if s == nil || queueID == 0 {
		return
	}
	s.mu.Lock()
	if stored, exists := s.tickets[queueID]; exists {
		s.deleteTicketLocked(queueID, stored)
	}
	s.mu.Unlock()
}

// ReleaseAccount ends Agent's active-account reservation after the session's
// domain cleanup has drained. It is idempotent so disconnect paths can safely
// invoke it for authenticated and partially joined sessions alike.
func (s *AdmissionTicketStore) ReleaseAccount(accountID uint32) {
	if s == nil || accountID == 0 {
		return
	}
	s.mu.Lock()
	delete(s.active, accountID)
	s.mu.Unlock()
}

// IsAccountActive is a narrow diagnostic/test helper; it never exposes a
// username, QueueID, or ticket material.
func (s *AdmissionTicketStore) IsAccountActive(accountID uint32) bool {
	if s == nil || accountID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, active := s.active[accountID]
	return active
}

// Len returns the current number of non-expired tickets. It is intended for
// diagnostics and tests; ticket contents are never exposed through it.
func (s *AdmissionTicketStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	return len(s.tickets)
}

// NormalizeUsername defines the identity comparison used by admission tickets.
// Preserve the original casing in AdmissionTicket.Username for downstream
// account lookups while preventing case-only replay variants.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// SourceIPFromAddr extracts a canonical source IP from a network address. A
// non-IP or unavailable address returns an invalid netip.Addr and therefore
// does not claim a source-IP binding is available.
func SourceIPFromAddr(address net.Addr) netip.Addr {
	if address == nil {
		return netip.Addr{}
	}
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		if parsed, ok := netip.AddrFromSlice(tcpAddress.IP); ok {
			return parsed.Unmap()
		}
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return netip.Addr{}
	}
	parsed, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return parsed.Unmap()
}

func (s *AdmissionTicketStore) nextQueueID() (uint32, error) {
	var bytes [4]byte
	if s.randomRead == nil {
		return 0, fmt.Errorf("%w: random source unavailable", ErrAdmissionTicketInvalid)
	}
	n, err := s.randomRead(bytes[:])
	if err != nil {
		return 0, fmt.Errorf("read admission ticket randomness: %w", err)
	}
	if n != len(bytes) {
		return 0, fmt.Errorf("read admission ticket randomness: %w", io.ErrUnexpectedEOF)
	}
	queueID := binary.LittleEndian.Uint32(bytes[:])
	if queueID == 0 {
		return 0, ErrAdmissionTicketCollision
	}
	return queueID, nil
}

func (s *AdmissionTicketStore) expireLocked(now time.Time) int {
	removed := 0
	for queueID, stored := range s.tickets {
		if !now.Before(stored.ticket.ExpiresAt) {
			s.deleteTicketLocked(queueID, stored)
			removed++
		}
	}
	return removed
}

func (s *AdmissionTicketStore) deleteTicketLocked(queueID uint32, stored storedAdmissionTicket) {
	delete(s.tickets, queueID)
	if pendingQueueID, pending := s.pending[stored.ticket.AccountID]; pending && pendingQueueID == queueID {
		delete(s.pending, stored.ticket.AccountID)
	}
}

func validateAdmissionTicket(ticket AdmissionTicket, requireQueueID bool) error {
	if (requireQueueID && ticket.QueueID == 0) || ticket.AccountID == 0 || ticket.ShardID == 0 || NormalizeUsername(ticket.Username) == "" || !ticket.HasContentID || !ticket.SourceIP.IsValid() || !validAdmissionTraceID(ticket.TraceID) {
		return ErrAdmissionTicketInvalid
	}
	return nil
}

func validAdmissionTraceID(traceID string) bool {
	if traceID == "" {
		return true
	}
	if len(traceID) > 64 {
		return false
	}
	for _, character := range traceID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func cloneTicket(ticket AdmissionTicket) AdmissionTicket {
	ticket.SourceIP = ticket.SourceIP.Unmap()
	return ticket
}
