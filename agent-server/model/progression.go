package model

import (
	"math"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const SkillExperiencePerPoint = uint64(400)

// ProgressionResult is the server-authoritative outcome of a mob EXP award.
type ProgressionResult struct {
	Level            int
	Experience       uint64
	SkillExperience  uint64
	SkillPoints      int
	StatPoints       int
	LevelsGained     int
	StatPointsGained int
}

// BuildLevelUpAnimationPacket reproduces SR_GameServer's 0x3054 promotion
// broadcast. The stock client consumes exactly the promoted entity UID and
// creates the level-up visual locally.
func BuildLevelUpAnimationPacket(uniqueID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityAnimationPromote
	packet.WriteUInt32(uniqueID)
	return packet
}

// ApplyExperience advances a character using the client-matched level table.
// EXP is stored relative to the current level, as expected by LevelData.txt.
func ApplyExperience(table *LevelTable, level int, experience, skillExperience, gainedExperience, gainedSkillExperience uint64, skillPoints, statPoints int) ProgressionResult {
	result := ProgressionResult{Level: level, Experience: experience, SkillExperience: skillExperience, SkillPoints: skillPoints, StatPoints: statPoints}
	if table == nil || level <= 0 {
		return result
	}
	result.Experience = saturatingAdd(result.Experience, gainedExperience)
	result.SkillExperience = saturatingAdd(result.SkillExperience, gainedSkillExperience)
	if result.SkillExperience >= SkillExperiencePerPoint {
		gainedPoints := result.SkillExperience / SkillExperiencePerPoint
		result.SkillExperience %= SkillExperiencePerPoint
		if gainedPoints > uint64(math.MaxInt-result.SkillPoints) {
			result.SkillPoints = math.MaxInt
		} else {
			result.SkillPoints += int(gainedPoints)
		}
	}
	for result.Level < table.MaxLevel() {
		row, exists := table.Level(result.Level)
		if !exists || row.RequiredExperience == 0 || result.Experience < row.RequiredExperience {
			break
		}
		result.Experience -= row.RequiredExperience
		result.Level++
		result.LevelsGained++
		result.StatPoints += 3
		result.StatPointsGained += 3
	}
	return result
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}
