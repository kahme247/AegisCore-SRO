package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

type JobPrice struct {
	RefItemID uint32
	Price     uint32
}

type SafeTradeUpdate struct {
	// Active is the first byte emitted by SR_GameServer 0x0051A960. It is the
	// persisted safe-trade active flag, not a generic packet result value.
	Active bool
	// Code is stock's raw safe-trade transition byte.
	Code    byte
	Current byte
	Limit   byte
}

const (
	JobSafeTradeCodeTransition40 byte = 0x40
	JobSafeTradeErrorLimit       byte = 0x41
	JobSafeTradeCodeProgress     byte = 0x50
	JobSafeTradeCodeRestore51    byte = 0x51
)

type JobPriceUpdateCodec struct{}
type JobCOSDistanceCodec struct{}
type JobTradeScaleCodec struct{}
type JobSafeTradeCodec struct{}

func (JobPriceUpdateCodec) Encode(prices []JobPrice) (network.Packet, error) {
	if len(prices) > math.MaxUint8 {
		return network.Packet{}, fmt.Errorf("job price list has %d entries", len(prices))
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.JobUpdatePrice
	packet.WriteByte(byte(len(prices)))
	for _, price := range prices {
		packet.WriteUInt32(price.RefItemID)
		packet.WriteUInt32(price.Price)
	}
	return packet, packet.WriteError()
}

func (JobCOSDistanceCodec) Encode(reason byte) (network.Packet, error) {
	if reason != 1 && reason != 2 {
		return network.Packet{}, fmt.Errorf("job COS distance reason %d is invalid", reason)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.JobCOSDistance
	packet.WriteByte(reason)
	return packet, packet.WriteError()
}

func (JobTradeScaleCodec) Encode(scale byte) (network.Packet, error) {
	if scale > 5 {
		return network.Packet{}, fmt.Errorf("job trade scale %d is invalid", scale)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.JobUpdateTradeScale
	packet.WriteByte(scale)
	return packet, packet.WriteError()
}

func (JobSafeTradeCodec) Encode(update SafeTradeUpdate) (network.Packet, error) {
	if update.Code != JobSafeTradeCodeTransition40 && update.Code != JobSafeTradeErrorLimit && update.Code != JobSafeTradeCodeProgress && update.Code != JobSafeTradeCodeRestore51 {
		return network.Packet{}, fmt.Errorf("job safe-trade transition 0x%02X is invalid", update.Code)
	}
	if update.Active != (update.Code == JobSafeTradeCodeProgress || update.Code == JobSafeTradeCodeRestore51) {
		return network.Packet{}, fmt.Errorf("job safe-trade transition 0x%02X has inconsistent active state", update.Code)
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.JobUpdateSafeTrade
	if update.Active {
		packet.WriteByte(1)
		packet.WriteByte(update.Code)
		packet.WriteByte(update.Current)
		packet.WriteByte(update.Limit)
	} else {
		packet.WriteByte(0)
		packet.WriteByte(update.Code)
	}
	return packet, packet.WriteError()
}
