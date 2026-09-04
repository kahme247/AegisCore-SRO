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

const COSPolicyRefreshInterval = 5 * time.Second

type COSPolicyManager struct {
	repository model.COSPolicyRepository
	interval   time.Duration
	current    atomic.Pointer[model.COSPolicySnapshot]
	startOnce  sync.Once
	started    atomic.Bool
	done       chan struct{}
}

func NewCOSPolicyManager(repository model.COSPolicyRepository, interval time.Duration) (*COSPolicyManager, error) {
	if repository == nil {
		return nil, fmt.Errorf("COS policy repository is required")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("COS policy refresh interval must be positive")
	}
	manager := &COSPolicyManager{repository: repository, interval: interval, done: make(chan struct{})}
	defaults := model.DefaultCOSPolicySnapshot()
	manager.current.Store(&defaults)
	return manager, nil
}

func (m *COSPolicyManager) Current() model.COSPolicySnapshot {
	if m == nil {
		return model.DefaultCOSPolicySnapshot()
	}
	current := m.current.Load()
	if current == nil {
		return model.DefaultCOSPolicySnapshot()
	}
	return *current
}

func (m *COSPolicyManager) Refresh(ctx context.Context) error {
	if m == nil || m.repository == nil {
		return model.ErrCOSPolicyRepositoryUnavailable
	}
	policy, err := m.repository.COSPolicy(ctx)
	if err != nil {
		return err
	}
	if !policy.Valid() {
		return model.ErrCOSPolicyInvalid
	}
	previous := m.Current()
	m.current.Store(&policy)
	if policy != previous {
		log.Infof("COS gameplay policy refreshed growth-owner-speed=%t ability-owner-speed=%t follow-radius=%.1f pickup-range=%.1f combat-leash=%.1f", policy.ForKind(model.COSGrowthPet).InheritOwnerSpeed, policy.ForKind(model.COSAbilityPet).InheritOwnerSpeed, policy.Settings.FollowBaseRadius, policy.Settings.PickupCollectionRange, policy.Settings.CombatLeashRange)
	}
	return nil
}

func (m *COSPolicyManager) Start(ctx context.Context) {
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
				log.Warnf("COS policy refresh failed; retaining cached policy: %v", err)
			}
			ticker := time.NewTicker(m.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := m.Refresh(ctx); err != nil {
						log.Warnf("COS policy refresh failed; retaining cached policy: %v", err)
					}
				}
			}
		}()
	})
}

func (m *COSPolicyManager) Wait(ctx context.Context) error {
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
