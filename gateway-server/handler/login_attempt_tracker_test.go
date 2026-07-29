package handler

import (
	"net/netip"
	"testing"
	"time"
)

func TestLoginAttemptTrackerUsesBoundedAccountAndIPWindows(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	tracker := NewLoginAttemptTracker(LoginAttemptTrackerOptions{
		Window:       time.Minute,
		AccountLimit: 2,
		IPLimit:      3,
		Capacity:     16,
		Now:          func() time.Time { return now },
	})
	ip := netip.MustParseAddr("198.51.100.25")

	for want := uint32(1); want <= 2; want++ {
		failure := tracker.RecordFailure("  ALICE ", ip)
		if failure.Code != LoginFailureCounter || failure.CounterLimit != 2 || failure.CounterAttempts != want {
			t.Fatalf("failed attempt %d = %+v, want counter 2/%d", want, failure, want)
		}
	}
	if failure, blocked := tracker.Preflight("alice", ip); !blocked || failure.Code != LoginFailureCounter || failure.CounterAttempts != 2 {
		t.Fatalf("account preflight = (%+v, %t), want bounded counter block", failure, blocked)
	}

	// The third distinct account reaches the IP limit. The next lookup is
	// rejected before touching credentials with the documented IP outcome.
	if failure := tracker.RecordFailure("bob", ip); failure.Code != LoginFailureCounter {
		t.Fatalf("third IP failure = %+v, want account counter", failure)
	}
	if failure, blocked := tracker.Preflight("charlie", ip); !blocked || failure.Code != LoginFailureIPLimit {
		t.Fatalf("IP preflight = (%+v, %t), want IP-limit block", failure, blocked)
	}
}

func TestLoginAttemptTrackerExpiresAndClearsOnlySuccessfulAccount(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	tracker := NewLoginAttemptTracker(LoginAttemptTrackerOptions{
		Window:       time.Minute,
		AccountLimit: 1,
		IPLimit:      4,
		Capacity:     16,
		Now:          func() time.Time { return now },
	})
	ip := netip.MustParseAddr("2001:db8::20")

	tracker.RecordFailure("Alice", ip)
	tracker.RecordFailure("Bob", ip)
	tracker.ClearAccount("alice")
	if _, blocked := tracker.Preflight("alice", ip); blocked {
		t.Fatal("successful account remained rate-limited")
	}
	if failure, blocked := tracker.Preflight("bob", ip); !blocked || failure.Code != LoginFailureCounter {
		t.Fatalf("other account preflight = (%+v, %t), want retained account block", failure, blocked)
	}

	now = now.Add(time.Minute)
	if _, blocked := tracker.Preflight("bob", ip); blocked {
		t.Fatal("expired account window remained rate-limited")
	}
	if got := tracker.Len(); got != 0 {
		t.Fatalf("tracker entries after expiry = %d, want 0", got)
	}
}

func TestLoginAttemptTrackerEvictsToSharedCapacity(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	tracker := NewLoginAttemptTracker(LoginAttemptTrackerOptions{
		Window:       time.Hour,
		AccountLimit: 100,
		IPLimit:      100,
		Capacity:     3,
		Now:          func() time.Time { return now },
	})

	for i, username := range []string{"alice", "bob", "carol", "dave", "erin"} {
		tracker.RecordFailure(username, netip.AddrFrom4([4]byte{198, 51, 100, byte(10 + i)}))
		now = now.Add(time.Second)
		if got := tracker.Len(); got > 3 {
			t.Fatalf("tracker entries = %d, want at most shared capacity 3", got)
		}
	}
}
