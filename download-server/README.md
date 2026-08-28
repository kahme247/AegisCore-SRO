# DownloadServer

This service implements the launcher-facing Joymax file-transfer endpoint. It
does not build patches, select the live release, or modify PK2 archives. The
database/catalog decisions belong to Gateway and `cmd/patch-admin`; the
existing launcher owns PK2 import after it downloads and decompresses each
prepared payload.

Implemented wire flow:

1. Complete the normal Silkroad security and `0x2001` module-identification exchange.
2. Server sends empty `0x6005` (ready).
3. Launcher sends `0x6004`: `uint32 file_id`, `uint32 resume_offset`.
4. Server streams prepared bytes in `0x1001` packets.
5. Server sends `0xA004` with byte `1` for success or `0` for a catalog/request failure.

The current launcher sends a zero offset, but the server supports safe resume.
It streams files rather than loading a complete patch into memory.

## Operations

The protocol listener is `15880` by default and is separate from the HTTP health
listener on port `15881`:

- `GET /healthz` confirms the process is alive.
- `GET /readyz` returns `200` only after both listeners bind, and returns
  `503` while starting or draining.

Transfers are fail-fast limited by `DOWNLOAD_MAX_TRANSFERS` (default `32`)
globally and `DOWNLOAD_MAX_TRANSFERS_PER_IP` (default `4`) per source IP. An
over-limit legacy request receives `0xA004 [00]`; the server does not queue an
unbounded number of open files. Every completed, rejected, or failed request
emits one structured audit record with client IP, file ID, resume offset,
bytes sent, result, reason, and duration. Shutdown marks readiness false,
stops accepting sessions, and drains active transfers for up to the service's
shutdown deadline.

## Catalog

DownloadServer uses a filesystem serving manifest while the database remains
authoritative for releases, versions, hashes, and Gateway negotiation. It does
not query MySQL at runtime.
`cmd/patch-admin build` generates and merges this serving view after assigning
the database file IDs.

Create an ignored local `Patch` directory:

```text
Patch/
  manifest.json
  files/
    189/
      media-itemdata.bin
```

Example `manifest.json`:

```json
{
  "files": [
    {
      "id": 1,
      "storage_path": "189/media-itemdata.bin",
      "size": 12345,
      "sha256": "0000000000000000000000000000000000000000000000000000000000000000"
    }
  ]
}
```

`size` is the size of the prepared download payload, not the decompressed file.
Startup verifies every record. Requests also recheck the size, reject unknown
IDs and offsets beyond EOF, and never convert client input into a path. Paths
that escape the configured root, including through symlinks, are rejected.

## Run

The service remains opt-in until a tested release is ready:

```powershell
docker compose --profile patching up --build -d download-server
docker compose logs --tail=200 download-server
```

Use `SRO_PATCH_DIR` when the catalog is not at `./Patch`. The mounted directory
is read-only. Useful settings are `DOWNLOAD_PUBLIC_PORT`,
`DOWNLOAD_CHUNK_SIZE`, `DOWNLOAD_MODULE_ID`, and `DOWNLOAD_SESSION_TRACE`.
`DOWNLOAD_SESSION_IDLE_SECONDS` defaults to 30 minutes so a slow transfer is
not cut off by the shared transport's shorter pre-application idle budget.
The concurrency settings are `DOWNLOAD_MAX_TRANSFERS` and
`DOWNLOAD_MAX_TRANSFERS_PER_IP`; the health bind can be changed with
`DOWNLOAD_HEALTH_HOST` and `DOWNLOAD_HEALTH_PORT`.

Run focused tests from the `go-sro` worktree:

```powershell
go test ./download-server/... ./framework/server ./framework/network/...
```

DownloadServer must be restarted after its manifest changes because it verifies
and loads the complete catalog at startup. See `cmd/patch-admin/README.md` for
the controlled 188-to-189 build, testing, and promotion workflow.
