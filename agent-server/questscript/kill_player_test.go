package questscript

import "testing"

func TestKillPlayerObjectivesRecoversVariableJobStates(t *testing.T) {
	tests := []struct {
		name            string
		region          float64
		wantRegionValue int32
		wantRegionID    int16
	}{
		{name: "negative signed region", region: -32752, wantRegionValue: -32752, wantRegionID: -32752},
		{name: "maximum unsigned server region", region: 65535, wantRegionValue: 65535, wantRegionID: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := []Value{
				{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
				{Kind: ValueGlobal, String: "MISSION_TYPE_KILL_PLAYER"},
				{Kind: ValueString, String: "SN_CON_QNO_SD_GU_008_1"}, {Kind: ValueNumber, Number: 1},
				{Kind: ValueString, String: "NPC_SD_GUILD_AGENT"}, {Kind: ValueNumber, Number: 20},
				{Kind: ValueNumber, Number: 3}, {Kind: ValueGlobal, String: "MKP_LEVEL"},
				{Kind: ValueGlobal, String: "MKP_JOB"}, {Kind: ValueGlobal, String: "MKP_REGION"},
				{Kind: ValueNumber, Number: test.region}, {Kind: ValueNumber, Number: 105},
				{Kind: ValueNumber, Number: 2}, {Kind: ValueGlobal, String: "OBJ_JOBSTATE_TRADER"},
				{Kind: ValueGlobal, String: "OBJ_JOBSTATE_HUNTER"},
			}
			objectives, err := KillPlayerObjectives([]Definition{{CodeName: "QNO_SD_GU_008", Declarations: []Call{{Function: "LuaSetKillPlayerMissionData", Args: args}}}})
			if err != nil {
				t.Fatalf("KillPlayerObjectives: %v", err)
			}
			if len(objectives) != 1 || objectives[0].RegionValue != test.wantRegionValue || objectives[0].RegionID != test.wantRegionID ||
				objectives[0].RequiredCount != 20 || objectives[0].MinimumLevel != 105 || len(objectives[0].FilterKeys) != 3 ||
				len(objectives[0].JobStates) != 2 || objectives[0].JobStates[1] != "OBJ_JOBSTATE_HUNTER" {
				t.Fatalf("objectives = %#v", objectives)
			}
		})
	}
}

func TestKillPlayerObjectivesRejectsJobStateCountMismatch(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber}, {Kind: ValueGlobal, String: "MISSION_TYPE_KILL_PLAYER"},
		{Kind: ValueString, String: "OBJECTIVE"}, {Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "NPC"},
		{Kind: ValueNumber, Number: 20}, {Kind: ValueNumber, Number: 3}, {Kind: ValueGlobal, String: "LEVEL"},
		{Kind: ValueGlobal, String: "JOB"}, {Kind: ValueGlobal, String: "REGION"}, {Kind: ValueNumber},
		{Kind: ValueNumber, Number: 105}, {Kind: ValueNumber, Number: 2}, {Kind: ValueGlobal, String: "STATE"},
	}
	if _, err := KillPlayerObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetKillPlayerMissionData", Args: args}}}}); err == nil {
		t.Fatal("KillPlayerObjectives accepted a mismatched job-state count")
	}
}
