package questscript

import "fmt"

const missionTypeTalismanCollection = "MISSION_TYPE_TALISMANCOLLECTION"

// TalismanObjective preserves the exact seven-argument declaration used by
// the supplied script. SR_GameServer's Lua registration at 0x00870D0C binds
// callback 0x0086BA20, which reads exactly seven arguments, while 0x0091E110
// proves that the final numeric field is a non-zero progress-callback gate;
// it is not a required count.
type TalismanObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	ObjectiveKey  string
	TargetNPCCode string
	ThemeCode     string
	Parameter6    uint32
	Parameters    []Value
}

func TalismanObjectives(definitions []Definition) ([]TalismanObjective, error) {
	objectives := make([]TalismanObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetTalismanMissionData" {
				continue
			}
			if len(call.Args) != 7 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], missionTypeTalismanCollection) ||
				call.Args[3].Kind != ValueString || call.Args[3].String == "" ||
				call.Args[4].Kind != ValueString || call.Args[4].String == "" ||
				call.Args[5].Kind != ValueString || call.Args[5].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has malformed talisman declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, missionOK := unsignedInteger(call.Args[1])
			parameter6, parameterOK := unsignedInteger(call.Args[6])
			if !missionOK || !parameterOK {
				return nil, fmt.Errorf("%w: %s variant %d has non-integral talisman values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objectives = append(objectives, TalismanObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, TargetNPCCode: call.Args[4].String,
				ThemeCode: call.Args[5].String, Parameter6: parameter6,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}
