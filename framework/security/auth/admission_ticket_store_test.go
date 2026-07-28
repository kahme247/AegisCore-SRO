package auth

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testAdmissionSourceIP = netip.MustParseAddr("203.0.113.7")

func TestAdmissionTicketStoreRejectsUnknownAndZeroValueAdmissions(t *testing.T) {
	store := NewAdmissionTicketStore()
	if _, err := store.Redeem(AdmissionCredentials{QueueID: 777, Username: "alice"}); !errors.Is(err, ErrAdmissionTicketUnknown) {
		t.Fatalf("Redeem(unknown) error = %v, want ErrAdmissionTicketUnknown", err)
	}
	if _, err := store.Redeem(AdmissionCredentials{}); !errors.Is(err, ErrAdmissionTicketInvalid) {
		t.Fatalf("Redeem(zero value) error = %v, want ErrAdmissionTicketInvalid", err)
	}
}

func TestAdmissionTicketStoreRedeemsOnlyMatchingTicketOnce(t *testing.T) {
	store := NewAdmissionTicketStore()
	ticket := AdmissionTicket{
		QueueID:      0x10203040,
		AccountID:    77,
		Username:     "Alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     testAdmissionSourceIP,
	}
	if err := store.Install(ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	wrongCredentials := AdmissionCredentials{
		QueueID:      ticket.QueueID,
		Username:     "Mallory",
		ContentID:    ticket.ContentID,
		HasContentID: true,
		SourceIP:     ticket.SourceIP,
	}
	if _, err := store.Redeem(wrongCredentials); !errors.Is(err, ErrAdmissionTicketMismatch) {
		t.Fatalf("Redeem(wrong username) error = %v, want ErrAdmissionTicketMismatch", err)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("wrong credentials consumed ticket: store length = %d, want 1", got)
	}

	wrongContent := wrongCredentials
	wrongContent.Username = "alice"
	wrongContent.ContentID++
	if _, err := store.Redeem(wrongContent); !errors.Is(err, ErrAdmissionTicketMismatch) {
		t.Fatalf("Redeem(wrong content ID) error = %v, want ErrAdmissionTicketMismatch", err)
	}

	wrongIP := wrongCredentials
	wrongIP.Username = "alice"
	wrongIP.SourceIP = netip.MustParseAddr("203.0.113.8")
	if _, err := store.Redeem(wrongIP); !errors.Is(err, ErrAdmissionTicketMismatch) {
		t.Fatalf("Redeem(wrong source IP) error = %v, want ErrAdmissionTicketMismatch", err)
	}

	redeemed, err := store.Redeem(AdmissionCredentials{
		QueueID:      ticket.QueueID,
		Username:     " alice ",
		ContentID:    ticket.ContentID,
		HasContentID: true,
		SourceIP:     ticket.SourceIP,
	})
	if err != nil {
		t.Fatalf("Redeem(valid ticket) error = %v", err)
	}
	if got, want := redeemed.AccountID, ticket.AccountID; got != want {
		t.Fatalf("redeemed account ID = %d, want %d", got, want)
	}
	if got, want := redeemed.ShardID, ticket.ShardID; got != want {
		t.Fatalf("redeemed shard ID = %d, want %d", got, want)
	}
	if _, err := store.Redeem(AdmissionCredentials{QueueID: ticket.QueueID, Username: ticket.Username}); !errors.Is(err, ErrAdmissionTicketUnknown) {
		t.Fatalf("Redeem(replay) error = %v, want ErrAdmissionTicketUnknown", err)
	}
}

func TestAdmissionTicketStoreExpiresTickets(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	store := NewAdmissionTicketStoreWithOptions(45*time.Second, 8, func() time.Time { return now })
	ticket := AdmissionTicket{
		QueueID:      9,
		AccountID:    8,
		Username:     "alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     testAdmissionSourceIP,
	}
	if err := store.Install(ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	now = now.Add(45 * time.Second)
	if _, err := store.Redeem(AdmissionCredentials{QueueID: ticket.QueueID, Username: ticket.Username, ContentID: ticket.ContentID, HasContentID: true, SourceIP: ticket.SourceIP}); !errors.Is(err, ErrAdmissionTicketExpired) {
		t.Fatalf("Redeem(expired) error = %v, want ErrAdmissionTicketExpired", err)
	}
}

func TestAdmissionTicketStoreIssueUsesNonzeroQueueIDsAndAtomicRedeem(t *testing.T) {
	store := NewAdmissionTicketStore()
	ticket, err := store.Issue(AdmissionTicket{AccountID: 11, Username: "alice", ShardID: 1, ContentID: 56, HasContentID: true, SourceIP: testAdmissionSourceIP})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if ticket.QueueID == 0 {
		t.Fatal("Issue() returned a zero QueueID")
	}
	if _, err := store.Redeem(AdmissionCredentials{QueueID: ticket.QueueID, Username: ticket.Username, ContentID: ticket.ContentID, HasContentID: true, SourceIP: ticket.SourceIP}); !errors.Is(err, ErrAdmissionTicketPending) {
		t.Fatalf("Redeem(unacknowledged ticket) error = %v, want ErrAdmissionTicketPending", err)
	}
	if err := store.Acknowledge(ticket.QueueID); err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Redeem(AdmissionCredentials{QueueID: ticket.QueueID, Username: ticket.Username, ContentID: ticket.ContentID, HasContentID: true, SourceIP: ticket.SourceIP}); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("concurrent successful redemptions = %d, want 1", got)
	}
}

func TestAdmissionTicketStoreIssueSkipsZeroAndCollidingQueueIDs(t *testing.T) {
	store := NewAdmissionTicketStore()
	if err := store.Install(AdmissionTicket{QueueID: 7, AccountID: 1, Username: "existing", ShardID: 1, ContentID: 56, HasContentID: true, SourceIP: testAdmissionSourceIP}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	queueIDs := []uint32{0, 7, 9}
	store.randomRead = func(bytes []byte) (int, error) {
		binary.LittleEndian.PutUint32(bytes, queueIDs[0])
		queueIDs = queueIDs[1:]
		return len(bytes), nil
	}

	ticket, err := store.Issue(AdmissionTicket{AccountID: 2, Username: "alice", ShardID: 1, ContentID: 56, HasContentID: true, SourceIP: testAdmissionSourceIP})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if got, want := ticket.QueueID, uint32(9); got != want {
		t.Fatalf("Issue() QueueID = %d, want %d after zero and collision retries", got, want)
	}
}

func TestAdmissionTicketStoreRejectsTicketsWithoutRequiredBindings(t *testing.T) {
	store := NewAdmissionTicketStore()
	if _, err := store.Issue(AdmissionTicket{AccountID: 1, Username: "alice", ShardID: 1, ContentID: 56, HasContentID: true}); !errors.Is(err, ErrAdmissionTicketInvalid) {
		t.Fatalf("Issue(without source IP) error = %v, want ErrAdmissionTicketInvalid", err)
	}
	if err := store.Install(AdmissionTicket{QueueID: 1, AccountID: 1, Username: "alice", ShardID: 1, SourceIP: testAdmissionSourceIP}); !errors.Is(err, ErrAdmissionTicketInvalid) {
		t.Fatalf("Install(without content binding) error = %v, want ErrAdmissionTicketInvalid", err)
	}
}

func TestAdmissionTicketStoreReservesPendingAndActiveAccountsUntilRelease(t *testing.T) {
	store := NewAdmissionTicketStore()
	first := AdmissionTicket{
		QueueID:      100,
		AccountID:    77,
		Username:     "alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     testAdmissionSourceIP,
	}
	if err := store.Install(first); err != nil {
		t.Fatalf("Install(first) error = %v", err)
	}
	second := first
	second.QueueID = 101
	if err := store.Install(second); !errors.Is(err, ErrAdmissionTicketAlreadyConnected) {
		t.Fatalf("Install(duplicate pending account) error = %v, want ErrAdmissionTicketAlreadyConnected", err)
	}
	if _, err := store.Redeem(AdmissionCredentials{
		QueueID:      first.QueueID,
		Username:     first.Username,
		ContentID:    first.ContentID,
		HasContentID: true,
		SourceIP:     first.SourceIP,
	}); err != nil {
		t.Fatalf("Redeem(first) error = %v", err)
	}
	if !store.IsAccountActive(first.AccountID) {
		t.Fatal("redeemed account was not reserved as active")
	}
	if err := store.Install(second); !errors.Is(err, ErrAdmissionTicketAlreadyConnected) {
		t.Fatalf("Install(duplicate active account) error = %v, want ErrAdmissionTicketAlreadyConnected", err)
	}
	store.ReleaseAccount(first.AccountID)
	if store.IsAccountActive(first.AccountID) {
		t.Fatal("ReleaseAccount left the account active")
	}
	if err := store.Install(second); err != nil {
		t.Fatalf("Install(after release) error = %v", err)
	}
}
