package questscript

import "testing"

func TestDialogObjectivesRecoversFixedDeclaration(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_DIALOG"},
		{Kind: ValueString, String: "SN_CON_QNO_SD_NO_001_1"},
		{Kind: ValueNumber, Number: 1},
		{Kind: ValueString, String: "SN_TALK_QNO_SD_NO_001_05"},
	}
	objectives, err := DialogObjectives([]Definition{{CodeName: "QNO_SD_NO_001", Declarations: []Call{
		{Function: "LuaSetDialLogMissionData", Args: args},
		{Function: "InsertQuestMenuStringList", Args: []Value{
			{Kind: ValueString, String: "NPC_SD_M_AREA_SENMUTE"}, {Kind: ValueNumber, Number: 2},
			{Kind: ValueString, String: "BASIC_MENUSTRING_ACHIEVED"}, {Kind: ValueString, String: "SN_TALK_QNO_SD_NO_001_05"},
		}},
	}}})
	if err != nil {
		t.Fatalf("DialogObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].ObjectiveKey != "SN_CON_QNO_SD_NO_001_1" ||
		objectives[0].Parameter4 != 1 || objectives[0].DialogueKey != "SN_TALK_QNO_SD_NO_001_05" ||
		objectives[0].TargetNPCCode != "NPC_SD_M_AREA_SENMUTE" {
		t.Fatalf("objectives = %#v", objectives)
	}
}

func TestDialogObjectivesRejectsWrongMissionType(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_KILL_MONSTER"},
		{Kind: ValueString, String: "OBJECTIVE"}, {Kind: ValueNumber, Number: 1},
		{Kind: ValueString, String: "DIALOGUE"},
	}
	if _, err := DialogObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetDialLogMissionData", Args: args}}}}); err == nil {
		t.Fatal("DialogObjectives accepted the wrong mission type")
	}
}

func TestDialogObjectivesRejectsMissingNPCMenuRoute(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_DIALOG"}, {Kind: ValueString, String: "OBJECTIVE"},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "DIALOGUE"},
	}
	if _, err := DialogObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetDialLogMissionData", Args: args}}}}); err == nil {
		t.Fatal("DialogObjectives accepted an objective without an NPC menu route")
	}
}
