# SRO Nexus Agent server

Agent owns the post-Gateway vSRO 1.188 session: character lobby, world state,
movement, combat, economy and social handlers. The checked-in `vsro188` profile
is still `evidence-pending`; implemented behavior has automated coverage but is
not a claim of complete stock-client compatibility.

## Startup

Agent startup performs these operations in order:

1. Parse level data and load Agent certificate, key and Gateway client CA.
2. Open and ping the account and shard MySQL pools.
3. Build SQL-backed repositories and load reference, region and spawn catalogs.
4. Load a complete prepared navmesh cache or the raw PK2 archive.
5. Start game-time, spawn, respawn, mob-AI and gameplay-rate managers.
6. Bind public SRO port `15882`, then private mTLS admission port `15883`.

Startup never runs migrations or performs a complete schema preflight. Missing
tables used by initial reference/world loading are fatal; tables used only by a
later gameplay path can fail when that path is exercised. Initial gameplay-rate
load failure is nonfatal and falls back to 100% rates.

On shutdown, Agent stops public accepts and ticket installation, drains
sessions, flushes queued positions, stops managers and closes database pools.

## Data and cache inputs

- `AGENT_DATA_PATH` must name the PK2 file itself in raw mode, normally
  `Data/Data.pk2`. The native default `./Data` is a directory and must be
  overridden.
- `AGENT_LEVEL_DATA_FILE` should be
  `Media/Media/server_dep/silkroad/textdata/leveldata.txt` when running from the
  repository root. Compose maps `Media/Media` to `/etc/go-sro/Media`.
- `AGENT_PRELINKED_NAVDATA_FILE` is a prefix. For `prelinked_navdata.gob`, Agent
  expects `_regions`, `_objects`, and `_manifest.json` suffixes.
- If any prepared-cache artifact exists, the complete schema-3 cache must match
  profile `vsro188`, archive identity, artifact sizes and checksums. A stale or
  partial cache fails closed instead of silently falling back to raw data.

The current profile pins a `2,136,580,096` byte encrypted Data.pk2 with SHA-256
`0F6191BA393DF32B2258DEE38CC5AB100DC91210D1FA121C1D3845FDC74D8C3B` and
format `0x01000002`. Raw runtime loading does not currently re-hash the archive;
prepared-cache validation does.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `AGENT_HOST` | `127.0.0.1` | Public listener bind host |
| `AGENT_PORT` | `15882` | Public SRO listener |
| `AGENT_MODULE_ID` | `AgentServer` | Security module ID |
| `AGENT_DATA_PATH` | `./Data` | Raw PK2 path; override to a file |
| `AGENT_PRELINKED_NAVDATA_FILE` | `./prelinked_navdata.gob` | Prepared-cache prefix |
| `AGENT_LEVEL_DATA_FILE` | `./Media/server_dep/silkroad/textdata/leveldata.txt` | Level-data path |
| `AGENT_TRAFFIC_FILTER_PACKET_LIMIT` | `200` | Physical frames per window |
| `AGENT_TRAFFIC_FILTER_WINDOW_MS` | `1000` | Traffic-filter window |
| `AGENT_ADMISSION_HOST` | `127.0.0.1` | Private mTLS bind host |
| `AGENT_ADMISSION_PORT` | `15883` | Readiness/ticket API |
| `AGENT_ADMISSION_TLS_CERT_FILE` | none | Required Agent certificate |
| `AGENT_ADMISSION_TLS_KEY_FILE` | none | Required Agent key |
| `AGENT_ADMISSION_TLS_CLIENT_CA_FILE` | none | Required Gateway client CA |
| `GAME_TIME_TICKS_PER_SECOND` | `10` | World clock frequency |
| `GAME_TIME_DAY_SPEED` | `1.0` | World clock multiplier |
| `AGENT_POSITION_CHECKPOINT_SECONDS` | `30` | Position checkpoint interval |

Shared `DB_ACCOUNT_*`, `DB_SHARD_*`, `DATABASE_*_DRIVER` and `LOG_LEVEL`
settings are defined in `framework/config`. `SRO_SESSION_TRACE=1`,
`SRO_MOVEMENT_TRACE`, and `MOB_AI_TRACE` enable focused diagnostics.

## Implemented behavior

- One-time Gateway ticket redemption, source-IP binding and session cleanup.
- Character list, creation, name availability, seven-day delete/restore, join,
  inventory/configuration serialization and position persistence. Rename
  requests are parsed but rejected because rename persistence is absent.
- Bootstrap of quests, masteries, learned skills, quick slots, movement
  settings, inventory, vitals and return-city data. Quick-slot subtype `1`
  persists slots `0`-`50` with format/version byte `7`.
- Authoritative movement, region visibility, terrain/object collision,
  bounded collision-aware respawn and basic mob AI.
- Persistent STR/INT allocation, mastery upgrades and skill learning with
  prerequisite, mastery, stat and SP checks.
- Basic and reference-driven physical/magical attacks, self buffs, cooldowns,
  HP/MP use, death state, XP/skill-XP progression and DB-backed rates.
- Explicit resurrection and persisted vitals. Dead lobby joins recover at the
  configured return city; in-world resurrection currently restores in place for
  both accepted modes.
- NPC interaction, family-specific talk actions, teleport, buy/sell, inventory,
  storage, guild storage, repair, recovery-item use, elixir alchemy and
  transactional two-party exchange.
- Party lifecycle and matching, five-second logout/restart countdown, local,
  global, party and whisper chat, stall acknowledgement and GM commands.
- In-memory stall host create/update/leave/destroy handling. Customer entry,
  buying, persistence and stall talk are not implemented.

Local chat currently broadcasts world-wide, global chat does not consume its
inventory item, and notice/guild/union/NPC/academy chat is not implemented.

## World invariants

- Current position serialization writes model coordinates in `X,Y,Z` order.
  Alternative wire ordering requires capture evidence and deliberate serializer
  changes; do not swap model axes implicitly.
- Movement goes through game time and `WorldService.UpdatePosition`; server
  state, not untrusted client coordinates, drives visibility updates.
- Blocked terrain/object edges remain collision candidates.
- Group spawn admits ordinary NPCs and selected mob layouts. Unsupported COS,
  item and specialized structures are deferred to protect `0x3019` alignment.
- Normal service NPC spawns use talk flag `2`; selected-NPC responses provide
  family-specific actions that remain capture-provisional.
- Random respawn uses at most 32 resolved, same-region, direct-movement-safe
  candidates before falling back to the authored point.
- Players are staged during character bootstrap and become active only after
  `GameReady`; capture-backed player spawn delivery remains incomplete.

## Persistence and resources

Apply account `000001` and shard migrations through `000026` before runtime.
Shard `000007` is intentionally unresolved. Current paths depend on movement
settings (`000021`), rental metadata (`000022`), return city (`000023`),
progression rates (`000024`), mob rates (`000025`) and quick slots (`000026`) in
addition to earlier identity, economy, mastery, skill and reference migrations.

The world loader eagerly retains full navmesh maps. Compose documents more than
5 GiB after world load and applies an 8 GiB Agent memory limit. Prepared navdata
reduces parsing/startup work but is not region-scoped runtime storage.

## Known limitations

- No sanitized stock-client capture corpus is checked in.
- Player, COS/pet, dropped-item and specialized structure spawn delivery is
  incomplete.
- Skill learning and several attack/buff paths exist, but passive, healing,
  resurrection-skill and broader effect semantics remain incomplete.
- Quest bootstrap, repository operations and abandon exist; full
  accept/progress/complete/reward handlers do not.
- Magic parameters, socket/bind/advanced-elixir fields and several appearance
  or container formats remain placeholders.
- Guild/academy fields, stealth/invisibility, dungeons and neighbour planes are
  not implemented.
- GameReady deliberately emits no capture-unverified bootstrap spawn sequence.

## Verification

Run from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
go test ./agent-server/...
```

For the optional selected-package Linux race gate:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1 -Race
```

For a live rebuild, populate every required Compose variable and TLS file first:

```powershell
docker compose up -d --build --force-recreate agent-server
docker compose logs --tail=200 agent-server
```

See [migration safety](../db/migrations/README.md), the
[capture policy](../protocol/captures/README.md), and [LICENSE](../LICENSE).
