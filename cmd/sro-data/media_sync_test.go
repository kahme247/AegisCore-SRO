package main

import (
	"reflect"
	"testing"
)

func TestEvaluateMediaSyncMappingsFailsClosedForUnsupportedTable(t *testing.T) {
	mappings := []mediaTableMapping{
		{File: "leveldata.txt", Tables: "REF_LEVEL_DATA", Authority: "database", MediaSync: "export-media", ReconcileMode: "snapshot"},
		{File: "gameworlddata.txt", Tables: "REF_GAME_WORLD", Authority: "database", MediaSync: "not-supported", ReconcileMode: "snapshot"},
	}
	selected, blockers := evaluateMediaSyncMappings(mappings, "REF_GAME_WORLD", false)
	if !reflect.DeepEqual(selected, []string{"gameworlddata.txt"}) || !reflect.DeepEqual(blockers, []string{"gameworlddata.txt"}) {
		t.Fatalf("selected=%v blockers=%v", selected, blockers)
	}
}

func TestEvaluateMediaSyncMappingsAcceptsSnapshotExports(t *testing.T) {
	mappings := []mediaTableMapping{
		{File: "levelgold.txt", Tables: "REF_LEVEL_DATA", Authority: "database", MediaSync: "export-media", ReconcileMode: "snapshot"},
		{File: "leveldata.txt", Tables: "REF_LEVEL_DATA", Authority: "database", MediaSync: "export-media", ReconcileMode: "snapshot"},
		{File: "magicoption.txt", Tables: "REF_MAGIC_OPTION", Authority: "database", MediaSync: "not-supported", ReconcileMode: "overlay"},
	}
	selected, blockers := evaluateMediaSyncMappings(mappings, "REF_LEVEL_DATA", false)
	want := []string{"leveldata.txt", "levelgold.txt"}
	if !reflect.DeepEqual(selected, want) || len(blockers) != 0 {
		t.Fatalf("selected=%v blockers=%v", selected, blockers)
	}
}

func TestEvaluateMediaSyncMappingsSupportedOnlySkipsBlockers(t *testing.T) {
	mappings := []mediaTableMapping{
		{File: "leveldata.txt", Tables: "REF_LEVEL_DATA", Authority: "database", MediaSync: "export-media", ReconcileMode: "snapshot"},
		{File: "gameworlddata.txt", Tables: "REF_GAME_WORLD", Authority: "database", MediaSync: "not-supported", ReconcileMode: "snapshot"},
	}
	selected, blockers := evaluateMediaSyncMappings(mappings, "", true)
	if !reflect.DeepEqual(selected, []string{"leveldata.txt"}) || len(blockers) != 0 {
		t.Fatalf("selected=%v blockers=%v", selected, blockers)
	}
}
