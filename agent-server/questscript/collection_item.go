package questscript

import (
	"fmt"
	"math"
)

const (
	CollectionItemStandard = "standard"
	CollectionItemExtended = "extended"
)

type CollectionItemSource struct {
	MonsterCode string
	Parameter   float64
}

type CollectionItemObjective struct {
	QuestCodeName string
	Variant       int
	MissionIndex  uint32
	Kind          string
	ObjectiveKey  string
	TargetNPCCode string
	Parameter6    uint32
	RequiredCount uint32
	ItemCode      string
	Sources       []CollectionItemSource
	Parameters    []Value
}

func CollectionItemObjectives(definitions []Definition) ([]CollectionItemObjective, error) {
	objectives := make([]CollectionItemObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			kind := ""
			extraCount := 0
			switch call.Function {
			case "LuaSetCollectionItemMissionData":
				kind = CollectionItemStandard
			case "LuaSetCollectionItemMissionData_EX":
				kind = CollectionItemExtended
				extraCount = 6
			default:
				continue
			}
			if len(call.Args) < 11+extraCount || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], "MISSION_TYPE_GATHER_ITEM_FROM_MONSTER") ||
				call.Args[3].Kind != ValueString || call.Args[5].Kind != ValueString || call.Args[8].Kind != ValueString {
				return nil, fmt.Errorf("%w: %s variant %d has malformed collection-item declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, missionOK := unsignedInteger(call.Args[1])
			sourceCount, sourceCountOK := unsignedInteger(call.Args[4])
			parameter6, parameter6OK := unsignedInteger(call.Args[6])
			requiredCount, requiredOK := unsignedInteger(call.Args[7])
			if !missionOK || !sourceCountOK || sourceCount == 0 || !parameter6OK || !requiredOK || requiredCount == 0 ||
				uint64(len(call.Args)) != 9+uint64(sourceCount)*2+uint64(extraCount) || call.Args[5].String == "" ||
				call.Args[8].String == "" || (kind == CollectionItemStandard && call.Args[3].String == "") {
				return nil, fmt.Errorf("%w: %s variant %d has invalid collection-item declaration values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			sources := make([]CollectionItemSource, 0, sourceCount)
			for index := uint32(0); index < sourceCount; index++ {
				offset := 9 + int(index)*2
				monster := call.Args[offset]
				parameter := call.Args[offset+1]
				if monster.Kind != ValueString || monster.String == "" || parameter.Kind != ValueNumber ||
					math.IsNaN(parameter.Number) || math.IsInf(parameter.Number, 0) {
					return nil, fmt.Errorf("%w: %s variant %d has malformed collection-item source %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				sources = append(sources, CollectionItemSource{MonsterCode: monster.String, Parameter: parameter.Number})
			}
			for index := len(call.Args) - extraCount; index < len(call.Args); index++ {
				if call.Args[index].Kind != ValueNumber || math.IsNaN(call.Args[index].Number) || math.IsInf(call.Args[index].Number, 0) {
					return nil, fmt.Errorf("%w: %s variant %d has malformed collection-item EX parameter %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
			}
			objectives = append(objectives, CollectionItemObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex, Kind: kind,
				ObjectiveKey: call.Args[3].String, TargetNPCCode: call.Args[5].String, Parameter6: parameter6,
				RequiredCount: requiredCount, ItemCode: call.Args[8].String, Sources: sources,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}
