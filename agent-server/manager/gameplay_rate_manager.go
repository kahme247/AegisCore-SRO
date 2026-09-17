package manager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	log "github.com/sirupsen/logrus"
)

const GameplayRateRefreshInterval = 5 * time.Second

var safeGameplayRates = model.GameplayRates{
	ExperienceRatePercent:      100,
	PartyExperienceRatePercent: 100,
	SkillExperienceRatePercent: 100,
	MobCountRatePercent:        100,
	MobRespawnRatePercent:      100,
	LootDropRatePercent:        100,
	GoldDropAmountRatePercent:  100,
	PartyExperienceRange:       100,
	GroundPickupRange:          160,
	JobSuitEquipDelayMS:        30000,
	PartyInvitationTTLMS:       10000,
	MobAttackCooldownMS:        1500,
	MobWanderDelayMS:           8000,
	MobAggroReactionMinMS:      750,
	MobAggroReactionMaxMS:      1500,
	MobPursuitRefreshMS:        1000,
	GuildCreationCost:          500000,
	AcademyCreationCost:        100000,
	AcademyMinimumLevel:        60,
	GuildWarMaximumWager:       500000000,
	MarketLifetimeMinutes:      4320,
	CharacterDeletionHours:     168,
	RecallConfirmationTTLMS:    60000,
	GuildElectionDurationMin:   10,
	GuildMasterAbsenceDays:     45,
	GuildRecentMemberDays:      15,
	FriendLimit:                20,
	BlockLimit:                 20,
	MemoLimit:                  20,
	PKPenaltyPerKill:           10000,
	BerserkDurationPerPointMS:  12000,
	WinterDropChanceBP:         5000,
	WinterDropItemCount:        1,
	BattleArenaTicketRate:      0,
	BattleArenaWorldIDs:        "42,43,44,45,46,47,48,49,50",
	HwanBaseChanceBP:           800,
	HwanOverlevelGrace:         5,
	HwanPenaltyPerLevelBP:      25,
	HwanMinimumChanceBP:        25,
	HwanGiantMultiplier:        5,
	HwanEliteMultiplier:        2,
	ServerMaxLevel:             110,
	EUMasteryMaxTotal:          240,
	EUMasteryPointsPerLevel:    2,
	CHMasteryMaxTotal:          330,
	CHMasteryPointsPerLevel:    3,
	MaxSpeedEffectMinutes:      1440,
}

type GameplayRateManager struct {
	repository model.GameplayRateRepository
	interval   time.Duration
	current    atomic.Pointer[model.GameplayRates]
	startOnce  sync.Once
	started    atomic.Bool
	done       chan struct{}
}

func NewGameplayRateManager(repository model.GameplayRateRepository, interval time.Duration) (*GameplayRateManager, error) {
	if repository == nil {
		return nil, fmt.Errorf("gameplay rate repository is required")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("gameplay rate refresh interval must be positive")
	}
	manager := &GameplayRateManager{repository: repository, interval: interval, done: make(chan struct{})}
	manager.current.Store(&safeGameplayRates)
	model.StoreRuntimeGameplayPolicy(safeGameplayRates)
	return manager, nil
}

func (m *GameplayRateManager) Current() model.GameplayRates {
	if m == nil {
		return model.GameplayRates{}
	}
	current := m.current.Load()
	if current == nil {
		return model.GameplayRates{}
	}
	return *current
}

func (m *GameplayRateManager) Refresh(ctx context.Context) error {
	if m == nil || m.repository == nil {
		return model.ErrGameplayRateRepositoryUnavailable
	}
	rates, err := m.repository.GameplayRates(ctx)
	if err != nil {
		return err
	}
	previous := m.Current()
	m.current.Store(&rates)
	model.StoreRuntimeGameplayPolicy(rates)
	if rates != previous {
		log.Infof("gameplay rates refreshed experience=%d%% party-experience=%d%% skill-experience=%d%% mob-count=%d%% mob-respawn=%d%% loot-drop=%d%% gold-drop-amount=%d%%", rates.ExperienceRatePercent, rates.PartyExperienceRatePercent, rates.SkillExperienceRatePercent, rates.MobCountRatePercent, rates.MobRespawnRatePercent, rates.LootDropRatePercent, rates.GoldDropAmountRatePercent)
	}
	return nil
}

func (m *GameplayRateManager) ActivateOverride(ctx context.Context, eventID string, rates model.GameplayRateOverride) error {
	repository, ok := m.repository.(model.GameplayRateOverrideRepository)
	if !ok {
		return model.ErrGameplayRateRepositoryUnavailable
	}
	if err := repository.ActivateRateOverride(ctx, eventID, rates); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

func (m *GameplayRateManager) DeactivateOverride(ctx context.Context, eventID string) error {
	repository, ok := m.repository.(model.GameplayRateOverrideRepository)
	if !ok {
		return model.ErrGameplayRateRepositoryUnavailable
	}
	if err := repository.DeactivateRateOverride(ctx, eventID); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

func (m *GameplayRateManager) ActivateMobOverride(ctx context.Context, eventID string, rates model.GameplayMobRateOverride) error {
	repository, ok := m.repository.(model.GameplayRateOverrideRepository)
	if !ok {
		return model.ErrGameplayRateRepositoryUnavailable
	}
	if err := repository.ActivateMobRateOverride(ctx, eventID, rates); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

func (m *GameplayRateManager) DeactivateMobOverride(ctx context.Context, eventID string) error {
	repository, ok := m.repository.(model.GameplayRateOverrideRepository)
	if !ok {
		return model.ErrGameplayRateRepositoryUnavailable
	}
	if err := repository.DeactivateMobRateOverride(ctx, eventID); err != nil {
		return err
	}
	return m.Refresh(ctx)
}

func (m *GameplayRateManager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.startOnce.Do(func() {
		m.started.Store(true)
		go func() {
			defer close(m.done)
			if err := m.Refresh(ctx); err != nil {
				log.Warnf("gameplay rate refresh failed; retaining cached rates: %v", err)
			}
			ticker := time.NewTicker(m.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := m.Refresh(ctx); err != nil {
						log.Warnf("gameplay rate refresh failed; retaining cached rates: %v", err)
					}
				}
			}
		}()
	})
}

func (m *GameplayRateManager) Wait(ctx context.Context) error {
	if m == nil || !m.started.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
