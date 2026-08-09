package lobby

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

type fakeCharacterStore struct {
	character    model.Char
	characterErr error
	list         []model.Char
	listErr      error
	updated      bool
	updateErr    error
	nameExists   bool
	nameErr      error

	characterCalls int
	characterName  string
	characterOwner uint32
	characterShard uint16
	updateCalls    int
	updateName     string
	updateOwner    uint32
	updateShard    uint16
	updateDeleting bool
	listCalls      int
	listOwner      uint32
	listShard      uint16
	nameCalls      int
	checkedName    string
}

type blockingCharacterStore struct {
	fakeCharacterStore
	joinEntered     chan struct{}
	releaseJoin     chan struct{}
	deletionStarted chan struct{}
}

func (s *blockingCharacterStore) CharacterForOwner(_ context.Context, _ string, _ uint32, _ uint16) (model.Char, error) {
	s.joinEntered <- struct{}{}
	<-s.releaseJoin
	return model.Char{}, model.ErrCharacterNotFound
}

func (s *blockingCharacterStore) SetDeletionForOwner(_ context.Context, _ string, _ uint32, _ uint16, _ bool) (bool, error) {
	s.deletionStarted <- struct{}{}
	return true, nil
}

func (s *fakeCharacterStore) CharacterForOwner(_ context.Context, name string, userID uint32, shardID uint16) (model.Char, error) {
	s.characterCalls++
	s.characterName = name
	s.characterOwner = userID
	s.characterShard = shardID
	return s.character, s.characterErr
}

func (s *fakeCharacterStore) CharactersForOwner(_ context.Context, userID uint32, shardID uint16) ([]model.Char, error) {
	s.listCalls++
	s.listOwner = userID
	s.listShard = shardID
	return s.list, s.listErr
}

func (s *fakeCharacterStore) SetDeletionForOwner(_ context.Context, name string, userID uint32, shardID uint16, deleting bool) (bool, error) {
	s.updateCalls++
	s.updateName = name
	s.updateOwner = userID
	s.updateShard = shardID
	s.updateDeleting = deleting
	return s.updated, s.updateErr
}

func (s *fakeCharacterStore) CharacterNameExists(_ context.Context, name string) (bool, error) {
	s.nameCalls++
	s.checkedName = name
	return s.nameExists, s.nameErr
}

func TestCharacterSelectionJoinRejectsUnknownAndUnownedCharactersWithoutBootstrap(t *testing.T) {
	tests := []struct {
		name  string
		store *fakeCharacterStore
	}{
		{
			name:  "unknown character",
			store: &fakeCharacterStore{characterErr: model.ErrCharacterNotFound},
		},
		{
			name:  "character returned for a different owner",
			store: &fakeCharacterStore{character: model.Char{ID: 7, User: 99, Shard: 1, Name: "other-account"}},
		},
		{
			name:  "character returned for a different shard",
			store: &fakeCharacterStore{character: model.Char{ID: 7, User: 42, Shard: 2, Name: "other-account"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewCharacterSelectionHandler(test.store)
			session, clientConn := newCharacterSelectionTestSession(t)
			defer session.Close()
			defer clientConn.Close()
			session.MarkAuthenticated()
			session.UserContext.UserID = 42

			request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
				return packet.WriteString("other-account")
			})
			response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
				return handler.handleJoin(context.Background(), session, request)
			})
			if err != nil {
				t.Fatalf("handleJoin() error = %v", err)
			}
			assertCharacterSelectionResult(t, &response, OpcodeCharSelectionJoinResponse, nil, ResultFalse)
			assertNoAdditionalCharacterSelectionResponse(t, clientConn)

			if got, want := test.store.characterCalls, 1; got != want {
				t.Fatalf("CharacterForOwner calls = %d, want %d", got, want)
			}
			if got, want := test.store.characterOwner, uint32(42); got != want {
				t.Fatalf("CharacterForOwner owner = %d, want %d", got, want)
			}
			if got, want := test.store.characterShard, uint16(1); got != want {
				t.Fatalf("CharacterForOwner shard = %d, want %d", got, want)
			}
			if got, want := test.store.characterName, "other-account"; got != want {
				t.Fatalf("CharacterForOwner name = %q, want %q", got, want)
			}
		})
	}
}

func TestCharacterSelectionJoinRejectsMalformedPacketBeforeStoreAccess(t *testing.T) {
	store := &fakeCharacterStore{}
	handler := NewCharacterSelectionHandler(store)
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42

	malformed := network.NewPacket(make([]byte, network.HeaderSize))
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleJoin(context.Background(), session, malformed)
	})
	if err != nil {
		t.Fatalf("handleJoin(malformed) error = %v", err)
	}
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionJoinResponse, nil, ResultFalse)
	assertNoAdditionalCharacterSelectionResponse(t, clientConn)
	if store.characterCalls != 0 {
		t.Fatalf("CharacterForOwner calls = %d, want 0 for malformed input", store.characterCalls)
	}
}

func TestCharacterSelectionRejectsTrailingPayloadBeforeStoreAccess(t *testing.T) {
	store := &fakeCharacterStore{}
	handler := NewCharacterSelectionHandler(store)
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42

	join := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteString("owned-character") },
		func(packet *network.Packet) error { return packet.WriteByte(0xFF) },
	)
	joinResponse, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleJoin(context.Background(), session, join)
	})
	if err != nil {
		t.Fatalf("handleJoin(trailing) error = %v", err)
	}
	assertCharacterSelectionResult(t, &joinResponse, OpcodeCharSelectionJoinResponse, nil, ResultFalse)
	if store.characterCalls != 0 {
		t.Fatalf("CharacterForOwner calls = %d, want 0 for trailing join input", store.characterCalls)
	}

	deleteRequest := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteByte(CharActionDelete) },
		func(packet *network.Packet) error { return packet.WriteString("owned-character") },
		func(packet *network.Packet) error { return packet.WriteByte(0xFF) },
	)
	deleteResponse, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleAction(context.Background(), session, deleteRequest)
	})
	if err != nil {
		t.Fatalf("handleAction(trailing) error = %v", err)
	}
	expectedAction := byte(CharActionDelete)
	assertCharacterSelectionResult(t, &deleteResponse, OpcodeCharSelectionActionResponse, &expectedAction, ResultFalse)
	if store.updateCalls != 0 {
		t.Fatalf("SetDeletionForOwner calls = %d, want 0 for trailing delete input", store.updateCalls)
	}
}

func TestCharacterSelectionRejectsUnauthenticatedSessionEvenWithAnOwnerID(t *testing.T) {
	store := &fakeCharacterStore{}
	handler := NewCharacterSelectionHandler(store)
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.UserContext.UserID = 42

	request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteByte(CharActionList)
	})
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleAction(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handleAction(unauthenticated) error = %v", err)
	}
	expectedAction := byte(0)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &expectedAction, ResultFalse)
	if store.listCalls != 0 || store.characterCalls != 0 || store.updateCalls != 0 {
		t.Fatalf("store was accessed for unauthenticated session: %+v", store)
	}
}

func TestCharacterSelectionJoinAndActionShareOneSerializationLock(t *testing.T) {
	store := &blockingCharacterStore{
		joinEntered:     make(chan struct{}, 1),
		releaseJoin:     make(chan struct{}),
		deletionStarted: make(chan struct{}, 1),
	}
	handler := NewCharacterSelectionHandler(store)

	joinSession, joinClient := newCharacterSelectionTestSession(t)
	defer joinSession.Close()
	defer joinClient.Close()
	joinSession.MarkAuthenticated()
	joinSession.UserContext.UserID = 42

	actionSession, actionClient := newCharacterSelectionTestSession(t)
	defer actionSession.Close()
	defer actionClient.Close()
	actionSession.MarkAuthenticated()
	actionSession.UserContext.UserID = 42

	joinRequest := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteString("waiting-character")
	})
	actionRequest := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteByte(CharActionDelete) },
		func(packet *network.Packet) error { return packet.WriteString("waiting-character") },
	)

	joinDone := make(chan error, 1)
	go func() {
		joinDone <- handler.handleJoin(context.Background(), joinSession, joinRequest)
	}()
	select {
	case <-store.joinEntered:
	case <-time.After(time.Second):
		t.Fatal("join handler did not reach the character store")
	}

	actionDone := make(chan error, 1)
	go func() {
		actionDone <- handler.handleAction(context.Background(), actionSession, actionRequest)
	}()
	select {
	case <-store.deletionStarted:
		t.Fatal("lobby action reached persistence while join held the shared lock")
	case <-time.After(100 * time.Millisecond):
	}

	close(store.releaseJoin)
	joinResponse := readCharacterSelectionPacket(t, joinClient)
	assertCharacterSelectionResult(t, &joinResponse, OpcodeCharSelectionJoinResponse, nil, ResultFalse)
	actionResponse := readCharacterSelectionPacket(t, actionClient)
	expectedAction := byte(CharActionDelete)
	assertCharacterSelectionResult(t, &actionResponse, OpcodeCharSelectionActionResponse, &expectedAction, ResultTrue)

	for name, done := range map[string]chan error{"join": joinDone, "action": actionDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s handler error = %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s handler did not finish", name)
		}
	}
}

func TestCharacterSelectionDeletionActionsUseAuthenticatedOwnerAndHideForeignNames(t *testing.T) {
	tests := []struct {
		name           string
		action         byte
		deleting       bool
		updated        bool
		expectedResult byte
	}{
		{name: "owned character delete", action: CharActionDelete, deleting: true, updated: true, expectedResult: ResultTrue},
		{name: "foreign or unknown character delete", action: CharActionDelete, deleting: true, updated: false, expectedResult: ResultFalse},
		{name: "owned character restore", action: CharActionRestore, deleting: false, updated: true, expectedResult: ResultTrue},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCharacterStore{updated: test.updated}
			handler := NewCharacterSelectionHandler(store)
			session, clientConn := newCharacterSelectionTestSession(t)
			defer session.Close()
			defer clientConn.Close()
			session.MarkAuthenticated()
			session.UserContext.UserID = 42

			request := buildCharacterSelectionRequest(t,
				func(packet *network.Packet) error { return packet.WriteByte(test.action) },
				func(packet *network.Packet) error { return packet.WriteString("other-account") },
			)
			response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
				return handler.handleAction(context.Background(), session, request)
			})
			if err != nil {
				t.Fatalf("handleAction(deletion) error = %v", err)
			}
			expectedAction := test.action
			assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &expectedAction, test.expectedResult)
			assertNoAdditionalCharacterSelectionResponse(t, clientConn)

			if got, want := store.updateCalls, 1; got != want {
				t.Fatalf("SetDeletionForOwner calls = %d, want %d", got, want)
			}
			if got, want := store.updateOwner, uint32(42); got != want {
				t.Fatalf("SetDeletionForOwner owner = %d, want %d", got, want)
			}
			if got, want := store.updateShard, uint16(1); got != want {
				t.Fatalf("SetDeletionForOwner shard = %d, want %d", got, want)
			}
			if got, want := store.updateName, "other-account"; got != want {
				t.Fatalf("SetDeletionForOwner name = %q, want %q", got, want)
			}
			if store.updateDeleting != test.deleting {
				t.Fatalf("SetDeletionForOwner deleting = %t, want %t", store.updateDeleting, test.deleting)
			}
		})
	}
}

func TestCharacterSelectionListUsesAuthenticatedOwner(t *testing.T) {
	store := &fakeCharacterStore{list: []model.Char{}}
	handler := NewCharacterSelectionHandler(store)
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42

	request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteByte(CharActionList)
	})
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleAction(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handleAction(list) error = %v", err)
	}
	expectedAction := byte(CharActionList)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &expectedAction, ResultTrue)
	count, err := response.ReadByte()
	if err != nil {
		t.Fatalf("list response missing character count: %v", err)
	}
	if count != 0 {
		t.Fatalf("list response character count = %d, want 0", count)
	}
	assertNoAdditionalCharacterSelectionResponse(t, clientConn)
	if got, want := store.listCalls, 1; got != want {
		t.Fatalf("CharactersForOwner calls = %d, want %d", got, want)
	}
	if got, want := store.listOwner, uint32(42); got != want {
		t.Fatalf("CharactersForOwner owner = %d, want %d", got, want)
	}
	if got, want := store.listShard, uint16(1); got != want {
		t.Fatalf("CharactersForOwner shard = %d, want %d", got, want)
	}
}

func TestCharacterSelectionActionRejectsMalformedAndUnknownInputWithoutStoreAccess(t *testing.T) {
	tests := []struct {
		name              string
		request           network.Packet
		expectedAction    byte
		expectedErrorCode uint16
	}{
		{
			name:           "malformed action",
			request:        network.NewPacket(make([]byte, network.HeaderSize)),
			expectedAction: 0,
		},
		{
			name: "unknown action",
			request: buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
				return packet.WriteByte(0xFF)
			}),
			expectedAction:    0xFF,
			expectedErrorCode: ACTION_MSG_ERROR_ID,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCharacterStore{}
			handler := NewCharacterSelectionHandler(store)
			session, clientConn := newCharacterSelectionTestSession(t)
			defer session.Close()
			defer clientConn.Close()
			session.MarkAuthenticated()
			session.UserContext.UserID = 42

			response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
				return handler.handleAction(context.Background(), session, test.request)
			})
			if err != nil {
				t.Fatalf("handleAction() error = %v", err)
			}
			expectedAction := test.expectedAction
			assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &expectedAction, ResultFalse)
			if test.expectedErrorCode != 0 {
				code, err := response.ReadUInt16()
				if err != nil {
					t.Fatalf("failure response missing error code: %v", err)
				}
				if code != test.expectedErrorCode {
					t.Fatalf("failure response error code = 0x%04X, want 0x%04X", code, test.expectedErrorCode)
				}
			}
			assertNoAdditionalCharacterSelectionResponse(t, clientConn)
			if store.characterCalls != 0 || store.updateCalls != 0 || store.listCalls != 0 || store.nameCalls != 0 {
				t.Fatalf("store was accessed for rejected input: %+v", store)
			}
		})
	}
}

func TestCharacterSelectionRenameRemainsRejected(t *testing.T) {
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()

	request := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteByte(CharSelectionCharacterRename) },
		func(packet *network.Packet) error { return packet.WriteString("current-name") },
		func(packet *network.Packet) error { return packet.WriteString("requested-name") },
	)
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handleCharSelectionRenameRequest(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handleCharSelectionRenameRequest() error = %v", err)
	}
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionRenameResponse, nil, ResultFalse)
	assertNoAdditionalCharacterSelectionResponse(t, clientConn)
}

func newCharacterSelectionTestSession(t *testing.T) (*server.Session, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	session := server.NewSession(serverConn, network.EncodingOptions{}, nil, nil, "character-selection-test")
	session.UserContext.ShardID = 1
	return session, clientConn
}

func buildCharacterSelectionRequest(t *testing.T, writes ...func(*network.Packet) error) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	for _, write := range writes {
		if err := write(&packet); err != nil {
			t.Fatalf("build request: %v", err)
		}
	}
	wire, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return network.NewPacket(wire)
}

func dispatchCharacterSelectionHandler(t *testing.T, clientConn net.Conn, invoke func() error) (network.Packet, error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- invoke() }()

	response := readCharacterSelectionPacket(t, clientConn)
	select {
	case err := <-result:
		return response, err
	case <-time.After(time.Second):
		t.Fatal("character selection handler did not finish after its response")
		return network.Packet{}, nil
	}
}

func readCharacterSelectionPacket(t *testing.T, clientConn net.Conn) network.Packet {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})

	header := make([]byte, network.HeaderSize)
	if _, err := io.ReadFull(clientConn, header); err != nil {
		t.Fatalf("read character-selection response header: %v", err)
	}
	payloadSize := int(binary.LittleEndian.Uint16(header[:2]) &^ network.EncryptMask)
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(clientConn, payload); err != nil {
		t.Fatalf("read character-selection response payload: %v", err)
	}
	return network.NewPacket(append(header, payload...))
}

func assertCharacterSelectionResult(t *testing.T, response *network.Packet, messageID uint16, action *byte, result byte) {
	t.Helper()
	if got, want := response.MessageID, messageID; got != want {
		t.Fatalf("response opcode = 0x%04X, want 0x%04X", got, want)
	}
	if action != nil {
		gotAction, err := response.ReadByte()
		if err != nil {
			t.Fatalf("response missing action: %v", err)
		}
		if gotAction != *action {
			t.Fatalf("response action = %d, want %d", gotAction, *action)
		}
	}
	gotResult, err := response.ReadByte()
	if err != nil {
		t.Fatalf("response missing result: %v", err)
	}
	if gotResult != result {
		t.Fatalf("response result = %d, want %d", gotResult, result)
	}
}

func assertNoAdditionalCharacterSelectionResponse(t *testing.T, clientConn net.Conn) {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer clientConn.SetReadDeadline(time.Time{})
	var one [1]byte
	if _, err := clientConn.Read(one[:]); err == nil {
		t.Fatalf("received a second response byte: %02X", one[0])
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("second response read error = %v, want deadline", err)
	}
}

var _ CharacterStore = (*fakeCharacterStore)(nil)
var _ CharacterStore = (*blockingCharacterStore)(nil)
