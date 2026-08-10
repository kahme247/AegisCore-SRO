# AegisCore

AegisCore is a Go implementation of a Silkroad Online server stack focused on
one compatibility boundary: **vSRO 1.188** (Gateway version `188`, profile
`vsro188`). The current deployable
topology is MySQL + Gateway + Agent; game, world, chat, and shard behavior still
lives inside those processes until the protocol and ownership boundaries are
stable enough to justify extracting more services.

This repository is an active recovery and expansion effort, not a
production-complete emulator. Code paths described as implemented have
automated coverage, but wire formats remain provisional until they are backed
by sanitized stock-client captures.

The public project name is AegisCore. Its Go module path is
`github.com/Harvi8/AegisCore`, and local Compose resources use the `aegiscore`
slug. Original Ferdoran identifiers remain only where required for attribution.

## Current architecture

| Component | Default port | Responsibility | Current state |
| --- | ---: | --- | --- |
| MySQL | `3306` | `SRO_ACCOUNT`, `SRO_SHARD`, reference and gameplay state | Compose service plus explicit migrations |
| Gateway | `15779` | patch/profile reply, notices, shard list, login policy, Agent handoff | Implemented and unit-tested |
| Agent | `15882` | character lobby, world, movement, visibility, gameplay handlers | Implemented in stages; stock-client validation is incomplete |
| Admission API | `15883` internal only | one-time Gateway-to-Agent tickets over mTLS | Implemented; intentionally not host-published |

The older six-service diagram under `_docs/architecture.puml` describes a
possible long-term decomposition. It is not the current runtime topology.
The behavior-preserving internal ownership boundaries are documented in
[internal ownership](_docs/internal-ownership.md); they do not add processes or
change the Gateway/Agent wire contract.

## Progress snapshot

| Area | Implemented now | Remaining validation or work |
| --- | --- | --- |
| Build and safety | Go 1.26.5 pin, headless builds, verification script, safe PK2 reader, cache manifest checks | CI/runtime soak and dependency modernization |
| Transport and sessions | framed packet handling, encrypted-session support, session lifecycle, handler dispatch | sanitized fragmentation/coalescing and `0x600D` fixtures |
| Gateway | patch/profile, notices, shard list/ping, credential checks, login rate tracking, admission policy, mTLS ticket install | canonical client hash, locale, Content ID, module/version captures |
| Character lifecycle | list/create/delete scheduling and restoration, join, inventory/configuration serialization, position and quick-slot persistence | rename persistence, guild/academy fields and broader stock-client fixtures |
| World and movement | region/spawn repositories, Data.pk2 navmesh, visibility, authoritative movement, wall/object collision, position checkpoints, teleport | dungeon/neighbour-plane support, memory reduction, broader collision corpus |
| Entities and spawning | staged-to-active player admission, reciprocal player/NPC/mob group spawn/despawn, headings, bounded collision-aware respawn, basic mob AI | capture-backed player field validation, COS/pet/item/structure serializers and broader collision fixtures |
| Character systems | stat allocation, combat/XP/level progression, mastery and skill learning/persistence, death recovery, resurrection, quest abandon and NPC interaction/talk | skill casting breadth, quest lifecycle and capture-backed talk/action masks |
| Economy and inventory | move/equip, storage, guild storage, repair, item use, elixir alchemy, exchange, stall | complete item option/rent/bind formats and transactional live testing |
| Social | local/global/party/stall/whisper chat, party core and matching | guild, academy, jobs, stealth/invisibility rules |
| Data and migrations | target-bound runner through shard `000029`; identity, gameplay, reference catalog, movement, rental, return-city, rate, quick-slot and MySQL 8.4 compatibility schemas | reconcile version gap `000007`, production backup/restore runbook and importer provenance |

Current crash/collision safeguards include:

- current position serialization writes model coordinates in `X,Y,Z` order;
  any alternative wire ordering remains capture evidence to prove rather than
  a reason to swap model axes implicitly;
- normal service NPC spawns advertise talkability with flag `2`; selected-NPC
  responses supply family-specific actions that remain capture-provisional;
- outbound group spawn currently admits players, normal NPCs and mobs. COS and
  specialized structures are deferred instead of emitting malformed `0x3019`
  data that can crash the client;
- random respawn placement uses at most 32 same-region, collision-checked
  candidates before falling back to the authored point.

Mastery upgrades are stored in `CHAR_MASTERY` and included in character join
serialization. A valid mastery row is not itself a world-entry crash cause;
packet alignment and spawn entity type must be checked first.

## Required local data

Game assets, credentials, caches, database backups, and third-party source
checkouts are intentionally ignored by Git.

| Local input | Expected location | Use |
| --- | --- | --- |
| Canonical archive | `Data/Data.pk2` | navmesh and world files |
| Level/skill Media | `Media/Media/server_dep/silkroad/textdata/` | XP, level, mastery and skill reference input |
| Environment | `.env` | protocol identity, advertised Agent address and TLS mount paths; never commit secrets |
| TLS material | `.local/admission-tls/` or external directories | Gateway/Agent mutual TLS |
| Reference DBs | `VSRO_OLDDB/`, `VSRO_TestIn(EPVP)/` | offline comparison only; live AegisCore uses MySQL |
| Research sources | `.local/rsbot/`, `.local/SilkroadLauncher-source/` | packet-reader and launcher cross-checks |
| Prepared cache | configured cache prefix | manifest-bound navmesh acceleration |

The checked-in `protocol/profiles/vsro188.json` currently records this local
Data.pk2 evidence:

- size: `2,136,580,096` bytes;
- SHA-256: `0F6191BA393DF32B2258DEE38CC5AB100DC91210D1FA121C1D3845FDC74D8C3B`;
- PK2 format: `0x01000002`, encrypted;
- profile status: `evidence-pending` because the canonical client executable,
  locale, Content ID and sanitized captures are not recorded yet.

Use the offline verifier without importing or mutating data:

```powershell
go run ./cmd/sro-data verify --profile protocol/profiles/vsro188.json --archive Data/Data.pk2
```

## Running locally

Prerequisites are Docker Desktop and the local data/TLS inputs above. Copy the
required environment names into a private `.env`; do not commit that file.

Compose requires `MYSQL_PASSWORD`, `SRO_AGENT_TLS_DIR`, `SRO_GATEWAY_TLS_DIR`,
`AGENT_ADMISSION_TLS_SERVER_NAME`, `AGENT_PUBLIC_IP`,
`GATEWAY_PROTOCOL_CONTENT_ID`, `GATEWAY_PROTOCOL_MODULE_NAME`, and
`GATEWAY_PROTOCOL_VERSION`. Use `GATEWAY_ENVIRONMENT=development` only when the
advertised Agent address is loopback. The development topology publishes MySQL
`3306` on loopback, Gateway `15779`, and Agent `15882`; admission `15883` remains on
the Compose network.

Fresh database initialization creates no login or GM accounts. Provision the
first administrator explicitly after startup; never add credentials to the
checked-in snapshots or `.env` examples.

```powershell
docker compose up --build -d
docker compose ps
docker compose logs --tail=200 agent-server gateway-server
```

Normal startup does not apply schema migrations. Run each target separately in
an operator-controlled maintenance window:

```powershell
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

See [database operations](db/README.md), [migration safety](db/migrations/README.md),
[Gateway configuration](gateway-server/README.md), and
[Agent behavior](agent-server/README.md). Shared runtime details are in the
[framework guide](framework/README.md), while the maintenance CLIs are covered
by [migrate](cmd/migrate/README.md) and [sro-data](cmd/sro-data/README.md).

Reference-data imports are separate maintenance operations and require the
current shard schema first:

```powershell
go run ./cmd/sro-data import-reference --media Media/Media
go run ./cmd/sro-data import-vsro-skills --export-dir <export-directory>
go run ./cmd/sro-data import-vsro-items --export-dir <export-directory>
```

`GAMEPLAY_PROGRESSION_RATES` and `GAMEPLAY_MOB_RATES` refresh every five
seconds. Migration defaults are 1000% EXP/skill EXP and 100% mob count/respawn;
review them before opening a server. Initial read failure falls back to 100%,
and later refresh failures retain the last valid values.

## Verification

The repository is pinned to Go 1.26.5. On Windows, the verification script uses
`.tools/go/bin/go.exe` when present and otherwise requires that exact version.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1 -Race
```

The normal gate checks formatting, `go vet`, and `go test ./...`. The optional
race gate uses the pinned Linux Go container and requires Docker Desktop.

## Evidence and references

When sources conflict, use this order:

1. sanitized bytes captured from the pinned stock vSRO 1.88 client;
2. the two main documentation references:
   [DummkopfOfHachtenduden/SilkroadDoc](https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki)
   for version-pinned vSRO 1.188 evidence and
   [SRODevs](https://srodevs.gitbook.io/docs) as the primary browsable catalog,
   after verifying the applicable client profile;
3. [RSBot](https://github.com/myildirimofficial/RSBot) readers and
   [JellyBitz SilkroadLauncher](https://github.com/JellyBitz/SilkroadLauncher)
   behavior;
4. original [ferdoran/go-sro](https://github.com/ferdoran/go-sro) code and
   comments;
5. inference, which must be labeled and covered by tests before deployment.

SilkroadDoc is especially useful for the security handshake, massive
`0x600D` framing, Gateway replies, movement branches and state enums. RSBot is
useful as an independent packet-reader cross-check, not as final wire authority.
SRODevs is the main browsable packet-reference index, including its
[Agent catalog](https://srodevs.gitbook.io/docs/packets/agent); confirm the
client profile before relying on a packet layout.
Capture handling rules live in [protocol/captures/README.md](protocol/captures/README.md).
The complete, version-labeled research catalog—including historical emulators
and sources that are leads only—is maintained in [REFERENCES.md](REFERENCES.md).

## Prioritized TODO

### P0 - prevent client crashes and protocol drift

- Build a sanitized stock-client capture corpus for handshake, login, character
  join, spawn/despawn, movement, NPC talk, mastery, combat and inventory.
- Add golden encode/decode fixtures for group spawn `0x3019`, massive `0x600D`
  messages, character data and entity actions.
- Implement explicit COS, pet, dropped-item and structure spawn serializers;
  keep unsupported types fail-closed until then.
- Expand randomized-spawn and collision fixtures across authored regions,
  object meshes and boundary conditions.
- Finish NPC talk/action masks and remove placeholder fields from entity/item
  serializers.

### P1 - complete the playable core

- Capture-validate mastery and skill-learning responses, add skill upgrade and
  broader casting flows, and expand effect/SP-rule coverage.
- Complete damage formulas, skills, buffs, death/respawn and mob tactics.
- Implement full quest accept/progress/complete/reward persistence.
- Finish item magic options, sockets, advanced elixirs, durability, rental and
  binding formats.
- Live-test exchange, storage, guild storage, stalls and party transitions under
  disconnect and concurrent-write conditions.

### P2 - world, operations and expansion

- Replace eager whole-world navmesh retention with region-scoped loading or a
  compact prepared representation; current Compose sizing expects more than
  5 GiB after Data.pk2 world load and limits Agent to 8 GiB.
- Add dungeons, neighbour planes, robust cross-region pathing and spawn audits.
- Add guild/academy/jobs, events, market and administrative observability.
- Resolve migration numbering, document backup/restore drills, pin container
  images, and add repeatable live smoke tests.
- Extract Game/Shard/Chat services only after ownership and protocol contracts
  are stable.

## License and acknowledgement

AegisCore retains the original GO-SRO license; see [LICENSE](LICENSE). The
project builds on [ferdoran/go-sro](https://github.com/ferdoran/go-sro), which
credits DaxterSoul for packet and file-structure knowledge and the related
Ferdoran projects. Current recovery work also cross-checks the open SilkroadDoc,
RSBot and SilkroadLauncher sources listed above.
