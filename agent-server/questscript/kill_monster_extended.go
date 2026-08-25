package questscript

import (
	"fmt"
	"math"
)

const (
	ExtendedKillClassified  = "classified"
	ExtendedKillMultiTarget = "multi_target"
)

type ExtendedKillMonsterObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	Kind          string
	ObjectiveKey  string
	TargetClass   string
	Targets       []string
	Parameters    []Value
}

func ExtendedKillMonsterObjectives(definitions []Definition) ([]ExtendedKillMonsterObjective, error) {
	objectives := make([]ExtendedKillMonsterObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetMissionData_EX" {
				continue
			}
			if len(call.Args) < 11 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], missionTypeKillMonster) || call.Args[3].Kind != ValueString ||
				call.Args[3].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has malformed extended kill declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, missionOK := unsignedInteger(call.Args[1])
			targetCount, countOK := unsignedInteger(call.Args[4])
			if !missionOK || !countOK || targetCount == 0 {
				return nil, fmt.Errorf("%w: %s variant %d has invalid extended kill identity", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objective := ExtendedKillMonsterObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, Targets: make([]string, 0, targetCount),
				Parameters: append([]Value(nil), call.Args...),
			}
			if targetCount == 1 {
				objective.Kind = ExtendedKillClassified
				if len(call.Args) != 11 || call.Args[5].Kind != ValueString || call.Args[5].String == "" ||
					call.Args[7].Kind != ValueGlobal || call.Args[7].String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed classified extended kill declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
				}
				for _, index := range []int{6, 8, 9, 10} {
					if !finiteNonNegativeNumber(call.Args[index]) {
						return nil, fmt.Errorf("%w: %s variant %d has malformed classified parameter %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
					}
				}
				objective.Targets = append(objective.Targets, call.Args[5].String)
				objective.TargetClass = call.Args[7].String
			} else {
				objective.Kind = ExtendedKillMultiTarget
				if uint64(len(call.Args)) != uint64(targetCount)+9 {
					return nil, fmt.Errorf("%w: %s variant %d has mismatched extended kill target count", ErrInvalidChunk, definition.CodeName, definition.Variant)
				}
				for index := uint32(0); index < targetCount; index++ {
					target := call.Args[5+index]
					if target.Kind != ValueString || target.String == "" {
						return nil, fmt.Errorf("%w: %s variant %d has malformed extended kill target %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
					}
					objective.Targets = append(objective.Targets, target.String)
				}
				for index := 5 + int(targetCount); index < len(call.Args); index++ {
					if !finiteNonNegativeNumber(call.Args[index]) {
						return nil, fmt.Errorf("%w: %s variant %d has malformed multi-target parameter %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
					}
				}
			}
			objectives = append(objectives, objective)
		}
	}
	return objectives, nil
}

func finiteNonNegativeNumber(value Value) bool {
	return value.Kind == ValueNumber && value.Number >= 0 && !math.IsNaN(value.Number) && !math.IsInf(value.Number, 0)
}
