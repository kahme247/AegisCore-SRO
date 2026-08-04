package lobby

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
)

type fixedMovementSettingsRepository struct {
	settings model.MovementSettings
	err      error
}

type fixedSpeedEffectRepository struct{ effect model.SpeedEffect }

func (r fixedSpeedEffectRepository) LoadSpeedEffect(context.Context, int64) (model.SpeedEffect, bool, error) {
	return r.effect, true, nil
}

func (r fixedMovementSettingsRepository) MovementSettings(context.Context) (model.MovementSettings, error) {
	return r.settings, r.err
}

func TestCharacterSelectionUsesInjectedMovementSettings(t *testing.T) {
	handler := NewCharacterSelectionHandler(nil)
	if err := handler.ConfigureMovementSettingsRepository(fixedMovementSettingsRepository{
		settings: model.MovementSettings{PlayerWalkSpeed: 16, PlayerRunSpeed: 150},
	}); err != nil {
		t.Fatal(err)
	}

	settings, err := handler.playerMovementSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.PlayerWalkSpeed != 16 || settings.PlayerRunSpeed != 150 {
		t.Fatalf("settings = %#v, want database-backed 16/150", settings)
	}
}

func TestCharacterSelectionMovementSettingsCompatibilityAndErrors(t *testing.T) {
	handler := NewCharacterSelectionHandler(nil)
	settings, err := handler.playerMovementSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.PlayerWalkSpeed != legacyPlayerWalkSpeed || settings.PlayerRunSpeed != legacyPlayerRunSpeed {
		t.Fatalf("legacy settings = %#v, want %v/%v", settings, legacyPlayerWalkSpeed, legacyPlayerRunSpeed)
	}

	want := errors.New("movement settings unavailable")
	handler.movement = fixedMovementSettingsRepository{err: want}
	if _, err := handler.playerMovementSettings(context.Background()); !errors.Is(err, want) {
		t.Fatalf("playerMovementSettings error = %v, want %v", err, want)
	}
	if err := (*CharacterSelectionHandler)(nil).ConfigureMovementSettingsRepository(fixedMovementSettingsRepository{}); !errors.Is(err, model.ErrMovementSettingsRepositoryUnavailable) {
		t.Fatalf("nil handler configure error = %v", err)
	}
}

func TestCharacterJoinRestoresPersistedSpeedEffect(t *testing.T) {
	expiresAt := time.Now().Add(5 * time.Hour)
	handler := &CharacterSelectionHandler{speedEffects: fixedSpeedEffectRepository{effect: model.SpeedEffect{
		SkillID: 3977, Percent: 100, ExpiresAt: expiresAt,
	}}}
	player := &model.Player{ID: 7, RunSpeed: 30}
	if err := handler.loadPlayerSpeedEffect(context.Background(), player); err != nil {
		t.Fatalf("loadPlayerSpeedEffect() error = %v", err)
	}
	skillID, percent, restoredExpiration, _, token, exists := player.MovementSpeedEffect()
	if !exists || skillID != 3977 || percent != 100 || token == 0 || !restoredExpiration.Equal(expiresAt) {
		t.Fatalf("restored speed effect = skill %d percent %d expiration %v token %d exists %v", skillID, percent, restoredExpiration, token, exists)
	}
	if got := player.GetMovementSpeed(); got != 60 {
		t.Fatalf("restored movement speed = %f, want 60", got)
	}
}
