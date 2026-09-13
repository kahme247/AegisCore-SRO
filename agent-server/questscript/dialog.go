package questscript

import "fmt"

type DialogObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	ObjectiveKey  string
	Parameter4    uint32
	DialogueKey   string
	TargetNPCCode string
	Parameters    []Value
}

func DialogObjectives(definitions []Definition) ([]DialogObjective, error) {
	objectives := make([]DialogObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetDialLogMissionData" {
				continue
			}
			if len(call.Args) != 6 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], "MISSION_TYPE_DIALOG") || call.Args[3].Kind != ValueString ||
				call.Args[5].Kind != ValueString {
				return nil, fmt.Errorf("%w: %s variant %d has malformed dialog declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, missionOK := unsignedInteger(call.Args[1])
			parameter4, parameterOK := unsignedInteger(call.Args[4])
			if !missionOK || !parameterOK || call.Args[3].String == "" || call.Args[5].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has invalid dialog declaration values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			targetNPC, routeOK := dialogTargetNPC(definition.Declarations, call.Args[5].String)
			if !routeOK {
				return nil, fmt.Errorf("%w: %s variant %d does not have one dialog objective NPC route", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objectives = append(objectives, DialogObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, Parameter4: parameter4, DialogueKey: call.Args[5].String, TargetNPCCode: targetNPC,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}

// dialogTargetNPC correlates the mission's authored dialogue key with the NPC
// menu entry that exposes it. CMissionDialog matches this key before opening
// its pending continuation; the quest's broad start-NPC list is not an
// equivalent route because it can also contain the quest giver.
func dialogTargetNPC(declarations []Call, dialogueKey string) (string, bool) {
	target := ""
	for _, call := range declarations {
		if call.Function != "InsertQuestMenuStringList" || len(call.Args) < 4 || call.Args[0].Kind != ValueString || call.Args[0].String == "" {
			continue
		}
		matched := false
		for _, value := range call.Args[2:] {
			if value.Kind == ValueString && value.String == dialogueKey {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if target != "" && target != call.Args[0].String {
			return "", false
		}
		target = call.Args[0].String
	}
	return target, target != ""
}
