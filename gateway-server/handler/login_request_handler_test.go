package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/db"
	log "github.com/sirupsen/logrus"
)

var gatewayTestSourceIP = netip.MustParseAddr("203.0.113.17")

func TestLoginRequestHandlerWaitsForAgentInstallAcknowledgement(t *testing.T) {
	started := make(chan auth.AdmissionTicket, 1)
	acknowledge := make(chan struct{})
	installer := admissionInstallerFunc(func(ctx context.Context, ticket auth.AdmissionTicket) error {
		started <- ticket
		select {
		case <-acknowledge:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	handler := NewLoginRequestHandler(installer)
	handler.CredentialLookup = func(context.Context, string, string) (uint32, bool) {
		return 77, true
	}
	handler.AdmissionEvaluator = allowAdmission
	handler.InstallTimeout = time.Second
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{
			Session: session,
			Packet:  buildGatewayLoginRequest(t, "Alice", "stock-password"),
		})
		close(done)
	}()

	var installed auth.AdmissionTicket
	select {
	case installed = <-started:
	case <-time.After(time.Second):
		t.Fatal("Gateway did not request Agent ticket installation")
	}
	assertNoGatewayResponse(t, clientConn)
	close(acknowledge)

	response := readGatewayResponse(t, clientConn)
	if got, err := response.ReadByte(); err != nil || got != 1 {
		t.Fatalf("A102 result = %d, err = %v; want successful result 1 after Agent ACK", got, err)
	}
	queueID, err := response.ReadUInt32()
	if err != nil {
		t.Fatalf("successful A102 missing QueueID: %v", err)
	}
	if queueID == 0 || queueID != installed.QueueID {
		t.Fatalf("A102 QueueID = %#x, installed QueueID = %#x", queueID, installed.QueueID)
	}
	if got, want := installed.SourceIP, gatewayTestSourceIP; got != want {
		t.Fatalf("installed source IP = %s, want %s", got, want)
	}
	if got, want := installed.ContentID, byte(56); got != want || !installed.HasContentID {
		t.Fatalf("installed content binding = (%d, %t), want (%d, true)", got, installed.HasContentID, want)
	}
	if got := handler.TicketIssuer.Len(); got != 0 {
		t.Fatalf("Gateway retained an acknowledged issuer ticket: length = %d", got)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish after sending A102")
	}
	assertNoGatewayResponse(t, clientConn)
}

func TestLoginRequestHandlerWithholdsSuccessWhenAgentInstallFails(t *testing.T) {
	installer := admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		return errors.New("Agent installation unavailable")
	})
	handler := NewLoginRequestHandler(installer)
	handler.CredentialLookup = func(context.Context, string, string) (uint32, bool) {
		return 77, true
	}
	handler.AdmissionEvaluator = allowAdmission
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{
			Session: session,
			Packet:  buildGatewayLoginRequest(t, "Alice", "stock-password"),
		})
		close(done)
	}()

	response := readGatewayResponse(t, clientConn)
	if got, err := response.ReadByte(); err != nil || got != 2 {
		t.Fatalf("A102 result = %d, err = %v; want failure result 2", got, err)
	}
	if code, err := response.ReadByte(); err != nil || code != LoginFailureShardUnavailable {
		t.Fatalf("A102 failure code = %d, err = %v; want shard-unavailable code %d", code, err, LoginFailureShardUnavailable)
	}
	if got := handler.TicketIssuer.Len(); got != 0 {
		t.Fatalf("Gateway retained an unacknowledged local ticket: length = %d", got)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish after failed installation")
	}
	assertNoGatewayResponse(t, clientConn)
}

func TestLoginRequestHandlerBoundsAttemptsBeforeCredentialLookup(t *testing.T) {
	installer := admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		t.Fatal("invalid credentials reached Agent installer")
		return nil
	})
	handler := NewLoginRequestHandler(installer)
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	handler.AttemptTracker = NewLoginAttemptTracker(LoginAttemptTrackerOptions{
		Window:       time.Minute,
		AccountLimit: 2,
		IPLimit:      10,
		Capacity:     32,
		Now:          func() time.Time { return now },
	})
	var credentialCalls int
	handler.CredentialLookup = func(context.Context, string, string) (uint32, bool) {
		credentialCalls++
		return 0, false
	}

	for attempt := uint32(1); attempt <= 2; attempt++ {
		session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
		done := make(chan struct{})
		go func() {
			handler.handlePacket(context.Background(), server.PacketChannelData{Session: session, Packet: buildGatewayLoginRequest(t, "Alice", "wrong")})
			close(done)
		}()
		response := readGatewayResponse(t, clientConn)
		if got, err := response.ReadByte(); err != nil || got != 2 {
			t.Fatalf("attempt %d result = %d, err = %v; want failure", attempt, got, err)
		}
		code, err := response.ReadByte()
		if err != nil || code != LoginFailureCounter {
			t.Fatalf("attempt %d code = %d, err = %v; want counter", attempt, code, err)
		}
		limit, _ := response.ReadUInt32()
		count, _ := response.ReadUInt32()
		if limit != 2 || count != attempt {
			t.Fatalf("attempt %d counter = %d/%d, want %d/2", attempt, count, limit, attempt)
		}
		<-done
		session.Close()
		clientConn.Close()
	}

	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{Session: session, Packet: buildGatewayLoginRequest(t, "Alice", "correct-but-blocked")})
		close(done)
	}()
	response := readGatewayResponse(t, clientConn)
	if got, err := response.ReadByte(); err != nil || got != 2 {
		t.Fatalf("bounded result = %d, err = %v; want failure", got, err)
	}
	code, _ := response.ReadByte()
	limit, _ := response.ReadUInt32()
	count, _ := response.ReadUInt32()
	if code != LoginFailureCounter || limit != 2 || count != 2 {
		t.Fatalf("bounded counter = code %d, %d/%d; want code 1, 2/2", code, count, limit)
	}
	<-done
	session.Close()
	clientConn.Close()
	if credentialCalls != 2 {
		t.Fatalf("credential lookups = %d, want 2; blocked third attempt reached lookup", credentialCalls)
	}
}

func TestLoginRequestHandlerMapsAgentDuplicateToAlreadyConnected(t *testing.T) {
	handler := NewLoginRequestHandler(admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		return auth.ErrAdmissionTicketAlreadyConnected
	}))
	handler.CredentialLookup = func(context.Context, string, string) (uint32, bool) { return 77, true }
	handler.AdmissionEvaluator = allowAdmission
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{Session: session, Packet: buildGatewayLoginRequest(t, "Alice", "stock-password")})
		close(done)
	}()
	response := readGatewayResponse(t, clientConn)
	if got, err := response.ReadByte(); err != nil || got != 2 {
		t.Fatalf("A102 result = %d, err = %v; want failure", got, err)
	}
	if code, err := response.ReadByte(); err != nil || code != LoginFailureAlreadyConnected {
		t.Fatalf("A102 failure code = %d, err = %v; want already-connected %d", code, err, LoginFailureAlreadyConnected)
	}
	<-done
}

func TestLoginRequestHandlerExposesPolicyOutcomeBoundary(t *testing.T) {
	calledInstaller := false
	policyRequests := make(chan LoginAdmissionRequest, 1)
	handler := NewLoginRequestHandler(admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		calledInstaller = true
		return nil
	}))
	handler.CredentialLookup = func(context.Context, string, string) (uint32, bool) {
		return 77, true
	}
	handler.AdmissionEvaluator = func(_ context.Context, request LoginAdmissionRequest) LoginAdmissionDecision {
		policyRequests <- request
		return DenyLoginAdmission(LoginFailure{Code: LoginFailureIPLimit})
	}
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{
			Session: session,
			Packet:  buildGatewayLoginRequest(t, "Alice", "stock-password"),
		})
		close(done)
	}()
	response := readGatewayResponse(t, clientConn)
	if got, err := response.ReadByte(); err != nil || got != 2 {
		t.Fatalf("A102 result = %d, err = %v; want failure result 2", got, err)
	}
	if code, err := response.ReadByte(); err != nil || code != LoginFailureIPLimit {
		t.Fatalf("A102 failure code = %d, err = %v; want IP-limit code %#x", code, err, LoginFailureIPLimit)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish after policy denial")
	}
	select {
	case request := <-policyRequests:
		if request.AccountID != 77 || request.SourceIP != gatewayTestSourceIP {
			t.Fatalf("policy request = %+v, want verified account/source identity", request)
		}
	default:
		t.Fatal("Gateway did not invoke the admission policy")
	}
	if calledInstaller {
		t.Fatal("Gateway called Agent installer after local policy denial")
	}
}

func TestLoginRequestHandlerUsesInjectedCredentialRepository(t *testing.T) {
	type contextKey struct{}
	requestContext := context.WithValue(context.Background(), contextKey{}, "credential-context")
	credentialCalls := make(chan struct {
		username string
		password string
		context  any
	}, 1)
	handler := NewLoginRequestHandlerWithCredentialRepository(admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		return nil
	}), credentialRepositoryFunc(func(ctx context.Context, username, password string) (uint32, bool, error) {
		credentialCalls <- struct {
			username string
			password string
			context  any
		}{username: username, password: password, context: ctx.Value(contextKey{})}
		return 77, true, nil
	}))
	handler.AdmissionEvaluator = allowAdmission
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(requestContext, server.PacketChannelData{Session: session, Packet: buildGatewayLoginRequest(t, "Alice", "stock-password")})
		close(done)
	}()

	response := readGatewayResponse(t, clientConn)
	if result, err := response.ReadByte(); err != nil || result != 1 {
		t.Fatalf("A102 result = %d, err = %v; want successful result", result, err)
	}
	select {
	case call := <-credentialCalls:
		if call.username != "Alice" || call.password != "stock-password" || call.context != "credential-context" {
			t.Fatalf("credential repository call = %#v, want propagated username/password/context", call)
		}
	case <-time.After(time.Second):
		t.Fatal("Gateway did not call injected credential repository")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish")
	}
}

func TestLoginRequestHandlerFailsClosedOnCredentialRepositoryError(t *testing.T) {
	repositoryError := errors.New("account database unavailable")
	installerCalled := false
	policyCalled := false
	handler := NewLoginRequestHandlerWithCredentialRepository(admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		installerCalled = true
		return nil
	}), credentialRepositoryFunc(func(context.Context, string, string) (uint32, bool, error) {
		return 0, false, repositoryError
	}))
	handler.AdmissionEvaluator = func(context.Context, LoginAdmissionRequest) LoginAdmissionDecision {
		policyCalled = true
		return AllowLoginAdmission()
	}
	session, clientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer session.Close()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handler.handlePacket(context.Background(), server.PacketChannelData{Session: session, Packet: buildGatewayLoginRequest(t, "Alice", "stock-password")})
		close(done)
	}()

	response := readGatewayResponse(t, clientConn)
	if result, err := response.ReadByte(); err != nil || result != 2 {
		t.Fatalf("A102 result = %d, err = %v; want failure", result, err)
	}
	code, err := response.ReadByte()
	if err != nil || code != LoginFailureCounter {
		t.Fatalf("A102 failure code = %d, err = %v; want counter", code, err)
	}
	if installerCalled || policyCalled {
		t.Fatalf("credential repository error reached later admission work: installer=%t policy=%t", installerCalled, policyCalled)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish")
	}
}

func TestLoginRequestHandlerRedactsCredentialAndTicketErrorsFromLogs(t *testing.T) {
	logger := log.StandardLogger()
	oldOutput := logger.Out
	oldFormatter := logger.Formatter
	buffer := &bytes.Buffer{}
	logger.SetOutput(buffer)
	logger.SetFormatter(&log.TextFormatter{DisableTimestamp: true, DisableColors: true})
	t.Cleanup(func() {
		logger.SetOutput(oldOutput)
		logger.SetFormatter(oldFormatter)
	})

	credentialSecret := "password=stock-password mysql://operator:db-secret@db.internal/account"
	credentialHandler := NewLoginRequestHandlerWithCredentialRepository(nil, credentialRepositoryFunc(func(context.Context, string, string) (uint32, bool, error) {
		return 0, false, errors.New(credentialSecret)
	}))
	credentialSession, credentialClientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer credentialSession.Close()
	defer credentialClientConn.Close()

	credentialDone := make(chan struct{})
	go func() {
		credentialHandler.handlePacket(context.Background(), server.PacketChannelData{Session: credentialSession, Packet: buildGatewayLoginRequest(t, "Alice", "stock-password")})
		close(credentialDone)
	}()
	_ = readGatewayResponse(t, credentialClientConn)
	select {
	case <-credentialDone:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish credential failure")
	}
	if got := buffer.String(); strings.Contains(got, credentialSecret) {
		t.Fatalf("credential error leaked to Gateway logs: %q", got)
	}

	buffer.Reset()
	ticketSecret := "queue_id=0x10203040 ticket=private-admission-material"
	ticketHandler := NewLoginRequestHandler(admissionInstallerFunc(func(context.Context, auth.AdmissionTicket) error {
		return errors.New(ticketSecret)
	}))
	ticketHandler.CredentialLookup = func(context.Context, string, string) (uint32, bool) { return 77, true }
	ticketHandler.AdmissionEvaluator = allowAdmission
	ticketSession, ticketClientConn := newGatewayTestSession(t, gatewayTestSourceIP)
	defer ticketSession.Close()
	defer ticketClientConn.Close()

	ticketDone := make(chan struct{})
	go func() {
		ticketHandler.handlePacket(context.Background(), server.PacketChannelData{Session: ticketSession, Packet: buildGatewayLoginRequest(t, "Alice", "stock-password")})
		close(ticketDone)
	}()
	_ = readGatewayResponse(t, ticketClientConn)
	select {
	case <-ticketDone:
	case <-time.After(time.Second):
		t.Fatal("Gateway login handler did not finish ticket failure")
	}
	if got := buffer.String(); strings.Contains(got, ticketSecret) {
		t.Fatalf("ticket error leaked to Gateway logs: %q", got)
	}
}

func TestLoginRequestHandlerWithNilCredentialRepositoryFailsClosed(t *testing.T) {
	handler := NewLoginRequestHandlerWithCredentialRepository(nil, nil)
	if _, _, err := handler.authenticate(context.Background(), "Alice", "stock-password"); !errors.Is(err, db.ErrCredentialRepositoryUnavailable) {
		t.Fatalf("nil credential repository error = %v, want ErrCredentialRepositoryUnavailable", err)
	}
}

type admissionInstallerFunc func(context.Context, auth.AdmissionTicket) error

func (f admissionInstallerFunc) Install(ctx context.Context, ticket auth.AdmissionTicket) error {
	return f(ctx, ticket)
}

type credentialRepositoryFunc func(context.Context, string, string) (uint32, bool, error)

func (f credentialRepositoryFunc) Authenticate(ctx context.Context, username, password string) (uint32, bool, error) {
	return f(ctx, username, password)
}

func newGatewayTestSession(t *testing.T, sourceIP netip.Addr) (*server.Session, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	session := server.NewSession(&gatewayTestConn{
		Conn:   serverConn,
		remote: &net.TCPAddr{IP: sourceIP.AsSlice(), Port: 12000},
	}, network.EncodingOptions{}, nil, nil, "gateway-test")
	return session, clientConn
}

type gatewayTestConn struct {
	net.Conn
	remote net.Addr
}

func (c *gatewayTestConn) RemoteAddr() net.Addr {
	return c.remote
}

func buildGatewayLoginRequest(t *testing.T, username, password string) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(56); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteString(username); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteString(password); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteUInt16(1); err != nil {
		t.Fatal(err)
	}
	return packet
}

func readGatewayResponse(t *testing.T, clientConn net.Conn) network.Packet {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})
	header := make([]byte, network.HeaderSize)
	if _, err := io.ReadFull(clientConn, header); err != nil {
		t.Fatalf("failed to read A102 header: %v", err)
	}
	if got, want := binary.LittleEndian.Uint16(header[network.IDOffset:network.IDOffset+2]), uint16(0xA102); got != want {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", got, want)
	}
	payloadSize := int(binary.LittleEndian.Uint16(header[:2]) &^ network.EncryptMask)
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(clientConn, payload); err != nil {
		t.Fatalf("failed to read A102 payload: %v", err)
	}
	return network.NewPacket(append(header, payload...))
}

func assertNoGatewayResponse(t *testing.T, clientConn net.Conn) {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})
	var one [1]byte
	if _, err := clientConn.Read(one[:]); err == nil {
		t.Fatalf("received unexpected additional A102 data: %02X", one[0])
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("response read error = %v, want deadline", err)
	}
}
