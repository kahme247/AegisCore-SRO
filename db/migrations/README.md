# Database migrations

`framework/db` owns a target-bound migration catalog and runner. Migrations are
an explicit maintenance operation: Agent and Gateway neither import nor start
the runner.

## Targets and current catalog

| Target | Required database | Versions in this tree |
| --- | --- | --- |
| `account` | exactly `SRO_ACCOUNT` | `000001` normalized username |
| `shard` | exactly `SRO_SHARD` | `000001` normalized character name, `000002` unique inventory link, `000003` quests, `000004` economy/storage, `000005` shop catalog, `000006` teleports, `000008` masteries, `000009` guild storage, `000010` character skills, `000011` default mastery rows, `000012` reference default masteries, `000013` reference default skills, `000014` reference default quests, `000015` reference level and skill data, `000016` native vSRO skill catalog, `000017` vSRO unsigned tinyint compatibility, `000018` vSRO group-reference compatibility, `000019` vSRO item-opt-level link compatibility, `000020` vSRO item reference catalog, `000021` gameplay movement settings |

Shard version `000007` is intentionally documented as unresolved. Do not
renumber later files: deployed ledgers may already identify those versions.

## File layout

```text
000001_description.preflight.sql  # optional SELECT-only checks
000001_description.up.sql
000001_description.down.sql
```

Applied migration content is immutable. The ledger checksum covers preflight,
up and down SQL. A preflight query fails when it returns any row; only `SELECT`
preflight statements are accepted so a mistaken DML statement cannot mutate
data through the preflight path.

## Concurrency and dirty states

The runner:

1. binds a catalog to one target;
2. pins one MySQL connection;
3. verifies `SELECT DATABASE()` with case-sensitive target matching;
4. holds a target-specific `GET_LOCK` advisory lock;
5. records short ledger transitions around DDL.

MySQL DDL may commit implicitly. If DDL or a ledger transition becomes
uncertain, the row remains `applying` or `rolling_back` and automatic retries
stop. Reconcile the real schema and ledger with an operator; never delete the
dirty row merely to make the command run again.

Identity migrations add stored generated columns and may rebuild tables. Stop
account creation, character creation, rename and deletion writes during the
maintenance window. The duplicate preflight is not a live-write reservation.

## Running migrations

```powershell
go run ./cmd/migrate --target=account
go run ./cmd/migrate --target=shard
```

Or use the profile-gated Compose jobs one at a time:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

Back up first, review every new SQL file, and verify the resulting schema and
ledger before restoring application writes.
