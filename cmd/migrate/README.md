# Explicit schema migration job

`cmd/migrate` applies exactly one embedded, target-bound catalog and exits. It
is deliberately separate from Agent and Gateway startup.

```powershell
go run ./cmd/migrate --target=account
go run ./cmd/migrate --target=shard
```

The command accepts exactly one `--target` value. It opens only that target's
configured MySQL pool, loads SQL embedded from `db/migrations/<target>`, and
requires the selected database to be exactly `SRO_ACCOUNT` or `SRO_SHARD` as
appropriate.

For Compose, run one profile-gated job at a time:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

## Operator checklist

1. Stop writes for the selected schema.
2. Take and verify a backup.
3. Review the preflight and up SQL.
4. Run one target and inspect its exit status.
5. Verify the actual schema and `go_sro_schema_migrations` ledger.
6. Restore writes only after both agree.

The Compose jobs wait for database health, but normal `docker compose up` does
not start them. A failed or uncertain DDL transition deliberately leaves a
dirty ledger state; do not automate retries. Full invariants and the current
catalog are documented in [db/migrations/README.md](../../db/migrations/README.md).
