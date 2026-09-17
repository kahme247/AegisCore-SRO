package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/handler/character"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

const (
	guildStorageTalkAction = byte(0x0D)
	guildStorageChunkSize  = vsro188.StorageDataChunkSize
)

type GuildStorageHandler struct {
	world storageWorld
	repo  model.GuildStorageRepository
	send  func(*server.Session, network.Packet) error
}

func RegisterGuildStorageHandler(target *server.Server, repo model.GuildStorageRepository) {
	if target == nil || repo == nil {
		return
	}
	h := &GuildStorageHandler{world: service.GetWorldServiceInstance(), repo: repo}
	target.RegisterHandler(opcode.GuildStorageOpenRequest, h.handleOpen)
	target.RegisterHandler(opcode.GuildStorageInfoRequest, h.handleInfo)
	target.RegisterHandler(opcode.GuildStorageCloseRequest, h.handleClose)
}

func (h *GuildStorageHandler) handleOpen(ctx context.Context, session *server.Session, packet network.Packet) error {
	_, err := h.validateOpenRequest(session, &packet)
	if err != nil {
		return h.sendPacket(session, buildGuildStorageOpenFailure(0x4A))
	}
	if err = h.repo.OpenGuildStorage(ctx, int64(session.UserContext.CharacterID)); err != nil {
		logrus.WithError(err).Debug("rejected guild storage open")
		return h.sendPacket(session, buildGuildStorageOpenFailure(byte(guildStorageErrorCode(err))))
	}
	return h.sendPacket(session, buildGuildStorageOpenSuccess())
}

func (h *GuildStorageHandler) handleInfo(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 || session == nil || session.UserContext.CharacterID <= 0 {
		return h.sendPacket(session, vsro188.BuildGuildStorageListFailure(3))
	}
	snapshot, err := h.repo.LoadGuildStorage(ctx, int64(session.UserContext.CharacterID))
	if err != nil {
		logrus.WithError(err).Debug("rejected guild storage info")
		return h.sendPacket(session, vsro188.BuildGuildStorageListFailure(guildStorageErrorCode(err)))
	}
	for _, response := range buildGuildStorageResponses(snapshot) {
		if err = h.sendPacket(session, response); err != nil {
			return err
		}
	}
	return nil
}

func guildStorageErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrGuildStorageInUse):
		return 0x4C48
	case errors.Is(err, model.ErrGuildStorageLevelTooLow):
		return 0x4C4A
	default:
		return 0x4C49
	}
}

func (h *GuildStorageHandler) handleClose(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 || session == nil || session.UserContext.CharacterID <= 0 {
		return h.sendPacket(session, vsro188.BuildGuildStorageCloseFailure(3))
	}
	if err := h.repo.CloseGuildStorage(ctx, int64(session.UserContext.CharacterID)); err != nil {
		logrus.WithError(err).Debug("guild storage close cleanup")
		return h.sendPacket(session, vsro188.BuildGuildStorageCloseFailure(0x4C49))
	}
	return h.sendPacket(session, buildGuildStorageCloseSuccess())
}

func (h *GuildStorageHandler) validateOpenRequest(session *server.Session, packet *network.Packet) (*model.Player, error) {
	if h == nil || h.world == nil || packet == nil || packet.Remaining() != 0 || session == nil {
		return nil, ErrInventoryHandlerSessionIdentityInvalid
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, ErrInventoryHandlerSessionPlayerMismatch
	}
	interaction, ok := character.RecentNPCInteraction(session.UserContext.UniqueID, guildStorageTalkAction, 5*time.Minute)
	if !ok {
		return nil, ErrInventoryHandlerPlayerLookupUnavailable
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return nil, ErrInventoryHandlerPlayerLookupUnavailable
	}
	knownObject, ok := known.GetKnownObjects()[interaction.NPCUniqueID]
	if !ok || knownObject == nil {
		return nil, ErrInventoryHandlerPlayerLookupUnavailable
	}
	object, err := h.world.GetObjectByUniqueId(interaction.NPCUniqueID)
	if err != nil || object == nil || object != knownObject {
		return nil, ErrInventoryHandlerPlayerLookupUnavailable
	}
	npc, ok := object.(*model.NPC)
	if !ok || npc == nil || !npc.GetTypeInfo().IsNPCNpc() || !strings.Contains(strings.ToUpper(npc.GetName()), "GUILD") {
		return nil, fmt.Errorf("object %d is not a guild NPC", interaction.NPCUniqueID)
	}
	return player, nil
}

func (h *GuildStorageHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func buildGuildStorageOpenSuccess() network.Packet {
	return vsro188.BuildGuildStorageOpenSuccess()
}

func buildGuildStorageOpenFailure(code byte) network.Packet {
	return vsro188.BuildGuildStorageOpenFailure(code)
}

func buildGuildStorageCloseSuccess() network.Packet {
	return vsro188.BuildGuildStorageCloseSuccess()
}

func buildGuildStorageResponses(snapshot model.StorageSnapshot) []network.Packet {
	begin := vsro188.BuildStorageDataBegin(opcode.GuildStorageDataBegin, snapshot.Gold)

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
	responses = append(responses, vsro188.ChunkStorageData(opcode.GuildStorageData, data.Payload())...)
	end := vsro188.BuildStorageDataEnd(opcode.GuildStorageDataEnd)
	return append(responses, end)
}
