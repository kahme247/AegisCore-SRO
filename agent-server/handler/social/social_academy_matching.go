package social

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

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
	packet := buildAcademyInfoPacket(a, h.world)
	for _, member := range a.Members {
		player, err := h.world.GetPlayerByCharacterID(member.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		if err = h.sendPacket(player.Session, packet); err != nil {
			return err
		}
		buffs, loadErr := h.repo.AcademyBuffs(context.Background(), member.CharacterID, time.Now().UTC())
		if loadErr != nil {
			return loadErr
		}
		for _, buff := range buffs {
			if err = h.sendPacket(player.Session, buildAcademyBuffPacket(buff, time.Now().UTC())); err != nil {
				return err
			}
		}
	}
	return nil
}

// PublishAcademyBuffs sends committed mentor/apprentice accumulation state to
// whichever affected characters are online. Offline state remains durable.
func (h *Handler) PublishAcademyBuffs(states []model.AcademyBuffState) {
	now := time.Now().UTC()
	for _, state := range states {
		player, err := h.world.GetPlayerByCharacterID(state.CharacterID)
		if err != nil || player == nil || player.Session == nil {
			continue
		}
		_ = h.sendPacket(player.Session, buildAcademyBuffPacket(state, now))
	}
}

func buildAcademyInfoPacket(academy model.AcademySnapshot, world worldLookup) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AcademyInfo
	packet.WriteUInt32(uint32(academy.ID))
	packet.WriteUInt32(uint32(academy.LeaderCharacter))
	packet.WriteUInt32(academy.GraduatedCount)
	packet.WriteUInt32(academy.HonorPoints)
	packet.WriteUInt32(uint32(academy.HonorRank))
	packet.WriteByte(academy.HonorRank)
	packet.WriteString(academy.CommentTitle)
	packet.WriteString(academy.CommentBody)
	members := academy.Members
	if len(members) > 8 {
		members = members[:8]
	}
	packet.WriteByte(byte(len(members)))
	for _, member := range members {
		runtimeUID := uint32(0)
		if world != nil {
			if player, err := world.GetPlayerByCharacterID(member.CharacterID); err == nil && player != nil {
				runtimeUID = player.GetUniqueID()
			}
		}
		packet.WriteUInt32(uint32(member.CharacterID))
		packet.WriteUInt32(member.RefObjectID)
		packet.WriteUInt32(runtimeUID)
		packet.WriteString(member.Name)
		packet.WriteByte(member.Class)
		packet.WriteByte(member.Level)
		for range 4 {
			packet.WriteUInt32(0)
		}
		if member.Online {
			packet.WriteByte(1)
		} else {
			packet.WriteByte(0)
		}
		packet.WriteByte(0)
		packet.WriteUInt32(uint32(min(member.AccumulatedExperience, math.MaxUint32)))
		packet.WriteByte(0)
		packet.WriteUInt32(uint32(member.MentorID))
		for range 4 {
			packet.WriteUInt16(0)
		}
		packet.WriteString(member.MentorName)
	}
	return packet
}

func buildAcademyBuffPacket(buff model.AcademyBuffState, now time.Time) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.AcademyBuffUpdate
	packet.WriteByte(buff.Type)
	remaining := uint32(0)
	if buff.EndsAt.After(now) {
		minutes := (buff.EndsAt.Sub(now) + time.Minute - 1) / time.Minute
		remaining = uint32(min(minutes, time.Duration(math.MaxUint32)))
	}
	packet.WriteUInt32(remaining)
	packet.WriteUInt32(uint32(min(buff.Value, uint64(math.MaxUint32))))
	return packet
}
