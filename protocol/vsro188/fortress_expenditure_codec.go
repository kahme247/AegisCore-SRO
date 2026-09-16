package vsro188

import (
	"fmt"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type FortressExpenditureEntry struct {
	Timestamp     time.Time
	CharacterName string
	Amount        uint32
	Reason        byte
}

func DecodeFortressExpenditureHistoryRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid fortress expenditure history request")
	}
	return nil
}

func BuildFortressExpenditureHistory(entries []FortressExpenditureEntry) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildGPHistoryResponse
	if len(entries) > math.MaxUint8 {
		return p, fmt.Errorf("too many fortress expenditure entries: %d", len(entries))
	}
	p.WriteByte(1)
	p.WriteByte(byte(len(entries)))
	for _, entry := range entries {
		unix := entry.Timestamp.Unix()
		if entry.Timestamp.IsZero() || unix < 0 || unix > math.MaxUint32 || entry.Reason > 8 {
			return network.Packet{}, fmt.Errorf("invalid fortress expenditure entry")
		}
		if err := ValidateText(entry.CharacterName, TextRules{Field: "fortress expenditure character", MaxRunes: 64, RequireTrimmed: true}); err != nil {
			return network.Packet{}, err
		}
		p.WriteUInt32(uint32(unix))
		p.WriteString(entry.CharacterName)
		p.WriteUInt32(entry.Amount)
		p.WriteByte(entry.Reason)
	}
	return p, nil
}

func BuildFortressExpenditureHistoryFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildGPHistoryResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}
