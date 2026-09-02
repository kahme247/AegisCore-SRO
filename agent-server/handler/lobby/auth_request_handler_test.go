package lobby

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/security/auth"
	"github.com/Harvi8/AegisCore/framework/server"
)

var authTestSourceIP = netip.MustParseAddr("203.0.113.7")

func TestAuthRequestHandlerRejectsUnknownTicketWithExactlyOneResponse(t *testing.T) {
	handler := &AuthRequestHandler{Tickets: auth.NewAdmissionTicketStore()}
	session, clientConn := newAuthTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	response, handleErr := dispatchAuthRequest(t, handler, session, clientConn, buildAuthRequest(t, 0x12345678, "alice", "stock-password", 56))
	assertAuthResponse(t, response, ResultFalse)
	if !errors.Is(handleErr, auth.ErrAdmissionTicketUnknown) {
		t.Fatalf("HandlePacket(unknown) error = %v, want ErrAdmissionTicketUnknown", handleErr)
	}
	assertNoSecondAuthResponse(t, clientConn)
}

func TestAuthRequestHandlerKeepsWrongUserTicketAndRedeemsOnce(t *testing.T) {
	store := auth.NewAdmissionTicketStore()
	ticket := auth.AdmissionTicket{
		QueueID:      0x10203040,
		TraceID:      "gateway-session-123",
		AccountID:    77,
		Username:     "Alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     authTestSourceIP,
	}
	if err := store.Install(ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	handler := &AuthRequestHandler{Tickets: store}

	wrongSession, wrongClient := newAuthTestSession(t)
	wrongResponse, wrongErr := dispatchAuthRequest(t, handler, wrongSession, wrongClient, buildAuthRequest(t, ticket.QueueID, "mallory", "wrong-password", 56))
	assertAuthResponse(t, wrongResponse, ResultFalse)
	if !errors.Is(wrongErr, auth.ErrAdmissionTicketMismatch) {
		t.Fatalf("HandlePacket(wrong user) error = %v, want ErrAdmissionTicketMismatch", wrongErr)
	}
	assertNoSecondAuthResponse(t, wrongClient)
	wrongSession.Close()
	wrongClient.Close()

	validSession, validClient := newAuthTestSession(t)
	validResponse, validErr := dispatchAuthRequest(t, handler, validSession, validClient, buildAuthRequest(t, ticket.QueueID, " alice ", "different-client-password", 56))
	assertAuthResponse(t, validResponse, ResultTrue)
	if validErr != nil {
		t.Fatalf("HandlePacket(valid) error = %v", validErr)
	}
	if !validSession.IsAuthenticated() {
		t.Fatal("valid admission did not mark the session authenticated")
	}
	if got := validSession.TraceCorrelationID(); got != ticket.TraceID {
		t.Fatalf("trace correlation ID = %q, want %q", got, ticket.TraceID)
	}
	if got, want := validSession.UserContext.UserID, ticket.AccountID; got != want {
		t.Fatalf("authenticated user ID = %d, want %d", got, want)
	}
	assertNoSecondAuthResponse(t, validClient)
	validSession.Close()
	validClient.Close()

	replaySession, replayClient := newAuthTestSession(t)
	replayResponse, replayErr := dispatchAuthRequest(t, handler, replaySession, replayClient, buildAuthRequest(t, ticket.QueueID, "alice", "anything", 56))
	assertAuthResponse(t, replayResponse, ResultFalse)
	if !errors.Is(replayErr, auth.ErrAdmissionTicketUnknown) {
		t.Fatalf("HandlePacket(replay) error = %v, want ErrAdmissionTicketUnknown", replayErr)
	}
	assertNoSecondAuthResponse(t, replayClient)
	replaySession.Close()
	replayClient.Close()
}

func TestAuthRequestHandlerRejectsTrailingDataWithoutRedeemingTicket(t *testing.T) {
	store := auth.NewAdmissionTicketStore()
	ticket := auth.AdmissionTicket{
		QueueID:      0xA0B0C0D0,
		AccountID:    77,
		Username:     "alice",
		ShardID:      1,
		ContentID:    56,
		HasContentID: true,
		SourceIP:     authTestSourceIP,
	}
	if err := store.Install(ticket); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	handler := &AuthRequestHandler{Tickets: store}

	trailingSession, trailingClient := newAuthTestSession(t)
	trailingRequest := buildAuthRequest(t, ticket.QueueID, ticket.Username, "stock-password", ticket.ContentID)
	if err := trailingRequest.WriteByte(0xFF); err != nil {
		t.Fatalf("WriteByte(trailing data) error = %v", err)
	}
	trailingResponse, trailingErr := dispatchAuthRequest(t, handler, trailingSession, trailingClient, trailingRequest)
	assertAuthResponse(t, trailingResponse, ResultFalse)
	if trailingErr == nil {
		t.Fatal("HandlePacket(trailing request) succeeded")
	}
	assertNoSecondAuthResponse(t, trailingClient)
	trailingSession.Close()
	trailingClient.Close()

	validSession, validClient := newAuthTestSession(t)
	validResponse, validErr := dispatchAuthRequest(t, handler, validSession, validClient, buildAuthRequest(t, ticket.QueueID, ticket.Username, "stock-password", ticket.ContentID))
	assertAuthResponse(t, validResponse, ResultTrue)
	if validErr != nil {
		t.Fatalf("HandlePacket(valid after trailing data) error = %v", validErr)
	}
	validSession.Close()
	validClient.Close()
}

func newAuthTestSession(t *testing.T) (*server.Session, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	session := server.NewSession(&authTestConn{
		Conn:   serverConn,
		remote: &net.TCPAddr{IP: authTestSourceIP.AsSlice(), Port: 12000},
	}, network.EncodingOptions{}, nil, nil, "auth-test")
	return session, clientConn
}

type authTestConn struct {
	net.Conn
	remote net.Addr
}

func (c *authTestConn) RemoteAddr() net.Addr {
	return c.remote
}

func buildAuthRequest(t *testing.T, queueID uint32, username, password string, contentID byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	if err := packet.WriteUInt32(queueID); err != nil {
		t.Fatalf("WriteUInt32() error = %v", err)
	}
	if err := packet.WriteString(username); err != nil {
		t.Fatalf("WriteString(username) error = %v", err)
	}
	if err := packet.WriteString(password); err != nil {
		t.Fatalf("WriteString(password) error = %v", err)
	}
	if err := packet.WriteByte(contentID); err != nil {
		t.Fatalf("WriteByte(content ID) error = %v", err)
	}
	if err := packet.WriteBytes(make([]byte, MacAddressLength)); err != nil {
		t.Fatalf("WriteBytes(mac) error = %v", err)
	}
	return packet
}

func dispatchAuthRequest(t *testing.T, handler *AuthRequestHandler, session *server.Session, clientConn net.Conn, request network.Packet) (network.Packet, error) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		result <- handler.HandlePacket(server.PacketChannelData{Session: session, Packet: request})
	}()

	response := readPacketFromConn(t, clientConn)
	select {
	case err := <-result:
		return response, err
	case <-time.After(time.Second):
		t.Fatal("AuthRequestHandler did not finish after sending its response; it may have written a second response")
		return network.Packet{}, nil
	}
}

func assertAuthResponse(t *testing.T, response network.Packet, result byte) {
	t.Helper()
	if got, want := response.MessageID, OpcodeAuthResponse; got != want {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", got, want)
	}
	gotResult, err := response.ReadByte()
	if err != nil {
		t.Fatalf("response missing result: %v", err)
	}
	if gotResult != result {
		t.Fatalf("response result = %d, want %d", gotResult, result)
	}
	if result == ResultFalse {
		code, err := response.ReadByte()
		if err != nil {
			t.Fatalf("failure response missing error code: %v", err)
		}
		if code != 4 {
			t.Fatalf("failure response code = %d, want 4", code)
		}
	}
}

func assertNoSecondAuthResponse(t *testing.T, clientConn net.Conn) {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})
	var one [1]byte
	if _, err := clientConn.Read(one[:]); err == nil {
		t.Fatalf("received a second byte after the only expected 0xA103 response: %02X", one[0])
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("second response read error = %v, want deadline", err)
	}
}

func readPacketFromConn(t *testing.T, clientConn net.Conn) network.Packet {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})
	header := make([]byte, network.HeaderSize)
	if _, err := io.ReadFull(clientConn, header); err != nil {
		t.Fatalf("failed to read auth response header: %v", err)
	}
	payloadSize := int(binary.LittleEndian.Uint16(header[:2]) &^ network.EncryptMask)
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(clientConn, payload); err != nil {
		t.Fatalf("failed to read auth response payload: %v", err)
	}
	return network.NewPacket(append(header, payload...))
}
