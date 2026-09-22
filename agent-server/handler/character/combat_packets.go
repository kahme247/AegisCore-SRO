package character

import (
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func buildActionStateResponse(active bool) network.Packet {
	packet, _ := (vsro188.CombatActionStateResponseCodec{}).Encode(vsro188.CombatActionStateResponse{Active: active})
	return packet
}

func buildSkillCastErrorResponse(reason byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntitySkillCastBegin
	p.WriteByte(2)
	p.WriteByte(reason)
	p.WriteByte(skillCastErrorCategory)
	return p
}

func buildAttackBegin(sourceUID, targetUID, castUID, damage uint32, killed bool) network.Packet {
	return model.BuildAttackBegin(sourceUID, targetUID, castUID, damage, killed)
}

func buildExperienceUpdate(sourceUID uint32, gained, gainedSkillExperience uint64, progress model.ProgressionResult) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.EntityUpdateExperience
	p.WriteUInt32(sourceUID)
	p.WriteUInt64(gained)
	p.WriteUInt64(gainedSkillExperience)
	p.WriteByte(0)
	if progress.LevelsGained > 0 {
		p.WriteUInt16(uint16(progress.StatPointsGained))
	}
	return p
}

func buildServerInventoryItemAdded(slot byte, _ model.Item) network.Packet {
	// The stock 0x0E success branch is only two scalar bytes after the
	// discriminator. The second value is preserved from the recovered server
	// path without assigning it an unresolved business name.
	return vsro188.BuildItemOperationTwoByteSuccess(vsro188.ItemOperationServerAdd, slot, 0x3C)
}
