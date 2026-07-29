package lobby

import (
	"context"
	"errors"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
)

type characterSelectionRepositoryContextKey struct{}

type characterSelectionRepositoryFake struct {
	lookupContext context.Context
	listContext   context.Context
	deleteContext context.Context
}

func (f *characterSelectionRepositoryFake) GetCharacterForOwner(ctx context.Context, _ string, _ uint32, _ uint16) (model.Char, error) {
	f.lookupContext = ctx
	return model.Char{ID: 1, User: 42, Shard: 1}, nil
}

func (f *characterSelectionRepositoryFake) GetCharactersForOwner(ctx context.Context, _ uint32, _ uint16) ([]model.Char, error) {
	f.listContext = ctx
	return []model.Char{{ID: 1, User: 42, Shard: 1}}, nil
}

func (f *characterSelectionRepositoryFake) SetCharacterDeletionForOwner(ctx context.Context, _ string, _ uint32, _ uint16, _ bool) (bool, error) {
	f.deleteContext = ctx
	return true, nil
}

func (f *characterSelectionRepositoryFake) CharacterNameExists(ctx context.Context, _ string) (bool, error) {
	f.lookupContext = ctx
	return false, nil
}

type characterSelectionInventoryRepositoryFake struct {
	context context.Context
}

func (f *characterSelectionInventoryRepositoryFake) GetCharacterInventory(ctx context.Context, _ int64) (model.Inventory, error) {
	f.context = ctx
	return model.Inventory{Items: map[byte]model.Item{}}, nil
}

func (f *characterSelectionInventoryRepositoryFake) AddItemToInventory(context.Context, int64, int64, byte) error {
	return nil
}

func TestCharacterSelectionRepositoryHandlerUsesInjectedRepositoriesAndContext(t *testing.T) {
	characters := &characterSelectionRepositoryFake{}
	inventory := &characterSelectionInventoryRepositoryFake{}
	handler, err := NewCharacterSelectionHandlerWithRepositories(characters, inventory)
	if err != nil {
		t.Fatalf("NewCharacterSelectionHandlerWithRepositories() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), characterSelectionRepositoryContextKey{}, "request")
	if _, err := handler.characters.CharacterForOwner(ctx, "Alice", 42, 1); err != nil {
		t.Fatalf("CharacterForOwner() error = %v", err)
	}
	if _, err := handler.characters.CharactersForOwner(ctx, 42, 1); err != nil {
		t.Fatalf("CharactersForOwner() error = %v", err)
	}
	if updated, err := handler.characters.SetDeletionForOwner(ctx, "Alice", 42, 1, true); err != nil || !updated {
		t.Fatalf("SetDeletionForOwner() = (%t, %v), want (true, nil)", updated, err)
	}
	if _, err := handler.loadPlayerInventory(ctx, 1); err != nil {
		t.Fatalf("loadPlayerInventory() error = %v", err)
	}

	for name, received := range map[string]context.Context{
		"lookup":    characters.lookupContext,
		"list":      characters.listContext,
		"deletion":  characters.deleteContext,
		"inventory": inventory.context,
	} {
		if received == nil || received.Value(characterSelectionRepositoryContextKey{}) != "request" {
			t.Fatalf("%s repository did not receive the handler request context", name)
		}
	}
}

func TestCharacterSelectionRepositoryHandlerFailsClosedForMissingDependencies(t *testing.T) {
	inventory := &characterSelectionInventoryRepositoryFake{}
	if handler, err := NewCharacterSelectionHandlerWithRepositories(nil, inventory); handler != nil || !errors.Is(err, model.ErrCharacterRepositoryUnavailable) {
		t.Fatalf("missing character repository = (%v, %v), want ErrCharacterRepositoryUnavailable", handler, err)
	}
	characters := &characterSelectionRepositoryFake{}
	if handler, err := NewCharacterSelectionHandlerWithRepositories(characters, nil); handler != nil || !errors.Is(err, model.ErrInventoryRepositoryUnavailable) {
		t.Fatalf("missing inventory repository = (%v, %v), want ErrInventoryRepositoryUnavailable", handler, err)
	}
}
