package party

import (
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/utils"
)

// PartyMatchingErrorNotFound is the corroborated vSRO response code for a
// missing matching entry/party. It is a wire value, not a replacement for the
// service's internal error taxonomy.
const PartyMatchingErrorNotFound uint16 = 11292

const (
	partyMatchingJoinDenied   uint16 = 1
	partyMatchingJoinAccepted uint16 = 2
)

func buildInvitation(invite service.PartyInvitation) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyInvitation
	if invite.PartyNumber == 0 {
		packet.WriteByte(2)
	} else {
		packet.WriteByte(3)
	}
	packet.WriteUInt32(invite.InviterUniqueID)
	packet.WriteByte(invite.Settings.ToByte())
	return packet
}

func buildPartyOperationResponse(messageID uint16, success bool) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	if success {
		packet.WriteByte(1)
		return packet
	}
	// The 1.188 client consumes a uint16 error code for result 2 on both
	// 0xB060 and 0xB062. Omitting it advances the packet reader beyond the
	// payload and can terminate the client.
	packet.WriteByte(2)
	packet.WriteUInt16(0)
	return packet
}

func buildPartyCreateSuccess(playerUID uint32) network.Packet {
	packet := buildPartyOperationResponse(opcode.PartyCreateResponse, true)
	packet.WriteUInt32(playerUID)
	return packet
}

// buildPartyJoinAcknowledgement is 0xB067. The client records this successful
// joiner's runtime UID, then compares it with the 0x3065 master UID to decide
// whether kick/ban controls are enabled.
func buildPartyJoinAcknowledgement(joinerUID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMemberCountResponse
	packet.WriteByte(1)
	packet.WriteUInt32(joinerUID)
	return packet
}

func buildMatchingJoinResult(success bool, answer byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMatchingJoinResponse
	if success {
		packet.WriteByte(1)
		joinResult := partyMatchingJoinDenied
		if answer == 1 {
			joinResult = partyMatchingJoinAccepted
		}
		packet.WriteUInt16(joinResult)
	} else {
		packet.WriteByte(2)
		packet.WriteUInt16(PartyMatchingErrorNotFound)
	}
	return packet
}

func buildMatchingDeleteResult(success bool, matchingID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyMatchingDeleteResponse
	if success {
		packet.WriteByte(1)
		packet.WriteUInt32(matchingID)
		return packet
	}
	packet.WriteByte(2)
	packet.WriteUInt16(PartyMatchingErrorNotFound)
	return packet
}

func buildMatchingOperationFailure(messageID uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteByte(2)
	packet.WriteUInt16(PartyMatchingErrorNotFound)
	return packet
}

func buildPartySnapshot(snapshot service.PartySnapshot, matching bool) network.Packet {
	packet := network.EmptyPacket()
	if matching {
		packet.MessageID = opcode.PartyCreatedFromMatchingResponse
	} else {
		packet.MessageID = opcode.PartyCreate
	}
	if matching {
		packet.WriteByte(0xFF)
		packet.WriteUInt32(snapshot.Number)
	} else {
		// The stock 0x3865 parser treats this byte as a field mask and always
		// consumes the party number before the masked header/member sections.
		packet.WriteByte(0x03)
		packet.WriteUInt32(snapshot.Number)
	}
	packet.WriteUInt32(snapshot.MasterUniqueID)
	packet.WriteByte(snapshot.Settings.ToByte())
	packet.WriteByte(byte(len(snapshot.Members)))
	for _, member := range snapshot.Members {
		writePartyMember(&packet, member)
	}
	return packet
}

func buildPartyJoin(member *model.Player) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyUpdateResponse
	packet.WriteByte(2)
	writePartyMember(&packet, member)
	return packet
}

func buildPartyLeave(memberUID uint32, reason byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyUpdateResponse
	packet.WriteByte(3)
	packet.WriteUInt32(memberUID)
	packet.WriteByte(reason)
	return packet
}

func buildPartyDismiss() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyUpdateResponse
	packet.WriteByte(1)
	// The pinned client always consumes a uint16 after dismiss action 1.
	packet.WriteUInt16(0)
	return packet
}

func buildPartyLeader(masterUID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.PartyUpdateResponse
	packet.WriteByte(9)
	packet.WriteUInt32(masterUID)
	return packet
}

func writePartyMember(packet *network.Packet, player *model.Player) {
	if packet == nil || player == nil {
		return
	}
	player.Mutex.Lock()
	level := player.Level
	hp, mp := player.BaseStats.HP, player.BaseStats.MP
	strength, intelligence := player.BaseStats.Str, player.BaseStats.Int
	masteries := make(map[uint32]byte, len(player.Masteries))
	for id, masteryLevel := range player.Masteries {
		masteries[id] = masteryLevel
	}
	player.Mutex.Unlock()

	position := player.GetNavmeshPosition()
	packet.WriteByte(0xFF)
	packet.WriteUInt32(player.GetUniqueID())
	packet.WriteString(player.GetName())
	packet.WriteUInt32(player.GetRefObjectID())
	packet.WriteByte(byte(level))
	packet.WriteByte(packPartyVitals(hp, mp, utils.BaseHPOrMP(level, strength), utils.BaseHPOrMP(level, intelligence)))
	writePartyPosition(packet, position, model.GameWorldScope(player.GetGameWorldID()))
	packet.WriteString(player.GetGuildAffiliation().Name)
	packet.WriteByte(0x04)
	first, second := highestMasteries(masteries)
	packet.WriteUInt32(first)
	packet.WriteUInt32(second)
}

func packPartyVitals(hp, mp, maxHP, maxMP int) byte {
	percent := func(current, maximum int) byte {
		if maximum <= 0 || current <= 0 {
			return 0
		}
		value := current * 10 / maximum
		if value > 10 {
			value = 10
		}
		return byte(value)
	}
	return percent(hp, maxHP) | percent(mp, maxMP)<<4
}

func highestMasteries(masteries map[uint32]byte) (uint32, uint32) {
	ids := make([]uint32, 0, len(masteries))
	for id := range masteries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if masteries[ids[i]] == masteries[ids[j]] {
			return ids[i] < ids[j]
		}
		return masteries[ids[i]] > masteries[ids[j]]
	})
	if len(ids) == 0 {
		return 0, 0
	}
	if len(ids) == 1 {
		return ids[0], 0
	}
	return ids[0], ids[1]
}

func playerHighestMasteries(player *model.Player) (uint32, uint32) {
	if player == nil {
		return 0, 0
	}
	player.Mutex.Lock()
	masteries := make(map[uint32]byte, len(player.Masteries))
	for id, level := range player.Masteries {
		masteries[id] = level
	}
	player.Mutex.Unlock()
	return highestMasteries(masteries)
}

func writePartyPosition(packet *network.Packet, position navmeshv2.RtNavmeshPosition, scope model.GameWorldScope) {
	region := uint16(position.Region.ID)
	var x, y, z float32
	if position.Offset != nil {
		x, y, z = position.Offset.X, position.Offset.Y, position.Offset.Z
	}
	packet.WriteUInt16(region)
	if region&navmeshv2.DungeonMask != 0 {
		packet.WriteUInt32(uint32(int32(x)))
		packet.WriteUInt32(uint32(int32(z)))
		packet.WriteUInt32(uint32(int32(y)))
	} else {
		packet.WriteUInt16(uint16(int16(x)))
		packet.WriteUInt16(uint16(int16(z)))
		packet.WriteUInt16(uint16(int16(y)))
	}
	packet.WriteUInt16(scope.WorldID())
	packet.WriteUInt16(scope.LayerID())
}
