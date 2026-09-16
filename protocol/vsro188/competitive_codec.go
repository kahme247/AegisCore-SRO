package vsro188

import (
	"fmt"
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	FRPVPModeNone   byte = 0
	FRPVPModeRed    byte = 1
	FRPVPModeBlack  byte = 2
	FRPVPModeBlue   byte = 3
	FRPVPModeWhite  byte = 4
	FRPVPModeYellow byte = 5

	FlagWarBeginIn10       byte   = 2
	FlagWarBegin           byte   = 4
	FlagWarEnded           byte   = 9
	FlagWarOperationResult byte   = 0xff
	FlagWarUnavailable     uint16 = 1

	BattleArenaUpdateUnavailable byte = 4
)

type BattleArenaRequest struct {
	Register  bool
	GameType  byte
	MatchType byte
}

type BattleArenaRank struct {
	Team   byte
	Name   string
	Points uint32
}

type BattleArenaOperation struct {
	Operation    byte
	Update       byte
	MatchType    byte
	GameTypeMask uint16
	First        byte
	Second       byte
	MaxTime      uint32
	Result       byte
	CoinCount    byte
	SkillExp     uint32
	RequestType  byte
	GameType     byte
	Points       uint32
	RedPoints    uint32
	BluePoints   uint32
	Ranks        []BattleArenaRank
	Team         byte
	Name         string
	FirstValue   uint32
	SecondValue  uint32
	ElapsedTime  uint32
}

func DecodeFRPVPUpdateRequest(packet *network.Packet) (byte, error) {
	if packet == nil || packet.Remaining() != 1 {
		return 0, fmt.Errorf("invalid free PVP update request")
	}
	mode, err := packet.ReadByte()
	if err != nil || mode > FRPVPModeYellow {
		return 0, fmt.Errorf("invalid free PVP mode")
	}
	return mode, nil
}

func BuildFRPVPUpdateSuccess(uniqueID uint32, mode byte) (network.Packet, error) {
	if mode > FRPVPModeYellow {
		return network.Packet{}, fmt.Errorf("invalid free PVP mode")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.FRPVPUpdateResponse
	p.WriteByte(1)
	p.WriteUInt32(uniqueID)
	p.WriteByte(mode)
	return p, nil
}

func BuildFRPVPUpdateFailure(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.FRPVPUpdateResponse
	p.WriteByte(2)
	p.WriteUInt16(errorCode)
	return p
}

func DecodeFlagWarRegisterRequest(packet *network.Packet) error {
	if packet == nil || packet.Remaining() != 0 {
		return fmt.Errorf("invalid Flag War register request")
	}
	return nil
}

func BuildFlagWarUpdate(updateType byte) (network.Packet, error) {
	if updateType != FlagWarBeginIn10 && updateType != FlagWarBegin && updateType != FlagWarEnded {
		return network.Packet{}, fmt.Errorf("invalid Flag War update type")
	}
	p := network.EmptyPacket()
	p.MessageID = opcode.FlagWarUpdate
	p.WriteByte(updateType)
	return p, nil
}

func BuildFlagWarOperationResult(errorCode uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.FlagWarUpdate
	p.WriteByte(FlagWarOperationResult)
	p.WriteUInt16(errorCode)
	return p
}

func BuildFlagWarUnavailable() network.Packet {
	return BuildFlagWarOperationResult(FlagWarUnavailable)
}

func DecodeBattleArenaRequest(packet *network.Packet) (BattleArenaRequest, error) {
	if packet == nil || packet.Remaining() < 1 {
		return BattleArenaRequest{}, fmt.Errorf("invalid Battle Arena request")
	}
	action, err := packet.ReadByte()
	if err != nil {
		return BattleArenaRequest{}, err
	}
	switch action {
	case 1:
		if packet.Remaining() != 2 {
			return BattleArenaRequest{}, fmt.Errorf("invalid Battle Arena registration")
		}
		gameType, err := packet.ReadByte()
		if err != nil {
			return BattleArenaRequest{}, err
		}
		matchType, err := packet.ReadByte()
		return BattleArenaRequest{Register: true, GameType: gameType, MatchType: matchType}, err
	case 2:
		if packet.Remaining() != 0 {
			return BattleArenaRequest{}, fmt.Errorf("invalid Battle Arena cancellation")
		}
		return BattleArenaRequest{}, nil
	default:
		return BattleArenaRequest{}, fmt.Errorf("invalid Battle Arena action")
	}
}

// BuildBattleArenaOperation mirrors sro_client.exe On34d2 at 0x0089f860.
func BuildBattleArenaOperation(operation BattleArenaOperation) (network.Packet, error) {
	p := network.EmptyPacket()
	p.MessageID = opcode.BattleArenaOperation
	p.WriteByte(operation.Operation)
	switch operation.Operation {
	case 0, 1, 2, 3, 0x0d, 0x0e, 0x0f:
		p.WriteByte(operation.MatchType)
		p.WriteUInt16(operation.GameTypeMask)
	case 4:
		p.WriteByte(operation.First)
		p.WriteByte(operation.Second)
	case 5, 6, 7, 0x0b, 0x0c, 0x10:
		p.WriteByte(operation.MatchType)
	case 8:
		p.WriteUInt32(operation.MaxTime)
	case 9:
		p.WriteByte(operation.MatchType)
		p.WriteByte(operation.Result)
		p.WriteByte(operation.CoinCount)
		p.WriteUInt32(operation.SkillExp)
	case 0xff:
		if err := writeBattleArenaUpdate(&p, operation); err != nil {
			return network.Packet{}, err
		}
	default:
		return network.Packet{}, fmt.Errorf("invalid Battle Arena operation")
	}
	return p, nil
}

func BuildBattleArenaUnavailable() (network.Packet, error) {
	return BuildBattleArenaOperation(BattleArenaOperation{
		Operation: 0xff,
		Update:    BattleArenaUpdateUnavailable,
	})
}

func writeBattleArenaUpdate(p *network.Packet, operation BattleArenaOperation) error {
	p.WriteByte(operation.Update)
	switch operation.Update {
	case 0:
		p.WriteByte(operation.RequestType)
		p.WriteByte(operation.GameType)
	case 2, 3, 4, 5, 6, 8, 9, 0x0b, 0x0c, 0x0d, 0x81, 0x82, 0x83, 0x85, 0x86, 0x87, 0x89, 0xf1, 0xf2:
	case 0x40:
		p.WriteUInt32(operation.Points)
	case 0x41:
		if len(operation.Ranks) > math.MaxUint8 {
			return fmt.Errorf("too many Battle Arena ranks")
		}
		p.WriteUInt32(operation.RedPoints)
		p.WriteUInt32(operation.BluePoints)
		p.WriteByte(byte(len(operation.Ranks)))
		for _, rank := range operation.Ranks {
			p.WriteByte(rank.Team)
			if err := p.WriteString(rank.Name); err != nil {
				return err
			}
			p.WriteUInt32(rank.Points)
		}
	case 0x80:
		return p.WriteString(operation.Name)
	case 0x84:
		p.WriteByte(operation.Team)
		return p.WriteString(operation.Name)
	case 0x88:
		p.WriteUInt32(operation.FirstValue)
		p.WriteUInt32(operation.SecondValue)
	case 0xf0:
		p.WriteUInt32(operation.MaxTime)
		p.WriteUInt32(operation.ElapsedTime)
		p.WriteByte(operation.First)
		p.WriteByte(operation.Second)
	default:
		return fmt.Errorf("invalid Battle Arena update")
	}
	return nil
}
