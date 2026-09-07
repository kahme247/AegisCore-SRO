# AegisCore Agent server

Agent owns the post-Gateway vSRO 1.188 session: character lobby, world state,
movement, combat, economy, COS/pet and social handlers. The checked-in
`vsro188` profile is still `evidence-pending`; implemented behavior has
automated coverage but is not a claim of complete stock-client compatibility.

## Startup

Agent startup performs these operations in order:

1. Load Agent certificate, key and Gateway client CA.
2. Open and ping the account and shard MySQL pools, then load progression from
   `SRO_SHARD.REF_LEVEL_DATA`.
3. Build SQL-backed repositories and load reference, region and spawn catalogs.
4. Load a complete prepared navmesh cache or the raw PK2 archive.
5. Start game-time, spawn, respawn, mob-AI and gameplay-rate managers.
6. Bind public SRO port `15882`, then private mTLS admission port `15883`.
7. When enabled, bind the independent SMC management mTLS port `15884`.

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
- Progression data is loaded from `SRO_SHARD.REF_LEVEL_DATA`; the extracted
  `Media/Media` tree is not mounted or read during Agent startup. Keep it only
  as maintenance/reference input for `sro-data import-reference`.
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
| `AGENT_TRAFFIC_FILTER_PACKET_LIMIT` | `200` | Physical frames per window |
| `AGENT_TRAFFIC_FILTER_WINDOW_MS` | `1000` | Traffic-filter window |
| `AGENT_GIANT_MONSTER_SPAWN_RATIO` | `1000` | One-in-N giant generation ratio; `0` disables runtime giants |
| `AGENT_TIMED_BUFF_DURATION` | `0` | Optional duration override for development timed-buff tests |
| `AGENT_FEATURE_<NAME>_ENABLED` | `0` | Evidence-mapped optional legacy feature; unavailable features fail startup if enabled |
| `AGENT_DIAGNOSTICS_ENABLED` | `false` | Enable loopback-only pprof and runtime metrics |
| `AGENT_DIAGNOSTICS_PORT` | `6060` | Loopback diagnostics port |
| `AGENT_ADMISSION_HOST` | `127.0.0.1` | Private mTLS bind host |
| `AGENT_ADMISSION_PORT` | `15883` | Readiness/ticket API |
| `AGENT_ADMISSION_TLS_CERT_FILE` | none | Required Agent certificate |
| `AGENT_ADMISSION_TLS_KEY_FILE` | none | Required Agent key |
| `AGENT_ADMISSION_TLS_CLIENT_CA_FILE` | none | Required Gateway client CA |
| `AGENT_MANAGEMENT_ENABLED` | `false` | Enable the private SMC management API |
| `AGENT_MANAGEMENT_HOST` | `127.0.0.1` | Private management bind host |
| `AGENT_MANAGEMENT_PORT` | `15884` | Private management API port |
| `AGENT_MANAGEMENT_TLS_CERT_FILE` | none | Management server certificate |
| `AGENT_MANAGEMENT_TLS_KEY_FILE` | none | Management server private key |
| `AGENT_MANAGEMENT_TLS_CLIENT_CA_FILE` | none | CA that issued the SMC client certificate |
| `GAME_TIME_TICKS_PER_SECOND` | `20` | World clock frequency |
| `GAME_TIME_DAY_SPEED` | `1.0` | World clock multiplier |
| `AGENT_POSITION_CHECKPOINT_SECONDS` | `30` | Position checkpoint interval |

Shared `DB_ACCOUNT_*`, `DB_SHARD_*`, `DATABASE_*_DRIVER` and `LOG_LEVEL`
settings are defined in `framework/config`. `SRO_SESSION_TRACE=1` enables both
the legacy trace log and bounded JSONL session artifacts; destination, capacity
and optional non-sensitive prefixes use `SRO_SESSION_TRACE_DIR`,
`SRO_SESSION_TRACE_CAPACITY`, and `SRO_SESSION_TRACE_PREFIX_BYTES` respectively.
Completed sessions automatically produce `.analysis.json` and `.analysis.md`
reports beside their JSONL traces and log any ranked probable failure.
`SRO_MOVEMENT_TRACE` and `MOB_AI_TRACE` enable focused world diagnostics.

Exact feature names and their legacy directive/schedule evidence are defined in
`agent-server/feature/catalog.go`. Flags that are not yet `Available` remain
off and cannot be enabled; this prevents a configuration switch from exposing
an inferred or partial legacy implementation.

Diagnostics bind only to `127.0.0.1`. In Compose, collect a heap profile from
inside the Agent container, then copy it to the host for analysis:

```sh
docker compose exec agent-server wget -qO /tmp/agent-heap.pb.gz http://127.0.0.1:6060/debug/pprof/heap
docker compose cp agent-server:/tmp/agent-heap.pb.gz ./agent-heap.pb.gz
go tool pprof ./agent-heap.pb.gz
```

Scalar Go runtime metrics are available at `/debug/runtime/metrics`. Diagnostics
are disabled by default and are never attached to the public SRO or admission
listeners.

The optional management listener exposes `GET /v1/management/players`,
`GET /v1/management/health`, and
`POST /v1/management/player-disconnects` only to mutually authenticated SMC
clients. Snapshots include only sessions that completed character loading.
Disconnect commands require immutable character ID, the expected runtime UID,
and a reason. The listener has no plaintext, shared-secret, public-port, or
admission-port fallback.

## Implemented behavior

- One-time Gateway ticket redemption, source-IP binding and session cleanup.
- Character list, creation, name availability, seven-day delete/restore, rename,
  join, inventory/configuration serialization and position persistence.
- Bootstrap of quests, masteries, learned skills, quick slots, movement
  settings, inventory, vitals and return-city data. Quick-slot subtype `1`
  persists slots `0`-`50` with format/version byte `7`.
- Authoritative movement, region visibility, terrain/object collision,
  bounded collision-aware respawn and basic mob AI.
- Persistent STR/INT allocation, mastery upgrades and skill learning with
  prerequisite, mastery, stat and SP checks. Passive modifiers honor native
  equipped-item requirements and are recalculated after equipment changes.
- Basic and reference-driven physical/magical attacks, self buffs, cooldowns,
  timed effects, HP/MP use, death state, XP/skill-XP progression and DB-backed
  rates. Combat includes rarity-aware mobs, pursuit and bounded respawn paths.
  Attack/parry damage variance follows the pinned SR_GameServer arithmetic;
  see [`_docs/sr-gameserver-combat-variance.md`](../_docs/sr-gameserver-combat-variance.md)
  for binary identity, function addresses, formulas and remaining boundaries.
  Binary-confirmed threat tags and `2tnt` arithmetic are documented in
  [`_docs/sr-gameserver-aggro.md`](../_docs/sr-gameserver-aggro.md).
- Explicit and targeted skill resurrection with persisted vitals. Native
  `hste` fixed/percentage recovery and `resu` target-level caps are enforced;
  skill resurrection commits the target revival and caster MP debit in one
  transaction. Dead lobby joins recover at the configured return city;
  in-world explicit resurrection restores in place for both accepted modes.
- Terminal session drain captures vitals, authoritative position and summoned
  COS state under one two-second deadline and one idempotent character
  transaction. The session UUID is the replay key; queued older position
  checkpoints are retired before capture, and COS/world/admission teardown
  follows the save attempt.
- NPC interaction, family-specific talk actions, teleport, buy/sell, inventory,
  avatar inventory and Devil's Spirit expiry, storage, guild storage, repair,
  recovery-item use, elixir/stone/socket alchemy and transactional two-party
  exchange.
- Persistent Hwan state and speed, item equipment/appearance serialization,
  COS summon/unsummon, transport and pet state, follower movement, basic COS
  combat and GameReady rehydration.
- Party lifecycle and matching; persistent guild membership, permissions,
  notices, SP donation and invitations; academy membership and matching; job
  enrollment, aliases, ranking and rewards; five-second logout/restart
  countdown; local, global, party, guild, union, academy and whisper chat;
  character rename; stall acknowledgement and GM commands.
- In-memory stall host create/update/leave/destroy handling. Customer entry,
  buying, persistence and stall talk are not implemented.

Local chat is restricted to the sender's reciprocal known-player set. Chat
messages are filtered through the enabled rows in `CHAT_ABUSE_FILTER`; the
Agent loads the database snapshot at startup and refreshes it every minute.
Global chat does not consume its inventory item, and notice/NPC chat is not
implemented.

## World invariants

- The enabled `GAME_SERVER_REGION` catalog is validated as one complete
  partition before spawn or navmesh work begins. Every normal-world region has
  exactly one continent/runtime owner; duplicate assignments, empty groups and
  dungeon IDs fail startup until dungeon ownership is implemented. Navmesh
  terrain is staged for the whole partition and committed atomically, so a
  missing terrain cannot publish a partial world.
- Current position serialization writes model coordinates in `X,Y,Z` order.
  Alternative wire ordering requires capture evidence and deliberate serializer
  changes; do not swap model axes implicitly.
- Movement goes through game time and `WorldService.UpdatePosition`; server
  state, not untrusted client coordinates, drives visibility updates.
- Blocked terrain/object edges remain collision candidates.
- Group spawn admits players, ordinary NPCs and selected mob layouts.
  Unsupported COS, item and specialized structures are deferred to protect
  `0x3019` alignment.
- Normal service NPC spawns use talk flag `2`; selected-NPC responses provide
  family-specific actions that remain capture-provisional.
- Random respawn uses at most 32 resolved, same-region, direct-movement-safe
  candidates before falling back to the authored point.
- Ability-pet rental end times are enforced both when summoning and by game
  time while summoned. Expiry persists and removes the COS through the normal
  unsummon path before publishing its termination and item-state update.
- Players are staged during character bootstrap, become active only after
  `GameReady`, and are then delivered through reciprocal visibility queues.

## Item Mall purchase quantities

Item Mall purchases use the shared item-operation request/response opcodes
`0x7034`/`0xB034`. The modern mall operation is `0x18`. Its request quantity is
the number of packages being bought, not the number of individual items inside
each package. The success response must echo that package count. The delivered
item count comes from `ITEM_MALL_GOODS.QUANTITY` and the matching client Media
package definition.

For example, `PACKAGE_ITEM_MALL_REVERSE_RETURN_SCROLL` contains 11 scrolls.
Buying one package persists a stack count of 11 but returns package count `1`;
buying five persists 55 scrolls, split at the reference item's stack limit,
and returns package count `5`. Do not write the expanded counts `11` or `55`
into the response package-count field. The vSRO 1.188 client uses that field
while walking the destination list and can terminate with an access violation
when the expanded item count is supplied.

## Persistence and resources

Apply account migrations and shard migrations through `000053` before runtime.
Shard `000007` is intentionally unresolved. Current paths also depend on
movement settings (`000021`), rental metadata (`000022`), return city (`000023`),
progression and mob rates (`000024`-`000025`), quick slots and durable speed
effects (`000026`-`000027`), MySQL 8.4 compatibility (`000028`-`000029`), Item
Mall and timed services (`000030`-`000031`), creation policy and regions
(`000032`-`000036`), Premium activation and character buy-back (`000037`-`000038`),
experience services and active buffs (`000039`-`000041`), COS/pets (`000042`),
equipment and avatar systems (`000043`), alchemy references (`000044`), Hwan
state (`000045`), three-page inventory (`000046`), social systems (`000047`)
aggregate version/idempotency/outbox foundations (`000048`) and durable outbox
dispatch leases (`000049`), persistent schedule definitions and run history
(`000050`), forced guild-rename state (`000051`) and the permanent character
deletion lifecycle (`000052`) and recovered PK administration state (`000053`).

The world loader eagerly retains full navmesh maps. Compose documents more than
5 GiB after world load and applies an 8 GiB Agent memory limit. Prepared navdata
reduces parsing/startup work but is not region-scoped runtime storage.

## Known limitations

- No sanitized stock-client capture corpus is checked in.
- Dropped-item and specialized-structure spawn delivery is incomplete, and
  player/COS/pet spawn fields still need capture-backed validation.
- Skill learning and several attack/buff paths exist, but passive, healing,
  resurrection-skill and broader effect semantics remain incomplete.
- Quest bootstrap, repository operations and abandon exist; full
  accept/progress/complete/reward handlers do not.
- Magic parameters, some socket/bind/advanced-elixir fields and several
  appearance or container formats remain placeholders.
- Guild elections/wars, academy honor buffs, job safe-trade,
  stealth/invisibility, dungeons and neighbour planes are not implemented.
- GameReady restores social state, active item services and owned COS state;
  capture-unverified optional spawn variants remain gated.

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
