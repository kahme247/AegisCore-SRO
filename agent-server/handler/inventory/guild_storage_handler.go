package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ferdoran/go-sro/agent-server/handler/character"
	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/sirupsen/logrus"
)

const (
	guildStorageTalkAction = byte(0x0D)
	guildStorageChunkSize  = 4096
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
	_, npcUID, err := h.validateRequest(session, &packet)
	if err != nil {
		return h.sendPacket(session, buildGuildStorageOpenFailure(0x4A))
	}
	if !character.HasRecentNPCInteraction(session.UserContext.UniqueID, npcUID, guildStorageTalkAction, 5*time.Minute) {
		return h.sendPacket(session, buildGuildStorageOpenFailure(0x4A))
	}
	if err = h.repo.OpenGuildStorage(ctx, int64(session.UserContext.CharacterID)); err != nil {
		code := byte(0x4A)
		if errors.Is(err, model.ErrGuildStorageInUse) {
			code = 0x48
		}
		logrus.WithError(err).Debug("rejected guild storage open")
		return h.sendPacket(session, buildGuildStorageOpenFailure(code))
	}
	return h.sendPacket(session, buildGuildStorageOpenSuccess())
}

func (h *GuildStorageHandler) handleInfo(ctx context.Context, session *server.Session, packet network.Packet) error {
	_, _, err := h.validateRequest(session, &packet)
	if err != nil {
		return nil
	}
	snapshot, err := h.repo.LoadGuildStorage(ctx, int64(session.UserContext.CharacterID))
	if err != nil {
		logrus.WithError(err).Debug("rejected guild storage info")
		return nil
	}
	for _, response := range buildGuildStorageResponses(snapshot) {
		if err = h.sendPacket(session, response); err != nil {
			return err
		}
	}
	return nil
}

func (h *GuildStorageHandler) handleClose(ctx context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 4 {
		return fmt.Errorf("guild storage close payload length %d, want 4", packet.Remaining())
	}
	if _, err := packet.ReadUInt32(); err != nil {
		return err
	}
	if session == nil || session.UserContext.CharacterID <= 0 {
		return nil
	}
	if err := h.repo.CloseGuildStorage(ctx, int64(session.UserContext.CharacterID)); err != nil {
		logrus.WithError(err).Debug("guild storage close cleanup")
	}
	return h.sendPacket(session, buildGuildStorageCloseSuccess())
}

func (h *GuildStorageHandler) validateRequest(session *server.Session, packet *network.Packet) (*model.Player, uint32, error) {
	if h == nil || h.world == nil || packet == nil || packet.Remaining() != 4 || session == nil {
		return nil, 0, ErrInventoryHandlerSessionIdentityInvalid
	}
	npcUID, err := packet.ReadUInt32()
	if err != nil {
		return nil, 0, err
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, 0, ErrInventoryHandlerSessionPlayerMismatch
	}
	known := player.GetKnownObjectList()
	if known == nil {
		return nil, 0, ErrInventoryHandlerPlayerLookupUnavailable
	}
	knownObject, ok := known.GetKnownObjects()[npcUID]
	if !ok || knownObject == nil {
		return nil, 0, ErrInventoryHandlerPlayerLookupUnavailable
	}
	object, err := h.world.GetObjectByUniqueId(npcUID)
	if err != nil || object == nil || object != knownObject {
		return nil, 0, ErrInventoryHandlerPlayerLookupUnavailable
	}
	npc, ok := object.(*model.NPC)
	if !ok || npc == nil || !npc.GetTypeInfo().IsNPCNpc() || !strings.Contains(strings.ToUpper(npc.GetName()), "GUILD") {
		return nil, 0, fmt.Errorf("object %d is not a guild NPC", npcUID)
	}
	return player, npcUID, nil
}

func (h *GuildStorageHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func buildGuildStorageOpenSuccess() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildStorageOpenResponse
	p.WriteByte(1)
	return p
}

func buildGuildStorageOpenFailure(code byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildStorageOpenResponse
	p.WriteByte(2)
	p.WriteByte(code)
	p.WriteByte(0x4C)
	return p
}

func buildGuildStorageCloseSuccess() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildStorageCloseResponse
	p.WriteByte(1)
	return p
}

func buildGuildStorageResponses(snapshot model.StorageSnapshot) []network.Packet {
	begin := network.EmptyPacket()
	begin.MessageID = opcode.GuildStorageDataBegin
	begin.WriteUInt64(snapshot.Gold)

	data := network.EmptyPacket()
	capacity := snapshot.Capacity
	if capacity == 0 {
		capacity = baseStorageSize
	}
	data.WriteByte(capacity)
	slots := model.SortedInventorySlots(snapshot.Items)
	data.WriteByte(byte(len(slots)))
	for _, slot := range slots {
		item := snapshot.Items[slot]
		data.WriteByte(slot)
		model.WriteRentInfo(&data, item)
		data.WriteUInt32(item.GetRefObjectID())
		model.WriteInventoryItem(&data, item)
	}

	responses := []network.Packet{begin}
	payload := data.Payload()
	for len(payload) > 0 {
		size := len(payload)
		if size > guildStorageChunkSize {
			size = guildStorageChunkSize
		}
		chunk := network.EmptyPacket()
		chunk.MessageID = opcode.GuildStorageData
		chunk.WriteBytes(payload[:size])
		responses = append(responses, chunk)
		payload = payload[size:]
	}
	end := network.EmptyPacket()
	end.MessageID = opcode.GuildStorageDataEnd
	return append(responses, end)
}
