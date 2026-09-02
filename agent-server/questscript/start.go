package questscript

import "fmt"

const (
	StartMethodConversation = "QM_CONVERSATION"
	DependencyAllClear      = "all_clear"
	DependencyOneClear      = "one_clear"
)

type StartCondition struct {
	QuestCodeName  string
	Variant        int
	ConditionCount uint32
	LevelUpper     uint32
	LevelLower     uint32
	SystemRequired bool
	Parameters     []Value
}

type StartMethod struct {
	QuestCodeName string
	Variant       int
	MethodKey     string
	NPCs          []string
	Parameters    []Value
}

type Dependency struct {
	QuestCodeName string
	Variant       int
	GroupIndex    uint32
	Mode          string
	QuestCodes    []string
	Parameters    []Value
}

func StartConditions(definitions []Definition) ([]StartCondition, error) {
	conditions := make([]StartCondition, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetStartCodition" {
				continue
			}
			if len(call.Args) != 5 && len(call.Args) != 6 {
				return nil, fmt.Errorf("%w: %s variant %d has malformed start condition", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			count, countOK := unsignedInteger(call.Args[0])
			upper, upperOK := unsignedInteger(call.Args[len(call.Args)-2])
			lower, lowerOK := unsignedInteger(call.Args[len(call.Args)-1])
			systemRequired := len(call.Args) == 6
			validKeys := globalValue(call.Args[1], "QSC_QUEST") && globalValue(call.Args[2], "QSC_LEVEL")
			if systemRequired {
				validKeys = validKeys && globalValue(call.Args[3], "QSC_SYSTEM")
			}
			if !countOK || count != uint32(len(call.Args)-3) || !upperOK || !lowerOK || lower > upper || !validKeys {
				return nil, fmt.Errorf("%w: %s variant %d has invalid start condition values", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			conditions = append(conditions, StartCondition{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, ConditionCount: count,
				LevelUpper: upper, LevelLower: lower, SystemRequired: systemRequired,
				Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return conditions, nil
}

func StartMethods(definitions []Definition) ([]StartMethod, error) {
	methods := make([]StartMethod, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetStartMethod" {
				continue
			}
			if len(call.Args) < 3 || !globalValue(call.Args[0], StartMethodConversation) {
				return nil, fmt.Errorf("%w: %s variant %d has malformed start method", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			count, ok := unsignedInteger(call.Args[1])
			if !ok || count == 0 || uint64(count) != uint64(len(call.Args)-2) {
				return nil, fmt.Errorf("%w: %s variant %d has mismatched start NPC count", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			npcs := make([]string, count)
			for index := range npcs {
				value := call.Args[index+2]
				if value.Kind != ValueString || value.String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed start NPC %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				npcs[index] = value.String
			}
			methods = append(methods, StartMethod{
				QuestCodeName: definition.CodeName, Variant: definition.Variant,
				MethodKey: call.Args[0].String, NPCs: npcs, Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return methods, nil
}

func Dependencies(definitions []Definition) ([]Dependency, error) {
	dependencies := make([]Dependency, 0)
	for _, definition := range definitions {
		groupIndex := uint32(0)
		for _, call := range definition.Declarations {
			mode := ""
			switch call.Function {
			case "LuaInsertDependancyQuests":
				mode = DependencyAllClear
			case "LuaInsertDependancyQuestsOneClear":
				mode = DependencyOneClear
			default:
				continue
			}
			if len(call.Args) < 2 {
				return nil, fmt.Errorf("%w: %s variant %d has malformed dependency group", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			count, ok := unsignedInteger(call.Args[0])
			if !ok || count == 0 || uint64(count) != uint64(len(call.Args)-1) {
				return nil, fmt.Errorf("%w: %s variant %d has mismatched dependency count", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			codes := make([]string, count)
			for index := range codes {
				value := call.Args[index+1]
				if value.Kind != ValueString || value.String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed dependency %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				codes[index] = value.String
			}
			dependencies = append(dependencies, Dependency{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, GroupIndex: groupIndex,
				Mode: mode, QuestCodes: codes, Parameters: append([]Value(nil), call.Args...),
			})
			groupIndex++
		}
	}
	return dependencies, nil
}
