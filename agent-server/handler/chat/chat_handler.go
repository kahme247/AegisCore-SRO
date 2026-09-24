package chat

import (
	"context"
	"fmt"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
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
	mu     sync.Mutex
	social model.SocialRepository
	filter model.ChatFilter
}

func RegisterChatHandler(target *server.Server, social model.SocialRepository, filters ...model.ChatFilter) {
	handler := &ChatHandler{}
	handler.social = social
	if len(filters) > 0 {
		handler.filter = filters[0]
	}
	target.RegisterHandler(opcode.ChatRequest, handler.handlePacket)
}

func sendChatResult(session *server.Session, request MessageRequest, result byte) error {
	packet, err := buildChatResult(request, result)
	if err != nil {
		return err
	}
	return session.Send(packet)
}

func buildChatResult(request MessageRequest, result byte) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatResponse
	packet.WriteByte(result)
	packet.WriteByte(request.ChatType)
	packet.WriteByte(request.ChatIndex)
	if err := packet.WriteError(); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func buildNamedChatUpdate(request MessageRequest, senderName string) (network.Packet, error) {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatUpdate
	packet.WriteByte(request.ChatType)
	packet.WriteString(senderName)
	packet.WriteString(request.Message)
	if err := packet.WriteError(); err != nil {
		return network.Packet{}, err
	}
	return packet, nil
}

func (h *ChatHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
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
	if h.filter != nil {
		request.Message, _ = h.filter.Filter(request.Message)
	}

	switch request.ChatType {
	case All:
		return handleAllMessage(request, session)
	case Party:
		return handlePartyMessage(request, session)
	case Guild:
		return h.handleSocialMessage(ctx, request, session)
	case Global:
		return handleGlobalMessage(request, session)
	case Notice:
		gmh := GetGmMessageHandlerInstance()
		return gmh.HandleAdminMessage(request, session)
	case Stall:
		return handleStallMessage(request, session)
	case Union:
		return h.handleSocialMessage(ctx, request, session)
	case NPC:

	case Academy:
		return h.handleSocialMessage(ctx, request, session)
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

func (h *ChatHandler) handleSocialMessage(ctx context.Context, request MessageRequest, session *server.Session) error {
	if h == nil || h.social == nil || session == nil || session.UserContext.CharacterID <= 0 {
		return h.sendSocialFailure(session, request)
	}
	recipients, err := h.social.SocialRecipients(ctx, session.UserContext.CharacterID, request.ChatType)
	if err != nil || len(recipients) == 0 {
		return h.sendSocialFailure(session, request)
	}
	ack := network.EmptyPacket()
	ack.MessageID = opcode.ChatResponse
	ack.WriteByte(1)
	ack.WriteByte(request.ChatType)
	ack.WriteByte(request.ChatIndex)
	if err = session.Send(ack); err != nil {
		return err
	}
	update, err := buildNamedChatUpdate(request, session.UserContext.CharName)
	if err != nil {
		return err
	}
	world := service.GetWorldServiceInstance()
	for _, characterID := range recipients {
		player, lookupErr := world.GetPlayerByCharacterID(characterID)
		if lookupErr != nil || player == nil || player.GetSession() == nil || !player.GetSession().IsInWorld() {
			continue
		}
		if sendErr := player.GetSession().Send(update); sendErr != nil {
			logrus.WithError(sendErr).Debugf("failed social chat delivery to character %d", characterID)
		}
	}
	return nil
}

func (h *ChatHandler) sendSocialFailure(session *server.Session, request MessageRequest) error {
	if session == nil {
		return fmt.Errorf("social chat has no session")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.ChatResponse
	p.WriteByte(2)
	p.WriteByte(request.ChatType)
	p.WriteByte(request.ChatIndex)
	return session.Send(p)
}
