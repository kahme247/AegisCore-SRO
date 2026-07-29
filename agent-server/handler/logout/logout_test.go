package logout

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

func TestLogoutCountdownCancelsOnExplicitCancelAndSessionClose(t *testing.T) {
	for _, closeSession := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit cancel", true: "session close"}[closeSession], func(t *testing.T) {
			handler := newLogoutHandler()
			handler.countdown = 25 * time.Millisecond
			serverConn, clientConn := net.Pipe()
			session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "logout-test")
			defer session.Close()
			defer clientConn.Close()

			request := network.EmptyPacket()
			if err := request.WriteByte(LogoutModeExit); err != nil {
				t.Fatal(err)
			}
			logoutDone := make(chan error, 1)
			go func() { logoutDone <- handler.handleLogoutPacket(context.Background(), session, request) }()
			response := readLogoutResponse(t, clientConn)
			if err := <-logoutDone; err != nil {
				t.Fatalf("handleLogoutPacket() error = %v", err)
			}
			if response.MessageID != opcode.AgentLogoutResponse {
				t.Fatalf("logout response opcode = 0x%04X, want 0x%04X", response.MessageID, opcode.AgentLogoutResponse)
			}

			if closeSession {
				session.Close()
			} else {
				cancelDone := make(chan error, 1)
				go func() { cancelDone <- handler.handleCancelPacket(context.Background(), session, network.EmptyPacket()) }()
				cancelResponse := readLogoutResponse(t, clientConn)
				if err := <-cancelDone; err != nil {
					t.Fatalf("handleCancelPacket() error = %v", err)
				}
				if cancelResponse.MessageID != opcode.AgentLogoutCancelResponse {
					t.Fatalf("logout cancel response opcode = 0x%04X, want 0x%04X", cancelResponse.MessageID, opcode.AgentLogoutCancelResponse)
				}
			}

			time.Sleep(75 * time.Millisecond)
			if !closeSession {
				assertNoLogoutSuccess(t, clientConn)
			}
			handler.mu.Lock()
			pending := len(handler.pending)
			handler.mu.Unlock()
			if pending != 0 {
				t.Fatalf("pending logout countdowns = %d, want 0", pending)
			}
		})
	}
}

func readLogoutResponse(t *testing.T, connection net.Conn) network.Packet {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	defer connection.SetReadDeadline(time.Time{})
	header := make([]byte, network.HeaderSize)
	if _, err := io.ReadFull(connection, header); err != nil {
		t.Fatalf("read logout response header: %v", err)
	}
	payload := make([]byte, int(binary.LittleEndian.Uint16(header[:2])&^network.EncryptMask))
	if _, err := io.ReadFull(connection, payload); err != nil {
		t.Fatalf("read logout response payload: %v", err)
	}
	return network.NewPacket(append(header, payload...))
}

func assertNoLogoutSuccess(t *testing.T, connection net.Conn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	defer connection.SetReadDeadline(time.Time{})
	var byteBuffer [1]byte
	if _, err := connection.Read(byteBuffer[:]); err == nil {
		t.Fatalf("unexpected delayed logout data: %02X", byteBuffer[0])
	} else if networkErr, ok := err.(net.Error); !ok || !networkErr.Timeout() {
		t.Fatalf("delayed logout read error = %v, want deadline", err)
	}
}
