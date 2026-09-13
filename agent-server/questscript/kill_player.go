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
	FilterKeys      []string
	LevelFilterKey  string
	JobFilterKey    string
	RegionFilterKey string
	RegionValue     int32
	RegionID        int16
	Parameter12     uint32
	RequiredCount   uint32
	MinimumLevel    uint8
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
			if len(call.Args) < 12 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], "MISSION_TYPE_KILL_PLAYER") || call.Args[3].Kind != ValueString ||
				call.Args[5].Kind != ValueString {
				return nil, fmt.Errorf("%w: %s variant %d has malformed kill-player declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, ok1 := unsignedInteger(call.Args[1])
			parameter4, ok4 := unsignedInteger(call.Args[4])
			parameter6, ok6 := unsignedInteger(call.Args[6])
			filterCount, ok7 := unsignedInteger(call.Args[7])
			if !ok1 || !ok4 || !ok6 || !ok7 || filterCount == 0 || uint64(len(call.Args)) < 11+uint64(filterCount) {
				return nil, fmt.Errorf("%w: %s variant %d has invalid kill-player declaration values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			filterKeys := make([]string, 0, filterCount)
			for index := uint32(0); index < filterCount; index++ {
				value := call.Args[8+index]
				if value.Kind != ValueGlobal || value.String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed kill-player filter %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				filterKeys = append(filterKeys, value.String)
			}
			tail := 8 + int(filterCount)
			regionValue, ok11 := signedInteger32(call.Args[tail])
			regionID, regionOK := serverRegionID(call.Args[tail])
			parameter12, ok12 := unsignedInteger(call.Args[tail+1])
			jobStateCount, ok13 := unsignedInteger(call.Args[tail+2])
			if !ok11 || !regionOK || !ok12 || parameter12 == 0 || parameter12 > math.MaxUint8 || !ok13 || jobStateCount == 0 ||
				uint64(len(call.Args)) != uint64(tail+3)+uint64(jobStateCount) || call.Args[3].String == "" || call.Args[5].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has invalid kill-player declaration values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			jobStates := make([]string, 0, jobStateCount)
			for index := uint32(0); index < jobStateCount; index++ {
				value := call.Args[tail+3+int(index)]
				if value.Kind != ValueGlobal || value.String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed kill-player job state %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				jobStates = append(jobStates, value.String)
			}
			objective := KillPlayerObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, Parameter4: parameter4, TargetNPCCode: call.Args[5].String,
				Parameter6: parameter6, RequiredCount: parameter6, FilterCount: filterCount, FilterKeys: filterKeys,
				RegionValue: regionValue, RegionID: regionID, Parameter12: parameter12, MinimumLevel: uint8(parameter12),
				JobStates: jobStates, Parameters: append([]Value(nil), call.Args...),
			}
			if len(filterKeys) == 3 {
				objective.LevelFilterKey, objective.JobFilterKey, objective.RegionFilterKey = filterKeys[0], filterKeys[1], filterKeys[2]
			}
			objectives = append(objectives, objective)
		}
	}
	return objectives, nil
}

func serverRegionID(value Value) (int16, bool) {
	if value.Kind != ValueNumber || value.Number < math.MinInt16 || value.Number > math.MaxUint16 || math.Trunc(value.Number) != value.Number {
		return 0, false
	}
	return int16(uint16(int64(value.Number))), true
}

func signedInteger32(value Value) (int32, bool) {
	if value.Kind != ValueNumber || value.Number < math.MinInt32 || value.Number > math.MaxInt32 || math.Trunc(value.Number) != value.Number {
		return 0, false
	}
	return int32(value.Number), true
}
