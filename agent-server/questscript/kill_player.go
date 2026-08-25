package questscript

import (
	"fmt"
	"math"
)

type KillPlayerObjective struct {
	QuestCodeName   string
	Variant         int
	MissionIndex    uint32
	ObjectiveKey    string
	Parameter4      uint32
	TargetNPCCode   string
	Parameter6      uint32
	FilterCount     uint32
	LevelFilterKey  string
	JobFilterKey    string
	RegionFilterKey string
	RegionValue     int32
	Parameter12     uint32
	JobStates       []string
	Parameters      []Value
}

func KillPlayerObjectives(definitions []Definition) ([]KillPlayerObjective, error) {
	objectives := make([]KillPlayerObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetKillPlayerMissionData" {
				continue
			}
			if len(call.Args) < 15 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], "MISSION_TYPE_KILL_PLAYER") || call.Args[3].Kind != ValueString ||
				call.Args[5].Kind != ValueString || call.Args[8].Kind != ValueGlobal ||
				call.Args[9].Kind != ValueGlobal || call.Args[10].Kind != ValueGlobal {
				return nil, fmt.Errorf("%w: %s variant %d has malformed kill-player declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, ok1 := unsignedInteger(call.Args[1])
			parameter4, ok4 := unsignedInteger(call.Args[4])
			parameter6, ok6 := unsignedInteger(call.Args[6])
			filterCount, ok7 := unsignedInteger(call.Args[7])
			regionValue, ok11 := signedInteger32(call.Args[11])
			parameter12, ok12 := unsignedInteger(call.Args[12])
			jobStateCount, ok13 := unsignedInteger(call.Args[13])
			if !ok1 || !ok4 || !ok6 || !ok7 || !ok11 || !ok12 || !ok13 || jobStateCount == 0 ||
				uint64(len(call.Args)) != 14+uint64(jobStateCount) || call.Args[3].String == "" || call.Args[5].String == "" ||
				call.Args[8].String == "" || call.Args[9].String == "" || call.Args[10].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has invalid kill-player declaration values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			jobStates := make([]string, 0, jobStateCount)
			for index := uint32(0); index < jobStateCount; index++ {
				value := call.Args[14+index]
				if value.Kind != ValueGlobal || value.String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed kill-player job state %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				jobStates = append(jobStates, value.String)
			}
			objectives = append(objectives, KillPlayerObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, Parameter4: parameter4, TargetNPCCode: call.Args[5].String,
				Parameter6: parameter6, FilterCount: filterCount, LevelFilterKey: call.Args[8].String,
				JobFilterKey: call.Args[9].String, RegionFilterKey: call.Args[10].String, RegionValue: regionValue,
				Parameter12: parameter12, JobStates: jobStates, Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}

func signedInteger32(value Value) (int32, bool) {
	if value.Kind != ValueNumber || value.Number < math.MinInt32 || value.Number > math.MaxInt32 || math.Trunc(value.Number) != value.Number {
		return 0, false
	}
	return int32(value.Number), true
}
