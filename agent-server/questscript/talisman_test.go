package questscript

import (
	"errors"
	"testing"
)

func TestTalismanObjectivesDecodesRecoveredLayout(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"},
		{Kind: ValueNumber, Number: 0},
		{Kind: ValueGlobal, String: missionTypeTalismanCollection},
		{Kind: ValueString, String: "SN_CON_QNO_FW_BA1_001_1"},
		{Kind: ValueString, String: "NPC_KT_MINISTER"},
		{Kind: ValueString, String: "THEME_GOD_FLAME_BURNING_ABYSS"},
		{Kind: ValueNumber, Number: 1},
	}
	objectives, err := TalismanObjectives([]Definition{{
		CodeName: "QNO_FW_BA1_001", Variant: 0,
		Declarations: []Call{{Function: "LuaSetTalismanMissionData", Args: args}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(objectives) != 1 {
		t.Fatalf("objective count = %d, want 1", len(objectives))
	}
	objective := objectives[0]
	if objective.MissionIndex != 0 || objective.ObjectiveKey != "SN_CON_QNO_FW_BA1_001_1" ||
		objective.TargetNPCCode != "NPC_KT_MINISTER" || objective.ThemeCode != "THEME_GOD_FLAME_BURNING_ABYSS" ||
		objective.Parameter6 != 1 || len(objective.Parameters) != 7 {
		t.Fatalf("objective = %#v", objective)
	}
}

func TestTalismanObjectivesRejectsMalformedLayout(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: missionTypeTalismanCollection},
		{Kind: ValueString, String: "OBJECTIVE"}, {Kind: ValueString, String: "NPC"},
		{Kind: ValueString, String: "THEME"}, {Kind: ValueNumber, Number: 1.5},
	}
	_, err := TalismanObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetTalismanMissionData", Args: args}}}})
	if !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("error = %v, want ErrInvalidChunk", err)
	}
}
