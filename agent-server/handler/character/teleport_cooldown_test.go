package character

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type teleportCooldownSnapshotFake struct {
	characterID int
	kind        model.CharacterCooldownKind
	states      []model.CharacterCooldownState
}

func (f *teleportCooldownSnapshotFake) ActiveCooldowns(_ context.Context, characterID int, kind model.CharacterCooldownKind) ([]model.CharacterCooldownState, error) {
	f.characterID, f.kind = characterID, kind
	return f.states, nil
}

func TestTeleportBootstrapReloadsActiveItemCooldowns(t *testing.T) {
	repository := &teleportCooldownSnapshotFake{states: []model.CharacterCooldownState{{SourceID: 35883, Remaining: 45 * time.Minute}}}
	handler := &TeleportHandler{cooldowns: repository}
	player := &model.Player{ID: 88}
	states, err := handler.activeItemCooldowns(context.Background(), player)
	if err != nil {
		t.Fatal(err)
	}
	if repository.characterID != player.ID || repository.kind != model.CharacterCooldownItem || !reflect.DeepEqual(states, repository.states) {
		t.Fatalf("cooldown load character=%d kind=%d states=%+v", repository.characterID, repository.kind, states)
	}
}
