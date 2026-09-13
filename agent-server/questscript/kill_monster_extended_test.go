package questscript

import "testing"

func TestExtendedKillMonsterObjectivesRecoversClassifiedFraction(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: missionTypeKillMonster}, {Kind: ValueString, String: "OBJECTIVE"},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "MOB"}, {Kind: ValueNumber, Number: 1},
		{Kind: ValueGlobal, String: "MONSTER_CLASS_GIANT"}, {Kind: ValueNumber, Number: 1.5},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueNumber, Number: 1},
	}
	objectives, err := ExtendedKillMonsterObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetMissionData_EX", Args: args}}}})
	if err != nil {
		t.Fatalf("ExtendedKillMonsterObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].Kind != ExtendedKillClassified || objectives[0].TargetClass != "MONSTER_CLASS_GIANT" || len(objectives[0].Targets) != 1 {
		t.Fatalf("objectives = %#v", objectives)
	}
	if objectives[0].RequiredCount != 1 {
		t.Fatalf("required count = %d, want server-truncated value 1", objectives[0].RequiredCount)
	}
}

func TestExtendedKillMonsterObjectivesRecoversMultiTargetList(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: missionTypeKillMonster}, {Kind: ValueString, String: "OBJECTIVE"},
		{Kind: ValueNumber, Number: 2}, {Kind: ValueString, String: "MOB_ONE"}, {Kind: ValueString, String: "MOB_TWO"},
		{Kind: ValueNumber}, {Kind: ValueNumber, Number: 500}, {Kind: ValueNumber}, {Kind: ValueNumber, Number: 1},
	}
	objectives, err := ExtendedKillMonsterObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetMissionData_EX", Args: args}}}})
	if err != nil {
		t.Fatalf("ExtendedKillMonsterObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].Kind != ExtendedKillMultiTarget || len(objectives[0].Targets) != 2 || objectives[0].Targets[1] != "MOB_TWO" {
		t.Fatalf("objectives = %#v", objectives)
	}
	if objectives[0].RequiredCount != 500 {
		t.Fatalf("required count = %d, want 500", objectives[0].RequiredCount)
	}
}

func TestExtendedKillMonsterObjectivesRejectsTargetCountMismatch(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: missionTypeKillMonster}, {Kind: ValueString, String: "OBJECTIVE"},
		{Kind: ValueNumber, Number: 2}, {Kind: ValueString, String: "MOB_ONE"},
		{Kind: ValueNumber}, {Kind: ValueNumber, Number: 1}, {Kind: ValueNumber}, {Kind: ValueNumber, Number: 1},
	}
	if _, err := ExtendedKillMonsterObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetMissionData_EX", Args: args}}}}); err == nil {
		t.Fatal("ExtendedKillMonsterObjectives accepted a mismatched target count")
	}
}
