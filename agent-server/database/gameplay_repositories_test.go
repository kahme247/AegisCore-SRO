package database

import (
	"strings"
	"testing"
)

func TestNewGameplayRepositoriesRejectsMissingPoolsWithoutPartialSuccess(t *testing.T) {
	repositories, err := NewGameplayRepositories(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "configure Agent") {
		t.Fatalf("NewGameplayRepositories() error = %v, want repository configuration failure", err)
	}
	if repositories.ReferenceData != nil || repositories.Characters != nil || repositories.Inventory != nil {
		t.Fatal("failed repository composition published a partial set")
	}
}
