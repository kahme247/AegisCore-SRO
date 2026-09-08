# Repository and workspace layout

This repository intentionally keeps source code separate from large local runtime assets and analysis output.

## Tracked source directories

- `agent-server/` — Agent runtime, gameplay services, handlers, models, and managers.
- `gateway-server/` — Gateway runtime and login/session admission.
- `smc-server/` — SMC management service.
- `framework/` — Shared networking, database, security, scheduling, and server infrastructure.
- `protocol/` — vSRO protocol profiles, codecs, captures, and compatibility tests.
- `patching/` — Patch catalog and client patching logic.
- `cmd/` — Maintenance and data-import commands.
- `db/` — Schema snapshots, reversible migrations, and database tooling.
- `deployment/` — Deployment contracts and Compose-facing checks.
- `scripts/` — Small repository maintenance scripts.
- `_docs/` — Technical investigations, packet notes, and operational documentation.

## Intentionally ignored local directories

These directories are local inputs or generated output and must not be committed:

- `.local/` — Local runtime state, crash captures, session traces, backups, generated binaries, and experiments.
- `.tools/` — Local build products and tool caches.
- `.cache/`, `.gocache/`, `.gotmp/` — Regenerable Go/build caches.
- `Data/`, `Media/`, `Patch/`, `PatchSource/`, `VSRO_Client/`, `VSRO_OLDDB/`, `VSRO_TestIn(EPVP)/` — Client assets, patch material, and legacy database files.
- `db/DB_BKUP/` and `db/media/` — Local database backups and imported media.

Do not remove `.local/backups`, `.local/session-traces`, crash dumps, or database backups as part of ordinary source cleanup. They may be needed to reproduce incidents or validate migrations.

## Preserved nested repositories

The following nested repositories are retained deliberately because they are local working copies or build snapshots, not part of the root repository history:

- `.local/deploy-cos-login/` — Detached deployment/debug snapshot.
- `.tools/clean-agent-build/` — Clean AgentServer build snapshot.
- `.local/SilkroadLauncher-source/.../SRO.PK2API/` — Nested upstream/source checkout.

Keep them ignored and do not stage their contents from the root repository. If one becomes an active development target, work on its own branch or move it to a separate workspace before making changes.

## Housekeeping rules

1. Keep source, migrations, tests, and operational documentation in tracked directories.
2. Keep dumps, captures, binaries, generated reports, caches, and database backups in ignored local directories.
3. Do not use `git clean -x` in this repository without reviewing the complete candidate list first.
4. Before deleting a local artifact, verify whether it is the only copy of a crash dump, backup, or reverse-engineering result.
5. Run `git status --short --ignored` after cleanup to confirm that only expected local directories remain ignored.
