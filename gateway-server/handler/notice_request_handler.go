package handler

import (
	"context"
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	"github.com/Harvi8/AegisCore/gateway-server/db"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const maxGatewayNotices = math.MaxUint8

// NoticeRequestHandler keeps ContentID validation at the Gateway boundary.
// The current NOTICE schema is single-profile/global (there is no CONTENT_ID
// column), so every loaded entry belongs to the one accepted profile after the
// request ContentID has matched that profile. A future multi-profile schema
// must filter at the repository boundary before calling BuildNoticeResponse.
type NoticeRequestHandler struct {
	LoadProfile func() (config.ProtocolProfile, error)
	Notices     db.NoticeCatalog
}

func NewNoticeRequestHandler(
	loadProfile func() (config.ProtocolProfile, error),
	notices db.NoticeCatalog,
) *NoticeRequestHandler {
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	if notices == nil {
		notices = db.LegacyNoticeCatalog()
	}
	return &NoticeRequestHandler{LoadProfile: loadProfile, Notices: notices}
}

// RegisterNoticeRequestHandlerWithRepository installs a notice handler backed
// by an injected context-aware catalog. Gateway startup should pass a
// NoticeRepository constructed from its long-lived account pool here.
func RegisterNoticeRequestHandlerWithRepository(target *server.Server, notices db.NoticeCatalog) *NoticeRequestHandler {
	if target == nil {
		return nil
	}
	handler := NewNoticeRequestHandler(config.LoadProtocolProfile, notices)
	target.RegisterHandler(opcode.NoticeRequest, handler.Handle)
	return handler
}

// RegisterNoticeRequestHandler is retained for old startup wiring. New
// startup code should call RegisterNoticeRequestHandlerWithRepository so the
// handler does not allocate a short-lived connection for each request.
func RegisterNoticeRequestHandler(target *server.Server) {
	RegisterNoticeRequestHandlerWithRepository(target, db.LegacyNoticeCatalog())
}

func (h *NoticeRequestHandler) Handle(ctx context.Context, session *server.Session, packet network.Packet) error {
	if ctx == nil {
		ctx = context.Background()
	}
	contentID, err := parseNoticeRequest(packet)
	if err != nil {
		return err
	}
	loadProfile := h.LoadProfile
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	profile, err := loadProfile()
	if err != nil {
		return fmt.Errorf("load gateway notice profile: %w", err)
	}
	if err := config.ValidateProtocolProfile(profile); err != nil {
		return fmt.Errorf("invalid pinned gateway profile: %w", err)
	}
	if contentID != profile.ContentID {
		return fmt.Errorf("unsupported notice content ID %d", contentID)
	}
	if session != nil && session.UserContext.ProtocolProfile == config.VSRO274Profile {
		response, err := BuildNoticeResponse(nil)
		if err != nil {
			return err
		}
		return session.SendMassive(response)
	}
	notices := h.Notices
	if notices == nil {
		notices = db.LegacyNoticeCatalog()
	}
	loadedNotices, err := notices.List(ctx)
	if err != nil {
		return fmt.Errorf("load gateway notices: %w", err)
	}
	response, err := BuildNoticeResponse(loadedNotices)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("send notice response: missing session")
	}
	return session.SendMassive(response)
}

// parseNoticeRequest accepts only the documented one-byte ContentID request.
func parseNoticeRequest(packet network.Packet) (byte, error) {
	request, err := (vsro188.GatewayNoticeRequestCodec{}).Decode(packet)
	if err != nil {
		return 0, fmt.Errorf("malformed notice request: %w", err)
	}
	return request.ContentID, nil
}

// BuildNoticeResponse serializes a documented logical 0xA104 payload. Sender
// applies the shared MassiveCodec once, after the complete final size is
// known. This avoids hand-written 0x600D headers and preserves first/final
// fragments for large notice lists.
func BuildNoticeResponse(notices []db.Notice) (network.Packet, error) {
	if len(notices) > maxGatewayNotices {
		notices = notices[:maxGatewayNotices]
	}

	response := network.EmptyPacket()
	response.MessageID = opcode.NoticeResponse
	if err := response.WriteByte(byte(len(notices))); err != nil {
		return network.Packet{}, fmt.Errorf("write notice count: %w", err)
	}
	for index, notice := range notices {
		if err := writeGatewayNotice(&response, notice); err != nil {
			return network.Packet{}, fmt.Errorf("write notice %d: %w", index, err)
		}
	}
	return response, nil
}

func writeGatewayNotice(response *network.Packet, notice db.Notice) error {
	if response == nil {
		return fmt.Errorf("missing notice response")
	}
	if notice.Ctime.Year() < 0 || notice.Ctime.Year() > math.MaxUint16 {
		return fmt.Errorf("notice date year %d cannot be represented", notice.Ctime.Year())
	}
	if err := response.WriteString(notice.Subject); err != nil {
		return err
	}
	if err := response.WriteString(notice.Article); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Year())); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Month())); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Day())); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Hour())); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Minute())); err != nil {
		return err
	}
	if err := response.WriteUInt16(uint16(notice.Ctime.Second())); err != nil {
		return err
	}
	// The packet field is nanoseconds, not milliseconds. Nanosecond is always
	// 0..999,999,999 and therefore fits in the documented uint32 exactly.
	return response.WriteUInt32(uint32(notice.Ctime.Nanosecond()))
}
