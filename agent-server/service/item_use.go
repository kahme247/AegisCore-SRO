package service

import (
	"context"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/utils"
)

// RecoveryItemUseCase is the gameplay boundary consumed by the packet handler.
// It owns lock ordering, repository execution, and publication of the committed
// player snapshot; packet decoding and response encoding remain in the handler.
type RecoveryItemUseCase interface {
	UseRecoveryItem(context.Context, *model.Player, byte, uint16) (model.ItemUseResult, error)
}

type ItemUseService struct {
	repository model.ItemUseRepository
}

func NewItemUseService(repository model.ItemUseRepository) *ItemUseService {
	return &ItemUseService{repository: repository}
}

func (s *ItemUseService) UseRecoveryItem(ctx context.Context, player *model.Player, slot byte, itemType uint16) (model.ItemUseResult, error) {
	if s == nil || s.repository == nil || player == nil || player.ID <= 0 {
		return model.ItemUseResult{}, model.ErrItemUseInvalidRequest
	}
	player.LockEconomy()
	defer player.UnlockEconomy()

	// Combat changes HP concurrently with packet handling. Preserve the
	// existing lock order and hold the player lock through the transaction so a
	// potion cannot publish a stale pre-damage snapshot.
	player.Mutex.Lock()
	vitals := model.ItemUseVitals{
		HP: player.BaseStats.HP, MP: player.BaseStats.MP,
		MaxHP: utils.BaseHPOrMP(player.Level, player.BaseStats.Str),
		MaxMP: utils.BaseHPOrMP(player.Level, player.BaseStats.Int),
	}
	result, err := s.repository.UseRecoveryItem(ctx, int64(player.ID), slot, itemType, vitals)
	if err != nil {
		player.Mutex.Unlock()
		return model.ItemUseResult{}, err
	}
	player.BaseStats.HP = result.HP
	player.BaseStats.MP = result.MP
	player.Inventory = result.Inventory.Snapshot()
	player.Mutex.Unlock()
	return result, nil
}
