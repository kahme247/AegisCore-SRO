package cos

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/agent-server/service"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

const mountStateRequestSize = 5

type playerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type MountStateHandler struct {
	players   playerLookup
	send      func(*server.Session, network.Packet) error
	broadcast func(*model.Player, network.Packet) error
}

type mountStateRequest struct {
	mounted           bool
	transportUniqueID uint32
}

func NewMountStateHandler(players playerLookup) *MountStateHandler {
	return &MountStateHandler{players: players}
}

func RegisterMountStateHandler(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewMountStateHandler(service.GetWorldServiceInstance())
	target.RegisterHandler(opcode.COSUpdateMountStateRequest, handler.handlePacket)
}

func parseMountStateRequest(packet *network.Packet) (mountStateRequest, error) {
	if packet == nil || packet.Remaining() != mountStateRequestSize {
		remaining := 0
		if packet != nil {
			remaining = packet.Remaining()
		}
		return mountStateRequest{}, fmt.Errorf("COS mount-state payload length %d, want %d", remaining, mountStateRequestSize)
	}
	action, err := packet.ReadByte()
	if err != nil {
		return mountStateRequest{}, fmt.Errorf("read COS mount-state action: %w", err)
	}
	if action != 0 && action != 1 {
		return mountStateRequest{}, fmt.Errorf("invalid COS mount-state action %d", action)
	}
	uniqueID, err := packet.ReadUInt32()
	if err != nil {
		return mountStateRequest{}, fmt.Errorf("read COS transport unique ID: %w", err)
	}
	if uniqueID == 0 {
		return mountStateRequest{}, model.ErrInvalidTransport
	}
	if packet.Remaining() != 0 {
		return mountStateRequest{}, fmt.Errorf("COS mount-state request has %d trailing bytes", packet.Remaining())
	}
	return mountStateRequest{mounted: action == 1, transportUniqueID: uniqueID}, nil
}

func (h *MountStateHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	request, err := parseMountStateRequest(&packet)
	if err != nil {
		return err
	}
	player, err := h.resolvePlayer(session)
	if err != nil {
		return err
	}
	snapshot, err := player.COSState.SetMounted(request.transportUniqueID, request.mounted)
	if err != nil {
		if errors.Is(err, model.ErrNoActiveTransport) || errors.Is(err, model.ErrTransportNotOwned) || errors.Is(err, model.ErrInvalidTransport) {
			return h.sendPacket(session, buildMountStateFailure())
		}
		return err
	}
	response := buildMountStateSuccess(player.GetUniqueID(), snapshot.Mounted, snapshot.TransportUniqueID)
	if err = h.sendPacket(session, response); err != nil {
		return err
	}
	return h.broadcastPacket(player, response)
}

func (h *MountStateHandler) resolvePlayer(session *server.Session) (*model.Player, error) {
	if h == nil || h.players == nil || session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, errors.New("invalid COS mount-state session identity")
	}
	player, err := h.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		return nil, err
	}
	if player == nil || player.Session != session || player.GetUniqueID() != session.UserContext.UniqueID || player.ID != session.UserContext.CharacterID {
		return nil, errors.New("COS mount-state player ownership mismatch")
	}
	return player, nil
}

func (h *MountStateHandler) sendPacket(session *server.Session, packet network.Packet) error {
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func (h *MountStateHandler) broadcastPacket(owner *model.Player, packet network.Packet) error {
	if h.broadcast != nil {
		return h.broadcast(owner, packet)
	}
	if owner == nil || owner.GetCharKnownObjectList() == nil {
		return nil
	}
	for _, object := range owner.GetCharKnownObjectList().GetKnownObjects() {
		player, ok := object.(model.IPlayer)
		if !ok || player.GetSession() == nil {
			continue
		}
		if err := player.GetSession().Send(packet); err != nil {
			return err
		}
	}
	return nil
}

// buildMountStateSuccess matches RSBot's VSRO 1.188 B0CB parser exactly:
// success, owner unique ID, mounted flag, transport unique ID.
func buildMountStateSuccess(ownerUniqueID uint32, mounted bool, transportUniqueID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.COSUpdateMountStateResponse
	packet.WriteByte(1)
	packet.WriteUInt32(ownerUniqueID)
	packet.WriteBool(mounted)
	packet.WriteUInt32(transportUniqueID)
	return packet
}

func buildMountStateFailure() network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.COSUpdateMountStateResponse
	packet.WriteByte(2)
	return packet
}
