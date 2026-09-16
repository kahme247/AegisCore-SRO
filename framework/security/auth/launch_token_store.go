package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"sync"
	"time"
)

const (
	DefaultLaunchTokenTTL      = 30 * time.Second
	DefaultLaunchTokenCapacity = 4096
	launchTokenBytes           = 32
)

var (
	ErrLaunchTokenInvalid   = errors.New("invalid launch token")
	ErrLaunchTokenUnknown   = errors.New("unknown launch token")
	ErrLaunchTokenExpired   = errors.New("expired launch token")
	ErrLaunchTokenMismatch  = errors.New("launch token binding mismatch")
	ErrLaunchTokenStoreFull = errors.New("launch token store is full")
)

// LaunchTokenBinding is the authenticated identity and client context carried
// from the launcher to the stock Gateway login. The password is never stored.
type LaunchTokenBinding struct {
	AccountID uint32
	Username  string
	ShardID   uint16
	ContentID byte
	SourceIP  netip.Addr
}

// IssuedLaunchToken is returned only once to the authenticated launcher. The
// store retains a SHA-256 digest rather than the bearer value.
type IssuedLaunchToken struct {
	Value     string
	ExpiresAt time.Time
}

type LaunchTokenStore struct {
	mu         sync.Mutex
	tokens     map[[sha256.Size]byte]storedLaunchToken
	ttl        time.Duration
	capacity   int
	now        func() time.Time
	randomRead func([]byte) (int, error)
}

type storedLaunchToken struct {
	binding   LaunchTokenBinding
	expiresAt time.Time
}

func NewLaunchTokenStore() *LaunchTokenStore {
	return NewLaunchTokenStoreWithOptions(DefaultLaunchTokenTTL, DefaultLaunchTokenCapacity, time.Now)
}

func NewLaunchTokenStoreWithOptions(ttl time.Duration, capacity int, now func() time.Time) *LaunchTokenStore {
	if ttl <= 0 {
		ttl = DefaultLaunchTokenTTL
	}
	if capacity <= 0 {
		capacity = DefaultLaunchTokenCapacity
	}
	if now == nil {
		now = time.Now
	}
	return &LaunchTokenStore{
		tokens:     make(map[[sha256.Size]byte]storedLaunchToken),
		ttl:        ttl,
		capacity:   capacity,
		now:        now,
		randomRead: rand.Read,
	}
}

func (s *LaunchTokenStore) Issue(binding LaunchTokenBinding) (IssuedLaunchToken, error) {
	if s == nil || !validLaunchTokenBinding(binding) {
		return IssuedLaunchToken{}, ErrLaunchTokenInvalid
	}

	raw := make([]byte, launchTokenBytes)
	if n, err := s.randomRead(raw); err != nil || n != len(raw) {
		return IssuedLaunchToken{}, ErrLaunchTokenInvalid
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(value))

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.expireLocked(now)
	if len(s.tokens) >= s.capacity {
		return IssuedLaunchToken{}, ErrLaunchTokenStoreFull
	}
	if _, exists := s.tokens[digest]; exists {
		return IssuedLaunchToken{}, ErrLaunchTokenInvalid
	}
	expiresAt := now.Add(s.ttl)
	s.tokens[digest] = storedLaunchToken{binding: normalizeLaunchTokenBinding(binding), expiresAt: expiresAt}
	return IssuedLaunchToken{Value: value, ExpiresAt: expiresAt}, nil
}

// Redeem consumes a token only after every caller-controlled binding matches.
// Failed binding attempts leave the token available to its intended client.
func (s *LaunchTokenStore) Redeem(value string, presented LaunchTokenBinding) (LaunchTokenBinding, error) {
	if s == nil || value == "" || !validPresentedLaunchTokenBinding(presented) {
		return LaunchTokenBinding{}, ErrLaunchTokenInvalid
	}
	digest := sha256.Sum256([]byte(value))

	s.mu.Lock()
	defer s.mu.Unlock()
	stored, exists := s.tokens[digest]
	if !exists {
		return LaunchTokenBinding{}, ErrLaunchTokenUnknown
	}
	if !s.now().Before(stored.expiresAt) {
		delete(s.tokens, digest)
		return LaunchTokenBinding{}, ErrLaunchTokenExpired
	}
	presented = normalizeLaunchTokenBinding(presented)
	if presented.Username != stored.binding.Username ||
		presented.ShardID != stored.binding.ShardID ||
		presented.ContentID != stored.binding.ContentID ||
		presented.SourceIP != stored.binding.SourceIP {
		return LaunchTokenBinding{}, ErrLaunchTokenMismatch
	}
	delete(s.tokens, digest)
	return stored.binding, nil
}

func (s *LaunchTokenStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	return len(s.tokens)
}

func (s *LaunchTokenStore) expireLocked(now time.Time) {
	for digest, stored := range s.tokens {
		if !now.Before(stored.expiresAt) {
			delete(s.tokens, digest)
		}
	}
}

func validLaunchTokenBinding(binding LaunchTokenBinding) bool {
	return binding.AccountID != 0 && NormalizeUsername(binding.Username) != "" &&
		binding.ShardID != 0 && binding.SourceIP.IsValid()
}

func validPresentedLaunchTokenBinding(binding LaunchTokenBinding) bool {
	return NormalizeUsername(binding.Username) != "" && binding.ShardID != 0 && binding.SourceIP.IsValid()
}

func normalizeLaunchTokenBinding(binding LaunchTokenBinding) LaunchTokenBinding {
	binding.Username = NormalizeUsername(binding.Username)
	binding.SourceIP = binding.SourceIP.Unmap()
	return binding
}
