package questscript

import (
	"errors"
	"testing"
)

func TestKillMonsterObjectivesRecoversExactPositionalContract(t *testing.T) {
	definitions := []Definition{{CodeName: "QNO_CH_SMITH_2", Variant: 0, Declarations: []Call{{
		Function: "LuaSetMissionData",
		Args: []Value{
			{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber, Number: 0},
			{Kind: ValueGlobal, String: "MISSION_TYPE_KILL_MONSTER"}, {Kind: ValueString, String: "SN_CON_QNO_CH_SMITH_2"},
			{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "MOB_CH_BANDITARCHER_CLON"},
			{Kind: ValueNumber, Number: 0}, {Kind: ValueGlobal, String: "MONSTER_CLASS_NORMAL"},
			{Kind: ValueNumber, Number: 140}, {Kind: ValueNumber, Number: 0},
		},
	}}}}
	objectives, err := KillMonsterObjectives(definitions)
	if err != nil {
		t.Fatalf("KillMonsterObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].TargetCode != "MOB_CH_BANDITARCHER_CLON" || objectives[0].RequiredCount != 140 || objectives[0].ObjectiveKey != "SN_CON_QNO_CH_SMITH_2" {
		t.Fatalf("objectives = %#v", objectives)
	}
}

func TestKillMonsterObjectivesRejectsFractionalGoal(t *testing.T) {
	definitions := []Definition{{CodeName: "QNO_BAD", Declarations: []Call{{
		Function: "LuaSetMissionData",
		Args: []Value{
			{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
			{Kind: ValueGlobal, String: "MISSION_TYPE_KILL_MONSTER"}, {Kind: ValueString, String: "OBJECTIVE"},
			{Kind: ValueNumber}, {Kind: ValueString, String: "MOB"}, {Kind: ValueNumber},
			{Kind: ValueGlobal, String: "MONSTER_CLASS_NORMAL"}, {Kind: ValueNumber, Number: 1.5}, {Kind: ValueNumber},
		},
	}}}}
	if _, err := KillMonsterObjectives(definitions); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("error = %v, want ErrInvalidChunk", err)
	}
}
