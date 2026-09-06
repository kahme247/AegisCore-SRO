package character

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

const (
	quickSlotConfigType  byte = 1
	autoPotionConfigType byte = 2
)

type quickSlotWorld interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type QuickSlotHandler struct {
	mu         sync.Mutex
	world      quickSlotWorld
	repository model.CharacterQuickSlotRepository
	autoPotion model.AutoPotionSettingsRepository
}

func RegisterQuickSlotHandler(target *server.Server, repository model.CharacterQuickSlotRepository, autoPotion model.AutoPotionSettingsRepository) error {
	if target == nil || repository == nil || autoPotion == nil {
		return model.ErrCharacterQuickSlotRepositoryUnavailable
	}
	handler := &QuickSlotHandler{world: service.GetWorldServiceInstance(), repository: repository, autoPotion: autoPotion}
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
	if configType == autoPotionConfigType {
		if packet.Remaining() != model.AutoPotionSettingsSize {
			return fmt.Errorf("auto-potion config payload length %d, want %d", packet.Remaining(), model.AutoPotionSettingsSize)
		}
		payload, err := packet.ReadBytes(model.AutoPotionSettingsSize)
		if err != nil {
			return err
		}
		player, ok := h.resolvePlayer(session)
		if !ok {
			return nil
		}
		var settings model.AutoPotionSettings
		copy(settings[:], payload)
		if err := h.autoPotion.SaveAutoPotionSettings(ctx, player.ID, settings); err != nil {
			return err
		}
		player.SetAutoPotionSettings(settings, true)
		return nil
	}
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
	player, ok := h.resolvePlayer(session)
	if !ok {
		return nil
	}
	slot := model.CharacterQuickSlot{Sequence: sequence, ContentType: contentType, ContentID: contentID}
	if err := h.repository.SaveQuickSlot(ctx, player.ID, slot); err != nil {
		return err
	}
	player.CommitQuickSlot(slot)
	return nil
}

func (h *QuickSlotHandler) resolvePlayer(session *server.Session) (*model.Player, bool) {
	if h == nil || h.world == nil || session == nil || session.UserContext.UniqueID == 0 {
		return nil, false
	}
	player, err := h.world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID <= 0 || player.ID != session.UserContext.CharacterID {
		return nil, false
	}
	return player, true
}
