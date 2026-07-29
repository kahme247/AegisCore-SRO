package chat

import (
	"context"
	"fmt"
	"sync"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

const (
	All byte = iota + 1
	PM
	AllGM
	Party
	Guild
	Global
	Notice
	_
	Stall
	_
	Union
	_
	NPC
	_
	_
	Academy
)

type ChatHandler struct {
	// Chat helpers touch shared world and party state. The legacy opcode queue
	// processed them serially, so retain that compatibility boundary here.
	mu sync.Mutex
}

func RegisterChatHandler(target *server.Server) {
	handler := &ChatHandler{}
	target.RegisterHandler(opcode.ChatRequest, handler.handlePacket)
}

func (h *ChatHandler) handlePacket(_ context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	chatType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	chatIdx, err := packet.ReadByte()
	if err != nil {
		return err
	}

	request := MessageRequest{
		ChatType:  chatType,
		ChatIndex: chatIdx,
	}

	if chatType == PM {
		receiver, err := packet.ReadString()
		if err != nil {
			return err
		}
		logrus.Tracef("PM message receiver: %v\n", receiver)
		request.Receiver = receiver
		if err := vsro188.ValidateText(receiver, vsro188.TextRules{Field: "whisper receiver", MaxRunes: 32, RequireTrimmed: true}); err != nil {
			return err
		}
	}

	msg, err := packet.ReadString()
	if err != nil {
		return err
	}
	request.Message = msg
	if packet.Remaining() != 0 {
		return fmt.Errorf("chat request has trailing data")
	}
	if err := vsro188.ValidateText(msg, vsro188.TextRules{Field: "chat message"}); err != nil {
		return err
	}

	switch request.ChatType {
	case All:
		return handleAllMessage(request, session)
	case Party:
		return handlePartyMessage(request, session)
	case Guild:

	case Global:
		return handleGlobalMessage(request, session)
	case Notice:

	case Stall:
		return handleStallMessage(request, session)
	case Union:

	case NPC:

	case Academy:

	case PM:
		return handleWhisperMessage(request, session)
	case AllGM:
		gmh := GetGmMessageHandlerInstance()
		return gmh.HandleAdminMessage(request, session)
	default:
		logrus.Debugf("unhandled chat message %v\n", request)
	}
	return nil
}
