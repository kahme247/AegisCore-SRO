# Database migrations

`framework/db` owns embedded, target-bound migration catalogs. Migration is an
explicit maintenance operation; Agent and Gateway never start the runner.

## Current catalog

| Target | Required database | Versions |
| --- | --- | --- |
| `account` | exactly `SRO_ACCOUNT` | `000001` normalized username |
| `shard` | exactly `SRO_SHARD` | `000001` normalized character name; `000002` unique inventory link; `000003` quests; `000004` economy/storage; `000005` shop catalog; `000006` teleports; `000008` masteries; `000009` guild storage; `000010` character skills; `000011`-`000014` default mastery/skill/quest data; `000015` Media level/skill data; `000016`-`000019` native vSRO skill catalog and compatibility; `000020` native item catalog; `000021` movement settings; `000022` rental metadata; `000023` return city; `000024` progression rates; `000025` mob rates; `000026` quick slots |

The catalog may be non-contiguous, so a clean ledger proceeds from `000006` to
`000008`. A ledger row for missing version `000007` is rejected as unknown
history. Do not add or renumber that version without reconciling every deployed
ledger.

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
3. Acquires target lock `go-sro:migrations:account` or
   `go-sro:migrations:shard`, with a default 30-second wait.
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
