package inventory

import (
	"context"
	"fmt"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const baseStorageSize byte = 150
const storageDataChunkSize = vsro188.StorageDataChunkSize

type storageWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetObjectByUniqueId(uint32) (model.ISRObject, error)
}

type StorageHandler struct {
	world   storageWorld
	economy model.EconomyRepository
	send    func(*server.Session, network.Packet) error
}

func RegisterStorageHandler(target *server.Server, economy ...model.EconomyRepository) {
	if target == nil {
		return
	}
	handler := &StorageHandler{world: service.GetWorldServiceInstance()}
	if len(economy) > 0 {
		handler.economy = economy[0]
	}
	target.RegisterHandler(opcode.StorageOpenRequest, handler.handlePacket)
}

func (h *StorageHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 5 {
		return fmt.Errorf("storage open payload length %d, want 5", packet.Remaining())
	}
	npcUID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read storage NPC UID: %w", err)
	}
	mode, err := packet.ReadByte()
	if err != nil {
		return fmt.Errorf("read storage open mode: %w", err)
	}
	if mode != 0 {
		return fmt.Errorf("storage open mode %d, want 0", mode)
	}
	if err := h.validate(session, npcUID); err != nil {
		return err
	}

	if h.economy == nil {
		return fmt.Errorf("storage repository is unavailable")
	}
	snapshot, err := h.economy.LoadStorage(ctx, session.UserContext.UserID)
	if err != nil {
		return err
	}
	for _, response := range buildStorageResponses(snapshot) {
		if h.send != nil {
			err = h.send(session, response)
		} else {
			err = session.Send(response)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *StorageHandler) validate(session *server.Session, npcUID uint32) error {
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return ErrInventoryHandlerSessionIdentityInvalid
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil {
		return ErrInventoryHandlerPlayerLookupUnavailable
	}
	if player.Session != session || player.ID != session.UserContext.CharacterID || player.GetUniqueID() != session.UserContext.UniqueID {
		return ErrInventoryHandlerSessionPlayerMismatch
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return fmt.Errorf("storage NPC %d is not known", npcUID)
	}
	knownObject, exists := known.GetKnownObjects()[npcUID]
	if !exists || knownObject == nil {
		return fmt.Errorf("storage NPC %d is not known", npcUID)
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil || object != knownObject {
		return fmt.Errorf("storage NPC %d is unavailable", npcUID)
	}
	npc, ok := object.(*model.NPC)
	if !ok || npc == nil || !npc.GetTypeInfo().IsNPCNpc() || !strings.Contains(strings.ToUpper(npc.GetName()), "WAREHOUSE") {
		return fmt.Errorf("object %d is not a warehouse NPC", npcUID)
	}
	return nil
}

func buildEmptyStorageResponses() []network.Packet {
	return buildStorageResponses(model.StorageSnapshot{Capacity: baseStorageSize, Items: map[byte]model.Item{}})
}

func buildStorageResponses(snapshot model.StorageSnapshot) []network.Packet {
	begin := vsro188.BuildStorageDataBegin(opcode.StorageDataBegin, snapshot.Gold)

	capacity := snapshot.Capacity
	if capacity == 0 {
		capacity = baseStorageSize
	}
	slots := model.SortedInventorySlots(snapshot.Items)
	records := make([]vsro188.StorageItemRecord, 0, len(slots))
	for _, slot := range slots {
		item := snapshot.Items[slot]
		rent := network.EmptyPacket()
		model.WriteRentInfo(&rent, item)
		body := network.EmptyPacket()
		model.WriteInventoryItem(&body, item)
		records = append(records, vsro188.StorageItemRecord{Slot: slot, RentPayload: rent.Payload(), RefItemID: item.GetRefObjectID(), ItemPayload: body.Payload()})
	}
	data := vsro188.BuildStorageItemDataPayload(capacity, records)

	responses := []network.Packet{begin}
	for _, chunk := range chunkStorageData(data.Payload()) {
		responses = append(responses, chunk)
	}

	end := vsro188.BuildStorageDataEnd(opcode.StorageDataEnd)
	return append(responses, end)
}

func chunkStorageData(payload []byte) []network.Packet {
	return vsro188.ChunkStorageData(opcode.StorageData, payload)
}
