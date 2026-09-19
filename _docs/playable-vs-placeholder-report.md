# Playable vs Placeholder Report

**Snapshot:** 2026-07-25  
**Scope:** Maintained AegisCore stack in `go-sro/`, targeting the original vSRO 1.188 client.  
**Verdict:** AegisCore has a playable core loop, but it is not yet a complete or production-proven Silkroad server. Login through ordinary world play is represented by integrated runtime paths; several advanced systems are partial, capture-provisional, or intentionally unavailable.

## How to read this report

| Label | Meaning |
| --- | --- |
| **Playable** | An integrated server path exists for the normal client flow, including runtime/domain behavior and persistence where the feature requires it. |
| **Partial** | Useful behavior exists, but an important branch, wire format, persistence rule, or stock-client validation is incomplete. |
| **Placeholder / absent** | The runtime rejects, gates, acknowledges without complete gameplay, or does not implement the feature. |
| **Unverified** | Code or tests exist, but this checkout does not contain enough end-to-end stock-client evidence to call it playable. |

"Playable" here is a code-readiness classification, not proof from a live client session. No sanitized stock-client capture corpus is checked in, and this report did not start the private-asset Compose stack.

## Executive matrix

| Player journey / system | Classification | What a player can reasonably expect | Boundary or missing proof |
| --- | --- | --- | --- |
| Gateway login and Agent handoff | **Playable** | Version/shard/login flow and one-time, IP-bound Agent admission tickets | Requires correct private database, TLS, protocol tuple, and reachable Agent IP |
| Character lobby | **Playable** | List, create, name check, delete/restore, rename, select, and join | Appearance/container variants still need capture validation |
| Enter world and reconnect | **Playable** | Character bootstrap, GameReady activation, position/vitals/COS restoration, terminal save | Optional spawn variants remain gated; reconnect is not claimed production-proven |
| Movement and world visibility | **Playable** | Authoritative movement, reciprocal visibility, terrain/object collision, safe respawn, and automated two-client spawn/despawn coverage | Unimplemented specialized variants, neighbour planes, dungeon ownership, and live stock-client proof remain incomplete |
| Basic combat and progression | **Playable** | Physical/magical attacks, mob pursuit, damage, HP/MP, death, XP/SP, skills, masteries, stats, cooldowns, and self buffs | Broader effects, healing/passive semantics, invisibility, and some attack/buff branches remain incomplete |
| Resurrection | **Playable** | Explicit/targeted skill resurrection and return-city recovery with persisted vitals | Broader resurrection-skill semantics remain a documented boundary |
| NPC shops and inventory | **Playable** | NPC buy/sell/buyback, movement/equipment, storage, guild storage, repair, item use, mall packages, and two-party exchange | NPC economy has an overloaded partial operation; some item-use callbacks fail closed |
| Alchemy | **Partial** | Elixir, stone, socket, material, dismantle, avatar paths, complete equipment target bodies (rent/reference/magic/socket/advanced-elixir), and authenticated atomic magic-cube `0x27` push/`0x28` pop exist; `0x29` is correctly response-only | Non-equipment class scenarios and live stock-client proof remain incomplete |
| Ground items and drops | **Playable (basic)** | Inventory-backed drop/pickup plus nearby observer spawn/despawn delivery, including two independent automated client sessions | Specialized item variants and a two-client stock-client capture still need validation |
| Quests | **Partial** | Bootstrap, persistence, abandon, and substantial accept/progress/complete/reward code paths exist in the current tree | Maintainer documentation still says the full flow is incomplete; needs an end-to-end stock-client scenario before promotion |
| Parties | **Playable (basic)** | Party lifecycle and matching | Advanced event-party flows need separate validation |
| Guilds | **Partial** | Membership, invitations, permissions, notices, SP donation, and guild storage | Elections, wars, siege/fortress behavior, and some extensions are not established as playable |
| Academy | **Partial** | Membership and matching | Honor buffs and newer academy extensions are incomplete/unverified |
| Tri-job system | **Playable core** | Enrollment, withdrawal, alias, ranking, progression, rewards, and spawn/select state have implemented compatibility-ledger entries | Trade/event integration and full live-client coverage still need scenario proof |
| Pets / COS / transports | **Partial** | Summon, unsummon, naming/settings, persistence, follower movement, rental expiry, basic combat, and automated pet/transport visibility lifecycles | Spawn fields and specialized COS variants need stock-client capture-backed validation |
| Chat | **Partial** | Local, global, party, guild, union, academy, whisper, filtering, and GM spawn command | Global chat does not consume its item; notice and NPC chat are absent |
| Player stalls | **Placeholder / partial shell** | Host can create, update, leave, and destroy an in-memory stall | Customer entry, buying, stall talk, and persistence are not implemented |
| Open market | **Unverified** | Handlers/repositories exist for listing, search, purchase, cancel, settlement, and expiry in the current working tree | Changes are uncommitted and need migrations plus integration/client verification |
| Dungeons / Forgotten World | **Placeholder / unverified** | Some new handlers and dungeon code exist in the working tree | Maintainer docs still say dungeon ownership is not implemented; do not call this playable |
| Stealth / invisibility | **Placeholder / absent** | No complete gameplay contract | Explicitly documented as not implemented |
| Neighbour planes | **Placeholder / absent** | No runtime traversal support | Service returns a not-implemented error |
| Management and patching | **Optional, not gameplay** | SMC management and launcher patching can be enabled separately | Not required for the playable core and not evidence of gameplay completeness |

## Minimum defensible playable slice

A conservative playable demonstration is:

1. Start MySQL, Gateway, and Agent with the required private assets and migrations.
2. Log in through Gateway, redeem the one-time Agent ticket, and create/select a character.
3. Enter the normal world, move with collision and visibility, fight ordinary mobs, gain progression, and persist position/vitals.
4. Use ordinary NPC shop, inventory, storage, equipment, party, chat, and basic social flows.
5. Log out and rejoin to confirm the saved character state.

This is the correct "playable core" claim. It does **not** imply complete quests, stalls, dungeons, fortress/siege, every skill effect, every pet form, or complete packet parity.

## Highest-impact placeholder gaps

| Priority | Gap | Why it blocks a stronger claim | Promotion evidence |
| ---: | --- | --- | --- |
| 1 | Stock-client end-to-end corpus | Unit/integration tests cannot prove the client accepts every packet sequence | Sanitized login-to-logout captures and repeatable assertions for the core journey |
| 2 | Spawn coverage | **Automated matrix complete:** two independent sessions now verify exact spawn bytes, opcodes, despawn UIDs, and known-object state across players, mobs, drops, pets, transports, NPCs, and teleport structures | Run the same matrix against two stock vSRO 1.188 clients and retain sanitized captures; add serializers and cases for remaining gated variants |
| 3 | Quest journey | Repository or handler presence alone is not a playable quest | Accept, progress, death/reconnect, complete, reward-choice, abandon, and inventory-full cases on stock client |
| 4 | Advanced social/economy | Stall customer flow, guild war/election, academy buffs, and some market work remain incomplete or unverified | Transactional multi-client scenarios with restart/recovery checks |
| 5 | World breadth | Dungeons, neighbour planes, stealth, and specialized regions remain gated or absent | Authored-region ownership, teleport/instance lifecycle, visibility, persistence, and failure recovery tests |

## Evidence and caveats

- `README.md` describes the project as "core gameplay works" while explicitly saying stock-client compatibility is incomplete and not production-proven.
- `agent-server/README.md` lines 166-217 enumerate integrated behavior and explicit stall/chat boundaries; lines 228-253 define world/spawn invariants; lines 283-297 list known limitations.
- `protocol/vsro188/compatibility_ledger.go` distinguishes implemented, partial, framing-only, and fail-closed protocol surfaces. A correct codec is not automatically a complete gameplay transaction.
- `agent-server/service/world_visibility_test.go` verifies world admission/removal queues, while `agent-server/manager/spawn_manager_test.go` runs the two-client delivery matrix and checks exact spawn payloads, opcode envelopes, despawn UIDs, and per-client known-object state. Focused model/inventory tests cover the surrounding item and COS serialization boundaries.
- `agent-server/server.go` composes lobby, movement, combat, quest, social, stall, exchange, inventory, alchemy, party, COS, and optional market handlers into the Agent runtime.
- The working tree is heavily modified and contains untracked gameplay work. This report classifies those additions as **unverified** unless the maintained documentation and integrated evidence already support a stronger label.
- The repository guide references a sibling `SROCLIENTPROJECT/`, but that directory is absent from this workspace snapshot. No Godot-client playability claim is made.

## Recommended release wording

> **Playable development build:** AegisCore supports the vSRO 1.188 login, character, normal-world movement, basic combat/progression, inventory/NPC economy, party, chat, and persistence loop. Advanced quests, spawn variants, stalls, guild/academy extensions, dungeons, stealth, and some item/skill semantics remain partial or unavailable. Stock-client compatibility is not yet production-proven.
