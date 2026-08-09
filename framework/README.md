# AegisCore framework

`framework` is the shared runtime used by Gateway, Agent and maintenance tools.
It remains part of this monorepo rather than a separately versioned dependency.

## Packages

| Package | Current responsibility |
| --- | --- |
| `boot` | Legacy process-global phased startup registry; no rollback or shutdown orchestration |
| `client` | Reconnecting vSRO TCP client, handshake/module identification, encryption/EDC, massive reassembly and ordered writes |
| `config` | Viper defaults, environment aliases and account/shard DSN construction |
| `db` | Long-lived SQL pools plus target-bound migration catalogs, locks, checksums, dirty ledger and apply/rollback APIs |
| `network` | Packet readers/writers, incremental stream framing, Blowfish wire frames, sequence/CRC, bounded massive codec and opcode constants |
| `security` | Legacy vSRO key/challenge helpers and Blowfish implementation |
| `security/auth` | Bounded one-time admission tickets and private Gateway-to-Agent HTTPS/mTLS transport |
| `security/hashing` | bcrypt password verification and cost-12 hash generation |
| `server` | TCP listener, session registry, handshake/application admission, lifecycle gates, traffic limits, ordered sender and draining |
| `pk2` | Bounded fail-closed PK2 indexing/reads plus retained compatibility paths |
| `reference` | Offline profile/archive provenance and source-manifest/decoder contracts; not a PK2 importer |
| `logging`, `math`, `utils` | Common logging, geometry, region, time, transform, item and formula helpers |

Protocol ownership is still transitional: opcode constants live under
`framework/network/opcode`, while `protocol/vsro188` contains wire-neutral typed
IDs, states, text validation and safe client-request translations.

Optional frontends use separate build tags: `raylib && cgo`, `gtk && cgo`, or
`rendering`. Ordinary Gateway, Agent and test builds remain headless.

## Transport and session behavior

- TCP is decoded as a stream; fragmented headers/payloads and coalesced frames
  have synthetic tests.
- Normal frame payloads are limited to 32,767 bytes.
- Massive `0x600D` logical messages default to a 4 MiB aggregate limit, have a
  16 MiB hard ceiling and expire after 30 seconds.
- `Session.SendMassive` sends a non-interleaving wrapper batch. Ordinary
  `Session.Send` does not automatically fragment oversized logical messages.
- Sessions default to a 30-second idle timeout, five-second write timeout and
  200 validated physical frames per one-second traffic-filter window.
- Handshake, module identification and application opcodes are admitted in
  ordered phases; Agent adds lobby/loading/world lifecycle gates.
- `Session.Close` is idempotent. Registry removal occurs immediately; the
  configured terminal domain hook runs once after inbound work and sender drain.
  The legacy `ConnectionClosed` channel is best-effort only.

These limits are defensive implementation defaults, not capture-proven protocol
constants. Server outbound EDC fields are currently zero and automatic
post-handshake encryption excludes `0x600D` wrappers; both assumptions need
stock-client fixtures.

The public vSRO handshake uses legacy Blowfish plus unauthenticated modular key
exchange. It uses OS randomness and rejects trivial public values, but it is not
modern authenticated encryption and does not perform capture-unsupported
primality/subgroup checks. Gateway-to-Agent ticket installation is a separate
verified mTLS channel.

## Safety invariants

- Reject malformed lengths, unknown state transitions and out-of-bounds PK2
  reads instead of accepting partial data.
- Keep account and shard migrations bound to exact database targets.
- Keep session cleanup idempotent and domain cleanup drain-aware.
- Label inferred wire constants and prefer versioned, sanitized capture evidence.
- Treat admission tickets as single-use, source-bound and process-local; they
  are not durable session storage.

## Known limitations

- Stream framing, massive assembly, handshake and encryption helpers have
  synthetic coverage but no sanitized stock-client golden corpus.
- Actual opcodes and packet layouts are not yet consolidated under
  `protocol/vsro188`.
- The legacy `boot`, DB and PK2 compatibility APIs remain alongside safer paths.
- Admission tickets and active-account reservations are lost on process restart.
- Several older dependencies should be modernized only after protocol baselines
  are stable.

## Verification

Run from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
go test ./framework/... ./protocol/...
go test ./framework/network ./framework/server ./framework/client `
  ./framework/security/...
```

The optional Docker-backed race gate covers selected packages, not the entire
repository:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1 -Race
```

See the [capture policy](../protocol/captures/README.md) and repository
[LICENSE](../LICENSE).
