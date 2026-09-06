package service

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestCharacterMasteryTotalLimitMatchesRaceRules(t *testing.T) {
	tests := []struct {
		name   string
		player *model.Player
		want   int
	}{
		{name: "chinese", player: masteryLimitPlayer(1907, 120), want: 330},
		{name: "european below global cap", player: masteryLimitPlayer(14875, 80), want: 160},
		{name: "european at global cap", player: masteryLimitPlayer(14875, 120), want: 240},
		{name: "european above global cap", player: masteryLimitPlayer(14875, 140), want: 240},
		{name: "unknown race", player: masteryLimitPlayer(1, 120), want: 0},
		{name: "zero level", player: masteryLimitPlayer(1907, 0), want: 0},
		{name: "nil player", player: nil, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := characterMasteryTotalLimit(test.player); got != test.want {
				t.Fatalf("characterMasteryTotalLimit() = %d, want %d", got, test.want)
			}
		})
	}
}

func masteryLimitPlayer(refObjectID uint32, level int) *model.Player {
	return &model.Player{SRObject: model.SRObject{RefObjectID: refObjectID}, Level: level}
}
