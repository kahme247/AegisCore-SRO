package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	QuestResultSuccess byte = 1
	QuestResultFailure byte = 2

	QuestUpdateCreate byte = 1
	QuestUpdateDelta  byte = 2
	QuestUpdateRetire byte = 3
	QuestUpdateAbort  byte = 4

	QuestDeltaTimerFlag    byte = 0x04
	QuestDeltaStatusFlag   byte = 0x08
	QuestDeltaMissionsFlag byte = 0x10
	QuestDeltaValuesFlag   byte = 0x40
	questDeltaAllowedFlags byte = 0x7e // 0x02/0x20 are retained but add no bytes.

	QuestDialogueSelectionLimit = 64
)

type QuestIDRequest struct {
	QuestID uint32
}

func DecodeQuestIDRequest(packet *network.Packet) (QuestIDRequest, error) {
	if packet == nil || packet.Remaining() != 4 {
		return QuestIDRequest{}, questPayloadLengthError(packet, 4)
	}
	questID, err := packet.ReadUInt32()
	if err != nil {
		return QuestIDRequest{}, err
	}
	if questID == 0 {
		return QuestIDRequest{}, fmt.Errorf("quest ID is zero")
	}
	return QuestIDRequest{QuestID: questID}, nil
}

type QuestTalkSelection struct {
	Selection byte
}

func DecodeQuestTalkSelection(packet *network.Packet) (QuestTalkSelection, error) {
	if packet == nil || packet.Remaining() != 1 {
		return QuestTalkSelection{}, questPayloadLengthError(packet, 1)
	}
	selection, err := packet.ReadByte()
	return QuestTalkSelection{Selection: selection}, err
}

type QuestTalkMessage struct {
	Type       byte
	Text       string
	Selections []string
	ExtraText  string
	Argument   uint32
}

func BuildQuestTalk(message QuestTalkMessage) (network.Packet, error) {
	if message.Type != 4 && message.Type != 5 {
		if len(message.Selections) != 0 || message.ExtraText != "" || message.Argument != 0 {
			return network.Packet{}, fmt.Errorf("quest talk type %d has extended fields", message.Type)
		}
	} else if len(message.Selections) > QuestDialogueSelectionLimit {
		return network.Packet{}, fmt.Errorf("quest talk has %d selections, maximum is %d", len(message.Selections), QuestDialogueSelectionLimit)
	}
	if message.Type == 4 && (message.ExtraText != "" || message.Argument != 0) {
		return network.Packet{}, fmt.Errorf("quest talk type 4 has type-5 fields")
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestTalk
	packet.WriteByte(message.Type)
	if err := packet.WriteString(message.Text); err != nil {
		return network.Packet{}, fmt.Errorf("write quest talk text: %w", err)
	}
	if message.Type == 4 || message.Type == 5 {
		packet.WriteByte(byte(len(message.Selections)))
		for index, selection := range message.Selections {
			if err := packet.WriteString(selection); err != nil {
				return network.Packet{}, fmt.Errorf("write quest talk selection %d: %w", index, err)
			}
		}
	}
	if message.Type == 5 {
		if err := packet.WriteString(message.ExtraText); err != nil {
			return network.Packet{}, fmt.Errorf("write quest talk extra text: %w", err)
		}
		packet.WriteUInt32(message.Argument)
	}
	return packet, packet.WriteError()
}

type QuestMissionDelta struct {
	EntryKey       byte
	TransitionCode byte
	Text           string
	Values         []uint32
}

type QuestUpdateMessage struct {
	Subtype             byte
	QuestID             uint32
	PackedQuestByte     byte
	QuestDefinitionByte byte
	Flags               byte
	Timer               uint32
	Status              byte
	Missions            []QuestMissionDelta
	PendingValues       []uint32
}

func BuildQuestUpdate(message QuestUpdateMessage) (network.Packet, error) {
	if message.Subtype < QuestUpdateCreate || message.Subtype > QuestUpdateAbort {
		return network.Packet{}, fmt.Errorf("quest update subtype %d is outside 1..4", message.Subtype)
	}
	if message.QuestID == 0 {
		return network.Packet{}, fmt.Errorf("quest update ID is zero")
	}
	if message.Subtype >= QuestUpdateRetire {
		if message.PackedQuestByte != 0 || message.QuestDefinitionByte != 0 || message.Flags != 0 || message.Timer != 0 || message.Status != 0 || len(message.Missions) != 0 || len(message.PendingValues) != 0 {
			return network.Packet{}, fmt.Errorf("quest update subtype %d has a forbidden tail", message.Subtype)
		}
	} else if message.Flags&0x01 != 0 || message.Flags&^questDeltaAllowedFlags != 0 {
		return network.Packet{}, fmt.Errorf("quest update flags 0x%02X contain unsupported bits", message.Flags)
	}
	if message.Flags&QuestDeltaTimerFlag == 0 && message.Timer != 0 {
		return network.Packet{}, fmt.Errorf("quest update has a timer without flag 0x04")
	}
	if message.Flags&QuestDeltaStatusFlag == 0 && message.Status != 0 {
		return network.Packet{}, fmt.Errorf("quest update has a status without flag 0x08")
	}
	if message.Flags&QuestDeltaMissionsFlag == 0 && len(message.Missions) != 0 {
		return network.Packet{}, fmt.Errorf("quest update has missions without flag 0x10")
	}
	if message.Flags&QuestDeltaValuesFlag == 0 && len(message.PendingValues) != 0 {
		return network.Packet{}, fmt.Errorf("quest update has pending values without flag 0x40")
	}
	if len(message.Missions) > math.MaxUint8 || len(message.PendingValues) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("quest update count exceeds uint8")
	}

	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestUpdate
	packet.WriteByte(message.Subtype)
	packet.WriteUInt32(message.QuestID)
	if message.Subtype >= QuestUpdateRetire {
		return packet, packet.WriteError()
	}
	packet.WriteByte(message.PackedQuestByte)
	packet.WriteByte(message.QuestDefinitionByte)
	packet.WriteByte(message.Flags)
	if message.Flags&QuestDeltaTimerFlag != 0 {
		packet.WriteUInt32(message.Timer)
	}
	if message.Flags&QuestDeltaStatusFlag != 0 {
		packet.WriteByte(message.Status)
	}
	if message.Flags&QuestDeltaMissionsFlag != 0 {
		packet.WriteByte(byte(len(message.Missions)))
		for index, mission := range message.Missions {
			if len(mission.Values) > math.MaxUint8 {
				return network.Packet{}, fmt.Errorf("quest mission %d value count exceeds uint8", index)
			}
			packet.WriteByte(mission.EntryKey)
			packet.WriteByte(mission.TransitionCode)
			if err := packet.WriteString(mission.Text); err != nil {
				return network.Packet{}, fmt.Errorf("write quest mission %d text: %w", index, err)
			}
			packet.WriteByte(byte(len(mission.Values)))
			for _, value := range mission.Values {
				packet.WriteUInt32(value)
			}
		}
	}
	if message.Flags&QuestDeltaValuesFlag != 0 {
		packet.WriteByte(byte(len(message.PendingValues)))
		for _, value := range message.PendingValues {
			packet.WriteUInt32(value)
		}
	}
	return packet, packet.WriteError()
}

type QuestMark struct {
	RuntimeID     uint32
	Flags         byte
	Kind          byte
	AuthoredValue uint16
	X             int32
	Y             int32
	Z             int32
	TargetID      uint32
}

func BuildQuestMarkAdd(mark QuestMark) (network.Packet, error) {
	if mark.RuntimeID == 0 {
		return network.Packet{}, fmt.Errorf("quest mark runtime ID is zero")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestMarkAdd
	packet.WriteUInt32(mark.RuntimeID)
	packet.WriteByte(mark.Flags)
	packet.WriteByte(mark.Kind)
	packet.WriteUInt16(mark.AuthoredValue)
	packet.WriteUInt32(uint32(mark.X))
	packet.WriteUInt32(uint32(mark.Y))
	packet.WriteUInt32(uint32(mark.Z))
	if mark.Flags&0x02 != 0 {
		packet.WriteUInt32(mark.TargetID)
	} else if mark.TargetID != 0 {
		return network.Packet{}, fmt.Errorf("quest mark target ID requires flag 0x02")
	}
	return packet, packet.WriteError()
}

func BuildQuestMarkRemove(runtimeID uint32) (network.Packet, error) {
	if runtimeID == 0 {
		return network.Packet{}, fmt.Errorf("quest mark runtime ID is zero")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestMarkRemove
	packet.WriteUInt32(runtimeID)
	return packet, packet.WriteError()
}

func BuildQuestActionResult(packetOpcode uint16, value uint32, result byte) (network.Packet, error) {
	if packetOpcode != opcode.QuestGatherResult && packetOpcode != opcode.QuestCatchResult {
		return network.Packet{}, fmt.Errorf("opcode 0x%04X is not a quest action result", packetOpcode)
	}
	packet := network.EmptyPacket()
	packet.MessageID = packetOpcode
	packet.WriteUInt32(value)
	packet.WriteByte(result)
	return packet, packet.WriteError()
}

func BuildQuestNotification(token string) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestNotification
	if err := packet.WriteString(token); err != nil {
		return network.Packet{}, err
	}
	return packet, packet.WriteError()
}

func BuildQuestRewardPrompt(id uint32, mode byte, token string) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestRewardPrompt
	packet.WriteUInt32(id)
	packet.WriteByte(mode)
	if err := packet.WriteString(token); err != nil {
		return network.Packet{}, err
	}
	return packet, packet.WriteError()
}

type QuestRewardSelectionRequest struct {
	RewardID uint32
	EntryIDs []uint32
}

func DecodeQuestRewardSelectionRequest(packet *network.Packet) (QuestRewardSelectionRequest, error) {
	if packet == nil || packet.Remaining() < 5 {
		return QuestRewardSelectionRequest{}, fmt.Errorf("quest reward selection payload is truncated")
	}
	rewardID, err := packet.ReadUInt32()
	if err != nil {
		return QuestRewardSelectionRequest{}, err
	}
	count, err := packet.ReadByte()
	if err != nil {
		return QuestRewardSelectionRequest{}, err
	}
	if packet.Remaining() != int(count)*4 {
		return QuestRewardSelectionRequest{}, fmt.Errorf("quest reward selection count %d has %d trailing bytes", count, packet.Remaining())
	}
	entries := make([]uint32, count)
	for index := range entries {
		entries[index], err = packet.ReadUInt32()
		if err != nil {
			return QuestRewardSelectionRequest{}, err
		}
	}
	return QuestRewardSelectionRequest{RewardID: rewardID, EntryIDs: entries}, nil
}

func BuildQuestResponse(responseOpcode uint16, success bool, questID uint32, reason byte) (network.Packet, error) {
	switch responseOpcode {
	case opcode.QuestInteractionResponse, opcode.QuestAbandonResponse, opcode.QuestLocalActionResponse:
	default:
		return network.Packet{}, fmt.Errorf("opcode 0x%04X is not a quest response", responseOpcode)
	}
	packet := network.EmptyPacket()
	packet.MessageID = responseOpcode
	if success {
		if questID == 0 {
			return network.Packet{}, fmt.Errorf("successful quest response ID is zero")
		}
		packet.WriteByte(QuestResultSuccess)
		packet.WriteUInt32(questID)
	} else {
		packet.WriteByte(QuestResultFailure)
		packet.WriteByte(reason)
	}
	return packet, packet.WriteError()
}

func BuildQuestRewardSelectionResponse(success bool, reason byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.QuestRewardSelectionResponse
	if success {
		packet.WriteByte(QuestResultSuccess)
	} else {
		packet.WriteByte(QuestResultFailure)
		packet.WriteByte(reason)
	}
	return packet
}

func questPayloadLengthError(packet *network.Packet, want int) error {
	remaining := 0
	if packet != nil {
		remaining = packet.Remaining()
	}
	return fmt.Errorf("quest payload length %d, want %d", remaining, want)
}
