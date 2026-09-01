# AegisCore database

AegisCore uses MySQL with two logical schemas:

- `SRO_ACCOUNT` stores accounts, credentials, notices and shard admission data.
- `SRO_SHARD` stores characters, inventory, world/reference data and gameplay
  persistence. Some shard tables reference `SRO_ACCOUNT`, so the schemas are
  logically separate but not fully independent.

Compose builds `aegiscore-db:8.4.10` from the repository-owned `db/Dockerfile`, whose
official MySQL base is pinned by digest. A new server restores the SQL dump
selected by `DB_BACKUP_FILE`, then applies the application-user grants. The
backup is bind-mounted and is not copied into an image layer.
Development publishes classic MySQL `3306` on loopback only; production should
remove that host binding and keep database traffic on the private network.

## Schema sources

- `create_sro_account.sql` and `create_sro_shard.sql` are destructive fresh-install
  reference snapshots retained for schema review and testing. They include the final schema, migration-owned reference/backfill
  data, and applied migration ledgers through account `000004` and shard
  `000066` (with the intentional `000007` gap).
- `DB_BACKUP_FILE` is the required Compose bootstrap source for an empty
  `mysql-data` volume. It must be a `mysqldump --databases` backup containing
  both `SRO_ACCOUNT` and `SRO_SHARD`.
- The snapshots are not consumed by `cmd/migrate`; they must be loaded by a
  MySQL-capable client. Their pre-seeded ledgers let `cmd/migrate` verify the
  embedded catalog and apply only migrations added after the snapshot.
- `grant_user_permissions.sql` grants the application user access to both
  schemas.
- `migrations/account` and `migrations/shard` are the embedded forward catalog.
- Shard migration `000071_database_chat_abuse_filter` adds the runtime-owned
  abuse-term catalog used by Agent chat. Update its rows through explicit
  database maintenance; Agent refreshes enabled terms without a restart.
- `cmd/migrate` is the checked-in migration entry point.

Changing `DB_BACKUP_FILE`, `MYSQL_PASSWORD`, or the reference snapshots does
not update an existing `mysql-data` volume. Backup contents may include login,
character, and inventory records and must remain ignored and access-controlled.

The account catalog currently ends at `000004`, including the controlled patch
release catalog. The shard catalog reaches `000066` and covers normalized
identity, character limits and creation policy, Item Mall balances and
purchases, timed services, inventory constraints, quests, economy/storage,
shops, teleports, mastery/skills, guild storage, reference catalogs, movement
settings, rental metadata, return-city recovery, progression and mob rates,
quick slots, durable speed effects, MySQL 8.4 compatibility, character
buy-back, experience services, active buffs, COS/pets, equipment and avatar
systems, alchemy references, Hwan progression, three-page inventory and social
systems plus aggregate versions, command idempotency, the durable outbox and
lease-based outbox dispatch recovery plus persistent scheduler definitions,
leased execution and run history, character deletion lifecycle and recovered PK
administration state.
Version `000007` is absent; preserve that gap until deployed ledgers
and historical intent are reconciled.

Character creation enables the imported Chinese and European model ranges.
European characters start at the Constantinople gate and select level-one
European weapon and armor families from `ITEMDATA`.

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

Each target owns an `aegiscore_schema_migrations` ledger and requires
`SELECT DATABASE()` to match exactly. Dirty `applying` or `rolling_back` states
require manual schema/ledger reconciliation; do not delete rows or blindly
retry uncertain MySQL DDL.

Reference-data importers are separate maintenance tools, not migrations:

```powershell
go run ./cmd/sro-data import-reference --media Media/Media
go run ./cmd/sro-data import-media-catalogs --media Media/Media
go run ./cmd/sro-data import-vsro-skills --export-dir <export-directory>
go run ./cmd/sro-data import-vsro-items --export-dir <export-directory>
go run ./cmd/sro-data import-item-mall --media Media/Media
go run ./cmd/sro-data import-event-rewards --media Media/Media
go run ./cmd/sro-data export-media --output .local/media-export-189
```

The reference import requires the client-matched `levelgold.txt` catalog and
stores its `GoldMin`/`GoldMax` values in `REF_LEVEL_DATA`. Agent uses `GoldMin`
at the character's historical maximum level for paid skill/mastery restoration.

Local MSSQL backups under `VSRO_OLDDB/` and `VSRO_TestIn(EPVP)/` are ignored
comparison inputs, not the live AegisCore database.

The client-visible server name is stored in `SRO_ACCOUNT.SHARD.NAME`. Gateway
reads it on every shard-list request, so no process restart is required after a
valid update:

```sql
UPDATE SRO_ACCOUNT.SHARD SET NAME = 'AegisCore' WHERE ID = 1;
```

Keep the value nonblank, trimmed, valid UTF-8 and within the column's
32-character limit.

See [migration safety](migrations/README.md), the
[migration command](../cmd/migrate/README.md), and the
[data tool](../cmd/sro-data/README.md).
