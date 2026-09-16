package auth

import (
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestLaunchTokenStoreIssuesAndRedeemsOnce(t *testing.T) {
	store := NewLaunchTokenStore()
	binding := launchTokenTestBinding()
	issued, err := store.Issue(binding)
	if err != nil {
		t.Fatalf("issue launch token: %v", err)
	}
	if len(issued.Value) != 43 {
		t.Fatalf("token length = %d, want 43", len(issued.Value))
	}

	redeemed, err := store.Redeem(issued.Value, binding)
	if err != nil {
		t.Fatalf("redeem launch token: %v", err)
	}
	if redeemed.AccountID != binding.AccountID || redeemed.Username != "alice" {
		t.Fatalf("redeemed binding = %+v", redeemed)
	}
	if _, err := store.Redeem(issued.Value, binding); !errors.Is(err, ErrLaunchTokenUnknown) {
		t.Fatalf("second redemption error = %v, want unknown", err)
	}
}

func TestLaunchTokenStoreBindingFailureDoesNotConsumeToken(t *testing.T) {
	store := NewLaunchTokenStore()
	binding := launchTokenTestBinding()
	issued, err := store.Issue(binding)
	if err != nil {
		t.Fatalf("issue launch token: %v", err)
	}

	wrong := binding
	wrong.SourceIP = netip.MustParseAddr("203.0.113.11")
	if _, err := store.Redeem(issued.Value, wrong); !errors.Is(err, ErrLaunchTokenMismatch) {
		t.Fatalf("wrong binding error = %v, want mismatch", err)
	}
	if _, err := store.Redeem(issued.Value, binding); err != nil {
		t.Fatalf("intended client could not redeem after mismatch: %v", err)
	}
}

func TestLaunchTokenStoreExpiresAndBoundsEntries(t *testing.T) {
	now := time.Unix(100, 0)
	store := NewLaunchTokenStoreWithOptions(time.Second, 1, func() time.Time { return now })
	binding := launchTokenTestBinding()
	issued, err := store.Issue(binding)
	if err != nil {
		t.Fatalf("issue launch token: %v", err)
	}
	if _, err := store.Issue(binding); !errors.Is(err, ErrLaunchTokenStoreFull) {
		t.Fatalf("capacity error = %v, want store full", err)
	}
	now = now.Add(time.Second)
	if _, err := store.Redeem(issued.Value, binding); !errors.Is(err, ErrLaunchTokenExpired) {
		t.Fatalf("expiry error = %v, want expired", err)
	}
	if _, err := store.Issue(binding); err != nil {
		t.Fatalf("issue after expiry: %v", err)
	}
}

func launchTokenTestBinding() LaunchTokenBinding {
	return LaunchTokenBinding{
		AccountID: 42,
		Username:  " Alice ",
		ShardID:   1,
		ContentID: 22,
		SourceIP:  netip.MustParseAddr("203.0.113.10"),
	}
}
