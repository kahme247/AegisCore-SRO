package lobby

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

const (
	OpcodeCharSelectionRenameRequest  uint16 = 0x7450
	OpcodeCharSelectionRenameResponse uint16 = 0xB450

	// Actions
	CharSelectionCharacterRename uint8 = 1
	CharSelectionGuildRename     uint8 = 2
	CharSelectionGuildNameCheck  uint8 = 3

	// Errors
	RENAME_MSG_ERROR_ID                      uint8 = 6
	RENAME_SMERR_NOT_ALLOWED_CHARNAME        uint8 = 7
	RENAME_MSG_GUILDERR_SAME_GUILDNAME_EXIST uint8 = 6
	RENAME_MSG_GUILD_NOT_CREATE              uint8 = 7
)

func RegisterCharSelectionRenameRequestHandler(target *server.Server) {
	target.RegisterHandler(opcode.LobbyRenameRequest, handleCharSelectionRenameRequest)
}

func handleCharSelectionRenameRequest(_ context.Context, session *server.Session, packet network.Packet) error {
	if session == nil || !session.IsAuthenticated() {
		return errors.New("rename request arrived before authentication")
	}
	if !session.EnterLobby() {
		return errors.New("rename request arrived outside the lobby lifecycle")
	}
	action, err := packet.ReadByte()
	if err != nil {
		return err
	}

	if action == CharSelectionCharacterRename || action == CharSelectionGuildRename {
		currentGuildName, err := packet.ReadString()
		if err != nil {
			return err
		}

		newGuildName, err := packet.ReadString()
		if err != nil {
			return err
		}

		// Rename writes are deliberately unavailable until ownership-qualified
		// persistence exists. Parse the complete request so malformed input is
		// rejected by the session, but never acknowledge an unimplemented
		// privileged mutation as successful.
		_ = currentGuildName
		_ = newGuildName
	} else if action == CharSelectionGuildNameCheck {
		guildName, err := packet.ReadString()
		if err != nil {
			return err
		}

		_ = guildName
	}
	if packet.Remaining() != 0 {
		return errors.New("rename request contains trailing data")
	}

	p := network.EmptyPacket()
	p.MessageID = OpcodeCharSelectionRenameResponse
	if err := p.WriteByte(ResultFalse); err != nil {
		return err
	}
	return session.Send(p)
}
