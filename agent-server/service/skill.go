package service

import (
	"context"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type SkillUseCase interface {
	LearnSkill(context.Context, *model.Player, uint32) (model.SkillLearnResult, error)
}

type SkillService struct {
	repository model.SkillLearningRepository
	catalog    *model.ReferenceDataCatalog
}

func NewSkillService(repository model.SkillLearningRepository, catalog *model.ReferenceDataCatalog) *SkillService {
	return &SkillService{repository: repository, catalog: catalog}
}

func (s *SkillService) LearnSkill(ctx context.Context, player *model.Player, skillID uint32) (model.SkillLearnResult, error) {
	if s == nil || s.repository == nil || s.catalog == nil || player == nil || player.ID <= 0 {
		return model.SkillLearnResult{}, model.ErrSkillConflict
	}
	skill, exists := s.catalog.Skill(skillID)
	if !exists || skill.BasicLevel == 0 {
		return model.SkillLearnResult{}, model.ErrSkillInvalid
	}
	if !skillRaceAllowed(player, skill.RequiredRace) {
		return model.SkillLearnResult{}, model.ErrSkillRaceMismatch
	}

	owned := player.SkillSnapshot()
	for _, current := range owned {
		if current.ID == skillID {
			return model.SkillLearnResult{}, model.ErrSkillAlreadyLearned
		}
	}
	masteries := player.MasterySnapshot()
	if !skillMasteryMet(masteries, skill.Mastery1, skill.MasteryLevel1) || !skillMasteryMet(masteries, skill.Mastery2, skill.MasteryLevel2) {
		return model.SkillLearnResult{}, model.ErrSkillMasteryRequired
	}
	if player.BaseStats.Str < skill.RequiredStr {
		return model.SkillLearnResult{}, model.ErrSkillStrengthRequired
	}
	if player.BaseStats.Int < skill.RequiredInt {
		return model.SkillLearnResult{}, model.ErrSkillIntellectRequired
	}
	if player.SkillPoints < int(skill.RequiredSP) {
		return model.SkillLearnResult{}, model.ErrSkillInsufficientSP
	}

	command := model.SkillLearnCommand{Skill: skill}
	if skill.BasicLevel > 1 {
		command.ReplacedSkillID = s.ownedSkillAtLevel(owned, skill, skill.BasicLevel-1)
		if command.ReplacedSkillID == 0 {
			return model.SkillLearnResult{}, model.ErrSkillLevelChainInvalid
		}
	} else if s.ownedSkillInLine(owned, skill) != 0 {
		return model.SkillLearnResult{}, model.ErrSkillAlreadyLearned
	}

	requiredIDs := [3]uint32{skill.RequiredSkill1, skill.RequiredSkill2, skill.RequiredSkill3}
	requiredLevels := [3]byte{skill.RequiredLevel1, skill.RequiredLevel2, skill.RequiredLevel3}
	for i, requiredID := range requiredIDs {
		if requiredID == 0 {
			continue
		}
		required, ok := resolveSkillRequirement(s.catalog, requiredID)
		if !ok {
			return model.SkillLearnResult{}, model.ErrSkillInvalid
		}
		minimumLevel := requiredLevels[i]
		if minimumLevel == 0 {
			minimumLevel = required.BasicLevel
		}
		command.RequiredOwnedSkillIDs[i] = s.ownedSkillAtLeast(owned, required, minimumLevel)
		if command.RequiredOwnedSkillIDs[i] == 0 {
			return model.SkillLearnResult{}, model.ErrSkillPrerequisiteRequired
		}
	}

	result, err := s.repository.LearnSkill(ctx, player.ID, command)
	if err != nil {
		return model.SkillLearnResult{}, err
	}
	player.CommitSkill(result)
	player.RecalculatePassiveSkillModifiers(s.catalog)
	return result, nil
}

func skillRaceAllowed(player *model.Player, race byte) bool {
	return race == 3 || race == 0 && player.IsChinese() || race == 1 && player.IsEuropean()
}

func skillMasteryMet(masteries map[uint32]byte, id uint32, level byte) bool {
	return id == 0 || masteries[id] >= level
}

func (s *SkillService) ownedSkillInLine(owned []model.CharacterSkill, target model.RefSkill) uint32 {
	for _, candidate := range owned {
		if reference, ok := s.catalog.Skill(candidate.ID); ok && reference.SameLine(target) {
			return candidate.ID
		}
	}
	return 0
}

// resolveSkillRequirement follows the native REF_SKILL contract: REQLEARN_SKILL
// normally names a skill GROUP_ID, not a concrete skill ID. Group lookup must
// win when the two numeric namespaces collide (which they do for Wizard skill
// groups); the ID fallback preserves imported/legacy catalogs that stored a
// concrete skill ID instead.
func resolveSkillRequirement(catalog *model.ReferenceDataCatalog, requirement uint32) (model.RefSkill, bool) {
	var groupSkill model.RefSkill
	for _, skill := range catalog.SkillSnapshot() {
		if skill.GroupID != requirement || skill.BasicLevel == 0 {
			continue
		}
		if groupSkill.ID == 0 || skill.BasicLevel < groupSkill.BasicLevel || skill.BasicLevel == groupSkill.BasicLevel && skill.ID < groupSkill.ID {
			groupSkill = skill
		}
	}
	if groupSkill.ID != 0 {
		return groupSkill, true
	}
	return catalog.Skill(requirement)
}

func (s *SkillService) ownedSkillAtLevel(owned []model.CharacterSkill, target model.RefSkill, level byte) uint32 {
	for _, candidate := range owned {
		if reference, ok := s.catalog.Skill(candidate.ID); ok && reference.BasicLevel == level && reference.SameLine(target) {
			return candidate.ID
		}
	}
	return 0
}

func (s *SkillService) ownedSkillAtLeast(owned []model.CharacterSkill, target model.RefSkill, level byte) uint32 {
	for _, candidate := range owned {
		if reference, ok := s.catalog.Skill(candidate.ID); ok && reference.BasicLevel >= level && reference.SameLine(target) {
			return candidate.ID
		}
	}
	return 0
}
