package questscript

import (
	"errors"
	"testing"
)

func TestStartConditionsDecodeLevelAndSystemLayouts(t *testing.T) {
	definitions := []Definition{
		{CodeName: "NORMAL", Declarations: []Call{{Function: "LuaSetStartCodition", Args: []Value{
			{Kind: ValueNumber, Number: 2}, {Kind: ValueGlobal, String: "QSC_QUEST"},
			{Kind: ValueGlobal, String: "QSC_LEVEL"}, {Kind: ValueNumber, Number: 90}, {Kind: ValueNumber, Number: 71},
		}}}},
		{CodeName: "SYSTEM", Declarations: []Call{{Function: "LuaSetStartCodition", Args: []Value{
			{Kind: ValueNumber, Number: 3}, {Kind: ValueGlobal, String: "QSC_QUEST"},
			{Kind: ValueGlobal, String: "QSC_LEVEL"}, {Kind: ValueGlobal, String: "QSC_SYSTEM"},
			{Kind: ValueNumber, Number: 105}, {Kind: ValueNumber, Number: 105},
		}}}},
	}
	conditions, err := StartConditions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(conditions) != 2 || conditions[0].LevelLower != 71 || conditions[0].LevelUpper != 90 || conditions[0].SystemRequired ||
		conditions[1].ConditionCount != 3 || !conditions[1].SystemRequired {
		t.Fatalf("conditions = %#v", conditions)
	}
}

func TestStartConditionsRejectInvalidWindow(t *testing.T) {
	_, err := StartConditions([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetStartCodition", Args: []Value{
		{Kind: ValueNumber, Number: 2}, {Kind: ValueGlobal, String: "QSC_QUEST"},
		{Kind: ValueGlobal, String: "QSC_LEVEL"}, {Kind: ValueNumber, Number: 10}, {Kind: ValueNumber, Number: 20},
	}}}}})
	if !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("error = %v, want ErrInvalidChunk", err)
	}
}

func TestStartMethodsDecodeConversationNPCs(t *testing.T) {
	methods, err := StartMethods([]Definition{{CodeName: "QUEST", Declarations: []Call{{Function: "LuaSetStartMethod", Args: []Value{
		{Kind: ValueGlobal, String: StartMethodConversation}, {Kind: ValueNumber, Number: 2},
		{Kind: ValueString, String: "NPC_ONE"}, {Kind: ValueString, String: "NPC_TWO"},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || len(methods[0].NPCs) != 2 || methods[0].NPCs[1] != "NPC_TWO" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestDependenciesDecodeAllAndOneClearGroups(t *testing.T) {
	dependencies, err := Dependencies([]Definition{{CodeName: "QUEST", Declarations: []Call{
		{Function: "LuaInsertDependancyQuests", Args: []Value{{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "FIRST"}}},
		{Function: "LuaInsertDependancyQuestsOneClear", Args: []Value{{Kind: ValueNumber, Number: 2}, {Kind: ValueString, String: "SECOND"}, {Kind: ValueString, String: "THIRD"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 2 || dependencies[0].Mode != DependencyAllClear || dependencies[0].GroupIndex != 0 ||
		dependencies[1].Mode != DependencyOneClear || dependencies[1].GroupIndex != 1 || len(dependencies[1].QuestCodes) != 2 {
		t.Fatalf("dependencies = %#v", dependencies)
	}
}

func TestDependenciesRejectMismatchedCount(t *testing.T) {
	_, err := Dependencies([]Definition{{CodeName: "QUEST", Declarations: []Call{{
		Function: "LuaInsertDependancyQuests", Args: []Value{{Kind: ValueNumber, Number: 2}, {Kind: ValueString, String: "ONLY_ONE"}},
	}}}})
	if !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("error = %v, want ErrInvalidChunk", err)
	}
}
