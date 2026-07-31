package chat

import (
	"context"
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
	recall interface {
		RecallPlayer(*model.Player, *model.Player) error
	}
	accountModeration model.AccountModerationRepository
	groundItems       interface {
		MaterializeAndSpawn(context.Context, model.GroundItemMaterializer, model.GroundItemSpec, uint32) (*model.Item, error)
	}
	groundMaterializer model.GroundItemMaterializer
	combat             interface {
		ForceKillForGM(context.Context, *model.Player, *model.NPC, byte) error
	}
}

const (
	SetSpeed        string = "speed"
	CurrentPosition string = "curpos"
	JanganWest      string = "jgwest"
	Takla1          string = "takla1"
	Takla2          string = "takla2"
	LogLevel        string = "loglvl"
	SpawnMonster    string = "spawn"
	MakeItem        string = "makeitem"
	LoadMonster     string = "loadmonster"
	Zoe             string = "zoe"
	Zoe2            string = "zoe2"
	MobKill         string = "mobkill"
	RecallUser      string = "recalluser"
	RecallGuild     string = "recallguild"
	Kick            string = "kick"
	Ban             string = "ban"
	Mute            string = "mute"
	SetWeather      string = "weather"
	Invisible       string = "invisible"
	Invincible      string = "invincible"
	HwanMode        string = "hwanmode"
	GMSkill         string = "gmskill"
	Day             string = "day"
	Night           string = "night"
	Rain            string = "rain"
	Snow            string = "snow"
	SetSpeedAlias   string = "setspeed"
	Warp            string = "warp"
	MoveToNPC       string = "movetonpc"
	Sky             string = "sky"
	Ground          string = "ground"
	MapObject       string = "mapobj"
	CharacterRender string = "char"

	defaultGMSpawnCount         = 1
	maximumGMSpawnCount         = 20
	maximumGMItemCount          = model.ClientSafeMaxStack
	defaultGMSpawnRadius        = 100
	gmKillRewardsFull      byte = 0
	gmKillRewardsDropsOnly byte = 1
	gmKillRewardsNone      byte = 2
)

var gmMessageHandlerInstance *GmMessageHandler
var once sync.Once

func GetGmMessageHandlerInstance() *GmMessageHandler {
	once.Do(func() {
		gmMessageHandlerInstance = &GmMessageHandler{}
	})
	return gmMessageHandlerInstance
}

func ConfigureGMGroundItems(groundItems interface {
	MaterializeAndSpawn(context.Context, model.GroundItemMaterializer, model.GroundItemSpec, uint32) (*model.Item, error)
}, materializer model.GroundItemMaterializer) {
	handler := GetGmMessageHandlerInstance()
	handler.groundItems = groundItems
	handler.groundMaterializer = materializer
}
func ConfigureGMAccountModeration(repository model.AccountModerationRepository) {
	GetGmMessageHandlerInstance().accountModeration = repository
}

func ConfigureGMRecall(recall interface {
	RecallPlayer(*model.Player, *model.Player) error
}) {
	GetGmMessageHandlerInstance().recall = recall
}
func ConfigureGMCombat(combat interface {
	ForceKillForGM(context.Context, *model.Player, *model.NPC, byte) error
}) {
	GetGmMessageHandlerInstance().combat = combat
}

func (h *GmMessageHandler) HandleAdminMessage(request MessageRequest, session *server.Session) error {
	if request.ChatType != Notice {
		return nil
	}
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
			recipients, noticeErr := world.BroadcastNotice(notice)
			if noticeErr != nil {
				return sendGMSystemMessage(player.Session, noticeErr.Error())
			}
			logrus.WithFields(logrus.Fields{
				"gm_uid":     session.UserContext.UniqueID,
				"recipients": recipients,
			}).Info("broadcast GM notice")
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
		case RecallUser:
			name := strings.TrimSpace(args)
			if name == "" {
				return sendGMSystemMessage(player.Session, "Usage: recalluser CHARACTER_NAME")
			}
			if h.recall == nil {
				return sendGMSystemMessage(player.Session, "Player recall is unavailable.")
			}
			target, lookupErr := world.GetPlayerByCharName(name)
			if lookupErr != nil || target == nil || target.Session == nil {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Player %s is not online.", name))
			}
			if target == player {
				return sendGMSystemMessage(player.Session, "You are already at your own position.")
			}
			if recallErr := h.recall.RecallPlayer(player, target); recallErr != nil {
				logrus.WithError(recallErr).WithFields(logrus.Fields{
					"gm_uid": player.UniqueID, "target_uid": target.UniqueID,
				}).Warn("GM recalluser failed")
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Could not recall %s: %v", target.CharName, recallErr))
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Recalled %s to your position.", target.CharName))
		case RecallGuild:
			guildName := strings.TrimSpace(args)
			if guildName == "" {
				return sendGMSystemMessage(player.Session, "Usage: recallguild GUILD_NAME")
			}
			if h.recall == nil {
				return sendGMSystemMessage(player.Session, "Guild recall is unavailable.")
			}
			recalled := 0
			failed := 0
			for _, target := range world.GetActivePlayers() {
				if target == player || !strings.EqualFold(target.GetGuildAffiliation().Name, guildName) {
					continue
				}
				if recallErr := h.recall.RecallPlayer(player, target); recallErr != nil {
					failed++
					logrus.WithError(recallErr).WithFields(logrus.Fields{
						"gm_uid": player.UniqueID, "target_uid": target.UniqueID, "guild": guildName,
					}).Warn("GM recallguild member failed")
					continue
				}
				recalled++
			}
			if recalled == 0 && failed == 0 {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Guild %s has no other online members.", guildName))
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Recalled %d member(s) of %s; %d failed.", recalled, guildName, failed))
		case Ban:
			name := strings.TrimSpace(args)
			if name == "" {
				return sendGMSystemMessage(player.Session, "Usage: ban CHARACTER_NAME")
			}
			if h.accountModeration == nil {
				return sendGMSystemMessage(player.Session, "Account moderation is unavailable.")
			}
			target, lookupErr := world.GetPlayerByCharName(name)
			if lookupErr != nil || target == nil || target.Session == nil || target.Session.UserContext.UserID == 0 {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Player %s is not online.", name))
			}
			if target == player {
				return sendGMSystemMessage(player.Session, "You cannot ban your own account.")
			}
			if banErr := h.accountModeration.BanAccount(context.Background(), target.Session.UserContext.UserID); banErr != nil {
				logrus.WithError(banErr).WithFields(logrus.Fields{
					"gm_uid": player.UniqueID, "target_uid": target.UniqueID,
				}).Warn("GM account ban failed")
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Could not ban %s: %v", target.CharName, banErr))
			}
			target.Session.Close()
			logrus.WithFields(logrus.Fields{
				"gm_uid": player.UniqueID, "target_uid": target.UniqueID,
			}).Info("GM banned player account")
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Banned %s and disconnected the account.", target.CharName))
		case Kick:
			name := strings.TrimSpace(args)
			if name == "" {
				return sendGMSystemMessage(player.Session, "Usage: kick CHARACTER_NAME")
			}
			target, lookupErr := world.GetPlayerByCharName(name)
			if lookupErr != nil || target == nil || target.Session == nil {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Player %s is not online.", name))
			}
			if target == player {
				return sendGMSystemMessage(player.Session, "You cannot kick your own session.")
			}
			target.Session.Close()
			logrus.WithFields(logrus.Fields{
				"gm_uid": player.UniqueID, "target_uid": target.UniqueID,
			}).Info("GM kicked player")
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Kicked %s from the server.", target.CharName))
		case Mute:
			name := strings.TrimSpace(args)
			if name == "" {
				return sendGMSystemMessage(player.Session, "Usage: mute CHARACTER_NAME")
			}
			target, lookupErr := world.GetPlayerByCharName(name)
			if lookupErr != nil || target == nil || target.Session == nil {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Player %s is not online.", name))
			}
			if target == player {
				return sendGMSystemMessage(player.Session, "You cannot mute your own session.")
			}
			muted := target.ToggleGMMute()
			state := "unmuted"
			if muted {
				state = "muted"
			}
			logrus.WithFields(logrus.Fields{
				"gm_uid": player.UniqueID, "target_uid": target.UniqueID, "muted": muted,
			}).Info("GM changed player mute state")
			return sendGMSystemMessage(player.Session, fmt.Sprintf("%s is now %s in public chat.", target.CharName, state))
		case MakeItem:
			if h.groundItems == nil || h.groundMaterializer == nil {
				return sendGMSystemMessage(player.Session, "Ground-item creation is unavailable.")
			}
			identifier, count, parseErr := parseGMMakeItemArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			refID, ref, resolveErr := resolveGMMakeItemReference(identifier)
			if resolveErr != nil {
				return sendGMSystemMessage(player.Session, resolveErr.Error())
			}
			maxStack := ref.StackSize
			if maxStack < 1 {
				maxStack = 1
			}
			if count > maxStack {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("%s stack count must be between 1 and %d.", ref.CodeName, maxStack))
			}
			item, spawnErr := h.groundItems.MaterializeAndSpawn(context.Background(), h.groundMaterializer, model.GroundItemSpec{
				ReferenceItemID: refID,
				StackCount:      uint16(count),
				CreatorName:     player.CharName,
				NavmeshPosition: player.GetNavmeshPosition(),
			}, 0)
			if spawnErr != nil {
				logrus.WithError(spawnErr).WithFields(logrus.Fields{
					"gm_uid": player.UniqueID, "ref_item_id": refID, "count": count,
				}).Warn("GM makeitem failed")
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Could not drop %s: %v", ref.CodeName, spawnErr))
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Dropped %d x %s on the ground (UID %d).", count, ref.CodeName, item.GetUniqueID()))
		case SpawnMonster, LoadMonster:
			codeName, count, parseErr := parseGMSpawnArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			spawned := 0
			codeName, resolveErr := resolveGMMonsterCode(codeName)
			if resolveErr != nil {
				return sendGMSystemMessage(player.Session, resolveErr.Error())
			}
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
		case MobKill:
			mode, parseErr := strconv.Atoi(strings.TrimSpace(args))
			if parseErr != nil || (mode != 0 && mode != 1) {
				return sendGMSystemMessage(player.Session, "Usage: mobkill 0|1")
			}
			if h.combat == nil {
				return sendGMSystemMessage(player.Session, "GM combat control is unavailable.")
			}
			targetUID := session.UserContext.SelectedEntityUniqueID
			if targetUID == 0 {
				return sendGMSystemMessage(player.Session, "Select a monster before using mobkill.")
			}
			object, lookupErr := world.GetObjectByUniqueId(targetUID)
			target, ok := object.(*model.NPC)
			if lookupErr != nil || !ok || target == nil || !target.GetTypeInfo().IsNPCMob() {
				return sendGMSystemMessage(player.Session, "The selected target is not a live monster.")
			}
			rewardMode := gmKillRewardsFull
			if mode == 1 {
				rewardMode = gmKillRewardsNone
			}
			if killErr := h.combat.ForceKillForGM(context.Background(), player, target, rewardMode); killErr != nil {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("Could not kill %s: %v", target.GetName(), killErr))
			}
			session.UserContext.SelectedEntityUniqueID = 0
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Killed %s with mobkill mode %d.", target.GetName(), mode))
		case Zoe, Zoe2:
			if h.combat == nil {
				return sendGMSystemMessage(player.Session, "GM combat control is unavailable.")
			}
			identifier, count, parseErr := parseGMSpawnArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			codeName, resolveErr := resolveGMMonsterCode(identifier)
			if resolveErr != nil {
				return sendGMSystemMessage(player.Session, resolveErr.Error())
			}
			rewardMode := gmKillRewardsFull
			if command == Zoe2 {
				rewardMode = gmKillRewardsDropsOnly
			}
			killed := 0
			for killed < count {
				target, spawnErr := manager.GetRespawnManagerInstance().SpawnQuestNPC(codeName, player.GetNavmeshPosition(), defaultGMSpawnRadius)
				if spawnErr != nil {
					return sendGMSystemMessage(player.Session, fmt.Sprintf("Spawn failed for %s after %d/%d kills: %v", codeName, killed, count, spawnErr))
				}
				if killErr := h.combat.ForceKillForGM(context.Background(), player, target, rewardMode); killErr != nil {
					return sendGMSystemMessage(player.Session, fmt.Sprintf("Force-kill failed for %s after %d/%d kills: %v", codeName, killed, count, killErr))
				}
				killed++
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("%s killed %d x %s.", command, killed, codeName))
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
		case Sky, Ground, MapObject, CharacterRender:
			return sendGMSystemMessage(player.Session, "@AegisRender:"+command)
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
		case GMSkill:
			mode, parseErr := strconv.Atoi(strings.TrimSpace(args))
			if parseErr != nil || (mode != 0 && mode != 1) {
				return sendGMSystemMessage(player.Session, "Usage: gmskill 0|1")
			}
			packet := network.EmptyPacket()
			packet.MessageID = opcode.GMForceCircleUpdate
			packet.WriteByte(byte(mode))
			if err := packet.WriteError(); err != nil {
				return err
			}
			player.SendToSelfAndKnown(&packet)
			return sendGMSystemMessage(player.Session, fmt.Sprintf("GM force circle mode %d sent.", mode))
		case MoveToNPC:
			identifier := strings.TrimSpace(args)
			if identifier == "" {
				return sendGMSystemMessage(player.Session, "Usage: movetonpc NPC_ID|NPC_CODE_NAME")
			}
			codeName, refID, resolveErr := resolveGMNPCReference(identifier)
			if resolveErr != nil {
				return sendGMSystemMessage(player.Session, resolveErr.Error())
			}
			var target *model.NPC
			for _, candidate := range world.GetNPCs() {
				if candidate == nil || candidate.GetRefObjectID() != refID || candidate.GetLifeState() == model.Dead || candidate.GetGameWorldID() != player.GetGameWorldID() {
					continue
				}
				if target == nil || candidate.GetUniqueID() < target.GetUniqueID() {
					target = candidate
				}
			}
			if target == nil {
				return sendGMSystemMessage(player.Session, fmt.Sprintf("%s is not currently spawned in this world.", codeName))
			}
			world.StopMovingCharacter(player, target.GetNavmeshPosition(), "teleport", true)
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Moved to %s (UID %d).", codeName, target.GetUniqueID()))
		case Warp:
			regionID, x, y, z, parseErr := parseGMWarpArgs(args)
			if parseErr != nil {
				return sendGMSystemMessage(player.Session, parseErr.Error())
			}
			if warpErr := warpPlayer(session.UserContext.UniqueID, x, y, z, regionID); warpErr != nil {
				return sendGMSystemMessage(player.Session, warpErr.Error())
			}
			return sendGMSystemMessage(player.Session, fmt.Sprintf("Warped to region %d (%.2f, %.2f, %.2f).", regionID, x, y, z))
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
		default:
			recipients, noticeErr := world.BroadcastNotice(request.Message)
			if noticeErr != nil {
				return sendGMSystemMessage(player.Session, noticeErr.Error())
			}
			logrus.WithFields(logrus.Fields{
				"gm_uid":     session.UserContext.UniqueID,
				"recipients": recipients,
			}).Info("broadcast GM notice")
			return sendGMChatAck(session, request)
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
		return "", 0, errors.New("Usage: loadmonster|zoe|zoe2 MONSTER_ID|MOB_CODE_NAME [count]")
	}
	identifier := fields[0]
	if _, err := strconv.ParseUint(identifier, 10, 32); err != nil && !strings.HasPrefix(identifier, "MOB_") {
		return "", 0, errors.New("Monster must be a numeric reference ID or a MOB_ code name")
	}
	count := defaultGMSpawnCount
	if len(fields) == 2 {
		parsed, err := strconv.Atoi(fields[1])
		if err != nil || parsed < 1 || parsed > maximumGMSpawnCount {
			return "", 0, fmt.Errorf("Spawn count must be between 1 and %d", maximumGMSpawnCount)
		}
		count = parsed
	}
	return identifier, count, nil
}

func resolveGMMonsterCode(identifier string) (string, error) {
	if parsed, err := strconv.ParseUint(identifier, 10, 32); err == nil {
		refID := uint32(parsed)
		ref, ok := model.RefChars[refID]
		if !ok || !ref.TypeInfo.IsNPCMob() {
			return "", fmt.Errorf("Monster reference ID %d was not found", refID)
		}
		return ref.CodeName, nil
	}
	for _, ref := range model.RefChars {
		if strings.EqualFold(ref.CodeName, identifier) && ref.TypeInfo.IsNPCMob() {
			return ref.CodeName, nil
		}
	}
	return "", fmt.Errorf("Monster %s was not found", identifier)
}

func parseGMMakeItemArgs(arguments string) (string, int, error) {
	fields := strings.Fields(arguments)
	if len(fields) < 1 || len(fields) > 2 {
		return "", 0, errors.New("Usage: makeitem ITEM_ID|ITEM_CODE_NAME [count]")
	}
	identifier := fields[0]
	if _, err := strconv.ParseUint(identifier, 10, 32); err != nil && !strings.HasPrefix(identifier, "ITEM_") {
		return "", 0, errors.New("Item must be a numeric reference ID or an ITEM_ code name")
	}
	count := 1
	if len(fields) == 2 {
		parsed, err := strconv.Atoi(fields[1])
		if err != nil || parsed < 1 || parsed > maximumGMItemCount {
			return "", 0, fmt.Errorf("Item count must be between 1 and %d", maximumGMItemCount)
		}
		count = parsed
	}
	return identifier, count, nil
}

func resolveGMMakeItemReference(identifier string) (uint32, model.RefItem, error) {
	if parsed, err := strconv.ParseUint(identifier, 10, 32); err == nil {
		refID := uint32(parsed)
		ref, ok := model.RefItems[refID]
		if !ok || !ref.TypeInfo.IsItem() {
			return 0, model.RefItem{}, fmt.Errorf("Item reference ID %d was not found", refID)
		}
		return refID, ref, nil
	}
	for refID, ref := range model.RefItems {
		if strings.EqualFold(ref.CodeName, identifier) && ref.TypeInfo.IsItem() {
			return refID, ref, nil
		}
	}
	return 0, model.RefItem{}, fmt.Errorf("Item %s was not found", identifier)
}

func resolveGMNPCReference(identifier string) (string, uint32, error) {
	if parsed, err := strconv.ParseUint(identifier, 10, 32); err == nil {
		refID := uint32(parsed)
		ref, ok := model.RefChars[refID]
		if !ok || (!ref.TypeInfo.IsNPCNpc() && !ref.TypeInfo.IsNPCMob()) {
			return "", 0, fmt.Errorf("NPC reference ID %d was not found", refID)
		}
		return ref.CodeName, refID, nil
	}
	for refID, ref := range model.RefChars {
		if strings.EqualFold(ref.CodeName, identifier) && (ref.TypeInfo.IsNPCNpc() || ref.TypeInfo.IsNPCMob()) {
			return ref.CodeName, refID, nil
		}
	}
	return "", 0, fmt.Errorf("NPC %s was not found", identifier)
}

func parseGMWarpArgs(arguments string) (int16, float32, float32, float32, error) {
	fields := strings.Fields(arguments)
	if len(fields) != 4 {
		return 0, 0, 0, 0, errors.New("Usage: .warp REGION X Y Z")
	}
	regionID, err := strconv.ParseInt(fields[0], 10, 16)
	if err != nil {
		return 0, 0, 0, 0, errors.New("Region must be a valid integer")
	}
	x, err := strconv.ParseFloat(fields[1], 32)
	if err != nil {
		return 0, 0, 0, 0, errors.New("X coordinate must be a valid number")
	}
	y, err := strconv.ParseFloat(fields[2], 32)
	if err != nil {
		return 0, 0, 0, 0, errors.New("Y coordinate must be a valid number")
	}
	z, err := strconv.ParseFloat(fields[3], 32)
	if err != nil {
		return 0, 0, 0, 0, errors.New("Z coordinate must be a valid number")
	}
	return int16(regionID), float32(x), float32(y), float32(z), nil
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

func warpPlayer(playerUniqueId uint32, x, y, z float32, regionId int16) error {
	world := service.GetWorldServiceInstance()
	player, err := world.GetPlayerByUniqueId(playerUniqueId)
	if err != nil {
		logrus.Error(errors.Wrap(err, "failed to warp player"))
		return fmt.Errorf("failed to warp player: %w", err)
	}
	region, err := world.GetRegion(regionId)

	if err != nil {
		logrus.Errorf("failed to resolve GM warp region %d: %v", regionId, err)
		return fmt.Errorf("failed to resolve GM warp region %d: %w", regionId, err)
	}
	offset := math32.NewVector3(x, y, z)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		logrus.Errorf("failed to resolve GM warp cell in region %d: %v", regionId, err)
		return fmt.Errorf("failed to resolve GM warp cell in region %d: %w", regionId, err)
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
	return nil
}
