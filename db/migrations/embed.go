// Package migrations exposes the checked-in migration SQL as an immutable
// embedded filesystem for the explicit maintenance job. Keeping the files
// beside their source-controlled SQL prevents a container image from silently
// reading a host-mounted or mutable migration directory at runtime.
package migrations

import "embed"

// Files contains every target-specific migration file. Callers must still use
// framework/db.LoadMigrationCatalog with exactly one target directory; this
// package intentionally does not combine account and shard catalogs.
//
//go:embed account/*.sql shard/*.sql
var Files embed.FS
