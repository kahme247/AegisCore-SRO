package service

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
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

func TestPartyKeepsRuntimeUIDSeparateFromDurableJID(t *testing.T) {
	service, players := newPartyCoreFixture(10, 20, 30)
	players[10].ID = 1001
	players[20].ID = 1002
	players[30].ID = 1003
	created := acceptNewParty(t, service, 10, 20, model.ExpShareItemShare)
	party, err := service.GetParty(created.Party.Number)
	if err != nil {
		t.Fatalf("GetParty() error = %v", err)
	}
	if party.MasterJID != 1001 {
		t.Fatalf("initial master JID = %d, want 1001", party.MasterJID)
	}
	if _, _, err := service.Invite(10, 30, model.ExpShareItemShare, true); err != nil {
		t.Fatalf("Invite(existing) error = %v", err)
	}
	if _, err := service.AnswerInvitation(30, true); err != nil {
		t.Fatalf("AnswerInvitation(existing) error = %v", err)
	}
	if _, err := service.LeaveParty(10, 2); err != nil {
		t.Fatalf("LeaveParty(master) error = %v", err)
	}
	party, err = service.GetParty(created.Party.Number)
	if err != nil {
		t.Fatalf("GetParty() error = %v", err)
	}
	if party.MasterUniqueID != 20 || party.MasterJID != 1002 {
		t.Fatalf("promoted master = uid %d/jid %d, want uid 20/jid 1002", party.MasterUniqueID, party.MasterJID)
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

func TestSharedRewardSharesConservesRemainderInPartyOrder(t *testing.T) {
	for _, test := range []struct {
		reward, recipients uint64
		want               []uint64
	}{
		{0, 3, []uint64{0, 0, 0}},
		{10, 3, []uint64{4, 3, 3}},
		{2, 5, []uint64{1, 1, 0, 0, 0}},
	} {
		got := SharedRewardShares(test.reward, int(test.recipients))
		if fmt.Sprint(got) != fmt.Sprint(test.want) {
			t.Fatalf("SharedRewardShares(%d,%d) = %v, want %v", test.reward, test.recipients, got, test.want)
		}
		var total uint64
		for _, share := range got {
			total += share
		}
		if total != test.reward {
			t.Fatalf("shares total = %d, want %d", total, test.reward)
		}
	}
	if got := SharedRewardShares(10, 0); got != nil {
		t.Fatalf("zero-recipient shares = %v, want nil", got)
	}
}

func TestPartyServiceExperienceRecipientsFiltersEligibilityWithoutChangingOrder(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2, 3, 4, 5)
	acceptNewParty(t, service, 1, 2, model.ExpShareItemShare)
	for _, uid := range []uint32{3, 4, 5} {
		if _, _, err := service.Invite(1, uid, model.ExpShareItemShare, true); err != nil {
			t.Fatalf("Invite(%d) error = %v", uid, err)
		}
		if _, err := service.AnswerInvitation(uid, true); err != nil {
			t.Fatalf("AnswerInvitation(%d) error = %v", uid, err)
		}
	}
	position := func(region int16, x, z float32) navmeshv2.RtNavmeshPosition {
		return navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: region}, Offset: math32.NewVector3(x, 0, z)}
	}
	for _, uid := range []uint32{1, 2, 3, 4, 5} {
		players[uid].SetLifeState(model.Alive)
		players[uid].SetNavmeshPosition(position(1, 0, 0))
	}
	players[3].SetLifeState(model.Dead)
	players[4].SetNavmeshPosition(position(1, PartyExperienceRange+1, 0))
	players[5].SetNavmeshPosition(position(2, 0, 0))

	recipients := service.ExperienceRecipients(1)
	if len(recipients) != 2 || recipients[0] != players[1] || recipients[1] != players[2] {
		t.Fatalf("experience recipients = %v, want [1 2]", []uint32{recipients[0].GetUniqueID(), recipients[1].GetUniqueID()})
	}

	players[1].GetParty().PartySettingsFlag = model.ExpDistItemShare
	recipients = service.ExperienceRecipients(1)
	if len(recipients) != 1 || recipients[0] != players[1] {
		t.Fatalf("non-shared experience recipients = %v, want killer only", recipients)
	}
}

func TestPartyServiceItemRecipientRotatesAndHonorsShareMode(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2)
	transition := acceptNewParty(t, service, 1, 2, model.ExpDistItemDistNoInvite)
	for _, player := range players {
		player.SetLifeState(model.Alive)
	}
	for _, want := range []uint32{1, 2, 1} {
		recipient, shared, err := service.SelectItemRecipient(transition.Party.Number)
		if err != nil || shared || recipient == nil || recipient.GetUniqueID() != want {
			t.Fatalf("item recipient = uid %v shared %v err %v, want uid %d", recipient, shared, err, want)
		}
	}
	players[1].GetParty().PartySettingsFlag = model.ExpDistItemShare
	recipient, shared, err := service.SelectItemRecipient(transition.Party.Number)
	if err != nil || !shared || recipient != nil {
		t.Fatalf("shared item selection = recipient %v shared %v err %v", recipient, shared, err)
	}
	item := &model.Item{OwnerUniqueID: 123}
	if err := service.AssignItemOwner(transition.Party.Number, item); err != nil || item.OwnerUniqueID != 0 {
		t.Fatalf("shared AssignItemOwner() err=%v owner=%d, want open ownership", err, item.OwnerUniqueID)
	}
	players[1].GetParty().PartySettingsFlag = model.ExpDistItemDistNoInvite
	if err := service.AssignItemOwner(transition.Party.Number, item); err != nil || item.OwnerUniqueID != 2 {
		t.Fatalf("distributed AssignItemOwner() err=%v owner=%d, want 2", err, item.OwnerUniqueID)
	}
}

func TestPartyServiceAutoMatchUsesPurposeLevelAndStablePartyOrder(t *testing.T) {
	service, players := newPartyCoreFixture(1, 2, 3, 4)
	players[1].Level, players[3].Level, players[2].Level, players[4].Level = 20, 20, 10, 10
	first := service.FormParty(model.PartyFormRequest{MasterUniqueID: 1, MasterJID: 1, MasterName: "one", PartySettingsFlag: model.ExpShareItemShare, PurposeType: model.QuestingParty, LevelMin: 1, LevelMax: 20, Title: "first"})
	second := service.FormParty(model.PartyFormRequest{MasterUniqueID: 3, MasterJID: 3, MasterName: "three", PartySettingsFlag: model.ExpShareItemShare, PurposeType: model.QuestingParty, LevelMin: 1, LevelMax: 20, Title: "second"})
	if first == 0 || second == 0 || first >= second {
		t.Fatalf("formed party numbers = %d, %d", first, second)
	}
	match, err := service.AutoMatch(2, model.QuestingParty)
	if err != nil || match.Request == nil || match.Master.GetUniqueID() != 1 {
		t.Fatalf("AutoMatch() = %+v, err %v", match, err)
	}
	if _, err := service.AutoMatch(4, model.ThiefParty); !errors.Is(err, ErrPartyAutoMatchMissing) {
		t.Fatalf("incompatible AutoMatch() error = %v", err)
	}
}

func TestPartyMatchingUpdateAndDeleteRequireListingMaster(t *testing.T) {
	service, _ := newPartyCoreFixture(1, 2)
	number := service.FormParty(model.PartyFormRequest{
		MasterUniqueID: 1, MasterJID: 1, MasterName: "one",
		PartySettingsFlag: model.ExpShareItemShare, PurposeType: model.QuestingParty,
		LevelMin: 1, LevelMax: 20, Title: "original",
	})
	if number == 0 {
		t.Fatal("FormParty returned zero")
	}
	changed := model.Party{
		Number: number, MasterUniqueID: 1, PartySettingsFlag: model.ExpDistItemShare,
		PurposeType: model.ThiefParty, LevelMin: 5, LevelMax: 30, Title: "changed",
	}
	if err := service.UpdateMatchingEntry(changed); err != nil {
		t.Fatalf("UpdateMatchingEntry: %v", err)
	}
	party, err := service.GetParty(number)
	if err != nil || party.Title != "changed" || party.LevelMin != 5 || party.PurposeType != model.ThiefParty {
		t.Fatalf("updated party = %+v, err %v", party, err)
	}
	changed.MasterUniqueID = 2
	if err := service.UpdateMatchingEntry(changed); !errors.Is(err, ErrPartyPermissionDenied) {
		t.Fatalf("foreign UpdateMatchingEntry error = %v, want permission denial", err)
	}
	if err := service.DeleteMatchingEntry(number, 2); !errors.Is(err, ErrPartyPermissionDenied) {
		t.Fatalf("foreign DeleteMatchingEntry error = %v, want permission denial", err)
	}
	if err := service.DeleteMatchingEntry(number, 1); err != nil {
		t.Fatalf("DeleteMatchingEntry: %v", err)
	}
	changed.MasterUniqueID = 1
	if err := service.UpdateMatchingEntry(changed); !errors.Is(err, ErrPartyMatchingNotFound) {
		t.Fatalf("update after delete error = %v, want missing listing", err)
	}
}
