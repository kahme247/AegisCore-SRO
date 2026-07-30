# SRO Nexus database

SRO Nexus uses MySQL with two logical schemas:

- `SRO_ACCOUNT` stores accounts, credentials, notices and shard admission data.
- `SRO_SHARD` stores characters, inventory, world/reference data and gameplay
  persistence. Some shard tables reference `SRO_ACCOUNT`, so the schemas are
  logically separate but not fully independent.

Compose currently uses the external `ferdoran/go-sro-db:latest` image and
publishes `3306`/`33060` for development. The repository cannot prove that this
mutable image exactly matches the checked-in SQL. Production deployments should
pin an immutable image, verify its bootstrap contents and keep DB ports private.

## Schema sources

- `create_sro_account.sql` and `create_sro_shard.sql` are destructive historical
  bootstrap dumps. They drop/recreate tables, include sample data, and do not
  represent the fully migrated schema.
- The baseline dumps are not consumed by `cmd/migrate`; the shard dump also
  contains `DELIMITER` syntax that the migration parser intentionally rejects.
- `grant_user_permissions.sql` grants the application user access to both
  schemas.
- `migrations/account` and `migrations/shard` are the embedded forward catalog.
- `cmd/migrate` is the checked-in migration entry point.

The account catalog currently ends at `000001`. The shard catalog reaches
`000026` and covers normalized identity, inventory constraints, quests,
economy/storage, shops, teleports, mastery/skills, guild storage, reference
catalogs, movement settings, rental metadata, return-city recovery, progression
and mob rates, and character quick slots. Version `000007` is absent; preserve
that gap until deployed ledgers and historical intent are reconciled.

## Operations

Agent and Gateway never apply migrations. Stop dependent writes, back up the
target, review SQL, and run one target at a time:

```powershell
go run ./cmd/migrate --target=account
go run ./cmd/migrate --target=shard
```

Or use the profile-gated Compose jobs:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

Each target owns a `go_sro_schema_migrations` ledger and requires
`SELECT DATABASE()` to match exactly. Dirty `applying` or `rolling_back` states
require manual schema/ledger reconciliation; do not delete rows or blindly
retry uncertain MySQL DDL.

Reference-data importers are separate maintenance tools, not migrations:

```powershell
go run ./cmd/sro-data import-reference --media Media/Media
go run ./cmd/sro-data import-vsro-skills --export-dir <export-directory>
go run ./cmd/sro-data import-vsro-items --export-dir <export-directory>
```

Local MSSQL backups under `VSRO_OLDDB/` and `VSRO_TestIn(EPVP)/` are ignored
comparison inputs, not the live SRO Nexus database.

See [migration safety](migrations/README.md), the
[migration command](../cmd/migrate/README.md), and the
[data tool](../cmd/sro-data/README.md).
