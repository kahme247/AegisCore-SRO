# SRO Nexus database

SRO Nexus uses MySQL with two logical schemas:

- `SRO_ACCOUNT` stores accounts, credentials, notices and shard admission data.
- `SRO_SHARD` stores characters, inventory, world/reference data and gameplay
  persistence. Some shard tables reference `SRO_ACCOUNT`, so the schemas are
  logically separate but not fully independent.

Compose builds `gosro-db:8.4.10` from the repository-owned `db/Dockerfile`, whose
official MySQL base is pinned by digest and whose bootstrap order is explicit.
Development publishes classic MySQL `3306` on loopback only; production should
remove that host binding and keep database traffic on the private network.

## Schema sources

- `create_sro_account.sql` and `create_sro_shard.sql` are destructive fresh-install
  snapshots. They include the final schema, migration-owned seed/backfill data,
  sample data, and applied migration ledgers through account `000001` and shard
  `000029` (with the intentional `000007` gap).
- The snapshots are not consumed by `cmd/migrate`; they must be loaded by a
  MySQL-capable client. Their pre-seeded ledgers let `cmd/migrate` verify the
  embedded catalog and apply only migrations added after the snapshot.
- `grant_user_permissions.sql` grants the application user access to both
  schemas.
- `migrations/account` and `migrations/shard` are the embedded forward catalog.
- `cmd/migrate` is the checked-in migration entry point.

The account catalog currently ends at `000001`. The shard catalog reaches
`000029` and covers normalized identity, inventory constraints, quests,
economy/storage, shops, teleports, mastery/skills, guild storage, reference
catalogs, movement settings, rental metadata, return-city recovery, progression
and mob rates, character quick slots, durable speed effects, and MySQL 8.4
foreign-key compatibility. Version `000007` is absent; preserve that gap until
deployed ledgers and historical intent are reconciled.

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
