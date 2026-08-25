package questscript

import "testing"

func TestCollectionItemObjectivesRecoversVariableSources(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_GATHER_ITEM_FROM_MONSTER"},
		{Kind: ValueString, String: "SN_CON_QUEST"}, {Kind: ValueNumber, Number: 2},
		{Kind: ValueString, String: "NPC_QUEST"}, {Kind: ValueNumber, Number: 1},
		{Kind: ValueNumber, Number: 15}, {Kind: ValueString, String: "ITEM_QUEST"},
		{Kind: ValueString, String: "MOB_ONE"}, {Kind: ValueNumber, Number: 1.3},
		{Kind: ValueString, String: "MOB_TWO"}, {Kind: ValueNumber, Number: 50},
	}
	objectives, err := CollectionItemObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetCollectionItemMissionData", Args: args}}}})
	if err != nil {
		t.Fatalf("CollectionItemObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].Kind != CollectionItemStandard || len(objectives[0].Sources) != 2 ||
		objectives[0].Sources[0].Parameter != 1.3 || objectives[0].RequiredCount != 15 {
		t.Fatalf("objectives = %#v", objectives)
	}
}

func TestCollectionItemObjectivesRecoversExtendedTrailingParameters(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber, Number: 1},
		{Kind: ValueGlobal, String: "MISSION_TYPE_GATHER_ITEM_FROM_MONSTER"},
		{Kind: ValueString}, {Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "NPC"},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueNumber, Number: 15}, {Kind: ValueString, String: "ITEM"},
		{Kind: ValueString, String: "MOB"}, {Kind: ValueNumber, Number: 25},
		{Kind: ValueNumber}, {Kind: ValueNumber, Number: 1}, {Kind: ValueNumber},
		{Kind: ValueNumber}, {Kind: ValueNumber}, {Kind: ValueNumber},
	}
	objectives, err := CollectionItemObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetCollectionItemMissionData_EX", Args: args}}}})
	if err != nil {
		t.Fatalf("CollectionItemObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].Kind != CollectionItemExtended || objectives[0].ObjectiveKey != "" {
		t.Fatalf("objectives = %#v", objectives)
	}
}

func TestCollectionItemObjectivesRejectsSourceCountMismatch(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_GATHER_ITEM_FROM_MONSTER"},
		{Kind: ValueString, String: "OBJECTIVE"}, {Kind: ValueNumber, Number: 2},
		{Kind: ValueString, String: "NPC"}, {Kind: ValueNumber, Number: 1},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "ITEM"},
		{Kind: ValueString, String: "MOB"}, {Kind: ValueNumber, Number: 100},
	}
	if _, err := CollectionItemObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetCollectionItemMissionData", Args: args}}}}); err == nil {
		t.Fatal("CollectionItemObjectives accepted a mismatched source count")
	}
}
