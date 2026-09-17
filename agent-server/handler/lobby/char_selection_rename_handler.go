package lobby

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	OpcodeCharSelectionRenameRequest  uint16 = 0x7450
	OpcodeCharSelectionRenameResponse uint16 = 0xB450

	// Actions
	CharSelectionCharacterRename uint8 = 1
	CharSelectionGuildRename     uint8 = 2
	CharSelectionGuildNameCheck  uint8 = 3

	// Errors
	RENAME_MSG_ERROR_ID                      uint16 = 6
	RENAME_SMERR_NOT_ALLOWED_CHARNAME        uint16 = 7
	RENAME_MSG_GUILDERR_SAME_GUILDNAME_EXIST uint16 = 6
	RENAME_MSG_GUILD_NOT_CREATE              uint16 = 7
)

type characterRenameHandler struct {
	repository model.CharacterRenameRepository
}

func RegisterCharSelectionRenameRequestHandler(target *server.Server, repository ...model.CharacterRenameRepository) {
	if target == nil {
		return
	}
	handler := &characterRenameHandler{}
	if len(repository) > 0 {
		handler.repository = repository[0]
	}
	target.RegisterHandler(opcode.LobbyRenameRequest, handler.handlePacket)
}

func handleCharSelectionRenameRequest(ctx context.Context, session *server.Session, packet network.Packet) error {
	return (&characterRenameHandler{}).handlePacket(ctx, session, packet)
}

func (h *characterRenameHandler) handlePacket(ctx context.Context, session *server.Session, packet network.Packet) error {
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
	if action == CharSelectionCharacterRename {
		ctx = model.WithCharacterCommand(ctx, model.CharacterCommandIdentity{Key: fmt.Sprintf("character:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: fmt.Sprintf("lobby.rename.%d", action)})
	} else if action == CharSelectionGuildRename {
		ctx = model.WithSocialCommand(ctx, model.SocialCommandIdentity{Key: fmt.Sprintf("social:%s:%02x:%04x", session.ID, packet.Sequence, packet.MessageID), Name: "guild.forced-rename"})
	}

	if action == CharSelectionCharacterRename || action == CharSelectionGuildRename {
		currentName, err := packet.ReadString()
		if err != nil {
			return err
		}

		newName, err := packet.ReadString()
		if err != nil {
			return err
		}

		if packet.Remaining() != 0 {
			return errors.New("rename request contains trailing data")
		}
		if h.repository == nil || strings.EqualFold(strings.TrimSpace(currentName), strings.TrimSpace(newName)) {
			return sendRenameResult(session, action, ResultFalse, RENAME_SMERR_NOT_ALLOWED_CHARNAME)
		}
		if action == CharSelectionCharacterRename && modelNameValid(newName) {
			err = h.repository.RenameCharacter(ctx, session.UserContext.UserID, session.UserContext.ShardID, currentName, newName)
			if err == nil {
				return sendRenameResult(session, action, ResultTrue, 0)
			}
			if errors.Is(err, model.ErrCharacterRenameDuplicate) {
				return sendRenameResult(session, action, ResultFalse, RENAME_MSG_ERROR_ID)
			}
		}
		if action == CharSelectionCharacterRename {
			return sendRenameResult(session, action, ResultFalse, RENAME_SMERR_NOT_ALLOWED_CHARNAME)
		}
		if action == CharSelectionGuildRename && guildNameValid(newName) {
			err = h.repository.RenameGuild(ctx, session.UserContext.UserID, session.UserContext.ShardID, currentName, newName)
			if err == nil {
				return sendRenameResult(session, action, ResultTrue, 0)
			}
			if errors.Is(err, model.ErrGuildRenameDuplicate) {
				return sendRenameResult(session, action, ResultFalse, RENAME_MSG_GUILDERR_SAME_GUILDNAME_EXIST)
			}
		}
		return sendRenameResult(session, action, ResultFalse, RENAME_MSG_GUILD_NOT_CREATE)
	} else if action == CharSelectionGuildNameCheck {
		guildName, err := packet.ReadString()
		if err != nil {
			return err
		}

		if packet.Remaining() != 0 {
			return errors.New("rename request contains trailing data")
		}
		if h.repository != nil && guildNameValid(guildName) {
			available, checkErr := h.repository.GuildNameAvailable(ctx, guildName)
			if checkErr == nil && available {
				return sendRenameResult(session, action, ResultTrue, 0)
			}
			if checkErr == nil {
				return sendRenameResult(session, action, ResultFalse, RENAME_MSG_GUILDERR_SAME_GUILDNAME_EXIST)
			}
		}
		return sendRenameResult(session, action, ResultFalse, RENAME_MSG_GUILD_NOT_CREATE)
	}
	if packet.Remaining() != 0 {
		return errors.New("rename request contains trailing data")
	}

	return sendRenameResult(session, action, ResultFalse, RENAME_MSG_ERROR_ID)
}

func modelNameValid(name string) bool {
	return vsro188.ValidateText(name, characterNameRules()) == nil
}

func guildNameValid(name string) bool {
	return vsro188.ValidateText(name, vsro188.TextRules{Field: "guild name", MaxRunes: 20, RequireTrimmed: true}) == nil
}

func sendRenameResult(session *server.Session, action, result byte, code uint16) error {
	p := network.EmptyPacket()
	p.MessageID = OpcodeCharSelectionRenameResponse
	p.WriteByte(action)
	p.WriteByte(result)
	if result == ResultFalse {
		p.WriteUInt16(code)
	}
	return session.Send(p)
}
