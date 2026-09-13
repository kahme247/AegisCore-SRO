package chat

import "testing"

func TestParseAdminCommandSupportsCheatConsoleGrammar(t *testing.T) {
	for _, test := range []struct {
		message   string
		allowBare bool
		command   string
		arguments string
		ok        bool
	}{
		{message: ".spawn MOB_RM_ROC", command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: "spawn MOB_RM_ROC", allowBare: true, command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: ".spawn MOB_RM_ROC", allowBare: true, command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: "spawn MOB_RM_ROC", ok: false},
	} {
		command, arguments, ok := parseAdminCommand(test.message, test.allowBare)
		if command != test.command || arguments != test.arguments || ok != test.ok {
			t.Fatalf("parseAdminCommand(%q, %t) = (%q, %q, %t), want (%q, %q, %t)", test.message, test.allowBare, command, arguments, ok, test.command, test.arguments, test.ok)
		}
	}
}

func TestParseGMSpawnArgs(t *testing.T) {
	for _, test := range []struct {
		arguments string
		codeName  string
		count     int
	}{
		{arguments: "MOB_RM_ROC", codeName: "MOB_RM_ROC", count: 1},
		{arguments: "  MOB_TQ_WHITESNAKE   5 ", codeName: "MOB_TQ_WHITESNAKE", count: 5},
		{arguments: "MOB_TK_BONELORD 20", codeName: "MOB_TK_BONELORD", count: 20},
	} {
		codeName, count, err := parseGMSpawnArgs(test.arguments)
		if err != nil {
			t.Fatalf("parse %q: %v", test.arguments, err)
		}
		if codeName != test.codeName || count != test.count {
			t.Fatalf("parse %q = (%q, %d), want (%q, %d)", test.arguments, codeName, count, test.codeName, test.count)
		}
	}
}

func TestParseGMSpawnArgsRejectsUnsafeInput(t *testing.T) {
	for _, arguments := range []string{"", "NPC_CH_SMITH", "MOB_RM_ROC 0", "MOB_RM_ROC 21", "MOB_RM_ROC many", "MOB_RM_ROC 1 extra"} {
		if _, _, err := parseGMSpawnArgs(arguments); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", arguments)
		}
	}
}
