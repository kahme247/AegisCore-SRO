package stall

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type StallUpdateHandler struct {
	// The update helpers share stall and inventory state. Preserve the
	// single-worker behavior while those models retain their legacy locking.
	mu sync.Mutex
}

func RegisterStallUpdateHandler(target *server.Server) {
	handler := &StallUpdateHandler{}
	target.RegisterHandler(opcode.StallUpdateRequest, handler.handlePacket)
}

func newStallUpdateResponse(updateType byte) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.StallUpdateResponse
	if err := p.WriteByte(1); err != nil {
		return network.Packet{}, err
	}
	if err := p.WriteByte(updateType); err != nil {
		return network.Packet{}, err
	}
	return p, nil
}

func (s *StallUpdateHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := server.PacketChannelData{Session: session, Packet: packet}
	updateType, err := data.ReadByte()
	if err != nil {
		return err
	}
	switch updateType {
	case StallUpdateItem:
		return updateItem(data)
	case StallAddItem:
		fallthrough
	case StallRemoveItem:
		return addRemoveItem(data, updateType)
	case StallFleaMarketMode:
		return nil
	case StallState:
		return updateState(data)
	case StallMessage:
		return updateMessage(data)
	case StallName:
		return updateStallName(data)
	default:
		return fmt.Errorf("unsupported stall update type %d", updateType)
	}
}
