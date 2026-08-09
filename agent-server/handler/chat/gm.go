package chat

import (
	"fmt"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"regexp"
	"strconv"
	"sync"
)

var adminCommandRegex = regexp.MustCompile("^\\.(?P<command>\\w+)\\s?(?P<args>.*)$")

type GmMessageHandler struct {
}

const (
	SetSpeed        string = "speed"
	CurrentPosition string = "curpos"
	JanganWest      string = "jgwest"
	Takla1          string = "takla1"
	Takla2          string = "takla2"
	LogLevel        string = "loglvl"
)

var gmMessageHandlerInstance *GmMessageHandler
var once sync.Once

func GetGmMessageHandlerInstance() *GmMessageHandler {
	once.Do(func() {
		gmMessageHandlerInstance = &GmMessageHandler{}
	})
	return gmMessageHandlerInstance
}

func (h *GmMessageHandler) HandleAdminMessage(request MessageRequest, session *server.Session) error {
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil {
		logrus.Warnf("rejected GM chat for unknown player uid=%d: %v", session.UserContext.UniqueID, err)
		return nil
	}
	if !player.IsGm {
		// Treat the GM chat channel as privileged in its entirety. In
		// particular, commands such as .speed and .loglvl must never be
		// reachable merely by choosing the AllGM chat type on the wire.
		logrus.Warnf("rejected GM chat from non-GM uid=%d", session.UserContext.UniqueID)
		return nil
	}
	if adminCommandRegex.MatchString(request.Message) {
		strComponents := adminCommandRegex.FindStringSubmatch(request.Message)
		command := strComponents[1]
		args := strComponents[2]
		logrus.Debugf("[GM] - gm command %v with args %v\n", command, args)
		switch command {
		case SetSpeed:
			newSpeed, err := strconv.ParseFloat(args, 32)
			if err != nil {
				// TODO send message back
				logrus.Errorf("failed to parse %v as int\n", args)
				return nil
			}
			if newSpeed < 0 {
				logrus.Warnf("speed cannot be negative")
				return nil
			}

			player.SetRunSpeed(float32(newSpeed))
			p := network.EmptyPacket()
			p.MessageID = opcode.EntityUpdateMovementSpeed
			p.WriteUInt32(player.UniqueID)
			p.WriteFloat32(player.GetWalkSpeed()) // WalkSpeed
			p.WriteFloat32(player.GetRunSpeed())
			if err := p.WriteError(); err != nil {
				return err
			}
			if err := player.Session.Send(p); err != nil {
				return err
			}

			//spawnEngine := spawn.GetSpawnEngineInstance()
			//spawnEngine.UpdatedMovementSpeed(player)

			logrus.Infof("[GM] - updated %s's movement speed to %f\n", player.CharName, player.GetRunSpeed())
		case LogLevel:
			switch args {
			case "debug":
				logrus.SetLevel(logrus.DebugLevel)
			case "trace":
				logrus.SetLevel(logrus.TraceLevel)
			case "info":
				logrus.SetLevel(logrus.InfoLevel)
			}
		case CurrentPosition:
			p := network.EmptyPacket()
			p.MessageID = opcode.ChatUpdate
			p.WriteByte(PM)
			p.WriteString("System")
			p.WriteString(fmt.Sprintf("Current Position: ( %f | %f | %f )", player.GetNavmeshPosition().Offset.X, player.GetNavmeshPosition().Offset.Y, player.GetNavmeshPosition().Offset.Z))
			if err := p.WriteError(); err != nil {
				return err
			}
			if err := player.Session.Send(p); err != nil {
				return err
			}
		case JanganWest:
			warpPlayer(
				session.UserContext.UniqueID,
				435,
				0,
				1745,
				24999)
		case Takla1:
			warpPlayer(
				session.UserContext.UniqueID,
				1374.843750,
				-28.878524,
				937.109375,
				25991)
		case Takla2:
			warpPlayer(
				session.UserContext.UniqueID,
				939.031250,
				-522.488159,
				992.015625,
				26246)
		}
	} else {
		// TODO: Change all players to local region
		p := network.EmptyPacket()
		p.MessageID = opcode.ChatUpdate
		p.WriteByte(request.ChatType)
		p.WriteUInt32(session.UserContext.UniqueID)
		p.WriteString(request.Message)
		if err := p.WriteError(); err != nil {
			return err
		}
		world.Broadcast(p)

		p1 := network.EmptyPacket()
		p1.MessageID = opcode.ChatResponse
		p1.WriteByte(1)
		p1.WriteByte(request.ChatType)
		p1.WriteByte(request.ChatIndex)
		if err := p1.WriteError(); err != nil {
			return err
		}
		if err := session.Send(p1); err != nil {
			return err
		}
	}
	return nil
}

func warpPlayer(playerUniqueId uint32, x, y, z float32, regionId int16) {
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(playerUniqueId)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to warp player"))
		return
	}
	region, err := world.GetRegion(regionId)

	if err != nil {
		logrus.Errorf("failed to resolve GM warp region %d: %v", regionId, err)
		return
	}
	offset := math32.NewVector3(x, y, z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		logrus.Errorf("failed to resolve GM warp cell in region %d: %v", regionId, err)
		return
	}
	// A GM warp already supplies the client-facing Y coordinate; do not replace
	// it with the navmesh height-map value.
	newPosition := navmeshv2.RtNavmeshPosition{
		Offset:  offset,
		Heading: 0,
		Region:  region.Region,
		Cell:    &cell,
	}
	world.StopMovingCharacter(player, newPosition, "teleport", true)
}
