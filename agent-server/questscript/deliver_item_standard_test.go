package questscript

import "testing"

func TestDeliverItemObjectivesRecoversVariableItemList(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"},
		{Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_DELIVER_ITEM"},
		{Kind: ValueString, String: "SN_CON_QNO_SD_NO_015_1"},
		{Kind: ValueString, String: "NPC_SD_M_AREA_AHHA"},
		{Kind: ValueNumber, Number: 2},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "ITEM_QNO_SD_NO_015_1"},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "ITEM_QNO_SD_NO_015_2"},
		{Kind: ValueNumber, Number: 1},
		{Kind: ValueString, String: "SN_TALK_QNO_SD_NO_015_06"},
	}
	definitions := []Definition{{CodeName: "QNO_SD_NO_015", Declarations: []Call{{Function: "LuaSetDeliverMissionData", Args: args}}}}
	objectives, err := DeliverItemObjectives(definitions)
	if err != nil {
		t.Fatalf("DeliverItemObjectives: %v", err)
	}
	if len(objectives) != 1 || len(objectives[0].Items) != 2 || objectives[0].Items[1].ItemCode != "ITEM_QNO_SD_NO_015_2" ||
		objectives[0].Items[1].Quantity != 1 || objectives[0].DialogueKey != "SN_TALK_QNO_SD_NO_015_06" {
		t.Fatalf("objectives = %#v", objectives)
	}
}

func TestDeliverItemObjectivesRejectsCountMismatch(t *testing.T) {
	args := []Value{
		{Kind: ValueGlobal, String: "QUESTID"}, {Kind: ValueNumber},
		{Kind: ValueGlobal, String: "MISSION_TYPE_DELIVER_ITEM"},
		{Kind: ValueString, String: "OBJECTIVE"}, {Kind: ValueString, String: "NPC"},
		{Kind: ValueNumber, Number: 2}, {Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "ITEM"},
		{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "DIALOGUE"},
	}
	_, err := DeliverItemObjectives([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetDeliverMissionData", Args: args}}}})
	if err == nil {
		t.Fatal("DeliverItemObjectives accepted a mismatched item count")
	}
}
