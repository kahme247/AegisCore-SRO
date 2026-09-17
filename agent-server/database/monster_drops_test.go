package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

func TestMonsterDropQueriesResolveOnlyEnabledRuntimeReferences(t *testing.T) {
	for name, query := range map[string]string{
		"direct":   selectMonsterDirectDrops,
		"random":   selectMonsterRandomDrops,
		"groups":   selectDropGroups,
		"profiles": selectDropItemProfiles,
	} {
		if !strings.Contains(query, "REF_OBJ_COMMON_VSRO") || !strings.Contains(query, ".`SERVICE`=1") {
			t.Fatalf("%s query does not require an enabled common reference: %s", name, query)
		}
	}
	if !strings.Contains(selectInvalidDropReference, "enabled object missing ITEMDATA") ||
		!strings.Contains(selectInvalidDropReference, "enabled object missing CHAR_REF_DATA") {
		t.Fatalf("invalid-reference query does not distinguish unloaded enabled objects: %s", selectInvalidDropReference)
	}
}

func TestLiveMonsterDropCatalogLoadsEnabledReferenceSubset(t *testing.T) {
	if os.Getenv("AEGIS_TEST_LIVE_DROP_CATALOG") != "1" {
		t.Skip("set AEGIS_TEST_LIVE_DROP_CATALOG=1 with a configured read-only shard connection")
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(context.Background(), frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := NewMonsterDropCatalogRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLoadedDropTablesRejectsEnabledAssignmentWithoutCandidates(t *testing.T) {
	tables := map[uint32]service.MonsterDropTable{
		77: {Random: []service.DropGroupAssignment{{GroupID: 9}}},
	}
	if err := validateLoadedDropTables(tables); err == nil || !strings.Contains(err.Error(), "group 9 has no enabled candidates") {
		t.Fatalf("validation error = %v", err)
	}
	tables[77] = service.MonsterDropTable{
		Random: []service.DropGroupAssignment{{GroupID: 9}},
		Groups: map[uint32]service.DropGroup{9: {ID: 9, Candidates: []service.DropGroupCandidate{{ReferenceItemID: 100}}}},
	}
	if err := validateLoadedDropTables(tables); err != nil {
		t.Fatalf("valid table rejected: %v", err)
	}
}
