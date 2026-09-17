package character

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestApplyCharacterMoveStateMapsRecoveredClientActions(t *testing.T) {
	player := &model.Player{}
	tests := []struct {
		wire byte
		want model.MotionState
	}{
		{vsro188.CharacterMoveStateWalk, model.Walking},
		{vsro188.CharacterMoveStateRun, model.Running},
		{vsro188.CharacterMoveStateSit, model.Sitting},
	}
	for _, test := range tests {
		if !applyCharacterMoveState(player, test.wire) || player.GetMotionState() != test.want {
			t.Fatalf("wire %d motion = %d, want %d", test.wire, player.GetMotionState(), test.want)
		}
	}
	if applyCharacterMoveState(player, 1) {
		t.Fatal("accepted unsupported move state")
	}
}

func TestApplyCharacterMoveStateActionFourTogglesSitAndStand(t *testing.T) {
	player := &model.Player{MotionState: model.Running}
	if !applyCharacterMoveState(player, vsro188.CharacterMoveStateSit) || player.GetMotionState() != model.Sitting {
		t.Fatalf("first action 4 motion = %d, want sitting", player.GetMotionState())
	}
	if !applyCharacterMoveState(player, vsro188.CharacterMoveStateSit) || player.GetMotionState() != model.Stand {
		t.Fatalf("second action 4 motion = %d, want standing", player.GetMotionState())
	}
}

func TestResumePlayerFromSitRestoresRunningState(t *testing.T) {
	player := &model.Player{MotionState: model.Sitting}
	handler := &MovementHandler{}
	if !handler.resumePlayerFromSit(player) {
		t.Fatal("sitting player was not resumed")
	}
	if player.GetMotionState() != model.Running {
		t.Fatalf("motion state = %d, want running", player.GetMotionState())
	}
	if handler.resumePlayerFromSit(player) {
		t.Fatal("running player was resumed twice")
	}
}
