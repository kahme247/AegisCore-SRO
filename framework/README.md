# GO-SRO framework

`framework` contains the shared runtime used by Gateway, Agent and maintenance
commands. It is part of this monorepo rather than an external dependency.

## Packages

| Package | Responsibility |
| --- | --- |
| `boot` | ordered component startup and shutdown |
| `config` | Viper defaults, environment binding and MySQL pools |
| `db` | target-bound migrations, advisory locking and migration ledger |
| `network` | packet buffers, framing, opcodes, CRC/sequence and security messages |
| `security` | Blowfish/security primitives and admission-ticket mTLS support |
| `server` | TCP listener, sessions, handler dispatch and lifecycle hooks |
| `pk2` | fail-closed bounded reads and archive helpers |
| `reference` | shared reference-data models |
| `logging`, `math`, `utils` | common support code |

Viewer-only code is guarded by `raylib && cgo` build tags so normal Agent,
Gateway and test builds remain headless.

## Safety invariants

- Treat TCP as a stream: packets may be fragmented or coalesced.
- Reject malformed lengths and out-of-bounds PK2 reads instead of recovering
  partial data.
- Keep protocol-specific layouts in `protocol/vsro188` or typed model helpers;
  do not spread unexplained offsets through handlers.
- Keep account and shard migrations bound to their exact database targets.
- Close sessions and run registered cleanup hooks exactly once.
- Treat captured bytes as final wire authority and label inferred constants.

## Known gaps

- the stock-client capture corpus is empty;
- massive `0x600D` and handshake stream behavior need golden end-to-end fixtures;
- older dependencies should be modernized only after protocol baselines are
  stable;
- some legacy `engine` and viewer code remains alongside the active `navmeshv2`
  and service paths and needs consolidation.

## Verification

Run the repository gate from the root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
go test ./framework/...
```

Protocol notes under `docs/` are research material, not stronger authority than
the pinned capture/profile rules in the root README.

## License

See the repository [LICENSE](../LICENSE). The original acknowledgement to
DaxterSoul for packet and file-structure knowledge is retained.
