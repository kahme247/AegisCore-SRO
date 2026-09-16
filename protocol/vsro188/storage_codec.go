package vsro188

import (
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

// StorageDataChunkSize matches GameServer 0x430A20: each storage stream
// fragment is capped at the stock 4096-byte message capacity minus 0x20 bytes
// reserved by the message writer.
const StorageDataChunkSize = int(network.BufferSize) - 0x20

// StorageItemRecord keeps the fixed storage envelope separate from the
// TypeID-driven rent/item bodies owned by the domain serializer.
type StorageItemRecord struct {
	Slot        byte
	RentPayload []byte
	RefItemID   uint32
	ItemPayload []byte
}

// BuildStorageItemDataPayload writes the common capacity/count/slot record
// stream carried inside 0x3049 or 0x3255 data chunks.
func BuildStorageItemDataPayload(capacity byte, records []StorageItemRecord) network.Packet {
	packet := network.EmptyPacket()
	packet.WriteByte(capacity)
	packet.WriteByte(byte(len(records)))
	for _, record := range records {
		packet.WriteByte(record.Slot)
		_ = packet.WriteBytes(record.RentPayload)
		packet.WriteUInt32(record.RefItemID)
		_ = packet.WriteBytes(record.ItemPayload)
	}
	return packet
}

func BuildGuildStorageOpenSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildStorageOpenResponse
	packet.WriteByte(1)
	return packet
}

func BuildGuildStorageOpenFailure(code byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildStorageOpenResponse
	packet.WriteByte(2)
	packet.WriteByte(code)
	packet.WriteByte(0x4C)
	return packet
}

func BuildGuildStorageCloseSuccess() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildStorageCloseResponse
	packet.WriteByte(1)
	return packet
}

func BuildGuildStorageCloseFailure(errorCode uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildStorageCloseResponse
	packet.WriteByte(2)
	packet.WriteUInt16(errorCode)
	return packet
}

func BuildGuildStorageListFailure(errorCode uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.GuildStorageListResponse
	packet.WriteByte(2)
	packet.WriteUInt16(errorCode)
	return packet
}

// BuildStorageDataBegin writes the common storage-transfer begin body: two
// consecutive little-endian u32 values (represented here as their existing
// wire-equivalent uint64). The binary evidence does not assign public names
// to the two words.
func BuildStorageDataBegin(messageID uint16, valuePair uint64) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteUInt64(valuePair)
	return packet
}

// BuildStorageDataChunk wraps an already serialized storage fragment. The
// fragment may begin with the capacity/count header or continue a previous
// fragment; this function deliberately performs no item parsing.
func BuildStorageDataChunk(messageID uint16, payload []byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	packet.WriteBytes(payload)
	return packet
}

// BuildStorageDataEnd writes the empty storage-transfer terminator.
func BuildStorageDataEnd(messageID uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = messageID
	return packet
}

// ChunkStorageData splits a serialized storage stream into stock-compatible
// data packets. An empty stream produces no data packet; callers still send
// the begin and end packets.
func ChunkStorageData(messageID uint16, payload []byte) []network.Packet {
	if len(payload) == 0 {
		return nil
	}
	packets := make([]network.Packet, 0, (len(payload)+StorageDataChunkSize-1)/StorageDataChunkSize)
	for len(payload) > 0 {
		size := len(payload)
		if size > StorageDataChunkSize {
			size = StorageDataChunkSize
		}
		packets = append(packets, BuildStorageDataChunk(messageID, payload[:size]))
		payload = payload[size:]
	}
	return packets
}
