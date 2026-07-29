# GO-SRO Agent server

The Agent server owns the currently playable vSRO 1.88 session after Gateway
admission. It combines Agent protocol handling with character, world, gameplay,
chat and social behavior that may later move into dedicated services.

## Startup dependencies

Agent fails startup when required dependencies are missing or inconsistent:

- MySQL pools selecting exactly `SRO_ACCOUNT` and `SRO_SHARD`;
- `Data/Data.pk2` (or `AGENT_DATA_PATH`) for world/navmesh data;
- `Media/server_dep/silkroad/textdata/leveldata.txt` (or
  `AGENT_LEVEL_DATA_FILE`) for progression rules;
- an Agent certificate/key and Gateway client CA for the private mTLS ticket
  listener;
- DB-backed reference, character, inventory, economy, mastery, quest, region,
  spawn and position repositories.

Environment variables override config-file values. Defaults and bindings live
in `config/agent.go`; shared DB settings live in `framework/config/config.go`.
The public SRO port defaults to `15882`; admission port `15883` is private.

## Implemented behavior

- Gateway ticket authentication and session cleanup.
- Character list/create/delete/rename/join and inventory serialization.
- DB-backed position checkpoints and authoritative movement.
- Data.pk2 navmesh loading, prepared-cache validation, terrain/object collision,
  region visibility, spawn/despawn, respawn and basic mob AI.
- Entity selection, deselection, NPC interaction/talk and teleport handshake.
- Basic attacks, damage, XP/level progression and mastery upgrades.
- Quest abandon persistence.
- Inventory move/equip, item use, repair, storage, guild storage, elixir
  alchemy, player exchange and stalls.
- Party create/invite/answer/leave/kick/disconnect cleanup and party matching.
- Local/global/party/stall/whisper chat, notices and GM commands.

Most new paths have unit or repository tests. This does not mean every response
is capture-verified or complete enough for production play.

## World and spawn invariants

- Internal navmesh coordinates are `X,Y,Z`, where `Y` is vertical. vSRO 1.88
  spawn packets write the coordinate components as `X,Z,Y`. Do not swap fields
  in the model to imitate the wire layout.
- Authoritative movement goes through `GameTimeManager` and
  `WorldService.UpdatePosition`; visibility updates are broadcast from server
  state, not trusted client coordinates.
- Blocked terrain/object edges must remain collision candidates. Filtering them
  out enables wall passing.
- Group spawn currently supports normal NPCs and mobs. Unsupported COS or
  specialized structures are logged and deferred to protect `0x3019` packet
  alignment.
- Neutral NPC talk flags are zero until a stock capture proves a specific mask.
- Random respawn positions are sampled around authored spawn centers. Candidate
  walkability and region availability still need a bounded validation pass.

## Persistence and schema requirements

Agent expects the shard migrations for quests, economy/storage, shop catalog,
teleports, masteries and guild storage in addition to the baseline identity and
inventory constraints. Normal server startup does not run migrations. See
`../db/migrations/README.md` and run the maintenance command explicitly.

## Resource profile

The current world loader eagerly retains the Data.pk2 navmesh. Compose documents
that it settles above 5 GiB before players join and reserves 7 GiB for Agent.
Prepared navdata reduces startup work, but does not yet materially reduce
retained runtime memory. Region-scoped or compact loading is a priority.

## Known gaps

- no sanitized stock-client capture corpus;
- COS/pet/structure/dropped-item group-spawn formats are incomplete;
- skill casting, buffs, full death/respawn and mob tactics are incomplete;
- quests only have the currently registered subset, including abandon;
- item magic/rent/bind/socket formats contain placeholders;
- guild and academy character fields are not implemented;
- dungeon and neighbour-plane loading are not implemented;
- stealth/invisibility visibility rules and several party matching fields need
  capture-backed behavior;
- NPC talk/action masks require live protocol evidence.

The root [progress and TODO](../README.md#prioritized-todo) list is the canonical
roadmap.

## Verification

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ..\scripts\verify.ps1
go test ./agent-server/...
```

For a live rebuild:

```powershell
docker compose up -d --build --force-recreate agent-server
docker compose logs --tail=200 agent-server
```

## References and license

Wire decisions follow the evidence order in the root README: pinned stock-client
captures, SilkroadDoc, independent readers such as RSBot, then legacy code and
explicit inference. The original project acknowledgement to DaxterSoul and the
repository [LICENSE](../LICENSE) are retained.
