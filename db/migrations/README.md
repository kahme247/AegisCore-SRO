# Database migrations

`framework/db` owns embedded, target-bound migration catalogs. Migration is an
explicit maintenance operation; Agent and Gateway never start the runner.

## Current catalog

| Target | Required database | Versions |
| --- | --- | --- |
| `account` | exactly `SRO_ACCOUNT` | `000001` normalized username; `000002` Silk, Gift Silk and Silk Point balances; `000003` per-shard/default character limits; `000004` controlled patch releases, files, hashes and DownloadServer endpoints |
| `shard` | exactly `SRO_SHARD` | `000001`-`000053` core history; `000054`-`000066` native quest catalogs, declarations, objectives and rewards; `000067`-`000081` runtime/reference extensions, including drop catalogs, alchemy-merit, ferry teleport data, event rewards, Media text catalogs, and the separate party EXP rate (with intentional `000007` gap) |

The catalog may be non-contiguous, so a clean ledger proceeds from `000006` to
`000008`. A ledger row for missing version `000007` is rejected as unknown
history. Do not add or renumber that version without reconciling every deployed
ledger.

The fresh-install snapshots in `db/create_sro_account.sql` and
`db/create_sro_shard.sql` already contain the catalog's final schema, data
backfills and exact `applied` ledger rows. Running this command after loading
those snapshots is therefore a checksum/history verification and a no-op until
a newer migration is added. Existing deployments must continue using the
migration runner rather than reloading the destructive snapshots.

## Files and immutability

```text
000001_description.preflight.sql  # optional SELECT-only check
000001_description.up.sql         # required
000001_description.down.sql       # required
```

The SHA-256 ledger checksum covers version, name, preflight SQL, up SQL and down
SQL exactly, including line endings. Applied content is immutable.

Preflights must begin with `SELECT`; `WITH` and mutation statements are
rejected. Any returned row fails the migration. Current preflights guard account
username normalization, shard character-name normalization and duplicate
inventory item links.

## Runner guarantees

The runner:

1. Binds one catalog to one target and pins one MySQL connection.
2. Verifies case-sensitive `SELECT DATABASE()` before creating the ledger or
   executing migration DDL.
3. Acquires target lock `aegiscore:migrations:account` or
   `aegiscore:migrations:shard`, with a default 30-second wait.
4. Rejects dirty state, unknown versions, changed names/checksums and
   out-of-order history.
5. Records `applying`/`rolling_back` around DDL and discards the physical
   connection when lock release is uncertain.

MySQL DDL can commit implicitly, so schema and ledger changes are not one atomic
transaction. A dirty state deliberately blocks automatic continuation. Inspect
the real schema and ledger; never delete a dirty row only to unblock the tool.

Legacy ledgers without `state` are upgraded in place and existing rows become
`applied`. This does not verify the resulting schema beyond ledger history.

## CLI scope

```powershell
go run ./cmd/migrate --target=account
go run ./cmd/migrate --target=shard
```

Or run one profile-gated Compose job:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

The CLI applies every pending migration in ascending order. It has no status,
dry-run, target-version, step, preflight-only or rollback command. Framework
rollback exists for programmatic callers, but down SQL can destroy newly stored
data and is not necessarily lossless.

Back up first, stop affected writes, review both directions, inspect the ledger,
and perform migration-specific schema/data checks before restoring traffic.
