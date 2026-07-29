package character

import (
	"fmt"
	"math"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
)

const (
	questTalkExtendedTypeMin = byte(4)
	questTalkExtendedTypeMax = byte(5)
	questUpdateTypeMin       = byte(1)
	questUpdateTypeMax       = byte(4)
)

// buildQuestTalkPacket encodes the read order proven by the pinned stock
// client's handler at 0x00889D90: type, string, and for types 4/5 a byte count
// followed by that many strings. The values deliberately remain semantic-free
// until capture evidence identifies their game meaning.
func buildQuestTalkPacket(messageType byte, value string, entries []string) (network.Packet, error) {
	extended := messageType >= questTalkExtendedTypeMin && messageType <= questTalkExtendedTypeMax
	if !extended && len(entries) != 0 {
		return network.Packet{}, fmt.Errorf("quest talk type %d does not accept repeated strings", messageType)
	}
	if len(entries) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("quest talk repeated string count %d exceeds 255", len(entries))
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestTalk
	packet.WriteByte(messageType)
	if err := packet.WriteString(value); err != nil {
		return network.Packet{}, fmt.Errorf("write quest talk value: %w", err)
	}
	if extended {
		packet.WriteByte(byte(len(entries)))
		for index, entry := range entries {
			if err := packet.WriteString(entry); err != nil {
				return network.Packet{}, fmt.Errorf("write quest talk entry %d: %w", index, err)
			}
		}
	}
	return packet, nil
}

// buildQuestUpdatePacket encodes the one-byte selector and four-byte value
// consumed by the pinned stock client's handler at 0x008812B0.
func buildQuestUpdatePacket(updateType byte, value uint32) (network.Packet, error) {
	if updateType < questUpdateTypeMin || updateType > questUpdateTypeMax {
		return network.Packet{}, fmt.Errorf("quest update type %d is outside 1..4", updateType)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestUpdate
	packet.WriteByte(updateType)
	packet.WriteUInt32(value)
	return packet, nil
}

// buildQuestScriptPacket encodes the string then four-byte value consumed by
// the pinned stock client's handler at 0x008ABCC0.
func buildQuestScriptPacket(value string, argument uint32) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestScript
	if err := packet.WriteString(value); err != nil {
		return network.Packet{}, fmt.Errorf("write quest script value: %w", err)
	}
	packet.WriteUInt32(argument)
	return packet, nil
}
