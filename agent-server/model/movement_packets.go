package model

import (
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func BuildMovementUpdatePacket(character ICharacter, movement MovementData, startPosition navmeshv2.RtNavmeshPosition) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityMovementResponse
	packet.WriteUInt32(character.GetUniqueID())
	packet.WriteBool(movement.HasDestination)
	if movement.HasDestination {
		packet.WriteUInt16(uint16(movement.TargetPosition.Region.ID))
		packet.WriteUInt16(uint16(int16(movement.TargetPosition.Offset.X)))
		packet.WriteUInt16(uint16(movement.TargetPosition.Offset.Y))
		packet.WriteUInt16(uint16(int16(movement.TargetPosition.Offset.Z)))
		packet.WriteByte(1)
		packet.WriteUInt16(uint16(startPosition.Region.ID))
		packet.WriteUInt16(uint16(startPosition.Offset.X) * 10)
		packet.WriteFloat32(startPosition.Offset.Y)
		packet.WriteUInt16(uint16(startPosition.Offset.Z) * 10)
	} else {
		packet.WriteByte(1)
		packet.WriteUInt16(uint16(movement.DirectionAngle))
		// The stock B021 parser reads one control byte after every movement
		// structure. Destination movement uses 1 and follows it with the source
		// position above; directional movement must terminate that branch with 0.
		packet.WriteByte(0)
	}
	return packet
}

func BuildPositionUpdatePacket(character ICharacter) network.Packet {
	position := character.GetNavmeshPosition()
	packet := network.EmptyPacket()
	packet.MessageID = opcode.MovementPositionUpdate
	packet.WriteUInt32(character.GetUniqueID())
	packet.WriteUInt16(uint16(position.Region.ID))
	packet.WriteFloat32(position.Offset.X)
	packet.WriteFloat32(position.Offset.Y)
	packet.WriteFloat32(position.Offset.Z)
	packet.WriteUInt16(uint16(position.Heading))
	return packet
}
