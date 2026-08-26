package lobby

import (
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

type characterRenameRepositoryFake struct {
	userID               uint32
	shardID              uint16
	currentName, newName string
	err                  error
	guildAvailable       bool
}

func (f *characterRenameRepositoryFake) RenameCharacter(_ context.Context, userID uint32, shardID uint16, currentName, newName string) error {
	f.userID, f.shardID, f.currentName, f.newName = userID, shardID, currentName, newName
	return f.err
}

func (f *characterRenameRepositoryFake) RenameGuild(_ context.Context, userID uint32, shardID uint16, currentName, newName string) error {
	f.userID, f.shardID, f.currentName, f.newName = userID, shardID, currentName, newName
	return f.err
}

func (f *characterRenameRepositoryFake) GuildNameAvailable(_ context.Context, name string) (bool, error) {
	f.newName = name
	return f.guildAvailable, f.err
}

func TestCharacterRenameConsumesThroughOwnedRepository(t *testing.T) {
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 77
	repository := &characterRenameRepositoryFake{}
	handler := &characterRenameHandler{repository: repository}
	request := network.EmptyClientPacket()
	request.WriteByte(CharSelectionCharacterRename)
	request.WriteString("CurrentName")
	request.WriteString("NewName")
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handlePacket(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	action := byte(CharSelectionCharacterRename)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionRenameResponse, &action, ResultTrue)
	if response.Remaining() != 0 || repository.userID != 77 || repository.shardID != 1 || repository.currentName != "CurrentName" || repository.newName != "NewName" {
		t.Fatalf("rename response/repository = remaining %d, %+v", response.Remaining(), repository)
	}
}

func TestGuildRenameConsumesForcedRenameThroughOwnedRepository(t *testing.T) {
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 88
	repository := &characterRenameRepositoryFake{}
	handler := &characterRenameHandler{repository: repository}
	request := network.EmptyClientPacket()
	request.WriteByte(CharSelectionGuildRename)
	request.WriteString("OldGuild")
	request.WriteString("NewGuild")
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handlePacket(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	action := byte(CharSelectionGuildRename)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionRenameResponse, &action, ResultTrue)
	if repository.userID != 88 || repository.shardID != 1 || repository.currentName != "OldGuild" || repository.newName != "NewGuild" {
		t.Fatalf("guild rename repository call = %+v", repository)
	}
}

func TestGuildNameCheckReportsAvailability(t *testing.T) {
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	repository := &characterRenameRepositoryFake{guildAvailable: true}
	handler := &characterRenameHandler{repository: repository}
	request := network.EmptyClientPacket()
	request.WriteByte(CharSelectionGuildNameCheck)
	request.WriteString("AvailableGuild")
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handlePacket(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	action := byte(CharSelectionGuildNameCheck)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionRenameResponse, &action, ResultTrue)
	if repository.newName != "AvailableGuild" {
		t.Fatalf("guild availability name = %q", repository.newName)
	}
}
