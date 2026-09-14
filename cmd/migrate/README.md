# Explicit schema migration job

`cmd/migrate` applies every pending migration from exactly one embedded,
target-bound catalog and exits. It is deliberately separate from runtime server
startup.

Use this command only during an explicit database maintenance window. Back up
the selected schema first, run one target, and confirm a successful exit before
running the other target.

From `go-sro/`, the normal Compose commands are:

```text
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

Use the native commands below only when the database environment variables are
already configured in the current shell.

```powershell
go run ./cmd/migrate --target=account
go run ./cmd/migrate --target shard
```

Both `--target=value` and `--target value` are accepted. Values are trimmed and
case-insensitive. Missing or duplicate targets, positional arguments and unknown
flags are rejected; there is no separate `--help` path.

The command opens only the selected pool, loads SQL embedded at build time from
`db/migrations/<target>`, and requires the active database to be exactly
`SRO_ACCOUNT` or `SRO_SHARD`. It does not read a runtime migration directory.

Relevant configuration is:

- account: `DATABASE_ACCOUNT_DRIVER`, `DB_ACCOUNT_HOST`, `DB_ACCOUNT_PORT`,
  `DB_ACCOUNT_USER`, `DB_ACCOUNT_PW`, `DB_ACCOUNT_DATABASE`,
  `DB_ACCOUNT_PARAMS`;
- shard: `DATABASE_SHARD_DRIVER`, `DB_SHARD_HOST`, `DB_SHARD_PORT`,
  `DB_SHARD_USER`, `DB_SHARD_PW`, `DB_SHARD_DATABASE`, `DB_SHARD_PARAMS`.

The jobs wait for database health but do not start during ordinary
`docker compose up`. The image tests migration/framework packages while building
and runs the final binary as UID/GID 65532.

Because the `db` service is long-running, `docker compose up` remains attached
after a one-shot job exits. Inspect the migration container's exit status, or
use a terminating invocation when scripting a gate:

```powershell
docker compose --profile maintenance up --build --abort-on-container-exit --exit-code-from migrate-account migrate-account
```

Run the corresponding command with `migrate-shard` for the shard target. The
current fresh-install snapshot is account `000004` and shard `000066`; existing
deployments should apply only the pending embedded files.

## Operator checklist

1. Stop writes to the selected schema, then take and verify a backup.
2. Confirm `SELECT DATABASE()` and inspect the existing ledger.
3. Review every pending preflight, up, and down file.
4. Run one target and verify its exit status, schema, data, and ledger.
5. Restore writes only after all checks agree.

```sql
SELECT DATABASE();
SELECT version, name, checksum, state, applied_at
FROM aegiscore_schema_migrations
ORDER BY version;
```

A no-op and an applied run both print `<target> migrations applied
successfully`. The CLI has no status, dry-run, version limit or rollback mode and
does not perform a full post-DDL schema comparison. Dirty states require manual
reconciliation, not ledger deletion or automatic retries.

See the [catalog and safety contract](../../db/migrations/README.md).
