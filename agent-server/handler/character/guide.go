package character

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type GuideFlag uint64

const (
	None         GuideFlag = 0
	Welcome      GuideFlag = 1
	Minimap      GuideFlag = 2
	WearArmor    GuideFlag = 4
	MonIdentify  GuideFlag = 8
	HowToFight   GuideFlag = 16
	GetItem      GuideFlag = 32
	RecoveryLife GuideFlag = 64
	LevelUp      GuideFlag = 128
	HwanMode     GuideFlag = 256
	HowToParty   GuideFlag = 512
	Condition    GuideFlag = 1024
	HowToPK      GuideFlag = 2048
	JobChoice    GuideFlag = 4096
	Trader       GuideFlag = 8192
	Hunter       GuideFlag = 16384
	Bandit       GuideFlag = 32768
	MovingStep   GuideFlag = 65536
	CarryStep    GuideFlag = 131072
	Criminal     GuideFlag = 262144  //unverified
	Fine         GuideFlag = 524288  //unverified
	Quest        GuideFlag = 1048576 //unverified
	Apprentice   GuideFlag = 2097152
	StreetStall  GuideFlag = 4194304
	GetSkill     GuideFlag = 8388608
	BuyItem      GuideFlag = 16777216
	Action       GuideFlag = 33554432
	Academy      GuideFlag = 67108864
	OpenMarket   GuideFlag = 134217728
)

func RegisterGuideHandler(target *server.Server) {
	target.RegisterHandler(opcode.GuideRequest, handleGuideRequest)
}

func handleGuideRequest(_ context.Context, _ *server.Session, packet network.Packet) error {
	if packet.Remaining() != 8 {
		return fmt.Errorf("guide request payload length %d, want 8", packet.Remaining())
	}
	_, err := packet.ReadUInt64()
	if err != nil {
		return fmt.Errorf("read guide flag: %w", err)
	}

	// The previous TODO response guessed a success byte followed by the request
	// flag. Live vSRO 1.88 clients disconnect immediately after that nine-byte
	// B0EA, commonly when selecting NPCs that trigger contextual guide help.
	// Consume the valid request without emitting an unverified wire layout.
	return nil
}
