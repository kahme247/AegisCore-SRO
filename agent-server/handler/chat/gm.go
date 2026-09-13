package chat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var adminCommandRegex = regexp.MustCompile("^\\.(?P<command>\\w+)\\s?(?P<args>.*)$")
var cheatCommandRegex = regexp.MustCompile("^(?P<command>\\w+)\\s?(?P<args>.*)$")

type GmMessageHandler struct {
}

const (
	SetSpeed        string = "speed"
	CurrentPosition string = "curpos"
	JanganWest      string = "jgwest"
	Takla1          string = "takla1"
	Takla2          string = "takla2"
	LogLevel        string = "loglvl"
	SpawnMonster    string = "spawn"

	defaultGMSpawnCount  = 1
	maximumGMSpawnCount  = 20
	defaultGMSpawnRadius = 100
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
	if command, args, commandOK := parseAdminCommand(request.Message, request.ChatType == Notice); commandOK {
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
		case SpawnMonster:
			codeName, count, parseErr := parseGMSpawnArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			spawned := 0
			for spawned < count {
				if _, spawnErr := manager.GetRespawnManagerInstance().SpawnQuestNPC(
					codeName,
					player.GetNavmeshPosition(),
					defaultGMSpawnRadius,
				); spawnErr != nil {
					logrus.WithError(spawnErr).Warnf("[GM] failed to spawn %s for %s after %d successful spawns", codeName, player.CharName, spawned)
					return sendGMSystemMessage(player.Session, fmt.Sprintf("Spawn failed for %s after %d/%d: %v", codeName, spawned, count, spawnErr))
				}
				spawned++
			}
			logrus.Infof("[GM] spawned %d x %s near %s", spawned, codeName, player.CharName)
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Spawned %d x %s nearby.", spawned, codeName))
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

func parseAdminCommand(message string, allowBare bool) (string, string, bool) {
	matcher := adminCommandRegex
	if allowBare && !strings.HasPrefix(message, ".") {
		matcher = cheatCommandRegex
	}
	components := matcher.FindStringSubmatch(message)
	if len(components) != 3 {
		return "", "", false
	}
	return components[1], components[2], true
}

func parseGMSpawnArgs(arguments string) (string, int, error) {
	fields := strings.Fields(arguments)
	if len(fields) < 1 || len(fields) > 2 {
		return "", 0, errors.New("Usage: .spawn MOB_CODE_NAME [count]")
	}
	codeName := fields[0]
	if !strings.HasPrefix(codeName, "MOB_") {
		return "", 0, errors.New("Spawn code name must begin with MOB_")
	}
	count := defaultGMSpawnCount
	if len(fields) == 2 {
		parsed, err := strconv.Atoi(fields[1])
		if err != nil || parsed < 1 || parsed > maximumGMSpawnCount {
			return "", 0, fmt.Errorf("Spawn count must be between 1 and %d", maximumGMSpawnCount)
		}
		count = parsed
	}
	return codeName, count, nil
}

func sendGMSystemMessage(session *server.Session, message string) error {
	if session == nil {
		return errors.New("GM session is unavailable")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatUpdate
	packet.WriteByte(PM)
	packet.WriteString("System")
	packet.WriteString(message)
	if err := packet.WriteError(); err != nil {
		return err
	}
	return session.Send(packet)
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
