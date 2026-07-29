package lobby

import (
	"context"
	"errors"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
)

type fixedMovementSettingsRepository struct {
	settings model.MovementSettings
	err      error
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
