package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type gameplayRateRepositoryFake struct {
	mu    sync.Mutex
	rates model.GameplayRates
	err   error
}

func (f *gameplayRateRepositoryFake) GameplayRates(context.Context) (model.GameplayRates, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rates, f.err
}

func TestGameplayRateManagerRefreshesAtomicallyAndRetainsLastGoodRates(t *testing.T) {
	repository := &gameplayRateRepositoryFake{rates: model.GameplayRates{ExperienceRatePercent: 200, SkillExperienceRatePercent: 300, MobCountRatePercent: 400, MobRespawnRatePercent: 500}}
	manager, err := NewGameplayRateManager(repository, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.ExperienceRatePercent != 100 || got.SkillExperienceRatePercent != 100 || got.MobCountRatePercent != 100 || got.MobRespawnRatePercent != 100 {
		t.Fatalf("hardcoded safe rates = %#v", got)
	}
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := manager.Current(); got.ExperienceRatePercent != 200 || got.SkillExperienceRatePercent != 300 || got.MobCountRatePercent != 400 || got.MobRespawnRatePercent != 500 {
		t.Fatalf("refreshed rates = %#v", got)
	}
	repository.mu.Lock()
	repository.err = errors.New("database unavailable")
	repository.rates = model.GameplayRates{ExperienceRatePercent: 1, SkillExperienceRatePercent: 1}
	repository.mu.Unlock()
	if err := manager.Refresh(context.Background()); err == nil {
		t.Fatal("failed refresh returned nil")
	}
	if got := manager.Current(); got.ExperienceRatePercent != 200 || got.SkillExperienceRatePercent != 300 || got.MobCountRatePercent != 400 || got.MobRespawnRatePercent != 500 {
		t.Fatalf("rates after failed refresh = %#v", got)
	}
}

func TestGameplayRateManagerStopsWithContext(t *testing.T) {
	repository := &gameplayRateRepositoryFake{rates: model.GameplayRates{ExperienceRatePercent: 100, SkillExperienceRatePercent: 100}}
	manager, err := NewGameplayRateManager(repository, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := manager.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
}
