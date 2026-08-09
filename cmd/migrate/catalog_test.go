package main

import (
	"testing"

	migrationassets "github.com/Harvi8/AegisCore/db/migrations"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

func TestEmbeddedCatalogsLoadForEveryMigrationTarget(t *testing.T) {
	t.Parallel()

	for _, target := range []frameworkdb.MigrationTarget{
		frameworkdb.MigrationTargetAccount,
		frameworkdb.MigrationTargetShard,
	} {
		target := target
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()
			catalog, err := frameworkdb.LoadMigrationCatalog(migrationassets.Files, string(target))
			if err != nil {
				t.Fatalf("LoadMigrationCatalog embedded %s migrations: %v", target, err)
			}
			if catalog.Target() != target {
				t.Fatalf("embedded catalog target = %q, want %q", catalog.Target(), target)
			}
			if len(catalog.Migrations()) == 0 {
				t.Fatal("embedded catalog has no migrations")
			}
		})
	}
}
