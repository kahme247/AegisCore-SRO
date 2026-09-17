package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type guildStorageControlFake struct {
	model.GuildStorageRepository
	snapshot model.StorageSnapshot
	loadErr  error
	closeErr error
	closed   int64
}

func (f *guildStorageControlFake) LoadGuildStorage(context.Context, int64) (model.StorageSnapshot, error) {
	return f.snapshot, f.loadErr
}

func (f *guildStorageControlFake) CloseGuildStorage(_ context.Context, characterID int64) error {
	f.closed = characterID
	return f.closeErr
}

func TestGuildStorageInfoAndCloseRequestsAreEmpty(t *testing.T) {
	repository := &guildStorageControlFake{snapshot: model.StorageSnapshot{Capacity: 150, Items: map[byte]model.Item{}}}
	sent := make([]network.Packet, 0)
	h := &GuildStorageHandler{repo: repository, send: func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	session := &server.Session{UserContext: server.UserContext{CharacterID: 77}}
	if err := h.handleInfo(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.GuildStorageDataBegin || sent[2].MessageID != opcode.GuildStorageDataEnd {
		t.Fatalf("info responses = %#v", sent)
	}
	sent = nil
	if err := h.handleClose(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if repository.closed != 77 || len(sent) != 1 || sent[0].MessageID != opcode.GuildStorageCloseResponse || sent[0].Payload()[0] != 1 {
		t.Fatalf("close = character %d responses %#v", repository.closed, sent)
	}
}

func TestGuildStorageControlFailuresUseStockResponses(t *testing.T) {
	repository := &guildStorageControlFake{loadErr: model.ErrGuildStorageAccessDenied, closeErr: errors.New("close failed")}
	var sent network.Packet
	h := &GuildStorageHandler{repo: repository, send: func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }}
	session := &server.Session{UserContext: server.UserContext{CharacterID: 77}}
	if err := h.handleInfo(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if sent.MessageID != opcode.GuildStorageListResponse || len(sent.Payload()) != 3 || sent.Payload()[0] != 2 {
		t.Fatalf("info failure = %04x:% x", sent.MessageID, sent.Payload())
	}
	if err := h.handleClose(context.Background(), session, network.EmptyClientPacket()); err != nil {
		t.Fatal(err)
	}
	if sent.MessageID != opcode.GuildStorageCloseResponse || len(sent.Payload()) != 3 || sent.Payload()[0] != 2 {
		t.Fatalf("close failure = %04x:% x", sent.MessageID, sent.Payload())
	}
}

func TestGuildStorageErrorCodesPreserveStockDistinctions(t *testing.T) {
	if got := guildStorageErrorCode(model.ErrGuildStorageInUse); got != 0x4C48 {
		t.Fatalf("busy code = %#x", got)
	}
	if got := guildStorageErrorCode(model.ErrGuildStorageLevelTooLow); got != 0x4C4A {
		t.Fatalf("level code = %#x", got)
	}
	if got := guildStorageErrorCode(model.ErrGuildStorageAccessDenied); got != 0x4C49 {
		t.Fatalf("access code = %#x", got)
	}
}
