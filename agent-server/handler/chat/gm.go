package chat

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Harvi8/AegisCore/agent-server/engine/environment"
	"github.com/Harvi8/AegisCore/agent-server/manager"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

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
	LoadMonster     string = "loadmonster"
	SetWeather      string = "weather"
	Invisible       string = "invisible"
	Invincible      string = "invincible"
	HwanMode        string = "hwanmode"
	Day             string = "day"
	Night           string = "night"
	Rain            string = "rain"
	Snow            string = "snow"
	SetSpeedAlias   string = "setspeed"

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
	if request.ChatType == Notice {
		if notice, ok := parseGMNotice(request.Message); ok {
			if _, noticeErr := world.BroadcastNotice(notice); noticeErr != nil {
				return sendGMSystemMessage(player.Session, noticeErr.Error())
			}
			return sendGMChatAck(session, request)
		}
	}
	if command, args, commandOK := parseAdminCommand(request.Message, request.ChatType == Notice); commandOK {
		logrus.Debugf("[GM] - gm command %v with args %v\n", command, args)
		switch command {
		case SetSpeed, SetSpeedAlias:
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
		case SpawnMonster, LoadMonster:
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
		case SetWeather:
			weatherType, intensity, parseErr := parseGMWeatherArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			if setErr := environment.SetCurrentWeather(weatherType, intensity); setErr != nil {
				return sendGMSystemMessage(player.Session, setErr.Error())
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Weather changed to %s (%d%%).", strings.ToLower(argsWeatherName(weatherType)), intensity))
		case Rain, Snow:
			weatherType := model.Rain
			if command == Snow {
				weatherType = model.Snow
			}
			intensity, parseErr := parseGMWeatherIntensity(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			if setErr := environment.SetCurrentWeather(weatherType, intensity); setErr != nil {
				return setErr
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Weather changed to %s (%d%%).", argsWeatherName(weatherType), intensity))
		case Day, Night:
			clock := time.Now()
			if command == Day {
				clock = time.Date(clock.Year(), clock.Month(), clock.Day(), 12, 0, 0, 0, clock.Location())
			} else {
				clock = time.Date(clock.Year(), clock.Month(), clock.Day(), 0, 0, 0, 0, clock.Location())
			}
			world.Broadcast(vsro188.BuildGameServerTime(clock))
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Environment time changed to %s.", command))
		case Invisible, Invincible:
			state := model.GmInvisible
			if command == Invincible {
				state = model.GmInvincible
			}
			if player.GetBodyState() == state {
				state = model.Normal
			}
			player.SetBodyState(state)
			packet := model.BuildBodyStateUpdatePacket(player.GetUniqueID(), state)
			player.SendToSelfAndKnown(&packet)
			return sendGMSystemMessage(player.Session, fmt.Sprintf("GM %s mode %s.", command, map[bool]string{true: "enabled", false: "disabled"}[state != model.Normal]))
		case HwanMode:
			mode, parseErr := strconv.Atoi(strings.TrimSpace(args))
			if parseErr != nil || mode < 0 || mode > 3 {
				return sendGMSystemMessage(player.Session, "Usage: .hwanmode 0|1|2|3")
			}
			state := model.Normal
			variant := byte(0)
			if mode != 0 {
				state = model.Berserker
				if mode == 2 || mode == 3 {
					variant = 1
				}
			}
			player.SetBodyState(state)
			packet := model.BuildBodyStateUpdatePacket(player.GetUniqueID(), state, variant)
			player.SendToSelfAndKnown(&packet)
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Hwan mode %d applied.", mode))
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

func parseGMNotice(message string) (string, bool) {
	if !strings.HasPrefix(message, "~") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(message, "~")), true
}

func sendGMChatAck(session *server.Session, request MessageRequest) error {
	if session == nil {
		return errors.New("GM session is unavailable")
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatResponse
	packet.WriteByte(1)
	packet.WriteByte(request.ChatType)
	packet.WriteByte(request.ChatIndex)
	return session.Send(packet)
}

func parseGMWeatherArgs(arguments string) (model.WeatherType, byte, error) {
	fields := strings.Fields(strings.ToLower(arguments))
	if len(fields) < 1 || len(fields) > 2 {
		return 0, 0, errors.New("Usage: .weather clear|rain|snow [intensity 1-100]")
	}
	weatherTypes := map[string]model.WeatherType{"clear": model.Clear, "rain": model.Rain, "snow": model.Snow}
	weatherType, ok := weatherTypes[fields[0]]
	if !ok {
		return 0, 0, errors.New("Weather must be clear, rain, or snow")
	}
	if weatherType == model.Clear {
		if len(fields) != 1 {
			return 0, 0, errors.New("Clear weather does not accept an intensity")
		}
		return weatherType, 0, nil
	}
	intensity := 50
	if len(fields) == 2 {
		parsed, err := strconv.Atoi(fields[1])
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, 0, errors.New("Weather intensity must be between 1 and 100")
		}
		intensity = parsed
	}
	return weatherType, byte(intensity), nil
}

func parseGMWeatherIntensity(arguments string) (byte, error) {
	if strings.TrimSpace(arguments) == "" {
		return 50, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(arguments))
	if err != nil || parsed < 0 || parsed > 255 {
		return 0, errors.New("Weather intensity must be between 0 and 255")
	}
	if parsed == 0 {
		return 0, nil
	}
	// The client accepts 0..255; the shared runtime normalizes its gameplay
	// intensity to 0..100, preserving the command's full-strength semantics.
	return byte((parsed*100 + 127) / 255), nil
}

func argsWeatherName(weatherType model.WeatherType) string {
	switch weatherType {
	case model.Clear:
		return "Clear"
	case model.Rain:
		return "Rain"
	case model.Snow:
		return "Snow"
	default:
		return "Unknown"
	}
}

func parseAdminCommand(message string, allowBare bool) (string, string, bool) {
	message = strings.TrimSpace(message)
	hasPrefix := strings.HasPrefix(message, ".") || strings.HasPrefix(message, "/")
	if hasPrefix {
		message = strings.TrimSpace(message[1:])
	} else if !allowBare {
		return "", "", false
	}
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", "", false
	}
	command := strings.ToLower(fields[0])
	if strings.IndexFunc(command, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) }) >= 0 {
		return "", "", false
	}
	arguments := strings.TrimSpace(strings.TrimPrefix(message, fields[0]))
	return command, arguments, true
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
