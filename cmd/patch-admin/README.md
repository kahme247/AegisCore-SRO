# Patch administration

`patch-admin` is the explicit builder and release-control tool for the optional
DownloadServer path. It never scans
or watches a directory for publication. An administrator supplies a reviewed
JSON build specification listing every changed file.

Before first use, back up `SRO_ACCOUNT`, stop Gateway writes, and apply account
migration `000004_patch_release_catalog`. Normal service startup does not run
migrations.

## 188 to 189 workflow

1. Place the approved extracted target files beneath a controlled source root.
   For Media, generate a review candidate by comparing the deployed archive
   with that tree:

```powershell
go run ./cmd/patch-admin scan-media --archive .\VSRO_Client\Media.pk2 `
  --target-root .\Media\Media --output .\PatchSource\188-to-189.json
```

The scanner excludes `SV.T`, reports non-ASCII names that the legacy launcher
cannot safely parse, and does not create a release. Review every detected file.

For an `sro-data export-media` result, verify and scan only its manifest-listed
files:

```powershell
go run ./cmd/patch-admin scan-media --archive .\VSRO_Client\Media.pk2 `
  --target-root .\.local\media-export-189 `
  --export-manifest .\.local\media-export-189\media-export-manifest.json `
  --output .\PatchSource\188-to-189.json
```

The command checks every exported file's size and SHA-256 before creating the
review specification. It never includes `media-export-manifest.json` in the
client patch.

2. Review the generated specification (or the checked-in example); every
   target path must be explicit and administrator-approved.
3. Build immutable payloads and create a database draft:

```powershell
go run ./cmd/patch-admin build `
  --spec .\cmd\patch-admin\example-188-to-189.json `
  --source-root .\PatchSource `
  --storage-root .\Patch\files `
  --manifest .\Patch\manifest.json
```

When the specification came from `--export-manifest`, set `--source-root` to
that Media export directory instead of `PatchSource`.

The command prints the new release ID. It creates the launcher-compatible
payload wrapper, both SHA-256 hashes, database release/file rows, numeric file
IDs, and the DownloadServer manifest. Rebuilding the same source produces the
same prepared payload hash, but a duplicate database release is rejected.

4. Register the externally reachable DownloadServer. Leave it disabled until
the endpoint is ready:

```powershell
go run ./cmd/patch-admin register-server --host patch.example.com --port 15880
go run ./cmd/patch-admin register-server --host patch.example.com --port 15880 --enabled
```

5. Verify storage and move the draft to the testing channel:

```powershell
go run ./cmd/patch-admin testing --release-id <id> --expected-current 188 `
  --storage-root .\Patch\files --manifest .\Patch\manifest.json
```

Restart the test DownloadServer after changing its manifest. Run a separate
Gateway with `GATEWAY_PATCH_CHANNEL=testing`, then test a clean version-188
client through download, PK2 import, `SV.T=189`, launcher restart, game login,
and a second version check.

6. Only after that manual test succeeds, promote with a second complete
storage/hash verification:

```powershell
go run ./cmd/patch-admin promote --release-id <id> --expected-current 188 `
  --storage-root .\Patch\files --manifest .\Patch\manifest.json
```

Production Gateway uses `GATEWAY_PATCH_CHANNEL=published` by default. Promotion
is the point at which production begins advertising 189; it is intentionally
not part of `build` or `testing`.

## Subsequent versions and chaining

Each release is a single incremental link: version `N+1` must name `N` as its
base version. To publish 190 after 189, build and test a separate 189-to-190
specification and promote it with `--expected-current 189`. The database keeps
the older published release as a retired, still-downloadable chain link.

When a version-188 client asks for a live version 190, the catalog validates
the complete `188 -> 189 -> 190` sequence and returns both releases' files in
release-version order, then ordinal order within each release. A version-189
client receives only the 190 files. Missing, skipped, duplicated, or
out-of-order links are rejected as too old; the Gateway must never advertise a
target that cannot be reached through a complete incremental chain. Keep all
prepared payloads and manifest entries needed by the minimum supported patch
version before promoting the next release.

The Docker equivalent uses the `patch-admin` Compose service and the ignored
`PatchSource`/`Patch` mounts, for example:

```powershell
docker compose --profile patch-admin run --rm patch-admin build --spec /etc/aegiscore/spec.json --source-root /source
```
