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
}

func (f *characterRenameRepositoryFake) RenameCharacter(_ context.Context, userID uint32, shardID uint16, currentName, newName string) error {
	f.userID, f.shardID, f.currentName, f.newName = userID, shardID, currentName, newName
	return f.err
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
