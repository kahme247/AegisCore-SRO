package handler

import (
	"context"
	"net/netip"
	"strings"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/db"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	log "github.com/sirupsen/logrus"
)

func (h *LoginRequestHandler) sendLauncherTokenFailure(session *server.Session, failure LoginFailure) {
	failure = normalizeLoginFailure(failure)
	response := network.EmptyPacket()
	response.MessageID = opcode.LauncherTokenResponse
	if err := response.WriteByte(2); err != nil {
		return
	}
	if err := response.WriteByte(failure.Code); err != nil {
		return
	}
	h.sendLoginResponse(session, response)
}

func (h *LoginRequestHandler) handleLauncherTokenSessionPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	decoded, err := (vsro188.GatewayLoginRequestCodec{}).Decode(packet)
	if err != nil {
		h.sendLauncherTokenFailure(session, LoginFailure{Code: LoginFailureCounter, CounterLimit: defaultLoginAttemptLimit})
		return nil
	}
	sourceIP := sourceIPFromSession(session)
	if !sourceIP.IsValid() || h == nil || h.LaunchTokens == nil {
		h.sendLauncherTokenFailure(session, LoginFailure{Code: LoginFailureCounter, CounterLimit: defaultLoginAttemptLimit})
		return nil
	}
	tracker := h.AttemptTracker
	if tracker == nil {
		tracker = NewLoginAttemptTracker(LoginAttemptTrackerOptions{})
		h.AttemptTracker = tracker
	}
	if failure, blocked := tracker.Preflight(decoded.Username, sourceIP); blocked {
		h.sendLauncherTokenFailure(session, failure)
		return nil
	}
	accountID, valid, credentialErr := h.authenticate(ctx, decoded.Username, decoded.Password)
	decoded.Password = ""
	if credentialErr != nil || !valid || accountID == 0 {
		h.sendLauncherTokenFailure(session, tracker.RecordFailure(decoded.Username, sourceIP))
		return nil
	}
	issued, err := h.LaunchTokens.Issue(auth.LaunchTokenBinding{
		AccountID: accountID,
		Username:  decoded.Username,
		ShardID:   decoded.ShardID,
		ContentID: decoded.ContentID,
		SourceIP:  sourceIP,
	})
	if err != nil {
		h.sendLauncherTokenFailure(session, LoginFailure{Code: LoginFailureShardUnavailable})
		return nil
	}
	response := network.EmptyPacket()
	response.MessageID = opcode.LauncherTokenResponse
	if err := response.WriteByte(1); err != nil {
		return nil
	}
	if err := response.WriteString(issued.Value); err != nil {
		return nil
	}
	tracker.ClearAccount(decoded.Username)
	h.sendLoginResponse(session, response)
	return nil
}

func (h *LoginRequestHandler) sendInvalidLogin(session *server.Session, reason string) {
	log.Debugf("rejecting malformed Gateway login request: %s", reason)
	h.sendLoginFailure(session, LoginFailure{
		Code:         LoginFailureCounter,
		CounterLimit: defaultLoginAttemptLimit,
	})
}

func (h *LoginRequestHandler) sendLoginFailure(session *server.Session, failure LoginFailure) {
	failure = normalizeLoginFailure(failure)
	log.Infof("SERVER_GATEWAY_LOGIN_RESPONSE failure code=%d", failure.Code)
	response := network.EmptyPacket()
	response.MessageID = 0xA102
	err := response.WriteByte(2)
	if err == nil {
		err = response.WriteByte(failure.Code)
	}
	if err == nil && failure.Code == LoginFailureCounter {
		err = response.WriteUInt32(failure.CounterLimit)
	}
	if err == nil && failure.Code == LoginFailureCounter {
		err = response.WriteUInt32(failure.CounterAttempts)
	}
	if err == nil && len(failure.ExtraPayload) > 0 {
		err = response.WriteBytes(failure.ExtraPayload)
	}
	if err != nil {
		log.Errorf("failed to encode Gateway login failure: %v", err)
		return
	}
	h.sendLoginResponse(session, response)
}

func (h *LoginRequestHandler) sendLoginResponse(session *server.Session, response network.Packet) {
	if session == nil {
		log.Debug("discarding login response without a session")
		return
	}
	if err := session.Send(response); err != nil {
		log.Errorf("failed to send Gateway login response: %v", err)
	}
}

func (h *LoginRequestHandler) authenticate(ctx context.Context, username, password string) (uint32, bool, error) {
	if h != nil && h.CredentialLookup != nil {
		accountID, valid := h.CredentialLookup(ctx, username, password)
		return accountID, valid, nil
	}
	if h == nil || h.CredentialRepository == nil {
		return 0, false, db.ErrCredentialRepositoryUnavailable
	}
	return h.CredentialRepository.Authenticate(ctx, username, password)
}

func (h *LoginRequestHandler) authenticateLogin(ctx context.Context, binding auth.LaunchTokenBinding, password string) (uint32, bool, error) {
	if !strings.HasPrefix(password, launcherTokenPrefix) {
		return h.authenticate(ctx, binding.Username, password)
	}
	if h == nil || h.LaunchTokens == nil {
		return 0, false, nil
	}
	redeemed, err := h.LaunchTokens.Redeem(strings.TrimPrefix(password, launcherTokenPrefix), binding)
	if err != nil {
		return 0, false, nil
	}
	return redeemed.AccountID, true, nil
}

func allowAdmission(context.Context, LoginAdmissionRequest) LoginAdmissionDecision {
	return AllowLoginAdmission()
}

func sourceIPFromSession(session *server.Session) netip.Addr {
	if session == nil || session.Conn == nil {
		return netip.Addr{}
	}
	return auth.SourceIPFromAddr(session.Conn.RemoteAddr())
}

func normalizeLoginFailure(failure LoginFailure) LoginFailure {
	switch failure.Code {
	case LoginFailureCounter, LoginFailureBlocked, LoginFailureAlreadyConnected, LoginFailureShardUnavailable, LoginFailureIPLimit:
		// The policy result is one of the documented outcomes.
	default:
		failure = LoginFailure{Code: LoginFailureCounter}
	}
	if failure.Code == LoginFailureCounter && failure.CounterLimit == 0 {
		failure.CounterLimit = defaultLoginAttemptLimit
	}
	return failure
}
