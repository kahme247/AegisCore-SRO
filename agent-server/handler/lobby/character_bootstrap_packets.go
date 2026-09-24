package lobby

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/engine/environment"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	log "github.com/sirupsen/logrus"
)

func sendCharacterBootstrapBody(session *server.Session, player *model.Player) error {
	traceBootstrap := os.Getenv("SRO_SESSION_TRACE") == "1"
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE start character=%s id=%d", player.CharName, player.ID)
	}

	pDataBody := network.EmptyPacket()
	pDataBody.MessageID = opcode.CharacterDataBody
	pDataBody.WriteUInt32(utils.ToSilkroadTime(time.Now()))
	WriteCharDataToPacket(&pDataBody, player)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE character-data character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	WriteInventoryToPacket(&pDataBody, player.Inventory)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE inventory character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	WriteAvatarInventoryToPacket(&pDataBody, player.AvatarInventory)
	pDataBody.WriteByte(0) // Mastery section begin
	masteries := player.MasterySnapshot()
	masteryIDs := make([]uint32, 0, len(masteries))
	for masteryID := range masteries {
		masteryIDs = append(masteryIDs, masteryID)
	}
	sort.Slice(masteryIDs, func(i, j int) bool { return masteryIDs[i] < masteryIDs[j] })
	for _, masteryID := range masteryIDs {
		WriteMasteryOrSkill(&pDataBody, masteryID, masteries[masteryID])
	}
	pDataBody.WriteByte(2) // Mastery section end
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE masteries character=%s count=%d size=%d", player.CharName, len(masteryIDs), pDataBody.PayloadLen())
	}
	writeCharacterSkillSection(&pDataBody, player)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE skills character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	if err := model.WriteQuestLog(&pDataBody, player.QuestSnapshot()); err != nil {
		return fmt.Errorf("write character quest log: %w", err)
	}
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE quests character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(player.GetUniqueID())
	model.WritePosition(&pDataBody, player.GetNavmeshPosition())
	pDataBody.WriteByte(0) // HasDestination
	pDataBody.WriteByte(1)
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt16(uint16(player.GetNavmeshPosition().Heading))
	pDataBody.WriteByte(byte(model.Spawning))
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(byte(model.NoMotion))
	pDataBody.WriteByte(byte(player.BodyState))
	pDataBody.WriteFloat32(player.GetWalkSpeed())
	pDataBody.WriteFloat32(player.GetRunSpeed())
	pDataBody.WriteFloat32(player.GetHwanSpeed())
	model.WriteCharacterSpawnBuffs(&pDataBody, player, true, time.Now())
	pDataBody.WriteString(player.CharName)
	job := player.GetJob()
	if job.Active() {
		pDataBody.WriteString(job.Alias)
		pDataBody.WriteByte(job.Type)
		pDataBody.WriteByte(job.Level)
	} else {
		// Preserve the established no-job bootstrap representation until the
		// inactive branch is independently captured; active state must not be
		// replaced with these legacy placeholders.
		pDataBody.WriteUInt16(0)
		pDataBody.WriteByte(0)
		pDataBody.WriteByte(1)
	}
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0xFF)
	// Guide bit 0 suppresses the stock client's first-login defaults. Leaving
	// it clear makes vSRO overwrite restored quick slots 1-4 and persist those
	// defaults back through 0x7158 on every relog.
	pDataBody.WriteUInt64(1)
	// Stock vSRO identifies client configuration with the account JID, not the
	// shard character ID.
	pDataBody.WriteUInt32(session.UserContext.UserID)
	if player.IsGm {
		pDataBody.WriteByte(1)
	} else {
		pDataBody.WriteByte(0)
	}
	writeCharacterQuickSlotConfig(&pDataBody, player)
	writeAutoPotionConfig(&pDataBody, player)
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt32(0x00010001)
	pDataBody.WriteByte(0)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE sending-body character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	if err := session.Send(pDataBody); err != nil {
		return err
	}

	pDataEnd := network.EmptyPacket()
	pDataEnd.MessageID = opcode.CharacterDataEnd
	if err := session.Send(pDataEnd); err != nil {
		return err
	}

	// Stock 0x30D2 initializes the local player's fixed 32-entry abnormal-state
	// vector. The live character has no persisted combat statuses at join, so a
	// zero mask is sufficient. Omitting this packet leaves the vector empty;
	// the first 0x3057 update then throws at sro_client 0x00783930.
	badEffects := buildPlayerBadEffectBootstrap()
	if err := session.Send(badEffects); err != nil {
		return err
	}

	celestialPosition := environment.GetCelestialPositionForPlayer(player)
	pCelestialPos := network.EmptyPacket()
	pCelestialPos.MessageID = opcode.CelestialPosition
	pCelestialPos.WriteUInt32(celestialPosition.CharUniqueID)
	pCelestialPos.WriteUInt16(celestialPosition.Moonphase)
	pCelestialPos.WriteByte(celestialPosition.Hour)
	pCelestialPos.WriteByte(celestialPosition.Minute)
	if err := session.Send(pCelestialPos); err != nil {
		return err
	}

	weatherType, intensity := environment.GetCurrentWeather()
	weatherType, intensity = environment.WeatherForRegion(player.GetNavmeshPosition().Region.ID, weatherType, intensity)
	pWeather := environment.BuildWeatherUpdate(weatherType, intensity)
	if err := session.Send(pWeather); err != nil {
		return err
	}

	player.SendStatsUpdate()
	player.SendMovementStateUpdate()

	pReady := network.EmptyPacket()
	pReady.MessageID = 0x3077
	pReady.WriteByte(0)
	pReady.WriteByte(0)
	return session.Send(pReady)
}

func buildPlayerBadEffectBootstrap() network.Packet {
	return model.BuildPlayerBadEffectSnapshot(0, nil)
}

func writeCharacterQuickSlotConfig(packet *network.Packet, player *model.Player) {
	slots := player.QuickSlotSnapshot()
	packet.WriteByte(7) // Required vSRO quick-slot configuration format version.
	packet.WriteByte(byte(len(slots)))
	for _, slot := range slots {
		packet.WriteByte(slot.Sequence)
		packet.WriteByte(slot.ContentType)
		packet.WriteUInt32(slot.ContentID)
	}
}

var defaultAutoPotionSettings = model.AutoPotionSettings{0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}

func writeAutoPotionConfig(packet *network.Packet, player *model.Player) {
	settings, present := player.AutoPotionSettingsSnapshot()
	if !present {
		settings = defaultAutoPotionSettings
	}
	packet.WriteBytes(settings[:])
}

func writeCharacterSkillSection(packet *network.Packet, player *model.Player) {
	packet.WriteByte(0) // Unknown byte between mastery and skill lists.
	for _, skill := range player.SkillSnapshot() {
		if isImplicitBaseAttackSkill(skill.ID) {
			continue
		}
		packet.WriteByte(1)
		packet.WriteUInt32(skill.ID)
		packet.WriteByte(skill.Enabled)
	}
	packet.WriteByte(2) // Skill list terminator.
}

func isImplicitBaseAttackSkill(skillID uint32) bool {
	switch skillID {
	case 1, 2, 40, 70, 8419, 8420, 8421, 9354, 9355, 9944, 10625, 11162, 11526:
		return true
	default:
		return false
	}
}
