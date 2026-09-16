package vsro188

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type VietnamMRUpdate struct {
	ElapsedSeconds uint16
	StartHour      byte
	EndHour        byte
}

type ServerEventData struct {
	ServerEventID uint32 `json:"serverEventId"`
	Value         uint32 `json:"value"`
	Target        uint32 `json:"target"`
}

type ServerEventTimedReward struct {
	RewardID        uint32
	ServerEventID   uint32
	RewardClass     byte
	ReferenceValue1 uint32
	ReferenceValue2 uint32
	ReferenceValue3 uint32
	Remaining       uint32
}

func BuildGlobalEventUpdate(name string, value uint32) (network.Packet, error) {
	if err := ValidateText(name, TextRules{Field: "global event", MaxRunes: 128, RequireTrimmed: true}); err != nil || name == "" {
		return network.Packet{}, fmt.Errorf("invalid global event update")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GlobalEventUpdate
	if err := p.WriteString(name); err != nil {
		return network.Packet{}, err
	}
	p.WriteUInt32(value)
	return p, nil
}

func DecodeChristmasEventRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid Christmas event request")
	}
	return nil
}

func BuildChristmasEventSuccess() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ChristmasEventResponse
	p.WriteByte(1)
	return p
}

func BuildChristmasEventFailure(errorCode byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ChristmasEventResponse
	p.WriteByte(2)
	p.WriteByte(errorCode)
	return p
}

func BuildVietnamMRSystemUpdate(update VietnamMRUpdate) (network.Packet, error) {
	if update.StartHour >= 24 || update.EndHour >= 24 || update.StartHour >= update.EndHour {
		return network.Packet{}, fmt.Errorf("invalid Vietnam MR update")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.VietnamMRSystemUpdate
	p.WriteUInt16(update.ElapsedSeconds)
	p.WriteByte(update.StartHour)
	p.WriteByte(update.EndHour)
	return p, nil
}

func newServerEventPacket(kind byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ServerEventUpdate
	p.WriteByte(kind)
	return p
}

// BuildServerEventDataSnapshot is the binary-confirmed entry-game form:
// literal 02/03, count, then three u32 values per event record.
func BuildServerEventDataSnapshot(records []ServerEventData) network.Packet {
	p := newServerEventPacket(2)
	p.WriteByte(3)
	p.WriteUInt32(uint32(len(records)))
	for _, record := range records {
		p.WriteUInt32(record.ServerEventID)
		p.WriteUInt32(record.Value)
		p.WriteUInt32(record.Target)
	}
	return p
}

// BuildServerEventTimedRewards is the binary-confirmed remaining-reward form.
func BuildServerEventTimedRewards(rewards []ServerEventTimedReward) (network.Packet, error) {
	p := newServerEventPacket(7)
	p.WriteUInt32(uint32(len(rewards)))
	for _, reward := range rewards {
		if reward.RewardClass != 1 {
			return network.Packet{}, fmt.Errorf("unsupported timed server event reward class %d", reward.RewardClass)
		}
		p.WriteUInt32(reward.RewardID)
		p.WriteUInt32(reward.ServerEventID)
		p.WriteByte(reward.RewardClass)
		p.WriteUInt32(reward.ReferenceValue1)
		p.WriteUInt32(reward.ReferenceValue2)
		p.WriteUInt32(reward.ReferenceValue3)
		p.WriteUInt32(reward.Remaining)
	}
	return p, nil
}

func BuildServerEventRewardCompleted(serverEventID, rewardID uint32) (network.Packet, error) {
	if serverEventID == 0 || rewardID == 0 {
		return network.Packet{}, fmt.Errorf("server event and reward IDs are required")
	}
	p := newServerEventPacket(8)
	p.WriteUInt32(serverEventID)
	p.WriteUInt32(rewardID)
	return p, nil
}
