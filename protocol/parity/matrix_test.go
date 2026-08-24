package parity

import (
	"strings"
	"testing"
)

func TestLoadOpcodeEvidencePreservesAliasesWithoutGuessingIdentity(t *testing.T) {
	entries, err := LoadOpcodeEvidence(strings.NewReader("opcode,name,source,line,evidence\n0x7001,Join,a.go,10,current\n0x7001,Other,b.txt,20,secondary\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Opcode == nil || *entries[0].Opcode != 0x7001 {
		t.Fatalf("entries = %+v", entries)
	}
	if missing := entries[0].MissingRequired(); !contains(missing, "service") || !contains(missing, "direction") || !contains(missing, "phase") {
		t.Fatalf("unresolved opcode missing fields = %v", missing)
	}
}

func TestLoadSQLContractsGroupsExactObjectsAndPreservesServices(t *testing.T) {
	input := "file,kind,object,evidence_string\nSR_GameServer.exe,table,_CHAR,one\nSR_ShardManager.exe,table,_CHAR,two\nSR_GameServer.exe,procedure,_Save,three\n"
	entries, err := LoadSQLContracts(strings.NewReader(input), map[string]string{"SR_GameServer.exe": strings.Repeat("a", 64), "SR_ShardManager.exe": strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}
	var table MatrixEntry
	for _, entry := range entries {
		if entry.Kind == MatrixTable {
			table = entry
		}
	}
	if len(table.Services) != 2 || len(table.BinarySHA256) != 2 {
		t.Fatalf("grouped table = %+v", table)
	}
}

func TestLoadLegacyServerConfigIncludesDisabledCompiledSwitches(t *testing.T) {
	input := "SR_GameServer\n{\n ExpRatio 100\n //WINTER_EVENT EVENT_ON\n}\nSR_ShardManager\n{\n FlagEvent 0\n}\n"
	entries, err := LoadLegacyServerConfig(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || !entries[0].EnabledDefault || entries[1].EnabledDefault || entries[2].EnabledDefault {
		t.Fatalf("configuration entries = %+v", entries)
	}
}

func TestLoadLegacySchedulesIncludesBlockCommentedDefinitions(t *testing.T) {
	input := "Schedule Enabled\n{\n}\n/*\nSchedule Disabled\n{\n}\n*/\n"
	entries, err := LoadLegacySchedules(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].EnabledDefault || entries[1].EnabledDefault {
		t.Fatalf("schedule entries = %+v", entries)
	}
}

func TestScriptEntriesRejectsMissingFingerprint(t *testing.T) {
	if _, err := ScriptEntries([]ScriptArtifact{{Path: "Quest.sct", Size: 1}}); err == nil {
		t.Fatal("ScriptEntries accepted an artifact without a SHA-256")
	}
}

func TestRequiredEntityAndIntegrationInventoriesAreExplicit(t *testing.T) {
	if got, want := len(EntityFamilyEntries()), 18; got != want {
		t.Fatalf("entity family count = %d, want %d", got, want)
	}
	if got, want := len(ExternalIntegrationEntries()), 5; got != want {
		t.Fatalf("external integration count = %d, want %d", got, want)
	}
	for _, entry := range append(EntityFamilyEntries(), ExternalIntegrationEntries()...) {
		if len(entry.Blockers) == 0 || entry.Status == StatusVerified {
			t.Fatalf("inventory entry prematurely complete: %+v", entry)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
