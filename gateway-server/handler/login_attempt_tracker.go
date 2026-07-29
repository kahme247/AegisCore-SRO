package handler

import (
	"net/netip"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/framework/security/auth"
)

const (
	// DefaultLoginAttemptWindow bounds how long failed-login state is retained.
	// It intentionally outlives the 45-second admission ticket, but does not
	// persist a remote address or username beyond a short abuse-control window.
	DefaultLoginAttemptWindow = 15 * time.Minute

	// DefaultLoginAccountAttemptLimit preserves the documented four-attempt
	// counter while preventing unbounded per-username state.
	DefaultLoginAccountAttemptLimit uint32 = 4

	// DefaultLoginIPAttemptLimit caps password-spray attempts across distinct
	// account names from one source address during the same window.
	DefaultLoginIPAttemptLimit uint32 = 20

	// DefaultLoginAttemptTrackerCapacity is shared by account and IP entries.
	// Eviction is bounded and deterministic enough for abuse control; it never
	// lets a flood grow process memory without limit.
	DefaultLoginAttemptTrackerCapacity = 4096
)

// LoginAttemptTrackerOptions allows deployment configuration and deterministic
// tests. Invalid values fall back to conservative production defaults.
type LoginAttemptTrackerOptions struct {
	Window       time.Duration
	AccountLimit uint32
	IPLimit      uint32
	Capacity     int
	Now          func() time.Time
}

// LoginAttemptTracker provides bounded, time-windowed failed-login tracking.
// It owns no passwords and retains only normalized account names and canonical
// source IP strings for the configured window.
type LoginAttemptTracker struct {
	mu sync.Mutex

	window       time.Duration
	accountLimit uint32
	ipLimit      uint32
	capacity     int
	now          func() time.Time

	accounts map[string]loginAttemptEntry
	ips      map[string]loginAttemptEntry
}

type loginAttemptEntry struct {
	attempts  uint32
	expiresAt time.Time
	lastSeen  time.Time
}

// NewLoginAttemptTracker returns a bounded tracker with the supplied options.
func NewLoginAttemptTracker(options LoginAttemptTrackerOptions) *LoginAttemptTracker {
	if options.Window <= 0 {
		options.Window = DefaultLoginAttemptWindow
	}
	if options.AccountLimit == 0 {
		options.AccountLimit = DefaultLoginAccountAttemptLimit
	}
	if options.IPLimit == 0 {
		options.IPLimit = DefaultLoginIPAttemptLimit
	}
	if options.Capacity <= 0 {
		options.Capacity = DefaultLoginAttemptTrackerCapacity
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &LoginAttemptTracker{
		window:       options.Window,
		accountLimit: options.AccountLimit,
		ipLimit:      options.IPLimit,
		capacity:     options.Capacity,
		now:          options.Now,
		accounts:     make(map[string]loginAttemptEntry),
		ips:          make(map[string]loginAttemptEntry),
	}
}

// Preflight returns a documented failure before a credential lookup when an
// existing bounded window has reached its account or IP limit. It does not
// mutate state, so a successful login can still clear its account counter.
func (t *LoginAttemptTracker) Preflight(username string, sourceIP netip.Addr) (LoginFailure, bool) {
	if t == nil {
		return LoginFailure{}, false
	}
	now := t.now()
	accountKey := auth.NormalizeUsername(username)
	ipKey := loginAttemptIPKey(sourceIP)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(now)
	if ipKey != "" {
		if entry, ok := t.ips[ipKey]; ok && entry.attempts >= t.ipLimit {
			return LoginFailure{Code: LoginFailureIPLimit}, true
		}
	}
	if accountKey != "" {
		if entry, ok := t.accounts[accountKey]; ok && entry.attempts >= t.accountLimit {
			return LoginFailure{
				Code:            LoginFailureCounter,
				CounterLimit:    t.accountLimit,
				CounterAttempts: entry.attempts,
			}, true
		}
	}
	return LoginFailure{}, false
}

// RecordFailure records one failed credential lookup and returns the
// corresponding stock failure. Empty usernames are never entered into the
// account map; a valid source IP is still rate limited to contain sprays.
func (t *LoginAttemptTracker) RecordFailure(username string, sourceIP netip.Addr) LoginFailure {
	if t == nil {
		return LoginFailure{Code: LoginFailureCounter, CounterLimit: defaultLoginAttemptLimit}
	}
	now := t.now()
	accountKey := auth.NormalizeUsername(username)
	ipKey := loginAttemptIPKey(sourceIP)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(now)

	if ipKey != "" {
		entry := t.incrementLocked(t.ips, ipKey, now)
		if entry.attempts > t.ipLimit {
			return LoginFailure{Code: LoginFailureIPLimit}
		}
	}
	if accountKey == "" {
		return LoginFailure{Code: LoginFailureCounter, CounterLimit: t.accountLimit}
	}
	entry := t.incrementLocked(t.accounts, accountKey, now)
	if entry.attempts > t.accountLimit {
		return LoginFailure{Code: LoginFailureCounter, CounterLimit: t.accountLimit, CounterAttempts: t.accountLimit}
	}
	return LoginFailure{Code: LoginFailureCounter, CounterLimit: t.accountLimit, CounterAttempts: entry.attempts}
}

// ClearAccount clears only the successful account's counter. IP failures are
// deliberately retained so one valid password does not reset a spray window.
func (t *LoginAttemptTracker) ClearAccount(username string) {
	if t == nil {
		return
	}
	key := auth.NormalizeUsername(username)
	if key == "" {
		return
	}
	t.mu.Lock()
	delete(t.accounts, key)
	t.mu.Unlock()
}

// Len is a diagnostic/test helper. It returns the total bounded entry count
// after expiring stale state.
func (t *LoginAttemptTracker) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(t.now())
	return len(t.accounts) + len(t.ips)
}

func (t *LoginAttemptTracker) incrementLocked(entries map[string]loginAttemptEntry, key string, now time.Time) loginAttemptEntry {
	entry, exists := entries[key]
	if !exists {
		t.ensureCapacityLocked(now)
		entry = loginAttemptEntry{}
	}
	entry.attempts++
	entry.lastSeen = now
	entry.expiresAt = now.Add(t.window)
	entries[key] = entry
	return entry
}

func (t *LoginAttemptTracker) ensureCapacityLocked(now time.Time) {
	t.expireLocked(now)
	if len(t.accounts)+len(t.ips) < t.capacity {
		return
	}

	// Drop the oldest observed entry. This deliberately favors recent abuse
	// signals and keeps the process bounded even under many unique usernames.
	oldestKey := ""
	oldestIsAccount := false
	var oldest time.Time
	for key, entry := range t.accounts {
		if oldestKey == "" || entry.lastSeen.Before(oldest) {
			oldestKey, oldestIsAccount, oldest = key, true, entry.lastSeen
		}
	}
	for key, entry := range t.ips {
		if oldestKey == "" || entry.lastSeen.Before(oldest) {
			oldestKey, oldestIsAccount, oldest = key, false, entry.lastSeen
		}
	}
	if oldestIsAccount {
		delete(t.accounts, oldestKey)
	} else if oldestKey != "" {
		delete(t.ips, oldestKey)
	}
}

func (t *LoginAttemptTracker) expireLocked(now time.Time) {
	for key, entry := range t.accounts {
		if !now.Before(entry.expiresAt) {
			delete(t.accounts, key)
		}
	}
	for key, entry := range t.ips {
		if !now.Before(entry.expiresAt) {
			delete(t.ips, key)
		}
	}
}

func loginAttemptIPKey(sourceIP netip.Addr) string {
	if !sourceIP.IsValid() {
		return ""
	}
	return sourceIP.Unmap().String()
}
