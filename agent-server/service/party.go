package service

import (
	"fmt"
	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
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
	players            PartyPlayerLookup
	now                func() time.Time
	mutex              sync.Mutex
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
			players:            GetWorldServiceInstance(),
			now:                time.Now,
		}
	})

	return partyServiceInstance
}

func (p *PartyService) FormParty(partyFormRequest model.PartyFormRequest) uint32 {
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

func (p *PartyService) DeletePartyFromMatching(partyNumber, playerUniqueId uint32) {
	party, err := p.GetParty(partyNumber)

	if err != nil {
		logrus.Warnf("failed to delete party %d from matching: %s", partyNumber, err.Error())
		return
	}
	partyMaster, err := worldServiceInstance.GetPlayerByUniqueId(party.MasterUniqueID)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to delete party"))
		return
	}

	if party.MasterUniqueID == playerUniqueId {
		p.mutex.Lock()
		defer p.mutex.Unlock()
		delete(p.FormedParties, partyNumber)
		p := network.EmptyPacket()
		p.MessageID = opcode.PartyMatchingDeleteResponse
		p.WriteBool(true)
		p.WriteUInt32(partyNumber)
		partyMaster.GetSession().Send(p)
	} else {
		logrus.Warnf("failed to delete party %d from matching: player %d is not the owner", partyNumber, playerUniqueId)
	}
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
	if partyCount%PartiesPerPage != 0 {
		return 1 + byte(partyCount/PartiesPerPage)
	} else {
		return byte(partyCount / PartiesPerPage)
	}

}

func (p *PartyService) JoinFormedParty(partyNumber, playerUniqueId uint32) {
	party, err := p.GetParty(partyNumber)

	if err != nil {
		logrus.Error(errors.Wrap(err, "cannot join formed party"))
		return
	}

	player, err := worldServiceInstance.GetPlayerByUniqueId(playerUniqueId)

	if err != nil {
		logrus.Error(errors.Wrap(err, "cannot join formed party"))
		return
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	joinRequest := &model.PartyJoinRequest{
		PartyNumber:    partyNumber,
		RequestID:      p.joinRequestCounter,
		PlayerJID:      player.ID,
		PlayerUniqueID: player.GetUniqueID(),
		AcceptResult:   false,
		Mutex:          &sync.Mutex{},
	}
	p.JoinRequests[joinRequest.RequestID] = joinRequest
	p.joinRequestCounter++

	partyMaster, err := worldServiceInstance.GetPlayerByUniqueId(party.MasterUniqueID)
	if err != nil {
		logrus.Error(errors.Wrap(err, "cannot send join request to party master"))
		delete(p.JoinRequests, joinRequest.RequestID)
		return
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMatchingPlayerJoinResponse
	packet.WriteUInt32(joinRequest.RequestID) // RequestID
	packet.WriteUInt32(uint32(player.ID))     // PlayerJID
	packet.WriteUInt32(party.Number)
	packet.WriteUInt32(0)                 // PlayerMasteryPrimaryID
	packet.WriteUInt32(0)                 // PlayerMasterySecondaryID
	packet.WriteByte(4)                   // unkByte01
	packet.WriteByte(255)                 // unkByte01
	packet.WriteUInt32(uint32(player.ID)) // PlayerJID_x2
	packet.WriteString(player.CharName)
	packet.WriteUInt32(1907)  // char model id
	packet.WriteByte(5)       // level
	packet.WriteByte(170)     // current hp/mp
	packet.WriteUInt16(25000) // regionId
	packet.WriteUInt16(1007)  // x
	packet.WriteUInt16(65534) // y
	packet.WriteUInt16(1710)  // z
	packet.WriteUInt32(65537) // unknown
	packet.WriteUInt16(0)     // guild name length
	packet.WriteByte(4)       // unknown
	packet.WriteUInt32(257)   // primary skill tree
	packet.WriteUInt32(290)   // secondary skill tree
	partyMaster.Session.Send(packet)
}

func (p *PartyService) AnswerJoinRequest(requestId, joiningPlayerJID, acceptingPlayerJID uint32, acceptCode bool) {
	joinRequest, err := p.GetJoinReqeust(requestId)

	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to accept join request"))
		return
	}

	party, err := p.GetParty(joinRequest.PartyNumber)

	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to accept join request"))
		return
	}

	if acceptingPlayerJID != party.MasterJID {
		// only party master can accept
		logrus.Error(errors.New("only party master can accept party join requests"))
		return
	}

	if joiningPlayerJID != uint32(joinRequest.PlayerJID) {
		logrus.Error(errors.New("joining player id must match player id for join request"))
		return
	}

	joinRequest.AcceptResult = acceptCode

	requestingPlayer, err := worldServiceInstance.GetPlayerByUniqueId(joinRequest.PlayerUniqueID)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to send join request response to requesting player"))
		return
	}

	if joinRequest.AcceptResult {
		// add player to party
		party.Members = append(party.Members, requestingPlayer.GetUniqueID())
		party.MemberCount++

		// inform player
		partyJoinResponse := network.EmptyPacket()
		partyJoinResponse.MessageID = opcode.PartyMatchingJoinResponse
		partyJoinResponse.WriteByte(1)
		partyJoinResponse.WriteUInt16(1) // AcceptResult is somehow sent as uint16
		requestingPlayer.Session.Send(partyJoinResponse)

		// send member count to newly joined player
		partyMemberCountResponse := network.EmptyPacket()
		partyMemberCountResponse.MessageID = opcode.PartyMemberCountResponse
		partyMemberCountResponse.WriteByte(1)
		partyMemberCountResponse.WriteUInt32(uint32(party.MemberCount))
		requestingPlayer.Session.Send(partyMemberCountResponse)

		if party.MemberCount == 2 {
			// new party was created
			if ptMaster, err := worldServiceInstance.GetPlayerByUniqueId(party.MasterUniqueID); err != nil {
				logrus.Error(errors.Wrap(err, "failed to inform party master about created party"))
			} else {
				partyCreatedResponse := network.EmptyPacket()
				partyCreatedResponse.MessageID = opcode.PartyCreateResponse
				partyCreatedResponse.WriteByte(1)
				partyCreatedResponse.WriteUInt32(1)
				ptMaster.Session.Send(partyCreatedResponse)

				p1 := network.EmptyPacket()
				p1.MessageID = opcode.PartyCreatedFromMatchingResponse
				p1.WriteByte(0xFF) // splitter
				p1.WriteUInt32(party.Number)
				p1.WriteUInt32(uint32(ptMaster.ID))            // MasterJID
				p1.WriteByte(party.PartySettingsFlag.ToByte()) // type?
				p1.WriteByte(party.MemberCount)
				p1.WriteByte(255)                   // splitter
				p1.WriteUInt32(uint32(ptMaster.ID)) // MemberJID
				p1.WriteString(ptMaster.GetName())
				p1.WriteUInt32(1907)               // char model id
				p1.WriteByte(byte(ptMaster.Level)) // level
				p1.WriteByte(170)
				p1.WriteUInt16(uint16(ptMaster.GetNavmeshPosition().Region.ID))         // regionId
				p1.WriteUInt16(uint16(ptMaster.GetNavmeshPosition().Offset.X))          // x
				p1.WriteUInt16(uint16(ptMaster.GetNavmeshPosition().Offset.Y + 0xFFFF)) // y
				p1.WriteUInt16(uint16(ptMaster.GetNavmeshPosition().Offset.Z))          // z
				p1.WriteUInt32(65537)                                                   // unknown
				p1.WriteUInt16(0)                                                       // guild name length
				p1.WriteByte(4)                                                         // unknown
				p1.WriteUInt32(290)                                                     // primary skill tree
				p1.WriteUInt32(0)                                                       // secondary skill tree
				ptMaster.Session.Send(p1)

				p2 := network.EmptyPacket()
				p2.MessageID = opcode.PartyUpdateResponse
				p2.WriteByte(2)   // party action type | 1 = close pt | 2 = player joined | 3 = left pt or kicked | 6 = player info | 9 = new pt master
				p2.WriteByte(255) // splitter
				p2.WriteUInt32(uint32(requestingPlayer.ID))
				p2.WriteString(requestingPlayer.CharName)
				p2.WriteUInt32(1907)                                                            // Char model id
				p2.WriteByte(5)                                                                 // level
				p2.WriteByte(170)                                                               // HP / MP
				p2.WriteUInt16(uint16(requestingPlayer.GetNavmeshPosition().Region.ID))         // regionId
				p2.WriteUInt16(uint16(requestingPlayer.GetNavmeshPosition().Offset.X))          // x
				p2.WriteUInt16(uint16(requestingPlayer.GetNavmeshPosition().Offset.Y + 0xFFFF)) // y
				p2.WriteUInt16(uint16(requestingPlayer.GetNavmeshPosition().Offset.Z))          // z
				p2.WriteUInt32(65537)                                                           // unknown
				p2.WriteUInt16(0)                                                               // guild name length
				p2.WriteByte(4)                                                                 // unknown
				p2.WriteUInt32(290)                                                             // primary skill tree
				p2.WriteUInt32(65537)                                                           // secondary skill tree
				ptMaster.Session.Send(p2)
			}
		}
	} else {
		// inform player
		partyJoinResponse := network.EmptyPacket()
		partyJoinResponse.MessageID = opcode.PartyMatchingJoinResponse
		partyJoinResponse.WriteByte(1)
		partyJoinResponse.WriteUInt16(0) // AcceptResult is somehow sent as uint16
		requestingPlayer.Session.Send(partyJoinResponse)
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.JoinRequests, joinRequest.RequestID)
}

func (p *PartyService) GetJoinReqeust(requestId uint32) (*model.PartyJoinRequest, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if joinRequest, exists := p.JoinRequests[requestId]; exists {
		return joinRequest, nil
	} else {
		return nil, errors.New(fmt.Sprintf("party join request %d does not exist", requestId))
	}
}

func (p *PartyService) UpdateParty(changedParty model.Party) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	party := p.Parties[changedParty.Number]
	if party == nil {
		return
	}
	if party.MasterUniqueID != changedParty.MasterUniqueID {
		logrus.Warnf("failed to update party: party masters must match")
		return
	}
	party.PartySettingsFlag = changedParty.PartySettingsFlag
	party.PurposeType = changedParty.PurposeType
	party.LevelMin = changedParty.LevelMin
	party.LevelMax = changedParty.LevelMax
	party.Title = changedParty.Title
}

func (p *PartyService) KickPlayer(playerToKickUniqueId, requestingPlayerUniqueId uint32) {
	player, err := worldServiceInstance.GetPlayerByUniqueId(playerToKickUniqueId)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to kick player"))
		return
	}

	ptMaster, err := worldServiceInstance.GetPlayerByUniqueId(player.GetParty().MasterUniqueID)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to kick player: could not find pt master"))
		return
	}

	if ptMaster.GetUniqueID() != requestingPlayerUniqueId {
		logrus.Error(errors.Wrap(err, "failed to kick player: requesting player must be master"))
		return
	}

	if !player.HasParty() {
		logrus.Warn("player must be in party to be kicked")
		return
	}

	if !ptMaster.HasParty() {
		logrus.Warn("party master does not have a party")
		return
	}

	party := player.GetParty()

	partyLeftPacket := network.EmptyPacket()
	partyLeftPacket.MessageID = opcode.PartyUpdateResponse
	partyLeftPacket.WriteByte(3) // party left
	partyLeftPacket.WriteUInt32(uint32(player.ID))
	partyLeftPacket.WriteByte(4) // leave type

	for index, memberUniqueID := range party.Members {
		member, _ := worldServiceInstance.GetPlayerByUniqueId(memberUniqueID)
		member.GetSession().Send(partyLeftPacket)
		if memberUniqueID == player.UniqueID {
			party.Members = append(party.Members[:index], party.Members[index+1:]...)
		}
	}
}
