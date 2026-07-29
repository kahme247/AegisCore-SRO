package character

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

const quickSlotConfigType byte = 1

type quickSlotWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type QuickSlotHandler struct {
	mu         sync.Mutex
	world      quickSlotWorld
	repository model.CharacterQuickSlotRepository
}

func RegisterQuickSlotHandler(target *server.Server, repository model.CharacterQuickSlotRepository) error {
	if target == nil || repository == nil {
		return model.ErrCharacterQuickSlotRepositoryUnavailable
	}
	handler := &QuickSlotHandler{world: service.GetWorldServiceInstance(), repository: repository}
	target.RegisterHandler(opcode.CharacterConfigUpdateRequest, handler.handlePacket)
	return nil
}

func (h *QuickSlotHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if packet.Remaining() < 1 {
		return fmt.Errorf("character config update has no type")
	}
	configType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	// 0x7158 carries several unrelated client settings. Only subtype 1 is the
	// character hotbar; leave other subtypes wire-neutral until established.
	if configType != quickSlotConfigType {
		return nil
	}
	if packet.Remaining() != 6 {
		return fmt.Errorf("quick-slot config payload length %d, want 6", packet.Remaining())
	}
	sequence, err := packet.ReadByte()
	if err != nil {
		return err
	}
	contentType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	contentID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	if int(sequence) >= model.MaxCharacterQuickSlots {
		return fmt.Errorf("quick-slot sequence %d is out of range", sequence)
	}
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID <= 0 || player.ID != session.UserContext.CharacterID {
		return nil
	}
	slot := model.CharacterQuickSlot{Sequence: sequence, ContentType: contentType, ContentID: contentID}
	if err := h.repository.SaveQuickSlot(ctx, player.ID, slot); err != nil {
		return err
	}
	player.CommitQuickSlot(slot)
	return nil
}
