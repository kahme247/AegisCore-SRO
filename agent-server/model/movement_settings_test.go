package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestMovementSettingsRepositoryLoadsSingleton(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectMovementSettings, catalogRepositoryTestResponse{
		columns: []string{"PLAYER_WALK_SPEED", "PLAYER_RUN_SPEED"},
		rows:    [][]driver.Value{{float64(16), float64(150)}},
	})

	repository, err := NewMovementSettingsRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatalf("NewMovementSettingsRepository: %v", err)
	}
	settings, err := repository.MovementSettings(context.Background())
	if err != nil {
		t.Fatalf("MovementSettings: %v", err)
	}
	if settings.PlayerWalkSpeed != 16 || settings.PlayerRunSpeed != 150 {
		t.Fatalf("settings = %#v, want walk 16 and run 150", settings)
	}
}

func TestMovementSettingsRepositoryRejectsMissingDependencyAndInvalidValues(t *testing.T) {
	if repository, err := NewMovementSettingsRepository(nil); repository != nil || !errors.Is(err, ErrMovementSettingsRepositoryUnavailable) {
		t.Fatalf("NewMovementSettingsRepository(nil) = (%v, %v), want unavailable error", repository, err)
	}

	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectMovementSettings, catalogRepositoryTestResponse{
		columns: []string{"PLAYER_WALK_SPEED", "PLAYER_RUN_SPEED"},
		rows:    [][]driver.Value{{float64(16), float64(0)}},
	})
	repository, err := NewMovementSettingsRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MovementSettings(context.Background()); !errors.Is(err, ErrMovementSettingsInvalid) {
		t.Fatalf("MovementSettings invalid error = %v, want %v", err, ErrMovementSettingsInvalid)
	}
}
