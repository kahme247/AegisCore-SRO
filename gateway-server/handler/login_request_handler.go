package handler

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	"github.com/Harvi8/AegisCore/gateway-server/db"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	// LoginFailureCounter is the documented invalid-credential/counter result.
	LoginFailureCounter byte = 1
	// LoginFailureBlocked represents a ban or punishment decision.
	LoginFailureBlocked byte = 2
	// LoginFailureAlreadyConnected represents a duplicate-account decision.
	LoginFailureAlreadyConnected byte = 4
	// LoginFailureShardUnavailable represents an unavailable or full shard.
	LoginFailureShardUnavailable byte = 6
	// LoginFailureIPLimit represents an IP admission-window decision.
	LoginFailureIPLimit byte = 0x0B

	defaultLoginAttemptLimit uint32 = DefaultLoginAccountAttemptLimit
)

// CredentialLookup owns only the password check. The password is passed to it
// synchronously and discarded before any admission policy or Agent transport
// call. A false result is serialized as the documented counter outcome.
type CredentialLookup func(context.Context, string, string) (accountID uint32, valid bool)

// LoginAdmissionRequest contains the non-secret data available to account,
// ban, shard-capacity, duplicate-login, and IP-window decisions. It is the
// extension seam for Stage 2 policy without reopening credential handling or
// packet serialization.
type LoginAdmissionRequest struct {
	AccountID uint32
	Username  string
	ShardID   uint16
	ContentID byte
	SourceIP  netip.Addr
}

// LoginAdmissionEvaluator is called only after a credential lookup succeeds
// and Gateway has a parseable client source IP. It may allow admission or
// return one of the documented failure outcomes below.
type LoginAdmissionEvaluator func(context.Context, LoginAdmissionRequest) LoginAdmissionDecision

// LoginAdmissionDecision deliberately separates policy from the stock A102
// wire response. Allowed decisions continue to the Agent mTLS installation;
// denied decisions are encoded by LoginFailure.
type LoginAdmissionDecision struct {
	Allowed bool
	Failure LoginFailure
}

// LoginFailure carries a documented A102 error code. Counter fields are
// serialized only for LoginFailureCounter. ExtraPayload permits a future
// capture-verified profile-specific detail branch without changing admission
// control flow or accepting caller-provided packet objects.
type LoginFailure struct {
	Code            byte
	CounterLimit    uint32
	CounterAttempts uint32
	ExtraPayload    []byte
}

// AllowLoginAdmission constructs an explicit successful policy decision.
func AllowLoginAdmission() LoginAdmissionDecision {
	return LoginAdmissionDecision{Allowed: true}
}

// DenyLoginAdmission constructs an explicit documented policy denial.
func DenyLoginAdmission(failure LoginFailure) LoginAdmissionDecision {
	return LoginAdmissionDecision{Failure: failure}
}

type LoginRequestHandler struct {
	TicketIssuer    *auth.AdmissionTicketStore
	TicketInstaller auth.AdmissionTicketInstaller
	AttemptTracker  *LoginAttemptTracker
	// CredentialRepository is the context-aware Gateway persistence boundary.
	// It should be constructed from a long-lived framework/db account pool at
	// application startup and is never closed by this handler.
	CredentialRepository db.CredentialRepository
	// CredentialLookup is retained only as a compatibility/test override while
	// callers migrate to CredentialRepository. New code must inject a
	// CredentialRepository instead.
	CredentialLookup   CredentialLookup
	AdmissionEvaluator LoginAdmissionEvaluator
	InstallTimeout     time.Duration
}

// NewLoginRequestHandler constructs the Gateway admission boundary with the
// current database lookup/default allow policy. It always creates a bounded
// account/IP tracker, which runs before bcrypt/database credential work.
// Deployments can replace CredentialLookup and AdmissionEvaluator before the
// server begins accepting connections to add bans, shard state, and capacity
// checks without reopening password handling.
func NewLoginRequestHandler(installer auth.AdmissionTicketInstaller) *LoginRequestHandler {
	return NewLoginRequestHandlerWithCredentialRepository(installer, db.NewLegacyCredentialRepository())
}

// NewLoginRequestHandlerWithCredentialRepository constructs the Gateway
// admission boundary with an injected, context-aware account credential
// repository. Passing nil is deliberately fail-closed; use
// NewLoginRequestHandler only while legacy startup wiring is being migrated.
func NewLoginRequestHandlerWithCredentialRepository(installer auth.AdmissionTicketInstaller, credentials db.CredentialRepository) *LoginRequestHandler {
	return &LoginRequestHandler{
		TicketIssuer:         auth.NewAdmissionTicketStore(),
		TicketInstaller:      installer,
		AttemptTracker:       NewLoginAttemptTracker(LoginAttemptTrackerOptions{}),
		CredentialRepository: credentials,
		AdmissionEvaluator:   defaultGatewayAdmissionEvaluator,
		InstallTimeout:       auth.DefaultAdmissionTicketInstallTimeout,
	}
}

// RegisterLoginRequestHandler installs Gateway login handling directly on the
// per-server handler registry. Returning the handler makes the injected
// admission policy explicit and configurable before Server.Start.
func RegisterLoginRequestHandler(target *server.Server, installer auth.AdmissionTicketInstaller) *LoginRequestHandler {
	handler := NewLoginRequestHandler(installer)
	if target != nil {
		target.RegisterHandler(opcode.LoginRequest, handler.handleSessionPacket)
	}
	return handler
}

// RegisterLoginRequestHandlerWithCredentialRepository installs a Gateway
// login handler backed by an injected, long-lived account credential
// repository. This is the intended startup path once framework/db.Pools is
// available to the application bootstrap.
func RegisterLoginRequestHandlerWithCredentialRepository(target *server.Server, installer auth.AdmissionTicketInstaller, credentials db.CredentialRepository) *LoginRequestHandler {
	handler := NewLoginRequestHandlerWithCredentialRepository(installer, credentials)
	if target != nil {
		target.RegisterHandler(opcode.LoginRequest, handler.handleSessionPacket)
	}
	return handler
}

func (h *LoginRequestHandler) handleSessionPacket(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.handlePacket(ctx, server.PacketChannelData{Session: session, Packet: packet})
	// A protocol-level failure response has already been sent. Keep the
	// connection semantics compatible with the stock login retry flow rather
	// than closing the sender before its sole 0xA102 response can flush.
	return nil
}

func (h *LoginRequestHandler) handlePacket(ctx context.Context, packet server.PacketChannelData) {
	contentID, err := packet.ReadByte()
	if err != nil {
		h.sendInvalidLogin(packet.Session, "malformed content id")
		return
	}
	username, err := packet.ReadString()
	if err != nil {
		h.sendInvalidLogin(packet.Session, "malformed username")
		return
	}
	password, err := packet.ReadString()
	if err != nil {
		h.sendInvalidLogin(packet.Session, "malformed password")
		return
	}
	shardID, err := packet.ReadUInt16()
	if err != nil {
		h.sendInvalidLogin(packet.Session, "malformed shard id")
		return
	}
	if packet.Remaining() != 0 {
		h.sendInvalidLogin(packet.Session, "trailing login request data")
		return
	}

	sourceIP := sourceIPFromSession(packet.Session)
	if !sourceIP.IsValid() {
		// The ticket requirement is an actual source-IP binding. An unusual
		// proxy/non-TCP address must fail closed rather than create an unbound
		// QueueID that could be replayed from another source.
		h.sendInvalidLogin(packet.Session, "missing client source ip")
		return
	}
	tracker := h.AttemptTracker
	if tracker == nil {
		// Handlers are configured before Server.Start. Keep a safe fallback for
		// tests or external construction that omitted the mandatory tracker.
		tracker = NewLoginAttemptTracker(LoginAttemptTrackerOptions{})
		h.AttemptTracker = tracker
	}
	if failure, blocked := tracker.Preflight(username, sourceIP); blocked {
		h.sendLoginFailure(packet.Session, failure)
		return
	}

	// Gateway is the only component that validates the stock password. Do not
	// log, retain, or forward it after this synchronous lookup.
	accountID, isValidLogin, credentialErr := h.authenticate(ctx, username, password)
	password = ""
	if credentialErr != nil {
		// Preserve the established A102 counter outcome while keeping database
		// failures out of the panic path and out of client-visible detail.
		// Credential errors may originate in drivers or compatibility adapters;
		// do not render them because they can contain connection details.
		log.Error("Gateway credential lookup failed")
	}
	if credentialErr != nil || !isValidLogin || accountID == 0 {
		log.Info("CLIENT_GATEWAY_LOGIN_REQUEST rejected reason=invalid_credentials")
		h.sendLoginFailure(packet.Session, tracker.RecordFailure(username, sourceIP))
		return
	}

	request := LoginAdmissionRequest{
		AccountID: accountID,
		Username:  username,
		ShardID:   shardID,
		ContentID: contentID,
		SourceIP:  sourceIP,
	}
	evaluator := h.AdmissionEvaluator
	if evaluator == nil {
		evaluator = defaultGatewayAdmissionEvaluator
	}
	decision := evaluator(ctx, request)
	if !decision.Allowed {
		log.Infof("CLIENT_GATEWAY_LOGIN_REQUEST rejected reason=admission_policy code=%d", normalizeLoginFailure(decision.Failure).Code)
		h.sendLoginFailure(packet.Session, decision.Failure)
		return
	}

	queueID, issueErr := h.issueAgentTicket(ctx, request)
	if issueErr != nil {
		// A successful A102 is forbidden until the private Agent endpoint
		// acknowledges the installation. This remains a failure response even
		// when a network timeout makes Agent's final state ambiguous.
		// An installer error must never be treated as safe diagnostic text: a
		// custom transport could include QueueID or ticket material.
		log.Error("CLIENT_GATEWAY_LOGIN_REQUEST rejected reason=agent_ticket_install")
		if errors.Is(issueErr, auth.ErrAdmissionTicketAlreadyConnected) {
			h.sendLoginFailure(packet.Session, LoginFailure{Code: LoginFailureAlreadyConnected})
		} else {
			h.sendLoginFailure(packet.Session, LoginFailure{Code: LoginFailureShardUnavailable})
		}
		return
	}

	response := network.EmptyPacket()
	response.MessageID = 0xA102
	responseErr := response.WriteByte(1)
	if responseErr == nil {
		responseErr = response.WriteUInt32(queueID)
	}
	if responseErr == nil {
		responseErr = response.WriteString(viper.GetString(config.AgentPublicIp))
	}
	if responseErr == nil {
		responseErr = response.WriteUInt16(uint16(viper.GetInt(config.AgentPort)))
	}
	if responseErr != nil {
		// The Agent copy is single-use and will expire because the QueueID was
		// not exposed. Do not poison a valid password's failure window.
		log.Errorf("failed to encode successful Gateway login response: %v", responseErr)
		h.sendLoginFailure(packet.Session, LoginFailure{Code: LoginFailureShardUnavailable})
		return
	}
	tracker.ClearAccount(username)
	log.Infof("SERVER_GATEWAY_LOGIN_RESPONSE success agent=%s:%d", viper.GetString(config.AgentPublicIp), viper.GetInt(config.AgentPort))
	h.sendLoginResponse(packet.Session, response)
}

func (h *LoginRequestHandler) issueAgentTicket(ctx context.Context, request LoginAdmissionRequest) (uint32, error) {
	if h == nil || h.TicketIssuer == nil || h.TicketInstaller == nil || !request.SourceIP.IsValid() {
		return 0, auth.ErrAdmissionTicketInvalid
	}
	ticket, err := h.TicketIssuer.Issue(auth.AdmissionTicket{
		AccountID:    request.AccountID,
		Username:     request.Username,
		ShardID:      request.ShardID,
		ContentID:    request.ContentID,
		HasContentID: true,
		SourceIP:     request.SourceIP,
	})
	if err != nil {
		return 0, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := h.InstallTimeout
	if timeout <= 0 {
		timeout = auth.DefaultAdmissionTicketInstallTimeout
	}
	installCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := h.TicketInstaller.Install(installCtx, ticket); err != nil {
		// Gateway never exposed this QueueID to the client. Remove its issuer
		// copy immediately so an Agent outage cannot fill the local ticket map.
		h.TicketIssuer.Discard(ticket.QueueID)
		return 0, err
	}
	if err := h.TicketIssuer.Acknowledge(ticket.QueueID); err != nil {
		h.TicketIssuer.Discard(ticket.QueueID)
		return 0, err
	}
	// Agent owns the only ticket that can be redeemed. Keeping Gateway's
	// acknowledged issuer copy would consume its bounded map under login load.
	h.TicketIssuer.Discard(ticket.QueueID)
	return ticket.QueueID, nil
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
