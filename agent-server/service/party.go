package service

import (
	"fmt"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"sort"
	"sync"
	"time"
)

const (
	PartiesPerPage = 20
)

type PartyService struct {
	Parties            map[uint32]*model.Party
	FormedParties      map[uint32]*model.Party
	JoinRequests       map[uint32]*model.PartyJoinRequest
	partyNumberCounter uint32
	joinRequestCounter uint32
	pendingInvites     map[uint32]PartyInvitation
	itemCursors        map[uint32]int
	players            PartyPlayerLookup
	now                func() time.Time
	gameplayRates      interface{ Current() model.GameplayRates }
	mutex              sync.Mutex
}

func (p *PartyService) ConfigureGameplayRates(rates interface{ Current() model.GameplayRates }) {
	if p != nil {
		p.gameplayRates = rates
	}
}

func (p *PartyService) invitationTTL() time.Duration {
	if p != nil && p.gameplayRates != nil {
		if value := p.gameplayRates.Current().PartyInvitationTTLMS; value > 0 {
			return time.Duration(value) * time.Millisecond
		}
	}
	return partyInvitationTTL
}

func (p *PartyService) experienceRange() float32 {
	if p != nil && p.gameplayRates != nil {
		if value := p.gameplayRates.Current().PartyExperienceRange; value > 0 {
			return float32(value)
		}
	}
	return PartyExperienceRange
}

type PartyPlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type PartyInvitation struct {
	InviterUniqueID uint32
	TargetUniqueID  uint32
	PartyNumber     uint32
	Settings        model.PartySetting
	CreatedAt       time.Time
}

var partyServiceInstance *PartyService
var partyServiceOnce sync.Once

func GetPartyServiceInstance() *PartyService {
	partyServiceOnce.Do(func() {
		partyServiceInstance = &PartyService{
			Parties:            make(map[uint32]*model.Party),
			FormedParties:      make(map[uint32]*model.Party),
			JoinRequests:       make(map[uint32]*model.PartyJoinRequest),
			partyNumberCounter: 1,
			joinRequestCounter: 1,
			pendingInvites:     make(map[uint32]PartyInvitation),
			itemCursors:        make(map[uint32]int),
			players:            GetWorldServiceInstance(),
			now:                time.Now,
		}
	})

	return partyServiceInstance
}

func (p *PartyService) FormParty(partyFormRequest model.PartyFormRequest) uint32 {
	if p == nil || partyFormRequest.MasterUniqueID == 0 || partyFormRequest.PartySettingsFlag > 7 ||
		partyFormRequest.PurposeType > model.ThiefParty || partyFormRequest.LevelMin > partyFormRequest.LevelMax {
		return 0
	}
	player, err := p.player(partyFormRequest.MasterUniqueID)
	if err != nil {
		logrus.WithError(err).Warn("failed to form party matching entry")
		return 0
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if existing := player.GetParty(); existing != nil {
		if existing.MasterUniqueID != player.GetUniqueID() {
			return 0
		}
		existing.PartySettingsFlag = partyFormRequest.PartySettingsFlag
		existing.PurposeType = partyFormRequest.PurposeType
		existing.LevelMin = partyFormRequest.LevelMin
		existing.LevelMax = partyFormRequest.LevelMax
		existing.Title = partyFormRequest.Title
		p.FormedParties[existing.Number] = existing
		return existing.Number
	}

	party := &model.Party{
		Number:            p.nextPartyNumberLocked(),
		MasterUniqueID:    partyFormRequest.MasterUniqueID,
		MasterJID:         partyFormRequest.MasterJID,
		MasterName:        partyFormRequest.MasterName,
		CountryType:       partyFormRequest.CountryType,
		MemberCount:       1,
		PartySettingsFlag: partyFormRequest.PartySettingsFlag,
		PurposeType:       partyFormRequest.PurposeType,
		LevelMin:          partyFormRequest.LevelMin,
		LevelMax:          partyFormRequest.LevelMax,
		Title:             partyFormRequest.Title,
		Members:           []uint32{partyFormRequest.MasterUniqueID},
		Mutex:             &sync.Mutex{},
	}
	p.Parties[party.Number] = party
	p.FormedParties[party.Number] = party
	err = player.AddToParty(party)

	if err != nil {
		logrus.Error(errors.Wrap(err, fmt.Sprintf("failed to create party %d for player %s", party.Number, party.MasterName)))
		delete(p.Parties, party.Number)
		delete(p.FormedParties, party.Number)
		return 0
	}

	return party.Number
}

func (p *PartyService) DeletePartyFromMatching(partyNumber, playerUniqueId uint32) bool {
	return p.DeleteMatchingEntry(partyNumber, playerUniqueId) == nil
}

// DeleteMatchingEntry removes only the authenticated master's formed
// listing. The live party remains in Parties so existing members continue to
// function after the listing is withdrawn.
func (p *PartyService) DeleteMatchingEntry(partyNumber, playerUniqueID uint32) error {
	if p == nil || partyNumber == 0 || playerUniqueID == 0 {
		return ErrPartyInvalidRequest
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	party := p.FormedParties[partyNumber]
	if party == nil {
		return ErrPartyMatchingNotFound
	}
	if party.MasterUniqueID != playerUniqueID {
		return ErrPartyPermissionDenied
	}
	delete(p.FormedParties, partyNumber)
	return nil
}

func (p *PartyService) GetParty(partyNumber uint32) (*model.Party, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if party, exists := p.Parties[partyNumber]; exists {
		return party, nil
	}

	return nil, errors.New(fmt.Sprintf("party %d does not exist", partyNumber))
}

func (p *PartyService) GetFormedParties(playerUniqueId uint32, page byte) []*model.Party {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	_ = playerUniqueId
	ids := make([]uint32, 0, len(p.FormedParties))
	for id := range p.FormedParties {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	start := int(page) * PartiesPerPage
	if start >= len(ids) {
		return []*model.Party{}
	}
	end := start + PartiesPerPage
	if end > len(ids) {
		end = len(ids)
	}
	result := make([]*model.Party, 0, end-start)
	for _, id := range ids[start:end] {
		party := *p.FormedParties[id]
		party.Members = append([]uint32(nil), party.Members...)
		result = append(result, &party)
	}
	return result
}

func (p *PartyService) GetFormedPartiesPageCount() byte {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	partyCount := len(p.FormedParties)
	if partyCount == 0 {
		return 1
	}
	pages := (partyCount + PartiesPerPage - 1) / PartiesPerPage
	if pages > 255 {
		return 255
	}
	return byte(pages)

}

func (p *PartyService) JoinFormedParty(partyNumber, playerUniqueId uint32) {
	joinRequest, partyMaster, player, err := p.CreateMatchingJoinRequest(partyNumber, playerUniqueId)
	if err != nil {
		logrus.Error(errors.Wrap(err, "cannot send join request to party master"))
		return
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMatchingPlayerJoinNotification
	packet.WriteUInt32(joinRequest.RequestID)
	packet.WriteUInt32(player.GetUniqueID())
	packet.WriteUInt32(joinRequest.PartyNumber)
	// The old path used a fabricated model, level, position, and mastery
	// tuple. Keep only fields that are available from the authoritative player;
	// unresolved mastery/guild fields remain neutral instead of pretending to
	// be real values.
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
	packet.WriteByte(4)
	packet.WriteByte(0xFF)
	packet.WriteUInt32(player.GetUniqueID())
	packet.WriteString(player.GetName())
	packet.WriteUInt32(player.GetRefObjectID())
	packet.WriteByte(byte(player.Level))
	packet.WriteByte(0)
	position := player.GetNavmeshPosition()
	packet.WriteUInt16(uint16(position.Region.ID))
	packet.WriteUInt16(uint16(position.Offset.X))
	packet.WriteUInt16(uint16(position.Offset.Z))
	packet.WriteUInt16(uint16(position.Offset.Y))
	packet.WriteUInt16(1)
	packet.WriteUInt16(1)
	packet.WriteUInt16(0)
	packet.WriteByte(4)
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
	if partyMaster.GetSession() != nil {
		if sendErr := partyMaster.GetSession().Send(packet); sendErr != nil {
			logrus.WithError(sendErr).Warn("cannot send party join request")
		}
	}
}

func (p *PartyService) AnswerJoinRequest(requestId, joiningPlayerJID, acceptingPlayerJID uint32, acceptCode bool) {
	joinRequest, err := p.GetJoinReqeust(requestId)
	if err != nil {
		logrus.WithError(err).Warn("failed to accept join request")
		return
	}
	party, err := p.GetParty(joinRequest.PartyNumber)
	if err != nil {
		logrus.WithError(err).Warn("failed to accept join request")
		return
	}
	master, err := p.player(party.MasterUniqueID)
	if err != nil || uint32(master.ID) != acceptingPlayerJID {
		logrus.Warn("only the durable party master can accept party join requests")
		return
	}
	if joiningPlayerJID != uint32(joinRequest.PlayerJID) {
		logrus.Warn("joining character ID must match the pending request")
		return
	}
	transition, err := p.AnswerMatchingJoin(requestId, joinRequest.PlayerUniqueID, master.GetUniqueID(), acceptCode)
	if err != nil {
		logrus.WithError(err).Warn("failed to apply join request")
		return
	}
	requestingPlayer := transition.Target
	if requestingPlayer == nil || requestingPlayer.GetSession() == nil {
		return
	}
	partyJoinResponse := network.EmptyPacket()
	partyJoinResponse.MessageID = opcode.PartyMatchingJoinResponse
	partyJoinResponse.WriteByte(1)
	if acceptCode {
		partyJoinResponse.WriteUInt16(2)
	} else {
		partyJoinResponse.WriteUInt16(1)
	}
	if sendErr := requestingPlayer.GetSession().Send(partyJoinResponse); sendErr != nil {
		logrus.WithError(sendErr).Warn("failed to send join request response")
	}
	if !acceptCode {
		return
	}
	memberCount := network.EmptyPacket()
	memberCount.MessageID = opcode.PartyMemberCountResponse
	memberCount.WriteByte(1)
	memberCount.WriteUInt32(uint32(len(transition.Party.Members)))
	if sendErr := requestingPlayer.GetSession().Send(memberCount); sendErr != nil {
		logrus.WithError(sendErr).Warn("failed to send party member count")
	}
	join := network.EmptyPacket()
	join.MessageID = opcode.PartyUpdateResponse
	join.WriteByte(2)
	writeLegacyPartyMember(&join, requestingPlayer)
	for _, member := range transition.Existing {
		if member != nil && member.GetSession() != nil {
			if sendErr := member.GetSession().Send(join); sendErr != nil {
				logrus.WithError(sendErr).Warn("failed to send party join update")
			}
		}
	}
}

func writeLegacyPartyMember(packet *network.Packet, player *model.Player) {
	if packet == nil || player == nil {
		return
	}
	packet.WriteByte(0xFF)
	packet.WriteUInt32(player.GetUniqueID())
	packet.WriteString(player.GetName())
	packet.WriteUInt32(player.GetRefObjectID())
	packet.WriteByte(byte(player.Level))
	packet.WriteByte(0)
	position := player.GetNavmeshPosition()
	packet.WriteUInt16(uint16(position.Region.ID))
	packet.WriteUInt16(uint16(position.Offset.X))
	packet.WriteUInt16(uint16(position.Offset.Z))
	packet.WriteUInt16(uint16(position.Offset.Y))
	packet.WriteUInt16(1)
	packet.WriteUInt16(1)
	packet.WriteString("")
	packet.WriteByte(4)
	packet.WriteUInt32(0)
	packet.WriteUInt32(0)
}

func (p *PartyService) GetJoinReqeust(requestId uint32) (*model.PartyJoinRequest, error) {
	return p.GetMatchingJoinRequest(requestId)
}

// GetMatchingJoinRequest returns a detached request snapshot. The packet
// response contains a client-controlled opaque u32, so the handler must look
// up the applicant from this authoritative request instead of trusting that
// field as a character identity.
func (p *PartyService) GetMatchingJoinRequest(requestID uint32) (*model.PartyJoinRequest, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if joinRequest, exists := p.JoinRequests[requestID]; exists {
		copy := *joinRequest
		return &copy, nil
	} else {
		return nil, errors.New(fmt.Sprintf("party join request %d does not exist", requestID))
	}
}

func (p *PartyService) UpdateParty(changedParty model.Party) {
	_ = p.UpdateMatchingEntry(changedParty)
}

// UpdateMatchingEntry updates an existing listing and requires the current
// party master. It never creates a new listing from a client-supplied party
// number.
func (p *PartyService) UpdateMatchingEntry(changedParty model.Party) error {
	if p == nil || changedParty.Number == 0 || changedParty.MasterUniqueID == 0 || changedParty.PartySettingsFlag > 7 ||
		changedParty.PurposeType > model.ThiefParty || changedParty.LevelMin > changedParty.LevelMax {
		return ErrPartyInvalidRequest
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	party := p.Parties[changedParty.Number]
	if party == nil {
		return ErrPartyMatchingNotFound
	}
	if party.MasterUniqueID != changedParty.MasterUniqueID {
		return ErrPartyPermissionDenied
	}
	if _, listed := p.FormedParties[changedParty.Number]; !listed {
		return ErrPartyMatchingNotFound
	}
	party.PartySettingsFlag = changedParty.PartySettingsFlag
	party.PurposeType = changedParty.PurposeType
	party.LevelMin = changedParty.LevelMin
	party.LevelMax = changedParty.LevelMax
	party.Title = changedParty.Title
	return nil
}

// KickPlayer is retained for older callers, but delegates state mutation to
// the lock-safe core path. The former implementation dereferenced a missing
// party, used the process-global world lookup, and removed only the UID from
// the slice without detaching the player or updating MemberCount.
func (p *PartyService) KickPlayer(playerToKickUniqueID, requestingPlayerUniqueID uint32) {
	transition, err := p.KickPartyMember(requestingPlayerUniqueID, playerToKickUniqueID)
	if err != nil {
		logrus.WithError(err).Warn("failed to kick player")
		return
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyUpdateResponse
	packet.WriteByte(3)
	packet.WriteUInt32(playerToKickUniqueID)
	packet.WriteByte(4)
	for _, member := range transition.AllBefore {
		if member == nil || member.GetSession() == nil {
			continue
		}
		if sendErr := member.GetSession().Send(packet); sendErr != nil {
			logrus.WithError(sendErr).Warn("failed to send party kick update")
		}
	}
}
