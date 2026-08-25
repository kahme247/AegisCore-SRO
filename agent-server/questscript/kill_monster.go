package questscript

import (
	"fmt"
	"math"
)

const missionTypeKillMonster = "MISSION_TYPE_KILL_MONSTER"

// KillMonsterObjective is the exact ten-argument LuaSetMissionData form used
// by the supplied script. Parameters whose names are not yet recovered remain
// explicitly positional instead of receiving guessed semantics.
type KillMonsterObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	ObjectiveKey  string
	Parameter4    uint32
	TargetCode    string
	Parameter6    uint32
	TargetClass   string
	RequiredCount uint32
	Parameter9    uint32
}

func KillMonsterObjectives(definitions []Definition) ([]KillMonsterObjective, error) {
	objectives := make([]KillMonsterObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetMissionData" || len(call.Args) < 3 || call.Args[2].Kind != ValueGlobal || call.Args[2].String != missionTypeKillMonster {
				continue
			}
			if len(call.Args) != 10 || !globalValue(call.Args[0], "QUESTID") || call.Args[3].Kind != ValueString || call.Args[5].Kind != ValueString || call.Args[7].Kind != ValueGlobal {
				return nil, fmt.Errorf("%w: %s variant %d has malformed kill-monster declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, ok1 := unsignedInteger(call.Args[1])
			parameter4, ok4 := unsignedInteger(call.Args[4])
			parameter6, ok6 := unsignedInteger(call.Args[6])
			requiredCount, ok8 := unsignedInteger(call.Args[8])
			parameter9, ok9 := unsignedInteger(call.Args[9])
			if !ok1 || !ok4 || !ok6 || !ok8 || !ok9 || requiredCount == 0 {
				return nil, fmt.Errorf("%w: %s variant %d has non-integral kill-monster values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objectives = append(objectives, KillMonsterObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, Parameter4: parameter4, TargetCode: call.Args[5].String,
				Parameter6: parameter6, TargetClass: call.Args[7].String, RequiredCount: requiredCount, Parameter9: parameter9,
			})
		}
	}
	return objectives, nil
}

func globalValue(value Value, name string) bool {
	return value.Kind == ValueGlobal && value.String == name
}

func unsignedInteger(value Value) (uint32, bool) {
	if value.Kind != ValueNumber || value.Number < 0 || value.Number > math.MaxUint32 || math.Trunc(value.Number) != value.Number {
		return 0, false
	}
	return uint32(value.Number), true
}
