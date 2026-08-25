package questscript

import "fmt"

type DeliverItemEXObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	ObjectiveKey  string
	TargetNPCCode string
	ItemCode      string
	Parameters    []Value
}

func DeliverItemEXObjectives(definitions []Definition) ([]DeliverItemEXObjective, error) {
	objectives := make([]DeliverItemEXObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetDeliverMissionData_EX" {
				continue
			}
			if len(call.Args) != 22 || !globalValue(call.Args[0], "QUESTID") || !globalValue(call.Args[2], "MISSION_TYPE_DELIVER_ITEM") ||
				call.Args[3].Kind != ValueString || call.Args[4].Kind != ValueString || call.Args[13].Kind != ValueString {
				return nil, fmt.Errorf("%w: %s variant %d has malformed deliver-item EX declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, ok := unsignedInteger(call.Args[1])
			if !ok || call.Args[4].String == "" || call.Args[13].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has invalid deliver-item EX identity", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objectives = append(objectives, DeliverItemEXObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, TargetNPCCode: call.Args[4].String, ItemCode: call.Args[13].String,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}
