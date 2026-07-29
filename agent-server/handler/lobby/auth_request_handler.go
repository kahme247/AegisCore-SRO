package lobby

import (
	"context"
	"errors"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/security/auth"
	"github.com/ferdoran/go-sro/framework/server"
	log "github.com/sirupsen/logrus"
)

const (
	MacAddressLength          = 6
	OpcodeAuthResponse uint16 = 0xA103
)

// AuthRequestHandler validates Agent admission on the per-session registry.
// Its request-level entry point is deliberately synchronous so a malformed
// packet cannot panic a worker or cause duplicate responses.
type AuthRequestHandler struct {
	Tickets *auth.AdmissionTicketStore
}

// RegisterAuthRequestHandler installs the hardened auth handler directly on
// Agent's per-session registry instead of the legacy global opcode queue.
func RegisterAuthRequestHandler(target *server.Server, tickets *auth.AdmissionTicketStore) {
	if target == nil {
		return
	}
	handler := AuthRequestHandler{
		Tickets: tickets,
	}
	target.RegisterHandler(opcode.AuthRequest, handler.handleSessionPacket)
}

func (h *AuthRequestHandler) handleSessionPacket(_ context.Context, session *server.Session, packet network.Packet) error {
	if err := h.HandlePacket(server.PacketChannelData{Session: session, Packet: packet}); err != nil {
		log.Debugf("rejecting Agent auth request: %v", err)
	}
	// HandlePacket sends exactly one 0xA103 failure response before returning an
	// input error. Do not return it to Session here or shutdown could cancel the
	// ordered sender before that response reaches the client.
	return nil
}

// HandlePacket validates one stock Agent auth request and sends exactly one
// 0xA103 response when a session is available. The ticket store performs the
// atomic single-use redemption; a failed binding never consumes a ticket.
func (h *AuthRequestHandler) HandlePacket(packet server.PacketChannelData) error {
	if packet.Session == nil {
		return errors.New("Agent auth request has no session")
	}

	queueID, err := packet.ReadUInt32()
	if err != nil {
		return h.reject(packet.Session, err)
	}
	username, err := packet.ReadString()
	if err != nil {
		return h.reject(packet.Session, err)
	}
	// The client continues to send this field, but Gateway already validated it
	// before issuing the QueueID. Parse for wire compatibility, then discard it
	// without storing, comparing, or logging it.
	if _, err := packet.ReadString(); err != nil {
		return h.reject(packet.Session, err)
	}
	contentID, err := packet.ReadByte()
	if err != nil {
		return h.reject(packet.Session, err)
	}
	if _, err := packet.ReadBytes(MacAddressLength); err != nil {
		return h.reject(packet.Session, err)
	}
	if packet.Remaining() != 0 {
		return h.reject(packet.Session, errors.New("unexpected trailing Agent auth request data"))
	}

	sourceIP := auth.SourceIPFromAddr(nil)
	if packet.Session.Conn != nil {
		sourceIP = auth.SourceIPFromAddr(packet.Session.Conn.RemoteAddr())
	}
	if h == nil || h.Tickets == nil {
		return h.reject(packet.Session, auth.ErrAdmissionTicketInvalid)
	}
	ticket, err := h.Tickets.Redeem(auth.AdmissionCredentials{
		QueueID:      queueID,
		Username:     username,
		ContentID:    contentID,
		HasContentID: true,
		SourceIP:     sourceIP,
	})
	if err != nil {
		return h.reject(packet.Session, err)
	}

	packet.Session.UserContext = server.UserContext{
		UserID:   ticket.AccountID,
		ShardID:  ticket.ShardID,
		Username: ticket.Username,
	}
	// The ticket has been atomically consumed and the authenticated identity is
	// now established, so downstream admission policy may safely allow lobby
	// opcodes for this session.
	packet.Session.MarkAuthenticated()
	return h.respond(packet.Session, ResultTrue)
}

func (h *AuthRequestHandler) reject(session *server.Session, cause error) error {
	if err := h.respond(session, ResultFalse); err != nil {
		return err
	}
	return cause
}

func (h *AuthRequestHandler) respond(session *server.Session, result byte) error {
	p := network.EmptyPacket()
	p.MessageID = OpcodeAuthResponse
	if err := p.WriteByte(result); err != nil {
		return err
	}
	if result == ResultFalse {
		// Existing clients expect the generic invalid-admission error code.
		if err := p.WriteByte(4); err != nil {
			return err
		}
	}
	return session.Send(p)
}
