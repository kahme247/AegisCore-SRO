# GO-SRO

GO-SRO is a Go implementation of a Silkroad Online server stack. This branch
targets one compatibility boundary: **vSRO 1.88**. The current deployable
topology is MySQL + Gateway + Agent; game, world, chat, and shard behavior still
lives inside those processes until the protocol and ownership boundaries are
stable enough to justify extracting more services.

This is an active recovery and expansion branch, not a production-complete
emulator. Code paths described as implemented have automated coverage, but wire
formats remain provisional until they are backed by sanitized stock-client
captures.

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
| Character lifecycle | list/create/delete/rename/join, inventory serialization, position persistence | guild/academy fields and broader stock-client fixtures |
| World and movement | region/spawn repositories, Data.pk2 navmesh, visibility, authoritative movement, wall/object collision, position checkpoints, teleport | dungeon/neighbour-plane support, memory reduction, broader collision corpus |
| Entities and spawning | players, NPCs and mobs, group spawn/despawn, headings, respawn, basic mob AI | COS/structure group-spawn encoders and collision-safe random spawn sampling |
| Character systems | basic combat/XP/level progression, mastery upgrade/persistence, quest abandon, NPC interaction/talk | skill learning/casting breadth, quest lifecycle, exact talk/action masks |
| Economy and inventory | move/equip, storage, guild storage, repair, item use, elixir alchemy, exchange, stall | complete item option/rent/bind formats and transactional live testing |
| Social | local/global/party/stall/whisper chat, party core and matching | guild, academy, jobs, stealth/invisibility rules |
| Data and migrations | explicit target-bound migration runner, identity/inventory/quest/economy/shop/teleport/mastery/guild-storage schemas | reconcile version gap `000007`, production backup/restore runbook |

Recent crash/collision work corrected three important vSRO 1.88 details:

- world/navmesh coordinates remain `X,Y,Z`, while the spawn wire layout is
  `X,Z,Y`; swapping the model axes puts entities inside walls;
- neutral NPC spawn talk flags serialize as zero until a verified interaction
  mask is known;
- outbound group spawn currently admits normal NPCs and mobs only. COS and
  specialized structures are deferred instead of emitting malformed `0x3019`
  data that can crash the client.

Mastery upgrades are stored in `CHAR_MASTERY` and included in character join
serialization. A valid mastery row is not itself a world-entry crash cause;
packet alignment and spawn entity type must be checked first.

## Required local data

Game assets, credentials, caches, database backups, and third-party source
checkouts are intentionally ignored by Git.

| Local input | Expected location | Use |
| --- | --- | --- |
| Canonical archive | `Data/Data.pk2` | navmesh and world files |
| Level table | `Media/server_dep/silkroad/textdata/leveldata.txt` | XP, level and mastery limits |
| Environment | `.env` | local ports, protocol profile and TLS paths; never commit secrets |
| TLS material | `.local/admission-tls/` or external directories | Gateway/Agent mutual TLS |
| Reference DBs | `VSRO_OLDDB/`, `VSRO_TestIn(EPVP)/` | offline comparison only; live GO-SRO uses MySQL |
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
[Agent behavior](agent-server/README.md).

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
2. [DummkopfOfHachtenduden/SilkroadDoc](https://github.com/DummkopfOfHachtenduden/SilkroadDoc)
   vSRO 1.188 packet and file notes;
3. [RSBot](https://github.com/myildirimofficial/RSBot) readers and
   [JellyBitz SilkroadLauncher](https://github.com/JellyBitz/SilkroadLauncher)
   behavior;
4. original [ferdoran/go-sro](https://github.com/ferdoran/go-sro) code and
   comments;
5. inference, which must be labeled and covered by tests before deployment.

SilkroadDoc is especially useful for the security handshake, massive
`0x600D` framing, Gateway replies, movement branches and state enums. RSBot is
useful as an independent packet-reader cross-check, not as final wire authority.
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
- Validate random spawn candidates against a loaded, walkable navmesh region
  instead of retrying an unbounded radius sample.
- Finish NPC talk/action masks and remove placeholder fields from entity/item
  serializers.

### P1 - complete the playable core

- Expand mastery into skill learn/upgrade/cast flows and validate SP rules.
- Complete damage formulas, skills, buffs, death/respawn and mob tactics.
- Implement full quest accept/progress/complete/reward persistence.
- Finish item magic options, sockets, advanced elixirs, durability, rental and
  binding formats.
- Live-test exchange, storage, guild storage, stalls and party transitions under
  disconnect and concurrent-write conditions.

### P2 - world, operations and expansion

- Replace eager whole-world navmesh retention with region-scoped loading or a
  compact prepared representation; current Compose sizing expects more than
  5 GiB after Data.pk2 world load and reserves 7 GiB for Agent.
- Add dungeons, neighbour planes, robust cross-region pathing and spawn audits.
- Add guild/academy/jobs, events, market and administrative observability.
- Resolve migration numbering, document backup/restore drills, pin container
  images, and add repeatable live smoke tests.
- Extract Game/Shard/Chat services only after ownership and protocol contracts
  are stable.

## License and acknowledgement

GO-SRO retains the repository's original license; see [LICENSE](LICENSE).
The original project credits DaxterSoul for packet/file-structure knowledge and
the related Ferdoran projects. Current recovery work also cross-checks the open
SilkroadDoc, RSBot and SilkroadLauncher sources listed above.
