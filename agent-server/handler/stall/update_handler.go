package stall

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
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
