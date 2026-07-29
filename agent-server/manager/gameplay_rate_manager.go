package manager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	log "github.com/sirupsen/logrus"
)

const GameplayRateRefreshInterval = 5 * time.Second

var safeGameplayRates = model.GameplayRates{
	ExperienceRatePercent:      100,
	SkillExperienceRatePercent: 100,
	MobCountRatePercent:        100,
	MobRespawnRatePercent:      100,
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
	if rates != previous {
		log.Infof("gameplay rates refreshed experience=%d%% skill-experience=%d%% mob-count=%d%% mob-respawn=%d%%", rates.ExperienceRatePercent, rates.SkillExperienceRatePercent, rates.MobCountRatePercent, rates.MobRespawnRatePercent)
	}
	return nil
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
