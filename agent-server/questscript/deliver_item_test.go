package questscript

import "testing"

func TestDeliverItemEXObjectivesRecoversSmithMission(t *testing.T) {
	args := make([]Value, 22)
	args[0] = Value{Kind: ValueGlobal, String: "QUESTID"}
	args[1] = Value{Kind: ValueNumber}
	args[2] = Value{Kind: ValueGlobal, String: "MISSION_TYPE_DELIVER_ITEM"}
	args[3] = Value{Kind: ValueString, String: "SN_CON_QNO_CH_SMITH_1"}
	args[4] = Value{Kind: ValueString, String: "NPC_CH_SOLDIER_SO1"}
	args[13] = Value{Kind: ValueString, String: "ITEM_QNO_CH_SMITH_1"}
	definitions := []Definition{{CodeName: "QNO_CH_SMITH_1", Declarations: []Call{{Function: "LuaSetDeliverMissionData_EX", Args: args}}}}
	objectives, err := DeliverItemEXObjectives(definitions)
	if err != nil {
		t.Fatalf("DeliverItemEXObjectives: %v", err)
	}
	if len(objectives) != 1 || objectives[0].TargetNPCCode != "NPC_CH_SOLDIER_SO1" || objectives[0].ItemCode != "ITEM_QNO_CH_SMITH_1" {
		t.Fatalf("objectives = %#v", objectives)
	}
}
