package vsro188

import (
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/utils"
)

type CelestialUpdate struct {
	MoonPhase uint16
	Hour      byte
	Minute    byte
}

const (
	CharacterMoveStateWalk byte = 2
	CharacterMoveStateRun  byte = 3
	CharacterMoveStateSit  byte = 4
)

func DecodeCharacterMoveStateRequest(packet *network.Packet) (byte, error) {
	if packet == nil || packet.Remaining() != 1 {
		return 0, fmt.Errorf("invalid character move-state request")
	}
	state, err := packet.ReadByte()
	if err != nil || state < CharacterMoveStateWalk || state > CharacterMoveStateSit {
		return 0, fmt.Errorf("invalid character move state")
	}
	return state, nil
}

func BuildCharacterStateUpdate(state byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CharacterStateUpdate
	p.WriteByte(state)
	return p
}

func BuildEnvironmentCelestialUpdate(update CelestialUpdate) (network.Packet, error) {
	if update.MoonPhase > 30 || update.Hour > 23 || update.Minute > 59 {
		return network.Packet{}, fmt.Errorf("invalid celestial update")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.EnvironmentCelestialUpdate
	p.WriteUInt16(update.MoonPhase)
	p.WriteByte(update.Hour)
	p.WriteByte(update.Minute)
	return p, nil
}

func BuildClientReset() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ClientReset
	return p
}

func BuildGameServerTime(now time.Time) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.GameServerTime
	p.WriteUInt32(utils.ToSilkroadTime(now))
	return p
}

// BuildGameNotification preserves the 300C outer envelope recovered at
// sro_client.exe 0x00874EA0. Payload validation belongs to the individual
// notification-code producer because each code has a distinct layout.
func BuildGameNotification(code uint16, payload []byte) (network.Packet, error) {
	if code == 0 {
		return network.Packet{}, fmt.Errorf("invalid game notification code")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.GameNotify
	p.WriteUInt16(code)
	if err := p.WriteBytes(payload); err != nil {
		return network.Packet{}, err
	}
	return p, nil
}

func BuildCharacterMoveStateSuccess() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CharacterUpdateMoveStateResponse
	p.WriteByte(1)
	return p
}

func BuildCharacterMoveStateFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.CharacterUpdateMoveStateResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}
