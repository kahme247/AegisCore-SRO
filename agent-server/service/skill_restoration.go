package service

import (
	"context"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type SkillRestorationService struct {
	repository model.SkillRestorationRepository
	catalog    *model.ReferenceDataCatalog
	levels     *model.LevelTable
}

func NewSkillRestorationService(repository model.SkillRestorationRepository, catalog *model.ReferenceDataCatalog, levels *model.LevelTable) *SkillRestorationService {
	return &SkillRestorationService{repository: repository, catalog: catalog, levels: levels}
}

func (s *SkillRestorationService) WithdrawSkill(ctx context.Context, player *model.Player, itemRefID, skillID uint32, targetLevel byte) (model.SkillRestorationResult, error) {
	if s == nil || s.repository == nil || s.catalog == nil || player == nil || player.ID <= 0 {
		return model.SkillRestorationResult{}, model.ErrSkillRestorationConflict
	}
	item, exists := s.catalog.Item(itemRefID)
	if !exists {
		return model.SkillRestorationResult{}, model.ErrSkillRestorationInvalid
	}
	command, err := planSkillWithdrawal(s.catalog, player.SkillSnapshot(), item, skillID, targetLevel)
	if err != nil {
		return model.SkillRestorationResult{}, err
	}
	if command.Paid {
		command.GoldCost, err = restorationGoldCost(s.levels, player.MaxLevel, command.GoldMasteryLevels)
		if err != nil {
			return model.SkillRestorationResult{}, err
		}
	}
	result, err := s.repository.WithdrawSkill(ctx, player.ID, command)
	if err != nil {
		return model.SkillRestorationResult{}, err
	}
	player.CommitSkillWithdrawal(command.CurrentSkillID, result.ResultingSkillID, result.SkillPoints, result.ItemSlot, result.ItemRemaining)
	if result.GoldCharged != 0 {
		player.SetGold(result.Gold)
	}
	player.RecalculatePassiveSkillModifiers(s.catalog)
	return result, nil
}

func (s *SkillRestorationService) WithdrawMastery(ctx context.Context, player *model.Player, itemRefID, masteryID uint32, targetLevel byte) (model.SkillRestorationResult, error) {
	if s == nil || s.repository == nil || s.catalog == nil || s.levels == nil || player == nil || player.ID <= 0 {
		return model.SkillRestorationResult{}, model.ErrSkillRestorationConflict
	}
	item, exists := s.catalog.Item(itemRefID)
	if !exists {
		return model.SkillRestorationResult{}, model.ErrSkillRestorationInvalid
	}
	currentLevel, exists := player.MasteryLevel(masteryID)
	if !exists {
		return model.SkillRestorationResult{}, model.ErrSkillRestorationInvalid
	}
	command, err := planMasteryWithdrawal(s.catalog, s.levels, player.SkillSnapshot(), item, masteryID, currentLevel, targetLevel)
	if err != nil {
		return model.SkillRestorationResult{}, err
	}
	if command.Paid {
		goldLevels := make([]byte, 0, int(currentLevel-targetLevel))
		for level := currentLevel; level > targetLevel; level-- {
			goldLevels = append(goldLevels, level)
		}
		command.GoldCost, err = restorationGoldCost(s.levels, player.MaxLevel, goldLevels)
		if err != nil {
			return model.SkillRestorationResult{}, err
		}
	}
	result, err := s.repository.WithdrawMastery(ctx, player.ID, command)
	if err != nil {
		return model.SkillRestorationResult{}, err
	}
	player.CommitMasteryWithdrawal(masteryID, result.Mastery.Level, result.SkillPoints, result.ItemSlot, result.ItemRemaining)
	if result.GoldCharged != 0 {
		player.SetGold(result.Gold)
	}
	return result, nil
}

func planSkillWithdrawal(catalog *model.ReferenceDataCatalog, owned []model.CharacterSkill, item model.RefItem, skillID uint32, targetLevel byte) (model.SkillWithdrawCommand, error) {
	if catalog == nil || item.ID == 0 || skillID == 0 {
		return model.SkillWithdrawCommand{}, model.ErrSkillRestorationInvalid
	}
	paid, err := restorationItemPolicy(item)
	if err != nil {
		return model.SkillWithdrawCommand{}, err
	}
	current, exists := catalog.Skill(skillID)
	if !exists || current.BasicLevel == 0 || targetLevel >= current.BasicLevel || !playerOwnsSkill(owned, skillID) {
		return model.SkillWithdrawCommand{}, model.ErrSkillRestorationInvalid
	}

	byLevel := make(map[byte]model.RefSkill)
	for _, skill := range catalog.SkillSnapshot() {
		if skill.SameLine(current) {
			byLevel[skill.BasicLevel] = skill
		}
	}
	resultingSkillID := uint32(0)
	if targetLevel != 0 {
		resulting, ok := byLevel[targetLevel]
		if !ok {
			return model.SkillWithdrawCommand{}, model.ErrSkillRestorationInvalid
		}
		resultingSkillID = resulting.ID
	}
	refund := uint64(0)
	goldLevels := make([]byte, 0, int(current.BasicLevel-targetLevel))
	for level := int(targetLevel) + 1; level <= int(current.BasicLevel); level++ {
		skill, ok := byLevel[byte(level)]
		if !ok {
			return model.SkillWithdrawCommand{}, model.ErrSkillRestorationInvalid
		}
		refund += uint64(skill.RequiredSP)
		goldLevels = append(goldLevels, skill.MasteryLevel1)
	}
	if paid {
		refund -= refund / 5
	}
	forbidden := skillWithdrawalDependencies(catalog, current, targetLevel, skillID)
	if ownsAnySkill(owned, forbidden) {
		return model.SkillWithdrawCommand{}, model.ErrSkillRestorationDependency
	}
	return model.SkillWithdrawCommand{
		ItemRefID: item.ID, CurrentSkillID: skillID, ResultingSkillID: resultingSkillID,
		TargetLevel: targetLevel, RequiredItemQuantity: uint16(current.BasicLevel - targetLevel), RefundSkillPoints: refund,
		ForbiddenSkillIDs: forbidden,
		Paid:              paid, GoldMasteryLevels: goldLevels,
	}, nil
}

func planMasteryWithdrawal(catalog *model.ReferenceDataCatalog, levels *model.LevelTable, owned []model.CharacterSkill, item model.RefItem, masteryID uint32, currentLevel, targetLevel byte) (model.MasteryWithdrawCommand, error) {
	if catalog == nil || levels == nil || item.ID == 0 || masteryID == 0 || targetLevel >= currentLevel {
		return model.MasteryWithdrawCommand{}, model.ErrSkillRestorationInvalid
	}
	paid, err := restorationItemPolicy(item)
	if err != nil {
		return model.MasteryWithdrawCommand{}, err
	}
	forbidden := masteryWithdrawalDependencies(catalog, masteryID, targetLevel)
	if ownsAnySkill(owned, forbidden) {
		return model.MasteryWithdrawCommand{}, model.ErrSkillRestorationDependency
	}
	refund := uint64(0)
	for level := int(targetLevel) + 1; level <= int(currentLevel); level++ {
		row, ok := levels.Level(level)
		if !ok {
			return model.MasteryWithdrawCommand{}, model.ErrSkillRestorationInvalid
		}
		refund += row.RequiredSkillPoints
	}
	if paid {
		refund -= refund / 5
	}
	return model.MasteryWithdrawCommand{
		ItemRefID: item.ID, MasteryID: masteryID, ExpectedLevel: currentLevel, TargetLevel: targetLevel,
		RequiredItemQuantity: uint16(currentLevel - targetLevel), RefundSkillPoints: refund,
		ForbiddenSkillIDs: forbidden,
		Paid:              paid,
	}, nil
}

func restorationItemPolicy(item model.RefItem) (bool, error) {
	switch item.CodeName {
	case model.SkillRestorationMallItem, model.SkillRestorationQuestItem:
		return false, nil
	case model.SkillRestorationPaidItem:
		return true, nil
	default:
		return false, fmt.Errorf("%w: item %d", model.ErrSkillRestorationInvalid, item.ID)
	}
}

func restorationGoldCost(levels *model.LevelTable, maxLevel int, masteryLevels []byte) (uint64, error) {
	if levels == nil {
		return 0, model.ErrSkillRestorationPaidPath
	}
	row, exists := levels.Level(maxLevel)
	if !exists || row.DropGoldMin == 0 || len(masteryLevels) == 0 {
		return 0, model.ErrSkillRestorationPaidPath
	}
	return restorationGoldCostForMinimum(row.DropGoldMin, masteryLevels)
}

func restorationGoldCostForMinimum(dropGoldMin uint64, masteryLevels []byte) (uint64, error) {
	if dropGoldMin == 0 || len(masteryLevels) == 0 {
		return 0, model.ErrSkillRestorationPaidPath
	}
	total := uint64(0)
	for _, masteryLevel := range masteryLevels {
		factor := uint64(10) * uint64(10+masteryLevel)
		if dropGoldMin > ^uint64(0)/factor || total > ^uint64(0)-dropGoldMin*factor {
			return 0, model.ErrSkillRestorationInvalid
		}
		total += dropGoldMin * factor
	}
	return total, nil
}

func playerOwnsSkill(owned []model.CharacterSkill, skillID uint32) bool {
	for _, skill := range owned {
		if skill.ID == skillID {
			return true
		}
	}
	return false
}

func skillWithdrawalDependencies(catalog *model.ReferenceDataCatalog, withdrawn model.RefSkill, targetLevel byte, currentSkillID uint32) []uint32 {
	var dependencies []uint32
	for _, skill := range catalog.SkillSnapshot() {
		if skill.ID == currentSkillID {
			continue
		}
		requiredIDs := [3]uint32{skill.RequiredSkill1, skill.RequiredSkill2, skill.RequiredSkill3}
		requiredLevels := [3]byte{skill.RequiredLevel1, skill.RequiredLevel2, skill.RequiredLevel3}
		for index, requiredID := range requiredIDs {
			required, ok := resolveSkillRequirement(catalog, requiredID)
			if requiredID == 0 || !ok || !required.SameLine(withdrawn) {
				continue
			}
			minimum := requiredLevels[index]
			if minimum == 0 {
				minimum = required.BasicLevel
			}
			if minimum > targetLevel {
				dependencies = append(dependencies, skill.ID)
				break
			}
		}
	}
	return dependencies
}

func masteryWithdrawalDependencies(catalog *model.ReferenceDataCatalog, masteryID uint32, targetLevel byte) []uint32 {
	var dependencies []uint32
	for _, skill := range catalog.SkillSnapshot() {
		if skill.Mastery1 == masteryID && skill.MasteryLevel1 > targetLevel || skill.Mastery2 == masteryID && skill.MasteryLevel2 > targetLevel {
			dependencies = append(dependencies, skill.ID)
		}
	}
	return dependencies
}

func ownsAnySkill(owned []model.CharacterSkill, ids []uint32) bool {
	ownedSet := make(map[uint32]struct{}, len(owned))
	for _, skill := range owned {
		ownedSet[skill.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, exists := ownedSet[id]; exists {
			return true
		}
	}
	return false
}
