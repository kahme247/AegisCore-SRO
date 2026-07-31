package lobby

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type lobbyCooldownSnapshotFake struct {
	characterID int
	kind        model.CharacterCooldownKind
	states      []model.CharacterCooldownState
}

func (f *lobbyCooldownSnapshotFake) ActiveCooldowns(_ context.Context, characterID int, kind model.CharacterCooldownKind) ([]model.CharacterCooldownState, error) {
	f.characterID, f.kind = characterID, kind
	return f.states, nil
}

func TestCooldownBootstrapRestoresRemainingItemReuseDelay(t *testing.T) {
	packet, err := buildCooldownBootstrap([]model.CharacterCooldownState{
		{SourceID: 35883, Remaining: 90*time.Minute + 500*time.Microsecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 0x2B, 0x8C, 0, 0, 0xC1, 0x65, 0x52, 0, 0}
	if packet.MessageID != 0x3077 || !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("cooldown bootstrap opcode=0x%04X payload=%x, want 0x3077/%x", packet.MessageID, packet.Payload(), want)
	}
}

func TestCharacterJoinLoadsActiveItemCooldowns(t *testing.T) {
	repository := &lobbyCooldownSnapshotFake{states: []model.CharacterCooldownState{{SourceID: 35883, Remaining: time.Hour}}}
	handler := &CharacterSelectionHandler{cooldowns: repository}
	player := &model.Player{ID: 77}
	states, err := handler.activeItemCooldowns(context.Background(), player)
	if err != nil {
		t.Fatal(err)
	}
	if repository.characterID != player.ID || repository.kind != model.CharacterCooldownItem || !reflect.DeepEqual(states, repository.states) {
		t.Fatalf("cooldown load character=%d kind=%d states=%+v", repository.characterID, repository.kind, states)
	}
}
