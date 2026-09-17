package service

import (
	"context"
	"math/rand"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/utils"
)

// ItemUseCase is the gameplay boundary consumed by the packet handler.
// It owns lock ordering, repository execution, and publication of the committed
// player snapshot; packet decoding and response encoding remain in the handler.
type ItemUseCase interface {
	UseItem(context.Context, *model.Player, byte, uint16, byte, uint32) (model.ItemUseResult, error)
}

type DimensionHoleItemUseCase interface {
	UseDimensionHoleItem(context.Context, *model.Player, byte, uint16, uint32) (model.ItemUseResult, error)
}

// TargetedItemUseCase is an optional extension used by COS item families
// whose 0x704C request carries an object target.
type TargetedItemUseCase interface {
	UseTargetedItem(context.Context, *model.Player, byte, uint16, byte, uint32, model.COSItemTarget) (model.ItemUseResult, error)
}

type ItemUseService struct {
	repository         model.ItemUseRepository
	itemCureNow        func() time.Time
	itemCureRandomIntn func(int) int
}

func NewItemUseService(repository model.ItemUseRepository) *ItemUseService {
	return &ItemUseService{repository: repository, itemCureNow: time.Now, itemCureRandomIntn: rand.Intn}
}

// ConfigureItemCureRuntime replaces cure-only clock/random dependencies. It
// exists for deterministic packet-handler tests; production uses time.Now and
// math/rand.Intn.
func (s *ItemUseService) ConfigureItemCureRuntime(now func() time.Time, randomIntn func(int) int) {
	if s == nil {
		return
	}
	if now != nil {
		s.itemCureNow = now
	}
	if randomIntn != nil {
		s.itemCureRandomIntn = randomIntn
	}
}

func (s *ItemUseService) UseItem(ctx context.Context, player *model.Player, slot byte, itemType uint16, reverseOption byte, reverseMapID uint32) (model.ItemUseResult, error) {
	return s.useItem(ctx, player, slot, itemType, reverseOption, reverseMapID, 0)
}

func (s *ItemUseService) UseDimensionHoleItem(ctx context.Context, player *model.Player, slot byte, itemType uint16, preparedItemRefID uint32) (model.ItemUseResult, error) {
	if preparedItemRefID == 0 {
		return model.ItemUseResult{}, model.ErrItemUseInvalidRequest
	}
	return s.useItem(ctx, player, slot, itemType, 0, 0, preparedItemRefID)
}

func (s *ItemUseService) useItem(ctx context.Context, player *model.Player, slot byte, itemType uint16, reverseOption byte, reverseMapID uint32, preparedDimensionHoleRefID uint32) (model.ItemUseResult, error) {
	if s == nil || s.repository == nil || player == nil || player.ID <= 0 {
		return model.ItemUseResult{}, model.ErrItemUseInvalidRequest
	}
	player.LockEconomy()
	defer player.UnlockEconomy()

	// Combat changes HP concurrently with packet handling. Preserve the
	// existing lock order and hold the player lock through the transaction so a
	// potion cannot publish a stale pre-damage snapshot.
	player.Mutex.Lock()
	maxHP := utils.BaseHPOrMP(player.Level, player.BaseStats.Str+player.BonusStats.Str) + player.BonusStats.HP
	maxMP := utils.BaseHPOrMP(player.Level, player.BaseStats.Int+player.BonusStats.Int) + player.BonusStats.MP
	maxHP += maxHP * player.BonusStats.HpPercent / 100
	maxMP += maxMP * player.BonusStats.MpPercent / 100
	vitals := model.ItemUseVitals{
		HP: player.BaseStats.HP, MP: player.BaseStats.MP,
		MaxHP:                          maxHP,
		MaxMP:                          maxMP,
		Level:                          player.Level,
		ReverseReturnOption:            reverseOption,
		ReverseReturnMapID:             reverseMapID,
		DimensionHolePreparedItemRefID: preparedDimensionHoleRefID,
	}
	result, err := s.repository.UseItem(ctx, int64(player.ID), slot, itemType, vitals)
	if err != nil {
		player.Mutex.Unlock()
		return model.ItemUseResult{}, err
	}
	player.BaseStats.HP = result.HP
	player.BaseStats.MP = result.MP
	player.Inventory = result.Inventory.Snapshot()
	s.applyCommittedItemCureLocked(player, &result)
	player.Mutex.Unlock()
	if result.ItemService != nil {
		result.ItemServiceToken, result.ReplacedItemServiceToken = player.ApplyItemService(*result.ItemService)
	}
	return result, nil
}

func (s *ItemUseService) UseTargetedItem(ctx context.Context, player *model.Player, slot byte, itemType uint16, reverseOption byte, reverseMapID uint32, target model.COSItemTarget) (model.ItemUseResult, error) {
	if s == nil || s.repository == nil || player == nil || player.ID <= 0 || target.ObjectID == 0 {
		return model.ItemUseResult{}, model.ErrItemUseInvalidRequest
	}
	targeted, ok := s.repository.(model.TargetedItemUseRepository)
	if !ok {
		return model.ItemUseResult{}, model.ErrItemUseUnsupported
	}
	player.LockEconomy()
	defer player.UnlockEconomy()
	player.Mutex.Lock()
	maxHP := utils.BaseHPOrMP(player.Level, player.BaseStats.Str+player.BonusStats.Str) + player.BonusStats.HP
	maxMP := utils.BaseHPOrMP(player.Level, player.BaseStats.Int+player.BonusStats.Int) + player.BonusStats.MP
	maxHP += maxHP * player.BonusStats.HpPercent / 100
	maxMP += maxMP * player.BonusStats.MpPercent / 100
	vitals := model.ItemUseVitals{HP: player.BaseStats.HP, MP: player.BaseStats.MP, MaxHP: maxHP, MaxMP: maxMP, Level: player.Level, ReverseReturnOption: reverseOption, ReverseReturnMapID: reverseMapID}
	result, err := targeted.UseTargetedItem(ctx, int64(player.ID), slot, itemType, vitals, target)
	if err != nil {
		player.Mutex.Unlock()
		return model.ItemUseResult{}, err
	}
	player.BaseStats.HP, player.BaseStats.MP = result.HP, result.MP
	player.Inventory = result.Inventory.Snapshot()
	s.applyCommittedItemCureLocked(player, &result)
	player.Mutex.Unlock()
	return result, nil
}

func (s *ItemUseService) applyCommittedItemCureLocked(player *model.Player, result *model.ItemUseResult) {
	if s == nil || player == nil || result == nil || result.ItemCure == nil {
		return
	}
	now := time.Now()
	if s.itemCureNow != nil {
		now = s.itemCureNow()
		if now.IsZero() {
			now = time.Now()
		}
	}
	randomIntn := rand.Intn
	if s.itemCureRandomIntn != nil {
		randomIntn = s.itemCureRandomIntn
	}
	result.ItemCureAppliedAt = now
	result.ItemCureApplication = player.ApplyItemCureLocked(*result.ItemCure, now, randomIntn)
}
