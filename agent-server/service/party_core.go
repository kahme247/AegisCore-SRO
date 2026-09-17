package service

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
)

var (
	ErrPartyInvalidRequest    = errors.New("invalid party request")
	ErrPartyAlreadyMember     = errors.New("player is already in a party")
	ErrPartyTargetUnavailable = errors.New("party target is unavailable")
	ErrPartyInvitationPending = errors.New("party target already has a pending invitation")
	ErrPartyInvitationMissing = errors.New("party invitation does not exist")
	ErrPartyPermissionDenied  = errors.New("party permission denied")
	ErrPartyFull              = errors.New("party is full")
	ErrPartyItemUnavailable   = errors.New("no connected party member can receive the item")
	ErrPartyAutoMatchMissing  = errors.New("no compatible automatic party match")
	ErrPartyMatchingNotFound  = errors.New("party matching entry was not found")
)

const partyInvitationTTL = 10 * time.Second

// PartyExperienceRange is the maximum horizontal distance between the killer
// and a living party member for a shared EXP award. The stock party system
// evaluates proximity in the world; keeping the rule in the service makes the
// combat path independent of packet handlers.
const PartyExperienceRange = float32(100)

// PartyTransition is an immutable description of a completed party mutation.
// Network sends happen after the service lock is released.
type PartyTransition struct {
	Invitation    PartyInvitation
	Party         PartySnapshot
	Inviter       *model.Player
	Target        *model.Player
	Existing      []*model.Player
	AllBefore     []*model.Player
	Accepted      bool
	Dismissed     bool
	LeaderChanged bool
	RemovedUID    uint32
	RemovalReason byte
}

type PartySnapshot struct {
	Number         uint32
	MasterUniqueID uint32
	Settings       model.PartySetting
	Members        []*model.Player
}

// AutoMatchResult is the server-side result of automatic party matching. The
// request is still answered by the party master, just like a manual matching
// request; automatic matching only chooses the first compatible listing.
type AutoMatchResult struct {
	Request   *model.PartyJoinRequest
	Master    *model.Player
	Applicant *model.Player
}

func NewPartyService(players PartyPlayerLookup) *PartyService {
	return &PartyService{
		Parties:            make(map[uint32]*model.Party),
		FormedParties:      make(map[uint32]*model.Party),
		JoinRequests:       make(map[uint32]*model.PartyJoinRequest),
		partyNumberCounter: 1,
		joinRequestCounter: 1,
		pendingInvites:     make(map[uint32]PartyInvitation),
		itemCursors:        make(map[uint32]int),
		players:            players,
		now:                time.Now,
	}
}

// SelectItemRecipient applies the deterministic party-distribution rule. In
// item-share mode the caller leaves ownership open (Shared is true); in item-
// distribution mode the next connected member is selected in party order.
// The cursor is advanced only after a recipient is found, so disconnected or
// full members do not consume a turn.
func (p *PartyService) SelectItemRecipient(partyNumber uint32) (recipient *model.Player, shared bool, err error) {
	if p == nil || partyNumber == 0 {
		return nil, false, ErrPartyInvalidRequest
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	party := p.Parties[partyNumber]
	if party == nil || len(party.Members) == 0 {
		return nil, false, ErrPartyInvalidRequest
	}
	if p.itemCursors == nil {
		p.itemCursors = make(map[uint32]int)
	}
	if party.PartySettingsFlag.IsSharingItem() {
		return nil, true, nil
	}
	start := p.itemCursors[partyNumber] % len(party.Members)
	for offset := 0; offset < len(party.Members); offset++ {
		index := (start + offset) % len(party.Members)
		candidate, lookupErr := p.player(party.Members[index])
		if lookupErr != nil || candidate.GetSession() == nil || candidate.GetLifeState() != model.Alive {
			continue
		}
		p.itemCursors[partyNumber] = (index + 1) % len(party.Members)
		return candidate, false, nil
	}
	return nil, false, ErrPartyItemUnavailable
}

// AssignItemOwner applies the selected party policy to a newly materialized
// ground item. The drop creator can call this before registering the item in
// the world; a shared party leaves OwnerUniqueID zero, while distribution mode
// records the selected member for pickup validation.
func (p *PartyService) AssignItemOwner(partyNumber uint32, item *model.Item) error {
	if item == nil {
		return ErrPartyInvalidRequest
	}
	recipient, shared, err := p.SelectItemRecipient(partyNumber)
	if err != nil {
		return err
	}
	if shared {
		item.OwnerUniqueID = 0
		return nil
	}
	if recipient == nil {
		return ErrPartyItemUnavailable
	}
	item.OwnerUniqueID = recipient.GetUniqueID()
	return nil
}

// ConnectedMembers returns the live members of a party in party order. It is
// used by the ground-item lifecycle to fan out the binary-confirmed 0x3068
// distribution notification without exposing the mutable member slice.
func (p *PartyService) ConnectedMembers(partyNumber uint32) []*model.Player {
	if p == nil || partyNumber == 0 {
		return nil
	}
	p.mutex.Lock()
	party := p.Parties[partyNumber]
	if party == nil {
		p.mutex.Unlock()
		return nil
	}
	uids := append([]uint32(nil), party.Members...)
	p.mutex.Unlock()

	members := make([]*model.Player, 0, len(uids))
	for _, uid := range uids {
		member, err := p.player(uid)
		if err == nil && member != nil {
			members = append(members, member)
		}
	}
	return members
}

// AutoMatch selects a formed listing by purpose and level. Ordering by party
// number makes simultaneous requests deterministic and keeps the method easy
// to replace with the binary's asynchronous ShardManager job when that path is
// recovered.
func (p *PartyService) AutoMatch(applicantUID uint32, purpose model.PartyPurpose) (AutoMatchResult, error) {
	applicant, err := p.player(applicantUID)
	if err != nil {
		return AutoMatchResult{}, err
	}
	if applicant.GetParty() != nil {
		return AutoMatchResult{}, ErrPartyAlreadyMember
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.purgeExpiredMatchingRequestsLocked()
	ids := make([]uint32, 0, len(p.FormedParties))
	for number := range p.FormedParties {
		ids = append(ids, number)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, number := range ids {
		party := p.FormedParties[number]
		if party == nil || party.PurposeType != purpose || len(party.Members) >= party.PartySettingsFlag.MaxMembers() ||
			int(applicant.Level) < int(party.LevelMin) || int(applicant.Level) > int(party.LevelMax) {
			continue
		}
		master, lookupErr := p.player(party.MasterUniqueID)
		if lookupErr != nil || master.GetSession() == nil {
			continue
		}
		for _, pending := range p.JoinRequests {
			if pending.PlayerUniqueID == applicantUID && pending.PartyNumber == number && p.nowTime().Sub(pending.CreatedAt) < p.invitationTTL() {
				return AutoMatchResult{}, ErrPartyInvitationPending
			}
		}
		request := &model.PartyJoinRequest{
			PartyNumber: number, RequestID: p.nextJoinRequestLocked(), PlayerJID: applicant.ID,
			PlayerUniqueID: applicantUID, CreatedAt: p.nowTime(), Mutex: &sync.Mutex{},
		}
		p.JoinRequests[request.RequestID] = request
		return AutoMatchResult{Request: request, Master: master, Applicant: applicant}, nil
	}
	return AutoMatchResult{}, ErrPartyAutoMatchMissing
}

// ExperienceRecipients returns a stable, party-order list for a shared EXP
// award. The killer is always retained when they are in a party. Members in a
// different region, outside the party range, offline, or dead do not receive
// the award.
func (p *PartyService) ExperienceRecipients(uid uint32) []*model.Player {
	killer, err := p.player(uid)
	if err != nil {
		return nil
	}
	party := killer.GetParty()
	if party == nil {
		return []*model.Player{killer}
	}

	p.mutex.Lock()
	members := append([]uint32(nil), party.Members...)
	shared := party.PartySettingsFlag.IsSharingExp()
	p.mutex.Unlock()
	if !shared {
		return []*model.Player{killer}
	}

	anchor := killer.GetNavmeshPosition()
	recipients := make([]*model.Player, 0, len(members))
	for _, memberUID := range members {
		member, lookupErr := p.player(memberUID)
		if lookupErr != nil || member.GetLifeState() != model.Alive || member.GetSession() == nil {
			continue
		}
		position := member.GetNavmeshPosition()
		if position.Region.ID != anchor.Region.ID || navmeshv2.HorizontalDistance(position, anchor) > p.experienceRange() {
			continue
		}
		recipients = append(recipients, member)
	}
	if len(recipients) == 0 {
		return []*model.Player{killer}
	}
	return recipients
}

// SharedRewardShares conserves the complete reward while distributing the
// integer remainder in stable party order. This keeps one EXP unit from
// disappearing when a reward is not divisible by the recipient count.
func SharedRewardShares(reward uint64, recipients int) []uint64 {
	if recipients <= 0 {
		return nil
	}
	shares := make([]uint64, recipients)
	base, remainder := reward/uint64(recipients), reward%uint64(recipients)
	for index := range shares {
		shares[index] = base
		if uint64(index) < remainder {
			shares[index]++
		}
	}
	return shares
}

func (p *PartyService) player(uid uint32) (*model.Player, error) {
	if p == nil || p.players == nil || uid == 0 {
		return nil, ErrPartyTargetUnavailable
	}
	player, err := p.players.GetPlayerByUniqueId(uid)
	if err != nil || player == nil || player.GetSession() == nil {
		return nil, ErrPartyTargetUnavailable
	}
	return player, nil
}

func (p *PartyService) nowTime() time.Time {
	if p != nil && p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *PartyService) Invite(inviterUID, targetUID uint32, settings model.PartySetting, existing bool) (PartyInvitation, *model.Player, error) {
	if p == nil || inviterUID == 0 || targetUID == 0 || inviterUID == targetUID || settings > 7 {
		return PartyInvitation{}, nil, ErrPartyInvalidRequest
	}
	inviter, err := p.player(inviterUID)
	if err != nil {
		return PartyInvitation{}, nil, err
	}
	target, err := p.player(targetUID)
	if err != nil {
		return PartyInvitation{}, nil, err
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()
	if target.GetParty() != nil {
		return PartyInvitation{}, nil, ErrPartyAlreadyMember
	}
	now := p.nowTime()
	if pending, ok := p.pendingInvites[targetUID]; ok {
		if now.Sub(pending.CreatedAt) < p.invitationTTL() {
			return PartyInvitation{}, nil, ErrPartyInvitationPending
		}
		delete(p.pendingInvites, targetUID)
	}

	party := inviter.GetParty()
	partyNumber := uint32(0)
	if existing {
		if party == nil || !containsPartyMember(party.Members, inviterUID) {
			return PartyInvitation{}, nil, ErrPartyPermissionDenied
		}
		if party.MasterUniqueID != inviterUID && !party.PartySettingsFlag.HasGuestInvite() {
			return PartyInvitation{}, nil, ErrPartyPermissionDenied
		}
		if len(party.Members) >= party.PartySettingsFlag.MaxMembers() {
			return PartyInvitation{}, nil, ErrPartyFull
		}
		settings = party.PartySettingsFlag
		partyNumber = party.Number
	} else if party != nil {
		return PartyInvitation{}, nil, ErrPartyAlreadyMember
	}

	invite := PartyInvitation{InviterUniqueID: inviterUID, TargetUniqueID: targetUID, PartyNumber: partyNumber, Settings: settings, CreatedAt: now}
	p.pendingInvites[targetUID] = invite
	return invite, target, nil
}

func (p *PartyService) AnswerInvitation(targetUID uint32, accept bool) (PartyTransition, error) {
	if p == nil || targetUID == 0 {
		return PartyTransition{}, ErrPartyInvalidRequest
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	invite, ok := p.pendingInvites[targetUID]
	if !ok || p.nowTime().Sub(invite.CreatedAt) >= p.invitationTTL() {
		delete(p.pendingInvites, targetUID)
		return PartyTransition{}, ErrPartyInvitationMissing
	}
	delete(p.pendingInvites, targetUID)

	inviter, err := p.player(invite.InviterUniqueID)
	if err != nil {
		return PartyTransition{}, err
	}
	target, err := p.player(targetUID)
	if err != nil {
		return PartyTransition{}, err
	}
	transition := PartyTransition{Invitation: invite, Inviter: inviter, Target: target, Accepted: accept}
	if !accept {
		return transition, nil
	}
	if target.GetParty() != nil {
		return PartyTransition{}, ErrPartyAlreadyMember
	}

	var party *model.Party
	if invite.PartyNumber == 0 {
		if inviter.GetParty() != nil {
			return PartyTransition{}, ErrPartyAlreadyMember
		}
		party = &model.Party{
			Number: p.nextPartyNumberLocked(), MasterUniqueID: inviter.GetUniqueID(), MasterJID: uint32(inviter.ID),
			MasterName: inviter.GetName(), MemberCount: 2, PartySettingsFlag: invite.Settings,
			Members: []uint32{inviter.GetUniqueID(), target.GetUniqueID()},
		}
		if err = inviter.AddToParty(party); err != nil {
			return PartyTransition{}, err
		}
		if err = target.AddToParty(party); err != nil {
			inviter.RemoveFromPartyIf(party)
			return PartyTransition{}, err
		}
		p.Parties[party.Number] = party
		transition.Existing = []*model.Player{inviter}
	} else {
		party = p.Parties[invite.PartyNumber]
		if party == nil || inviter.GetParty() != party || !containsPartyMember(party.Members, inviter.GetUniqueID()) {
			return PartyTransition{}, ErrPartyPermissionDenied
		}
		if len(party.Members) >= party.PartySettingsFlag.MaxMembers() {
			return PartyTransition{}, ErrPartyFull
		}
		transition.Existing = p.playersForMembersLocked(party.Members)
		if err = target.AddToParty(party); err != nil {
			return PartyTransition{}, err
		}
		party.Members = append(party.Members, target.GetUniqueID())
		party.MemberCount = byte(len(party.Members))
	}
	transition.Party = p.snapshotLocked(party)
	return transition, nil
}

func (p *PartyService) CreateMatchingJoinRequest(partyNumber, applicantUID uint32) (*model.PartyJoinRequest, *model.Player, *model.Player, error) {
	applicant, err := p.player(applicantUID)
	if err != nil {
		return nil, nil, nil, err
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.purgeExpiredMatchingRequestsLocked()
	party := p.FormedParties[partyNumber]
	if party == nil || applicant.GetParty() != nil || len(party.Members) >= party.PartySettingsFlag.MaxMembers() {
		return nil, nil, nil, ErrPartyInvalidRequest
	}
	if applicant.Level < int(party.LevelMin) || applicant.Level > int(party.LevelMax) {
		return nil, nil, nil, ErrPartyInvalidRequest
	}
	for _, pending := range p.JoinRequests {
		if pending.PlayerUniqueID == applicantUID && p.nowTime().Sub(pending.CreatedAt) < p.invitationTTL() {
			return nil, nil, nil, ErrPartyInvitationPending
		}
	}
	master, err := p.player(party.MasterUniqueID)
	if err != nil {
		return nil, nil, nil, err
	}
	request := &model.PartyJoinRequest{
		PartyNumber: partyNumber, RequestID: p.nextJoinRequestLocked(), PlayerJID: applicant.ID,
		PlayerUniqueID: applicantUID, CreatedAt: p.nowTime(), Mutex: &sync.Mutex{},
	}
	p.JoinRequests[request.RequestID] = request
	return request, master, applicant, nil
}

func (p *PartyService) purgeExpiredMatchingRequestsLocked() {
	if p == nil {
		return
	}
	now := p.nowTime()
	for requestID, request := range p.JoinRequests {
		if request == nil || now.Sub(request.CreatedAt) >= p.invitationTTL() {
			delete(p.JoinRequests, requestID)
		}
	}
}

func (p *PartyService) AnswerMatchingJoin(requestID, applicantUID, masterUID uint32, accept bool) (PartyTransition, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	request := p.JoinRequests[requestID]
	if request == nil || request.PlayerUniqueID != applicantUID || p.nowTime().Sub(request.CreatedAt) >= p.invitationTTL() {
		delete(p.JoinRequests, requestID)
		return PartyTransition{}, ErrPartyInvitationMissing
	}
	delete(p.JoinRequests, requestID)
	party := p.Parties[request.PartyNumber]
	if party == nil || party.MasterUniqueID != masterUID {
		return PartyTransition{}, ErrPartyPermissionDenied
	}
	master, err := p.player(masterUID)
	if err != nil {
		return PartyTransition{}, err
	}
	applicant, err := p.player(applicantUID)
	if err != nil {
		return PartyTransition{}, err
	}
	transition := PartyTransition{Invitation: PartyInvitation{PartyNumber: party.Number}, Inviter: master, Target: applicant, Accepted: accept}
	if !accept {
		return transition, nil
	}
	if applicant.GetParty() != nil || len(party.Members) >= party.PartySettingsFlag.MaxMembers() {
		return PartyTransition{}, ErrPartyFull
	}
	transition.Existing = p.playersForMembersLocked(party.Members)
	if err := applicant.AddToParty(party); err != nil {
		return PartyTransition{}, err
	}
	party.Members = append(party.Members, applicantUID)
	party.MemberCount = byte(len(party.Members))
	transition.Party = p.snapshotLocked(party)
	return transition, nil
}

func (p *PartyService) LeaveParty(playerUID uint32, reason byte) (PartyTransition, error) {
	return p.removeMember(playerUID, playerUID, reason, false)
}

func (p *PartyService) KickPartyMember(requesterUID, targetUID uint32) (PartyTransition, error) {
	return p.removeMember(requesterUID, targetUID, 4, true)
}

func (p *PartyService) removeMember(requesterUID, targetUID uint32, reason byte, kick bool) (PartyTransition, error) {
	if p == nil || requesterUID == 0 || targetUID == 0 {
		return PartyTransition{}, ErrPartyInvalidRequest
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	requester, err := p.player(requesterUID)
	if err != nil {
		return PartyTransition{}, err
	}
	target, err := p.player(targetUID)
	if err != nil {
		return PartyTransition{}, err
	}
	party := target.GetParty()
	if party == nil || requester.GetParty() != party || !containsPartyMember(party.Members, targetUID) {
		return PartyTransition{}, ErrPartyPermissionDenied
	}
	if kick && (party.MasterUniqueID != requesterUID || requesterUID == targetUID) {
		return PartyTransition{}, ErrPartyPermissionDenied
	}
	transition := PartyTransition{Party: p.snapshotLocked(party), AllBefore: p.playersForMembersLocked(party.Members), Target: target, RemovedUID: targetUID, RemovalReason: reason}
	party.Members = removePartyMember(party.Members, targetUID)
	target.RemoveFromPartyIf(party)
	party.MemberCount = byte(len(party.Members))
	delete(p.pendingInvites, targetUID)

	if len(party.Members) < 2 {
		transition.Dismissed = true
		for _, member := range p.playersForMembersLocked(party.Members) {
			member.RemoveFromPartyIf(party)
		}
		delete(p.Parties, party.Number)
		delete(p.FormedParties, party.Number)
		return transition, nil
	}
	if party.MasterUniqueID == targetUID {
		party.MasterUniqueID = party.Members[0]
		if master, lookupErr := p.player(party.MasterUniqueID); lookupErr == nil {
			party.MasterJID = uint32(master.ID)
			party.MasterName = master.GetName()
		}
		transition.LeaderChanged = true
	}
	transition.Party = p.snapshotLocked(party)
	return transition, nil
}

func (p *PartyService) SnapshotParty(number uint32) (PartySnapshot, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	party := p.Parties[number]
	if party == nil {
		return PartySnapshot{}, fmt.Errorf("%w: party %d", ErrPartyInvalidRequest, number)
	}
	return p.snapshotLocked(party), nil
}

func (p *PartyService) snapshotLocked(party *model.Party) PartySnapshot {
	if party == nil {
		return PartySnapshot{}
	}
	return PartySnapshot{Number: party.Number, MasterUniqueID: party.MasterUniqueID, Settings: party.PartySettingsFlag, Members: p.playersForMembersLocked(party.Members)}
}

func (p *PartyService) playersForMembersLocked(members []uint32) []*model.Player {
	players := make([]*model.Player, 0, len(members))
	for _, uid := range members {
		if player, err := p.player(uid); err == nil {
			players = append(players, player)
		}
	}
	return players
}

func (p *PartyService) nextPartyNumberLocked() uint32 {
	for {
		number := p.partyNumberCounter
		p.partyNumberCounter++
		if p.partyNumberCounter == 0 {
			p.partyNumberCounter = 1
		}
		if number != 0 {
			if _, exists := p.Parties[number]; !exists {
				return number
			}
		}
	}
}

func (p *PartyService) nextJoinRequestLocked() uint32 {
	for {
		number := p.joinRequestCounter
		p.joinRequestCounter++
		if p.joinRequestCounter == 0 {
			p.joinRequestCounter = 1
		}
		if number != 0 {
			if _, exists := p.JoinRequests[number]; !exists {
				return number
			}
		}
	}
}

func containsPartyMember(members []uint32, uid uint32) bool {
	for _, member := range members {
		if member == uid {
			return true
		}
	}
	return false
}

func removePartyMember(members []uint32, uid uint32) []uint32 {
	result := make([]uint32, 0, len(members))
	for _, member := range members {
		if member != uid {
			result = append(result, member)
		}
	}
	return result
}
