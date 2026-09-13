package model

import "testing"

// TestNativeEvent18CoverageAudit keeps the complete concrete event-0x18
// override catalog recovered from SR_GameServer.exe (image base 0x00401000)
// tied to an AegisCore owner. This deliberately complements the retained Lua
// script audit: quests 140 and 162 have bespoke runtimes, while the remaining
// eight share the generalized catch-action runtime.
func TestNativeEvent18CoverageAudit(t *testing.T) {
	want := map[int]uint32{
		140: 0x008A0970,
		162: 0x008A5000,
		163: 0x008AA630,
		181: 0x008B2A10,
		203: 0x008C86A0,
		207: 0x008BB5F0,
		239: 0x008CA6E0,
		286: 0x008D6B10,
		357: 0x009038D0,
		371: 0x00909600,
	}
	got := map[int]uint32{
		OldWomanSearchQuestID: OldWomanSearchHandler,
		SlavePursuitQuestID:   SlavePursuitHandler,
	}
	for _, definition := range NativeCatchActionDefinitions {
		if previous, duplicate := got[definition.QuestID]; duplicate {
			t.Fatalf("duplicate native event-0x18 quest %d (handlers %#08x and %#08x)", definition.QuestID, previous, definition.Handler)
		}
		got[definition.QuestID] = definition.Handler
	}
	if len(got) != len(want) {
		t.Fatalf("owned native event-0x18 definitions = %d, want %d: %+v", len(got), len(want), got)
	}
	for questID, handler := range want {
		if got[questID] != handler {
			t.Errorf("quest %d handler = %#08x, want %#08x", questID, got[questID], handler)
		}
	}
}

func TestNativePartyInstanceHooksHaveExecutableCoverage(t *testing.T) {
	if len(NativePartyInstanceHooks) != 8 {
		t.Fatalf("native party-instance hooks = %d, want 8", len(NativePartyInstanceHooks))
	}
	seen := make(map[string]struct{}, len(NativePartyInstanceHooks))
	for _, hook := range NativePartyInstanceHooks {
		if hook.QuestID <= 0 || hook.CodeName == "" || (hook.Handler != 0x008D7270 && hook.Handler != 0x008BBB40) {
			t.Fatalf("invalid deferred hook: %+v", hook)
		}
		if _, duplicate := seen[hook.CodeName]; duplicate {
			t.Fatalf("duplicate native hook %q", hook.CodeName)
		}
		seen[hook.CodeName] = struct{}{}
		if resolved, ok := NativePartyInstanceHookForQuest(hook.QuestID); !ok || resolved != hook {
			t.Fatalf("native hook lookup %d = %+v, %t", hook.QuestID, resolved, ok)
		}
	}
}
