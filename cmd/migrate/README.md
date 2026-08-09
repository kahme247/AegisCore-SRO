# Explicit schema migration job

`cmd/migrate` applies every pending migration from exactly one embedded,
target-bound catalog and exits. It is deliberately separate from runtime server
startup.

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

For Compose, run one profile-gated job at a time:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

The jobs wait for database health but do not start during ordinary
`docker compose up`. The image tests migration/framework packages while building
and runs the final binary as UID/GID 65532.

## Operator checklist

1. Stop writes that can touch the selected schema.
2. Take and verify a backup.
3. Confirm `SELECT DATABASE()` and inspect the existing ledger.
4. Review every pending preflight, up and down file.
5. Run one target and inspect its exit status.
6. Verify the actual schema, data and migration ledger.
7. Restore writes only after all three agree.

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
