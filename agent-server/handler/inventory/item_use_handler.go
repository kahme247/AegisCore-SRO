package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/sirupsen/logrus"
)

type ItemUseHandler struct {
	players PlayerLookup
	useCase service.RecoveryItemUseCase
	send    func(*server.Session, network.Packet) error
}

func NewItemUseHandler(players PlayerLookup, items model.ItemUseRepository) *ItemUseHandler {
	return &ItemUseHandler{players: players, useCase: service.NewItemUseService(items)}
}

func RegisterItemUseHandler(target *server.Server, items model.ItemUseRepository) {
	if target == nil {
		return
	}
	h := NewItemUseHandler(service.GetWorldServiceInstance(), items)
	target.RegisterHandler(opcode.InventoryItemUseRequest, h.handlePacket)
}

func (h *ItemUseHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	if h == nil || h.useCase == nil || packet.Remaining() != 3 {
		return h.sendFailure(session)
	}
	slot, e1 := packet.ReadByte()
	itemType, e2 := packet.ReadUInt16()
	if errors.Join(e1, e2) != nil || packet.Remaining() != 0 {
		return h.sendFailure(session)
	}
	resolver := &InventoryHandler{players: h.players}
	player, err := resolver.resolvePlayer(session)
	if err != nil {
		return err
	}
	result, err := h.useCase.UseRecoveryItem(ctx, player, slot, itemType)
	if err != nil {
		if errors.Is(err, model.ErrItemUseOutcomeUnknown) {
			return err
		}
		logrus.WithError(err).Debug("rejected inventory item use")
		return h.sendFailure(session)
	}
	if err = h.sendPacket(session, buildItemUseSuccess(result.SourceSlot, result.ItemType, result.NewAmount)); err != nil {
		return err
	}
	player.SendVitalsUpdate()
	return nil
}

func (h *ItemUseHandler) sendFailure(session *server.Session) error {
	return h.sendPacket(session, buildItemUseFailure())
}
func (h *ItemUseHandler) sendPacket(session *server.Session, p network.Packet) error {
	if session == nil {
		return fmt.Errorf("item-use session is nil")
	}
	if h != nil && h.send != nil {
		return h.send(session, p)
	}
	return session.Send(p)
}
func buildItemUseSuccess(sourceSlot byte, itemType, newAmount uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUseResponse
	p.WriteByte(1)
	p.WriteByte(sourceSlot)
	p.WriteUInt16(newAmount)
	p.WriteUInt16(itemType)
	return p
}

func buildItemUseFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.InventoryItemUseResponse
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}
