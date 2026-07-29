package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
)

var (
	ErrPartyInvalidRequest    = errors.New("invalid party request")
	ErrPartyAlreadyMember     = errors.New("player is already in a party")
	ErrPartyTargetUnavailable = errors.New("party target is unavailable")
	ErrPartyInvitationPending = errors.New("party target already has a pending invitation")
	ErrPartyInvitationMissing = errors.New("party invitation does not exist")
	ErrPartyPermissionDenied  = errors.New("party permission denied")
	ErrPartyFull              = errors.New("party is full")
)

const partyInvitationTTL = 10 * time.Second

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

func NewPartyService(players PartyPlayerLookup) *PartyService {
	return &PartyService{
		Parties:            make(map[uint32]*model.Party),
		FormedParties:      make(map[uint32]*model.Party),
		JoinRequests:       make(map[uint32]*model.PartyJoinRequest),
		partyNumberCounter: 1,
		joinRequestCounter: 1,
		pendingInvites:     make(map[uint32]PartyInvitation),
		players:            players,
		now:                time.Now,
	}
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
		if now.Sub(pending.CreatedAt) < partyInvitationTTL {
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
	if !ok || p.nowTime().Sub(invite.CreatedAt) >= partyInvitationTTL {
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
			Number: p.nextPartyNumberLocked(), MasterUniqueID: inviter.GetUniqueID(), MasterJID: inviter.GetUniqueID(),
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
	party := p.FormedParties[partyNumber]
	if party == nil || applicant.GetParty() != nil || len(party.Members) >= party.PartySettingsFlag.MaxMembers() {
		return nil, nil, nil, ErrPartyInvalidRequest
	}
	if applicant.Level < int(party.LevelMin) || applicant.Level > int(party.LevelMax) {
		return nil, nil, nil, ErrPartyInvalidRequest
	}
	for _, pending := range p.JoinRequests {
		if pending.PlayerUniqueID == applicantUID && p.nowTime().Sub(pending.CreatedAt) < partyInvitationTTL {
			return nil, nil, nil, ErrPartyInvitationPending
		}
	}
	master, err := p.player(party.MasterUniqueID)
	if err != nil {
		return nil, nil, nil, err
	}
	request := &model.PartyJoinRequest{
		PartyNumber: partyNumber, RequestID: p.nextJoinRequestLocked(), PlayerJID: int(applicantUID),
		PlayerUniqueID: applicantUID, CreatedAt: p.nowTime(), Mutex: &sync.Mutex{},
	}
	p.JoinRequests[request.RequestID] = request
	return request, master, applicant, nil
}

func (p *PartyService) AnswerMatchingJoin(requestID, applicantUID, masterUID uint32, accept bool) (PartyTransition, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	request := p.JoinRequests[requestID]
	if request == nil || request.PlayerUniqueID != applicantUID || p.nowTime().Sub(request.CreatedAt) >= partyInvitationTTL {
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
		party.MasterJID = party.Members[0]
		if master, lookupErr := p.player(party.MasterUniqueID); lookupErr == nil {
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
