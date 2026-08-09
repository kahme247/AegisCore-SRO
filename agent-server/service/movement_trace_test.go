package service

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestConfiguredMovementTraceModeAliases(t *testing.T) {
	tests := []struct {
		value string
		want  movementTraceMode
	}{
		{value: "", want: movementTraceOff},
		{value: "0", want: movementTraceOff},
		{value: "off", want: movementTraceOff},
		{value: "1", want: movementTraceAll},
		{value: "all", want: movementTraceAll},
		{value: " EVERYTHING ", want: movementTraceAll},
		{value: "mob", want: movementTraceMobs},
		{value: "mobs", want: movementTraceMobs},
		{value: "player", want: movementTracePlayers},
		{value: "players", want: movementTracePlayers},
		{value: "char", want: movementTracePlayers},
		{value: "characters", want: movementTracePlayers},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("SRO_MOVEMENT_TRACE", test.value)
			if got := configuredMovementTraceMode(); got != test.want {
				t.Fatalf("configuredMovementTraceMode(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func TestMovementTraceModesFilterCharacterKinds(t *testing.T) {
	player := &model.Player{}
	mob := &model.NPC{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 1}}}
	serviceNPC := &model.NPC{SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 2}}}

	tests := []struct {
		mode                 string
		player, mob, service bool
	}{
		{mode: "off"},
		{mode: "players", player: true},
		{mode: "mobs", mob: true},
		{mode: "all", player: true, mob: true, service: true},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			t.Setenv("SRO_MOVEMENT_TRACE", test.mode)
			if got := movementTraceAllows(player); got != test.player {
				t.Fatalf("player allowed = %t, want %t", got, test.player)
			}
			if got := movementTraceAllows(mob); got != test.mob {
				t.Fatalf("mob allowed = %t, want %t", got, test.mob)
			}
			if got := movementTraceAllows(serviceNPC); got != test.service {
				t.Fatalf("service NPC allowed = %t, want %t", got, test.service)
			}
		})
	}
}
