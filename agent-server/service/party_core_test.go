package service

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/server"
)

type fakePartyPlayerLookup struct {
	players map[uint32]*model.Player
}

func (f *fakePartyPlayerLookup) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	player := f.players[uid]
	if player == nil {
		return nil, fmt.Errorf("player %d not found", uid)
	}
	return player, nil
}

func newPartyCoreFixture(uids ...uint32) (*PartyService, map[uint32]*model.Player) {
	players := make(map[uint32]*model.Player, len(uids))
	for _, uid := range uids {
		players[uid] = &model.Player{
			SRObject: model.SRObject{UniqueID: uid},
			CharName: fmt.Sprintf("player-%d", uid),
			Session:  &server.Session{},
		}
	}
	return NewPartyService(&fakePartyPlayerLookup{players: players}), players
}

func acceptNewParty(t *testing.T, service *PartyService, inviterUID, targetUID uint32, settings model.PartySetting) PartyTransition {
	t.Helper()
	if _, _, err := service.Invite(inviterUID, targetUID, settings, false); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	transition, err := service.AnswerInvitation(targetUID, true)
	if err != nil {
		t.Fatalf("AnswerInvitation() error = %v", err)
	}
	return transition
}

func TestPartyServiceAcceptNewInvitationAttachesBothExactlyOnce(t *testing.T) {
	service, players := newPartyCoreFixture(10, 20)
	if _, _, err := service.Invite(10, 20, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.AnswerInvitation(20, true)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var accepted, missing int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrPartyInvitationMissing):
			missing++
		default:
			t.Fatalf("AnswerInvitation() unexpected error = %v", err)
		}
	}
	if accepted != 1 || missing != 1 {
		t.Fatalf("concurrent answers: accepted=%d missing=%d", accepted, missing)
	}

	inviterParty := players[10].GetParty()
	if inviterParty == nil || players[20].GetParty() != inviterParty {
		t.Fatalf("accepted players were not attached to the same party")
	}
	if got := inviterParty.Members; len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("party members = %v, want [10 20]", got)
	}
	if service.Parties[inviterParty.Number] != inviterParty {
		t.Fatalf("party %d was not registered", inviterParty.Number)
	}
}

func TestPartyServiceExistingInvitationPermissionsAndCapacity(t *testing.T) {
	t.Run("guest invite denied by settings", func(t *testing.T) {
		service, _ := newPartyCoreFixture(1, 2, 3)
		acceptNewParty(t, service, 1, 2, model.ExpShareItemShareNoInvite)
		if _, _, err := service.Invite(2, 3, model.ExpShareItemShare, true); !errors.Is(err, ErrPartyPermissionDenied) {
			t.Fatalf("guest Invite() error = %v, want %v", err, ErrPartyPermissionDenied)
		}
	})

	t.Run("guest invite allowed", func(t *testing.T) {
		service, players := newPartyCoreFixture(1, 2, 3)
		acceptNewParty(t, service, 1, 2, model.ExpShareItemShare)
		if _, _, err := service.Invite(2, 3, model.ExpDistItemDistNoInvite, true); err != nil {
			t.Fatalf("guest Invite() error = %v", err)
		}
		if _, err := service.AnswerInvitation(3, true); err != nil {
			t.Fatalf("AnswerInvitation() error = %v", err)
		}
		party := players[1].GetParty()
		if players[3].GetParty() != party || len(party.Members) != 3 {
			t.Fatalf("guest invitation did not attach target; members=%v", party.Members)
		}
	})

	t.Run("full party rejected", func(t *testing.T) {
		service, players := newPartyCoreFixture(1, 2, 3, 4, 5)
		transition := acceptNewParty(t, service, 1, 2, model.ExpDistItemDist)
		party := players[1].GetParty()
		for _, uid := range []uint32{3, 4} {
			if err := players[uid].AddToParty(party); err != nil {
				t.Fatalf("AddToParty(%d) error = %v", uid, err)
			}
			party.Members = append(party.Members, uid)
		}
		party.MemberCount = byte(len(party.Members))
		if len(transition.Party.Members) != 2 {
			t.Fatalf("initial snapshot unexpectedly changed: %d members", len(transition.Party.Members))
		}
		if _, _, err := service.Invite(2, 5, model.ExpDistItemDist, true); !errors.Is(err, ErrPartyFull) {
			t.Fatalf("Invite() error = %v, want %v", err, ErrPartyFull)
		}
	})
}

func TestPartyServiceRejectClearsPendingInvitation(t *testing.T) {
	service, _ := newPartyCoreFixture(1, 2)
	if _, _, err := service.Invite(1, 2, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	transition, err := service.AnswerInvitation(2, false)
	if err != nil {
		t.Fatalf("reject error = %v", err)
	}
	if transition.Accepted {
		t.Fatal("rejected invitation reported as accepted")
	}
	if _, err := service.AnswerInvitation(2, true); !errors.Is(err, ErrPartyInvitationMissing) {
		t.Fatalf("second answer error = %v, want %v", err, ErrPartyInvitationMissing)
	}
	if _, _, err := service.Invite(1, 2, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("new Invite() after rejection error = %v", err)
	}
}

func TestPartyServiceLeaveDismissesAndCleansMembership(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2)
	created := acceptNewParty(t, service, 1, 2, model.ExpShareItemShare)
	service.FormedParties[created.Party.Number] = players[1].GetParty()

	transition, err := service.LeaveParty(2, 1)
	if err != nil {
		t.Fatalf("LeaveParty() error = %v", err)
	}
	if !transition.Dismissed || transition.RemovedUID != 2 || transition.RemovalReason != 1 {
		t.Fatalf("leave transition = %+v", transition)
	}
	if players[1].GetParty() != nil || players[2].GetParty() != nil {
		t.Fatalf("dismissed party left membership pointers behind")
	}
	if service.Parties[created.Party.Number] != nil || service.FormedParties[created.Party.Number] != nil {
		t.Fatalf("dismissed party remained registered")
	}
}

func TestPartyServiceLeaderLeaveTransfersLeadership(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2, 3)
	created := acceptNewParty(t, service, 1, 2, model.ExpShareItemShare)
	if _, _, err := service.Invite(1, 3, model.ExpShareItemShare, true); err != nil {
		t.Fatalf("Invite(existing) error = %v", err)
	}
	if _, err := service.AnswerInvitation(3, true); err != nil {
		t.Fatalf("AnswerInvitation(existing) error = %v", err)
	}

	transition, err := service.LeaveParty(1, 1)
	if err != nil {
		t.Fatalf("LeaveParty(leader) error = %v", err)
	}
	if transition.Dismissed || !transition.LeaderChanged || transition.Party.MasterUniqueID != 2 {
		t.Fatalf("leader leave transition = %+v", transition)
	}
	if players[1].GetParty() != nil || players[2].GetParty() != players[3].GetParty() {
		t.Fatalf("membership pointers incorrect after leader transfer")
	}
	if service.Parties[created.Party.Number].MasterUniqueID != 2 {
		t.Fatalf("stored party leader = %d, want 2", service.Parties[created.Party.Number].MasterUniqueID)
	}
}

func TestPartyServiceUnauthorizedKickDoesNotMutateParty(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2, 3)
	acceptNewParty(t, service, 1, 2, model.ExpShareItemShare)
	if _, _, err := service.Invite(1, 3, model.ExpShareItemShare, true); err != nil {
		t.Fatalf("Invite(existing) error = %v", err)
	}
	if _, err := service.AnswerInvitation(3, true); err != nil {
		t.Fatalf("AnswerInvitation(existing) error = %v", err)
	}

	party := players[1].GetParty()
	if _, err := service.KickPartyMember(2, 3); !errors.Is(err, ErrPartyPermissionDenied) {
		t.Fatalf("KickPartyMember() error = %v, want %v", err, ErrPartyPermissionDenied)
	}
	if len(party.Members) != 3 || players[3].GetParty() != party {
		t.Fatalf("unauthorized kick mutated party: members=%v", party.Members)
	}
}

func TestPartyServiceStaleInvitationExpires(t *testing.T) {
	service, _ := newPartyCoreFixture(1, 2)
	now := time.Unix(1_000, 0)
	service.now = func() time.Time { return now }
	if _, _, err := service.Invite(1, 2, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	now = now.Add(partyInvitationTTL)
	if _, err := service.AnswerInvitation(2, true); !errors.Is(err, ErrPartyInvitationMissing) {
		t.Fatalf("stale AnswerInvitation() error = %v, want %v", err, ErrPartyInvitationMissing)
	}
	if _, _, err := service.Invite(1, 2, model.ExpShareItemShare, false); err != nil {
		t.Fatalf("Invite() after expiry error = %v", err)
	}
}
