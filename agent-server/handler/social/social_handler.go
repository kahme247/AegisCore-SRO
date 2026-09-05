package social

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

const academyPageSize = 10

type worldLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
	GetPlayerByCharacterID(int) (*model.Player, error)
}

// Handler owns the GameServer-equivalent protocol and online-publication
// layer. Durable authority and transitions remain in SocialRepository.
type Handler struct {
	repo  model.SocialRepository
	world worldLookup
	send  func(*server.Session, network.Packet) error
}

func RegisterHandlers(target *server.Server, repo model.SocialRepository) *Handler {
	if target == nil || repo == nil {
		return nil
	}
	h := &Handler{repo: repo, world: service.GetWorldServiceInstance()}
	target.RegisterHandler(opcode.GuildCreateRequest, h.guildCreate)
	target.RegisterHandler(opcode.GuildDisbandRequest, h.guildDisband)
	target.RegisterHandler(opcode.GuildLeaveRequest, h.guildLeave)
	target.RegisterHandler(opcode.GuildInviteRequest, h.guildInvite)
	target.RegisterHandler(opcode.GuildKickRequest, h.guildKick)
	target.RegisterHandler(opcode.GuildNoticeRequest, h.guildNotice)
	target.RegisterHandler(opcode.GuildPromoteRequest, h.guildPromote)
	target.RegisterHandler(opcode.GuildUnionInviteRequest, h.guildUnionInvite)
	target.RegisterHandler(opcode.GuildUnionLeaveRequest, h.guildUnionLeave)
	target.RegisterHandler(opcode.GuildUnionKickRequest, h.guildUnionKick)
	target.RegisterHandler(opcode.GuildTransferRequest, h.guildTransfer)
	target.RegisterHandler(opcode.GuildPermissionRequest, h.guildPermissions)
	target.RegisterHandler(opcode.GuildNicknameRequest, h.guildNickname)
	target.RegisterHandler(opcode.GuildDonateRequest, h.guildDonate)
	target.RegisterHandler(opcode.AcademyCreateRequest, h.academyCreate)
	target.RegisterHandler(opcode.AcademyDisbandRequest, h.academyDisband)
	target.RegisterHandler(opcode.AcademyKickRequest, h.academyKick)
	target.RegisterHandler(opcode.AcademyLeaveRequest, h.academyLeave)
	target.RegisterHandler(opcode.AcademyCommentRequest, h.academyComment)
	target.RegisterHandler(opcode.AcademyGradeRequest, h.academyGrade)
	target.RegisterHandler(opcode.AcademyHonorRankRequest, h.academyHonorRank)
	target.RegisterHandler(opcode.AcademyMatchingRegisterRequest, h.academyMatchingRegister)
	target.RegisterHandler(opcode.AcademyMatchingChangeRequest, h.academyMatchingRegister)
	target.RegisterHandler(opcode.AcademyMatchingDeleteRequest, h.academyMatchingDelete)
	target.RegisterHandler(opcode.AcademyMatchingListRequest, h.academyMatchingList)
	target.RegisterHandler(opcode.AcademyMatchingJoinRequest, h.academyMatchingJoin)
	target.RegisterHandler(opcode.JobJoinRequest, h.jobJoin)
	target.RegisterHandler(opcode.JobLeaveRequest, h.jobLeave)
	target.RegisterHandler(opcode.JobAliasRequest, h.jobAlias)
	target.RegisterHandler(opcode.JobRankingRequest, h.jobRanking)
	target.RegisterHandler(opcode.JobOutcomeRequest, h.jobOutcome)
	target.RegisterHandler(opcode.JobPreviousInfoRequest, h.jobPrevious)
	return h
}

func (h *Handler) sendPacket(session *server.Session, packet network.Packet) error {
	if err := packet.WriteError(); err != nil {
		return err
	}
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}

func socialCharacter(session *server.Session) (int, error) {
	if session == nil || session.UserContext.CharacterID <= 0 || session.UserContext.UniqueID == 0 {
		return 0, model.ErrSocialInvalidRequest
	}
	return session.UserContext.CharacterID, nil
}

func operationPacket(messageID uint16, err error) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	if err == nil {
		p.WriteByte(1)
		return p
	}
	p.WriteByte(2)
	p.WriteUInt16(socialErrorCode(err))
	return p
}

func socialErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrSocialInsufficientLevel):
		return 0x4A01
	case errors.Is(err, model.ErrSocialInsufficientGold):
		return 0x4A02
	case errors.Is(err, model.ErrSocialAccessDenied):
		return 0x4A03
	case errors.Is(err, model.ErrSocialCapacity):
		return 0x4A04
	case errors.Is(err, model.ErrSocialConflict):
		return 0x4A05
	case errors.Is(err, model.ErrSocialNotFound):
		return 0x4A06
	default:
		return 0x4AFF
	}
}

func (h *Handler) guildCreate(ctx context.Context, session *server.Session, packet network.Packet) error {
	characterID, err := socialCharacter(session)
	if err != nil || packet.Remaining() < 6 {
		return fmt.Errorf("invalid guild create request")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	name, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild name")
	}
	guild, gold, err := h.repo.CreateGuild(ctx, characterID, name)
	if err != nil {
		return h.sendPacket(session, operationPacket(opcode.GuildCreateResponse, err))
	}
	response := operationPacket(opcode.GuildCreateResponse, nil)
	writeGuild(&response, guild)
	if err = h.sendPacket(session, response); err != nil {
		return err
	}
	h.commitGold(characterID, gold)
	return h.publishGuildSnapshot(guild)
}

func (h *Handler) guildDisband(ctx context.Context, session *server.Session, packet network.Packet) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild disband request")
	}
	before, _ := h.repo.Guild(ctx, characterID)
	err = h.repo.DisbandGuild(ctx, characterID)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildDisbandResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemoval(before)
	}
	return nil
}

func (h *Handler) guildLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild leave request")
	}
	before, _ := h.repo.Guild(ctx, characterID)
	err = h.repo.LeaveGuild(ctx, characterID)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemovalFor(before, characterID)
	}
	return nil
}

func (h *Handler) guildInvite(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild invite request")
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	target, err := h.world.GetPlayerByUniqueId(uid)
	if err != nil || target == nil {
		return h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, model.ErrSocialNotFound))
	}
	err = h.repo.InviteGuild(ctx, actor, target.ID)
	if err == nil {
		// Invitation type 5 is the stock guild invitation discriminator. The
		// durable invitation can be accepted by the generic invitation flow.
		invite := network.EmptyPacket()
		invite.MessageID = opcode.PlayerInvitationRequest
		invite.WriteByte(5)
		invite.WriteUInt32(session.UserContext.UniqueID)
		if sendErr := h.sendPacket(target.Session, invite); sendErr != nil {
			return sendErr
		}
	}
	return h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, err))
}

// AnswerGuildInvitation plugs into the generic 0x3080 invitation dispatcher.
// Party invitations retain priority; this fallback consumes the response only
// when a durable, unexpired guild invitation exists for the character.
func (h *Handler) AnswerGuildInvitation(ctx context.Context, session *server.Session, accept bool) (bool, error) {
	characterID, err := socialCharacter(session)
	if err != nil {
		return false, err
	}
	guild, err := h.repo.AnswerGuildInvite(ctx, characterID, accept)
	if errors.Is(err, model.ErrSocialNotFound) {
		union, unionErr := h.repo.AnswerGuildUnionInvite(ctx, characterID, accept)
		if errors.Is(unionErr, model.ErrSocialNotFound) {
			return false, nil
		}
		if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, unionErr)); sendErr != nil {
			return true, sendErr
		}
		if unionErr == nil && accept {
			return true, h.publishUnion(ctx, characterID, union)
		}
		return true, nil
	}
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildInviteResponse, err)); sendErr != nil {
		return true, sendErr
	}
	if err == nil && accept {
		return true, h.publishGuildSnapshot(guild)
	}
	return true, nil
}

func (h *Handler) guildKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild kick request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Guild(ctx, actor)
	err = h.repo.KickGuildMember(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildRemovalFor(before, int(target))
	}
	return nil
}

func (h *Handler) guildNotice(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	body, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild notice request")
	}
	err = h.repo.UpdateGuildNotice(ctx, actor, title, body)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildNoticeResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			return h.publishGuildSnapshot(guild)
		}
	}
	return nil
}

func (h *Handler) guildPermissions(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	count, err := packet.ReadByte()
	if err != nil || count == 0 || count > 50 {
		return fmt.Errorf("invalid guild permission count")
	}
	updates := make(map[int]uint32, count)
	for i := byte(0); i < count; i++ {
		id, readErr := packet.ReadUInt32()
		if readErr != nil {
			return readErr
		}
		permissions, readErr := packet.ReadUInt32()
		if readErr != nil {
			return readErr
		}
		updates[int(id)] = permissions
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("guild permission trailing data")
	}
	err = h.repo.UpdateGuildPermissions(ctx, actor, updates)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildPermissionResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			return h.publishGuildSnapshot(guild)
		}
	}
	return nil
}

func (h *Handler) guildDonate(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild donation")
	}
	amount, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	total, err := h.repo.DonateGuildSP(ctx, actor, amount)
	response := operationPacket(opcode.GuildDonateResponse, err)
	if err == nil {
		response.WriteUInt32(amount)
		response.WriteUInt32(total)
	}
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			return h.publishGuildSnapshot(guild)
		}
	}
	return nil
}

func (h *Handler) guildPromote(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild promote request")
	}
	guild, gold, err := h.repo.PromoteGuild(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildPromoteResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
		return h.publishGuildSnapshot(guild)
	}
	return nil
}

func (h *Handler) guildTransfer(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 8 {
		if _, err = packet.ReadUInt32(); err != nil {
			return err
		}
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild transfer request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	guild, err := h.repo.TransferGuild(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildTransferResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishGuildSnapshot(guild)
	}
	return nil
}

func (h *Handler) guildNickname(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	nickname, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild nickname request")
	}
	err = h.repo.UpdateGuildNickname(ctx, actor, int(target), nickname)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildNicknameResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		guild, loadErr := h.repo.Guild(ctx, actor)
		if loadErr == nil {
			return h.publishGuildSnapshot(guild)
		}
	}
	return nil
}

func (h *Handler) guildUnionInvite(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild union invite")
	}
	uid, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	target, err := h.world.GetPlayerByUniqueId(uid)
	if err != nil || target == nil {
		return h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, model.ErrSocialNotFound))
	}
	err = h.repo.InviteGuildUnion(ctx, actor, target.ID)
	if err == nil {
		invite := network.EmptyPacket()
		invite.MessageID = opcode.PlayerInvitationRequest
		invite.WriteByte(6)
		invite.WriteUInt32(session.UserContext.UniqueID)
		if sendErr := h.sendPacket(target.Session, invite); sendErr != nil {
			return sendErr
		}
	}
	return h.sendPacket(session, operationPacket(opcode.GuildUnionInviteResponse, err))
}

func (h *Handler) guildUnionLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid guild union leave")
	}
	recipients, _ := h.repo.SocialRecipients(ctx, actor, 11)
	err = h.repo.LeaveGuildUnion(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishUnionClear(recipients)
	}
	return nil
}

func (h *Handler) guildUnionKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid guild union kick")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	recipients, _ := h.repo.SocialRecipients(ctx, actor, 11)
	err = h.repo.KickGuildUnion(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.GuildUnionKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		_ = h.publishUnionClear(recipients)
		if union, loadErr := h.repo.GuildUnion(ctx, actor); loadErr == nil {
			return h.publishUnion(ctx, actor, union)
		}
	}
	return nil
}

func writeUnion(packet *network.Packet, union model.GuildUnionSnapshot) {
	packet.WriteUInt32(uint32(union.ID))
	packet.WriteUInt32(uint32(union.LeaderGuild))
	packet.WriteByte(byte(len(union.Guilds)))
	for _, guild := range union.Guilds {
		packet.WriteUInt32(uint32(guild.GuildID))
		packet.WriteString(guild.Name)
		packet.WriteByte(guild.Level)
		packet.WriteString(guild.MasterName)
		packet.WriteByte(guild.Members)
	}
}

func (h *Handler) publishUnion(ctx context.Context, actor int, union model.GuildUnionSnapshot) error {
	ids, err := h.repo.SocialRecipients(ctx, actor, 11)
	if err != nil {
		return err
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildUnionInfo
	writeUnion(&p, union)
	for _, id := range ids {
		player, lookupErr := h.world.GetPlayerByCharacterID(id)
		if lookupErr == nil && player != nil && player.Session != nil {
			if sendErr := h.sendPacket(player.Session, p); sendErr != nil {
				return sendErr
			}
		}
	}
	return nil
}

func (h *Handler) publishUnionClear(ids []int) error {
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildUnionInfo
	p.WriteUInt32(0)
	for _, id := range ids {
		player, err := h.world.GetPlayerByCharacterID(id)
		if err == nil && player != nil && player.Session != nil {
			if sendErr := h.sendPacket(player.Session, p); sendErr != nil {
				return sendErr
			}
		}
	}
	return nil
}

func writeGuild(packet *network.Packet, guild model.GuildSnapshot) {
	packet.WriteUInt32(uint32(guild.ID))
	packet.WriteString(guild.Name)
	packet.WriteByte(guild.Level)
	packet.WriteUInt32(guild.GatheredSP)
	packet.WriteString(guild.NoticeTitle)
	packet.WriteString(guild.NoticeBody)
	packet.WriteUInt32(guild.CrestRevision)
	packet.WriteByte(guild.MercenaryAttr)
	packet.WriteByte(byte(len(guild.Members)))
	for _, m := range guild.Members {
		packet.WriteUInt32(uint32(m.CharacterID))
		packet.WriteString(m.Name)
		packet.WriteByte(m.Class)
		packet.WriteByte(m.Level)
		packet.WriteUInt32(m.Donation)
		packet.WriteUInt32(m.Permissions)
		packet.WriteUInt32(m.Contribution)
		packet.WriteUInt32(0)
		packet.WriteUInt32(0)
		packet.WriteString(m.Nickname)
		packet.WriteUInt32(m.RefObjectID)
		packet.WriteByte(m.SiegeAuthority)
		if m.Online {
			packet.WriteByte(0)
		} else {
			packet.WriteByte(1)
		}
	}
}

func (h *Handler) publishGuildSnapshot(guild model.GuildSnapshot) error {
	for _, m := range guild.Members {
		player, err := h.world.GetPlayerByCharacterID(m.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		begin := network.EmptyPacket()
		begin.MessageID = opcode.GuildInfoBegin
		if err = h.sendPacket(player.Session, begin); err != nil {
			return err
		}
		data := network.EmptyPacket()
		data.MessageID = opcode.GuildInfoData
		writeGuild(&data, guild)
		if err = h.sendPacket(player.Session, data); err != nil {
			return err
		}
		end := network.EmptyPacket()
		end.MessageID = opcode.GuildInfoEnd
		if err = h.sendPacket(player.Session, end); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) publishGuildRemoval(guild model.GuildSnapshot) error {
	for _, m := range guild.Members {
		if err := h.sendGuildRemove(m.CharacterID); err != nil {
			return err
		}
	}
	return nil
}
func (h *Handler) publishGuildRemovalFor(guild model.GuildSnapshot, characterID int) error {
	if err := h.sendGuildRemove(characterID); err != nil {
		return err
	}
	if len(guild.Members) > 0 {
		return h.publishGuildSnapshotAfterRemoval(guild, characterID)
	}
	return nil
}
func (h *Handler) publishGuildSnapshotAfterRemoval(before model.GuildSnapshot, removed int) error {
	for _, m := range before.Members {
		if m.CharacterID == removed {
			continue
		}
		current, err := h.repo.Guild(context.Background(), m.CharacterID)
		if err == nil {
			return h.publishGuildSnapshot(current)
		}
	}
	return nil
}
func (h *Handler) sendGuildRemove(characterID int) error {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil || player.Session == nil {
		return nil
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GuildEntityRemove
	p.WriteUInt32(player.UniqueID)
	return h.sendPacket(player.Session, p)
}

func (h *Handler) academyCreate(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() == 4 {
		_, err = packet.ReadUInt32()
	}
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy create request")
	}
	academy, gold, err := h.repo.CreateAcademy(ctx, actor)
	response := operationPacket(opcode.AcademyCreateResponse, err)
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
		return h.publishAcademy(academy)
	}
	return nil
}
func (h *Handler) academyDisband(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy disband request")
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.DisbandAcademy(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyDisbandResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemoval(before)
	}
	return nil
}
func (h *Handler) academyLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy leave request")
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.LeaveAcademy(ctx, actor)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyLeaveResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, actor)
	}
	return nil
}
func (h *Handler) academyKick(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy kick request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.KickAcademyMember(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyKickResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, int(target))
	}
	return nil
}
func (h *Handler) academyComment(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	body, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy comment")
	}
	err = h.repo.UpdateAcademyComment(ctx, actor, title, body)
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyCommentResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		academy, loadErr := h.repo.Academy(ctx, actor)
		if loadErr == nil {
			return h.publishAcademy(academy)
		}
	}
	return nil
}

func (h *Handler) academyGrade(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy grade request")
	}
	target, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	before, _ := h.repo.Academy(ctx, actor)
	err = h.repo.GraduateAcademyMember(ctx, actor, int(target))
	if sendErr := h.sendPacket(session, operationPacket(opcode.AcademyGradeResponse, err)); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademyRemovalFor(before, int(target))
	}
	return nil
}

func (h *Handler) academyHonorRank(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	if packet.Remaining() > 1 {
		return fmt.Errorf("invalid academy honor rank request")
	}
	if packet.Remaining() == 1 {
		if _, err := packet.ReadByte(); err != nil {
			return err
		}
	}
	ranking, err := h.repo.AcademyHonorRanking(ctx, 20)
	response := operationPacket(opcode.AcademyHonorRankResponse, err)
	if err == nil {
		response.WriteByte(byte(len(ranking)))
		for i, m := range ranking {
			response.WriteByte(byte(i + 1))
			response.WriteString(m.LeaderName)
			response.WriteUInt32(m.GraduatedCount)
			response.WriteByte(m.HonorRank)
		}
	}
	return h.sendPacket(session, response)
}
func (h *Handler) academyMatchingRegister(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	country, err := packet.ReadByte()
	if err != nil {
		return err
	}
	title, err := packet.ReadString()
	if err != nil {
		return err
	}
	display, err := packet.ReadByte()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy matching registration")
	}
	err = h.repo.RegisterAcademyMatch(ctx, actor, title, country, display)
	responseID := opcode.AcademyMatchingRegisterResponse
	if packet.MessageID == opcode.AcademyMatchingChangeRequest {
		responseID = opcode.AcademyMatchingChangeResponse
	}
	return h.sendPacket(session, operationPacket(responseID, err))
}
func (h *Handler) academyMatchingDelete(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 0 {
		return fmt.Errorf("invalid academy matching delete")
	}
	err = h.repo.DeleteAcademyMatch(ctx, actor)
	return h.sendPacket(session, operationPacket(opcode.AcademyMatchingDeleteResponse, err))
}
func (h *Handler) academyMatchingList(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	if packet.Remaining() != 1 {
		return fmt.Errorf("invalid academy matching list")
	}
	page, err := packet.ReadByte()
	if err != nil {
		return err
	}
	matches, pages, err := h.repo.ListAcademyMatches(ctx, page, academyPageSize)
	response := buildAcademyMatchingListResponse(matches, pages, page, err, h.world)
	return h.sendPacket(session, response)
}

// buildAcademyMatchingListResponse follows the recovered 0xB47D entry shape.
// LeaderCharacter is a durable CHAR.id; the wire field is the live runtime
// UID and is therefore resolved only at serialization time.
func buildAcademyMatchingListResponse(matches []model.AcademyMatch, pages int, page byte, err error, world worldLookup) network.Packet {
	response := operationPacket(opcode.AcademyMatchingListResponse, err)
	if err != nil {
		return response
	}
	response.WriteByte(byte(pages))
	response.WriteByte(page)
	response.WriteByte(byte(len(matches)))
	for _, m := range matches {
		response.WriteUInt32(uint32(m.AcademyID))
		response.WriteUInt32(0)
		response.WriteByte(m.CountryType)
		response.WriteString(m.Title)
		response.WriteUInt32(0)
		response.WriteUInt32(0)
		response.WriteByte(m.DisplayLevel)
		response.WriteByte(m.LeaderLevel)
		leaderUID := uint32(0)
		if world != nil && m.LeaderCharacter > 0 {
			if leader, lookupErr := world.GetPlayerByCharacterID(m.LeaderCharacter); lookupErr == nil && leader != nil {
				leaderUID = leader.GetUniqueID()
			}
		}
		response.WriteUInt32(leaderUID)
		response.WriteString(m.LeaderName)
		response.WriteUInt32(m.GraduatedCount)
		response.WriteByte(m.HonorRank)
		response.WriteUInt64(0)
	}
	return response
}
func (h *Handler) academyMatchingJoin(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid academy matching join")
	}
	academyID, err := packet.ReadUInt32()
	if err != nil {
		return err
	}
	academy, err := h.repo.JoinAcademy(ctx, actor, int(academyID))
	response := operationPacket(opcode.AcademyMatchingJoinResponse, err)
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		return h.publishAcademy(academy)
	}
	return nil
}
func (h *Handler) publishAcademy(a model.AcademySnapshot) error {
	// The vSRO 1.188 client does not accept the former snapshot encoding for
	// 0x3C81. Its handler reads a different record shape and deliberately
	// raises an access violation when that contract is violated. Keep the
	// persistent Academy operations available, but do not publish an
	// unverified wire representation to connected clients.
	_ = a
	return nil
}
func (h *Handler) publishAcademyRemoval(a model.AcademySnapshot) error {
	_ = a
	return nil
}
func (h *Handler) publishAcademyRemovalFor(a model.AcademySnapshot, id int) error {
	_, _ = a, id
	return nil
}

func (h *Handler) jobJoin(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 5 {
		return fmt.Errorf("invalid job join")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	jobType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	job, gold, err := h.repo.JoinJob(ctx, actor, jobType)
	response := operationPacket(opcode.JobJoinResponse, err)
	if err == nil {
		response.WriteByte(job.Type)
		response.WriteByte(job.Level)
		response.WriteUInt32(uint32(min(job.Experience, math.MaxUint32)))
	}
	if sendErr := h.sendPacket(session, response); sendErr != nil {
		return sendErr
	}
	if err == nil {
		h.commitGold(actor, gold)
	}
	return nil
}
func (h *Handler) jobLeave(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid job leave")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	err = h.repo.LeaveJob(ctx, actor)
	return h.sendPacket(session, operationPacket(opcode.JobLeaveResponse, err))
}
func (h *Handler) jobAlias(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() < 7 {
		return fmt.Errorf("invalid job alias")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	action, err := packet.ReadByte()
	if err != nil {
		return err
	}
	alias, err := packet.ReadString()
	if err != nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid job alias")
	}
	response := network.EmptyPacket()
	response.MessageID = opcode.JobAliasResponse
	switch action {
	case 0:
		available, checkErr := h.repo.JobAliasAvailable(ctx, alias)
		if checkErr != nil {
			response = operationPacket(opcode.JobAliasResponse, checkErr)
		} else {
			response.WriteByte(1)
			if available {
				response.WriteByte(1)
			} else {
				response.WriteByte(2)
			}
			response.WriteString(alias)
		}
	case 1:
		err = h.repo.SetJobAlias(ctx, actor, alias)
		response = operationPacket(opcode.JobAliasResponse, err)
		if err == nil {
			response.WriteString(alias)
		}
	default:
		return fmt.Errorf("unknown job alias action")
	}
	return h.sendPacket(session, response)
}
func (h *Handler) jobRanking(ctx context.Context, session *server.Session, packet network.Packet) error {
	if _, err := socialCharacter(session); err != nil {
		return err
	}
	if packet.Remaining() != 6 {
		return fmt.Errorf("invalid job ranking")
	}
	if _, err := packet.ReadUInt32(); err != nil {
		return err
	}
	jobType, err := packet.ReadByte()
	if err != nil {
		return err
	}
	kind, err := packet.ReadByte()
	if err != nil || kind > 1 {
		return fmt.Errorf("invalid job ranking kind")
	}
	ranking, err := h.repo.JobRanking(ctx, jobType, kind == 1, 20)
	response := operationPacket(opcode.JobRankingResponse, err)
	if err == nil {
		response.WriteByte(jobType)
		response.WriteByte(kind)
		response.WriteByte(byte(len(ranking)))
		for i, j := range ranking {
			response.WriteByte(byte(i + 1))
			response.WriteString(j.Alias)
			response.WriteByte(j.Level)
			response.WriteUInt32(uint32(min(j.Experience, math.MaxUint32)))
		}
	}
	return h.sendPacket(session, response)
}
func (h *Handler) jobOutcome(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 5 {
		return fmt.Errorf("invalid job outcome")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	if _, err = packet.ReadByte(); err != nil {
		return err
	}
	amount, gold, err := h.repo.ClaimJobReward(ctx, actor)
	response := operationPacket(opcode.JobOutcomeResponse, err)
	if err == nil {
		response.WriteUInt64(amount)
		h.commitGold(actor, gold)
	}
	return h.sendPacket(session, response)
}
func (h *Handler) jobPrevious(ctx context.Context, session *server.Session, packet network.Packet) error {
	actor, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if packet.Remaining() != 4 {
		return fmt.Errorf("invalid previous job info")
	}
	if _, err = packet.ReadUInt32(); err != nil {
		return err
	}
	job, err := h.repo.Job(ctx, actor)
	response := operationPacket(opcode.JobPreviousInfoResponse, err)
	if err == nil {
		response.WriteByte(job.Type)
		response.WriteByte(job.Level)
		response.WriteUInt32(uint32(min(job.Experience, math.MaxUint32)))
		response.WriteString(job.Alias)
	}
	return h.sendPacket(session, response)
}
func (h *Handler) commitGold(characterID int, gold uint64) {
	player, err := h.world.GetPlayerByCharacterID(characterID)
	if err != nil || player == nil {
		return
	}
	player.Mutex.Lock()
	player.Gold = gold
	player.Mutex.Unlock()
}

// Bootstrap sends the persistent social snapshots after the stock GameReady
// transition, when ordinary in-world packets are legal on the session.
func (h *Handler) Bootstrap(ctx context.Context, session *server.Session) error {
	characterID, err := socialCharacter(session)
	if err != nil {
		return err
	}
	if guild, loadErr := h.repo.Guild(ctx, characterID); loadErr == nil {
		if err = h.publishGuildSnapshot(guild); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if union, loadErr := h.repo.GuildUnion(ctx, characterID); loadErr == nil {
		if err = h.publishUnion(ctx, characterID, union); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if academy, loadErr := h.repo.Academy(ctx, characterID); loadErr == nil {
		if err = h.publishAcademy(academy); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	if job, loadErr := h.repo.Job(ctx, characterID); loadErr == nil {
		p := network.EmptyPacket()
		p.MessageID = opcode.JobUpdateExperience
		p.WriteByte(job.Type)
		p.WriteByte(job.Level)
		p.WriteUInt32(uint32(min(job.Experience, math.MaxUint32)))
		return h.sendPacket(session, p)
	} else if !errors.Is(loadErr, model.ErrSocialNotFound) {
		return loadErr
	}
	return nil
}
