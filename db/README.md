# GO-SRO database

GO-SRO uses MySQL with two logical schemas:

- `SRO_ACCOUNT` for accounts, credentials and shard admission state;
- `SRO_SHARD` for characters, inventory, world/reference data and gameplay
  persistence.

Compose starts the existing `ferdoran/go-sro-db:latest` image and publishes
`3306`/`33060` for local development. Production deployments should pin an
immutable image and keep database ports private.

## Schema sources

- `create_sro_account.sql` and `create_sro_shard.sql` are the baseline schema;
- `grant_user_permissions.sql` grants the application account access;
- `migrations/account` and `migrations/shard` contain forward evolution;
- `cmd/migrate` is the only supported checked-in migration entry point.

Current migrations cover normalized account/character identity, unique
inventory item links, character quests, economy/storage, shop reference data,
teleports, character masteries and guild storage. Shard version `000007` is not
present in the current tree; preserve the gap until its historical intent is
resolved rather than silently renumbering applied files.

## Operations

Normal Agent/Gateway startup never applies migrations. Stop writes, back up the
target schema, review preflight SQL and run exactly one maintenance target:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

Each target maintains its own `go_sro_schema_migrations` ledger and verifies
`SELECT DATABASE()` before DDL. Dirty `applying` or `rolling_back` states require
manual reconciliation; do not delete ledger rows or auto-retry uncertain DDL.

Local MSSQL vSRO reference backups under `VSRO_OLDDB/` and
`VSRO_TestIn(EPVP)/` are comparison inputs only. They are ignored by Git and are
not the live GO-SRO database.

See [migration safety](migrations/README.md) and the
[maintenance command](../cmd/migrate/README.md).
