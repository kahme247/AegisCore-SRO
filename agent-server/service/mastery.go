package service

import (
	"context"

	"github.com/ferdoran/go-sro/agent-server/model"
)

// MasteryUseCase owns mastery rules and persistence. The Agent packet handler
// remains responsible only for request shape, player/session resolution, error
// code mapping, and response encoding.
type MasteryUseCase interface {
	IncreaseMastery(context.Context, *model.Player, uint32) (model.MasteryIncreaseResult, error)
}

type MasteryService struct {
	repository model.MasteryRepository
	levels     *model.LevelTable
}

func NewMasteryService(repository model.MasteryRepository, levels *model.LevelTable) *MasteryService {
	return &MasteryService{repository: repository, levels: levels}
}

func (s *MasteryService) IncreaseMastery(ctx context.Context, player *model.Player, masteryID uint32) (model.MasteryIncreaseResult, error) {
	if s == nil || s.repository == nil || s.levels == nil || player == nil || player.ID <= 0 {
		return model.MasteryIncreaseResult{}, model.ErrMasteryConflict
	}
	currentLevel, allowed := player.MasteryLevel(masteryID)
	if !allowed || currentLevel == ^byte(0) {
		return model.MasteryIncreaseResult{}, model.ErrMasteryInvalid
	}
	nextLevel := currentLevel + 1
	if _, exists := s.levels.Level(int(nextLevel)); !exists {
		return model.MasteryIncreaseResult{}, model.ErrMasteryLevelLimit
	}
	levelData, exists := s.levels.Level(int(nextLevel))
	if !exists {
		return model.MasteryIncreaseResult{}, model.ErrMasteryLevelLimit
	}
	cost := levelData.RequiredSkillPoints
	totalLimit := characterMasteryTotalLimit(player)
	if totalLimit == 0 {
		return model.MasteryIncreaseResult{}, model.ErrMasteryInvalid
	}
	result, err := s.repository.IncreaseMastery(ctx, player.ID, masteryID, currentLevel, nextLevel, totalLimit, cost)
	if err != nil {
		return model.MasteryIncreaseResult{}, err
	}
	player.CommitMastery(result)
	return result, nil
}

func characterMasteryTotalLimit(player *model.Player) int {
	if player == nil || player.Level <= 0 {
		return 0
	}
	if player.IsChinese() {
		return 330
	}
	if player.IsEuropean() {
		return 2 * player.Level
	}
	return 0
}
