package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func decodeGuildElectionUint32(packet *network.Packet, operation string) (uint32, error) {
	if packet == nil || packet.Remaining() != 4 {
		return 0, fmt.Errorf("invalid guild election %s request", operation)
	}
	return packet.ReadUInt32()
}

func DecodeGuildElectionRequest(packet *network.Packet) (uint32, error) {
	return decodeGuildElectionUint32(packet, "start")
}

func DecodeGuildElectionParticipateRequest(packet *network.Packet) (uint32, error) {
	return decodeGuildElectionUint32(packet, "participate")
}

type GuildElectionVoteRequest struct {
	ElectionID  uint32
	CandidateID uint32
	Selection   byte
}

func DecodeGuildElectionVoteRequest(packet *network.Packet) (GuildElectionVoteRequest, error) {
	if packet == nil || packet.Remaining() != 9 {
		return GuildElectionVoteRequest{}, fmt.Errorf("invalid guild election vote request")
	}
	electionID, err := packet.ReadUInt32()
	if err != nil {
		return GuildElectionVoteRequest{}, err
	}
	candidateID, err := packet.ReadUInt32()
	if err != nil {
		return GuildElectionVoteRequest{}, err
	}
	selection, err := packet.ReadByte()
	if err != nil {
		return GuildElectionVoteRequest{}, err
	}
	return GuildElectionVoteRequest{ElectionID: electionID, CandidateID: candidateID, Selection: selection}, nil
}

func buildGuildElectionResult(messageID uint16, errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	if errorCode == 0 {
		p.WriteByte(1)
	} else {
		p.WriteByte(2)
		p.WriteUInt16(errorCode)
	}
	return p
}

func BuildGuildElectionResult(errorCode uint16) network.Packet {
	return buildGuildElectionResult(opcode.GuildElectionResponse, errorCode)
}

func BuildGuildElectionParticipateResult(errorCode uint16) network.Packet {
	return buildGuildElectionResult(opcode.GuildElectionParticipateResponse, errorCode)
}

type GuildElectionCandidate struct {
	CharacterID uint32
	Votes       byte
}

// BuildGuildElectionOpenResult matches B106's success parser at 0x881D00
// and the nested election-list parser at 0x99D920.
func BuildGuildElectionOpenResult(electionID, selectedCandidateID uint32, candidates []GuildElectionCandidate, errorCode uint16) (network.Packet, error) {
	if len(candidates) > 255 {
		return network.Packet{}, fmt.Errorf("guild election candidate count %d exceeds uint8", len(candidates))
	}
	if errorCode != 0 {
		return BuildGuildElectionParticipateResult(errorCode), nil
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildElectionParticipateResponse
	p.WriteByte(1)
	p.WriteUInt32(electionID)
	p.WriteUInt32(selectedCandidateID)
	p.WriteByte(byte(len(candidates)))
	for _, candidate := range candidates {
		p.WriteUInt32(candidate.CharacterID)
		p.WriteByte(candidate.Votes)
	}
	return p, nil
}

func BuildGuildElectionVoteResult(errorCode uint16) network.Packet {
	return buildGuildElectionResult(opcode.GuildElectionVoteResponse, errorCode)
}

func buildGuildElectionUpdate(subtype byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildElectionUpdate
	p.WriteByte(subtype)
	return p
}

// BuildGuildElectionOpened matches the client subtype-1 parser at 0x8897C0.
func BuildGuildElectionOpened(electionID uint32, phase byte, deadline uint32) network.Packet {
	p := buildGuildElectionUpdate(1)
	p.WriteUInt32(electionID)
	p.WriteByte(phase)
	p.WriteUInt32(deadline)
	return p
}

// BuildGuildElectionCandidateResult matches the client subtype-3 parser.
func BuildGuildElectionCandidateResult(candidateID uint32, result byte, relatedCharacterID uint32) network.Packet {
	p := buildGuildElectionUpdate(3)
	p.WriteUInt32(candidateID)
	p.WriteByte(result)
	if result == 1 {
		p.WriteUInt32(relatedCharacterID)
	}
	return p
}

// BuildGuildElectionCandidateState matches the client subtype-4 parser.
func BuildGuildElectionCandidateState(candidateID uint32, state, votes, rank byte) network.Packet {
	p := buildGuildElectionUpdate(4)
	p.WriteUInt32(candidateID)
	p.WriteByte(state)
	p.WriteByte(votes)
	p.WriteByte(rank)
	return p
}

// BuildGuildElectionWinner matches the client subtype-5 parser.
func BuildGuildElectionWinner(electionID, winnerID uint32) network.Packet {
	p := buildGuildElectionUpdate(5)
	p.WriteUInt32(electionID)
	p.WriteUInt32(winnerID)
	return p
}
