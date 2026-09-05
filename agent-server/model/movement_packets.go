package model

import (
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func BuildMovementUpdatePacket(character ICharacter, movement MovementData, startPosition navmeshv2.RtNavmeshPosition) network.Packet {
	if movement.HasDestination {
		packet, _ := (vsro188.EntityDestinationMovementResponseCodec{}).Encode(vsro188.EntityDestinationMovementResponse{
			EntityUniqueID: character.GetUniqueID(),
			TargetRegion:   uint16(movement.TargetPosition.Region.ID), TargetX: int16(movement.TargetPosition.Offset.X),
			TargetY: int16(movement.TargetPosition.Offset.Y), TargetZ: int16(movement.TargetPosition.Offset.Z),
		})
		return packet
	}
	packet, _ := (vsro188.EntityDirectionalMovementResponseCodec{}).Encode(vsro188.EntityDirectionalMovementResponse{
		EntityUniqueID: character.GetUniqueID(), Source: 1, Heading: uint16(movement.DirectionAngle),
	})
	return packet
}

func BuildPositionUpdatePacket(character ICharacter) network.Packet {
	position := character.GetNavmeshPosition()
	packet, _ := (vsro188.EntityPositionUpdateCodec{}).Encode(vsro188.EntityPositionUpdate{
		EntityUniqueID: character.GetUniqueID(), Region: uint16(position.Region.ID),
		X: position.Offset.X, Y: position.Offset.Y, Z: position.Offset.Z, Heading: uint16(position.Heading),
	})
	return packet
}

func BuildTurnUpdatePacket(character ICharacter) network.Packet {
	packet, _ := (vsro188.EntityTurnResponseCodec{}).Encode(vsro188.EntityTurnResponse{
		EntityUniqueID: character.GetUniqueID(),
		Heading:        uint16(character.GetNavmeshPosition().Heading),
	})
	return packet
}
