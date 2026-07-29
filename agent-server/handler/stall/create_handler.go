package stall

import (
	"context"
	"errors"

	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
	"github.com/ferdoran/go-sro/protocol/vsro188"
	log "github.com/sirupsen/logrus"
)

const (
	StallAvatarDefault        = 0
	StallAvatarMangyang       = 3847
	StallAvatarBigEyeGhost    = 3848
	StallAvatarEarthGhost     = 3849
	StallAvatarSpecialMonster = 3850
)

func RegisterStallCreateHandler(target *server.Server) {
	target.RegisterHandler(opcode.StallCreateRequest, handleStallCreate)
}

func handleStallCreate(_ context.Context, session *server.Session, packet network.Packet) error {
	stallName, err := packet.ReadString()
	if err != nil {
		return err
	}

	if packet.Remaining() != 0 {
		return errors.New("stall create request has trailing data")
	}
	if err := vsro188.ValidateText(stallName, vsro188.TextRules{Field: "stall name", RequireTrimmed: true}); err != nil {
		return err
	}
	log.Println(stallName)
	p := network.EmptyPacket()
	p.MessageID = opcode.StallCreateResponse
	p.WriteByte(1)
	if err := session.Send(p); err != nil {
		return err
	}

	p1 := network.EmptyPacket()
	p1.MessageID = opcode.StallEntityCreateResponse
	p1.WriteUInt32(session.UserContext.UniqueID)
	p1.WriteString(stallName)

	// TODO: Check which stall avatar is activated
	p1.WriteUInt32(StallAvatarDefault)
	return session.Send(p1)
}
