# SR_GameServer event systems

This is a binary-first recovery of the pinned TestIn `SR_GameServer.exe` event
subsystems. It covers Fortress War, Battle Arena, Capture the Flag (the binary
name is `FlagWorld`), generic server/GM events, and the evidence boundary for
Survival Arena, Magic Pop, and Lottery.

## Evidence boundary

The target is the 32-bit executable
`VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe` with SHA-256
`0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`.
Addresses and offsets below are valid only for that build.

“Confirmed” means that the value, memory access, branch, call, serializer
operation, or SQL literal is present in this executable. A conventional event
name from the client, eSRO source, or another server is not sufficient to make
the GameServer claim.

| Requested system | Status in this binary | Binary conclusion |
| --- | --- | --- |
| Fortress War | Confirmed | Dedicated Siege Fortress manager, scheduled request/war/tax jobs, fortress object state, and direct SQL/procedure persistence. |
| Battle Arena | Confirmed | Four participation/match schedule pairs, a Battle Arena world, team/occupy/point/flag state, and outbound `0x34D2` updates. |
| Capture the Flag | Confirmed | Implemented as `FlagWorld`; participation and world schedule jobs, team state, buffs, participant persistence, and outbound `0x34B1` updates. |
| Survival Arena | Not confirmed | No `Survival`/survival-arena class, string, schedule, SQL literal, or dedicated packet family was found in the target. |
| GM Events | Partially confirmed | The generic `ServerEventMiddleman` and reward lifecycle are present. A GameServer-side GM command handler is not established by this recovery. |
| Magic Pop | Not confirmed | Magic-option/alchemy code is present, but no `MagicPop` event manager, schedule, or event SQL contract is present. |
| Lottery | Quest content only | Lottery NPC/quest scripts and item/skill names are present; no standalone Lottery event manager or event packet/SQL lifecycle was found. |

The absence statements are deliberately scoped to the pinned GameServer. They
do not say that another service or a later client build lacks those features.

## Process lifecycle

The recovered initialization routine is `0x00412C20`.

1. It validates locale, resolves the current directory, loads
   `AbuseFilter.txt`, creates the object factory, and loads GameServer
   configuration through `0x00427050`.
2. It allocates the large work buffers, initializes the Siege Fortress manager,
   creates the game-world instance manager, and requires at least one loaded
   region.
3. It registers the general scheduled jobs with visible periods of `1000`,
   `300`, `600000`, `300`, and `10000` milliseconds.
4. It initializes skills, security, party, flea-market, guild, training-camp,
   Silk Mall, and server monitors, then projects reference quests.
5. It performs post-world Siege Fortress initialization and registers the
   event schedule catalog through `0x00676490`.
6. If the configuration flag at `DAT_00D6A8DC + 0x42498` is enabled, it calls
   `0x0041D090` and `0x00644CB0` to initialize FlagWorld. Failure logs
   `Check!! Failed to initialize flag world manager`.
7. It initializes the Battle Arena manager through `0x005ECFD0`. Failure logs
   `Check!! Failed to initialize Battle Arena manager`.
8. After `InitializeGame() Succeeded!`, an additional configuration byte at
   `DAT_00D6A8DC + 0x424A6` gates `0x0066F200`, the generic
   `ServerEventMiddleman` initializer. It loads sync data and instance reward
   data; failure logs `Failed to Initialize ServerEventMiddleman`.

The event jobs are registered before FlagWorld, Battle Arena, and the generic
server-event middleman are enabled. That ordering is a lifecycle contract, not
just a list of class names.

### Event schedule catalog

`0x00676490` allocates and registers the following job objects. The allocation
size is also observed in the call immediately before each constructor.

| Constructor | Allocation | Binary schedule definition |
| --- | ---: | --- |
| `0x00674DD0` | `0x9C` | `SCHEDULE_DEF_SPECIAL_GOODS_SELL` |
| `0x00675000` | `0x90` | `SCHEDULE_DEF_SIEGE_REQUEST_PERIOD` |
| `0x00675100` | `0x90` | `SCHEDULE_DEF_SIEGE_PERIOD` |
| `0x00675200` | `0x70` | `SCHEDULE_DEF_SIEGE_TAX_PERIOD` |
| `0x00675300` | `0x74` | `SCHEDULE_DEF_MEET_ROC` |
| `0x00675430` | `0x74` | `SCHEDULE_DEF_CHINS_TOMB` |
| `0x00675530` | `0x88` | `SCHEDULE_DEF_EGYPT_TEMPLE_OF_SELKIS_AND_NEITH` |
| `0x00675630` | `0x90` | `SCHEDULE_DEF_EGYPT_TEMPLE_OF_ANUBIS_AND_ISIS` |
| `0x00675730` | `0x88` | `SCHEDULE_DEF_EGYPT_TEMPLE_HAROERIS_AND_SETH` |
| `0x00675830` | `0x78` | `SCHEDULE_DEF_FLAG_WORLD_PARTICIPATION` |
| `0x00675930` | `0x74` | `SCHEDULE_DEF_FLAG_WORLD` |
| `0x00675A30` | `0x7C` | `SCHEDULE_DEF_BATTLE_ARENA_RANDOM_PARTICIPATION` |
| `0x00675BE0` | `0x74` | `SCHEDULE_DEF_BATTLE_ARENA_RANDOM` |
| `0x00675D90` | `0x7C` | `SCHEDULE_DEF_BATTLE_ARENA_PARTY_PARTICIPATION` |
| `0x00675E90` | `0x74` | `SCHEDULE_DEF_BATTLE_ARENA_PARTY` |
| `0x00675F90` | `0x7C` | `SCHEDULE_DEF_BATTLE_ARENA_GUILD_PARTICIPATION` |
| `0x00676090` | `0x74` | `SCHEDULE_DEF_BATTLE_ARENA_GUILD` |
| `0x00676190` | `0x7C` | `SCHEDULE_DEF_BATTLE_ARENA_JOB_PARTICIPATION` |
| `0x00676290` | `0x74` | `SCHEDULE_DEF_BATTLE_ARENA_JOB` |
| `0x00676390` | `0x70` | `SCHEDULE_DEF_EVENT_EUBUSINESS` |

The repeated participation/match pairs are the strongest binary evidence that
the four Battle Arena modes are separate scheduled flows rather than one mode
with only a client-side label.

## Inter-server opcode families

These are GameServer-emitted Agent/inter-server messages. They are not the
client request dispatcher entries in `sr-gameserver-testin-opcodes.csv`.

### `0x34B1`: FlagWorld updates

The opcode is loaded into the Agent message builder at many FlagWorld call
sites in `0x00644xxx`–`0x00647xxx`. The common control prefix is a sentinel byte
`0xFF` followed by a one-byte sub-operation; payload fields are serialized
after it. The following sub-operations are immediate-value confirmed:

| Sub-operation | Representative site | Confirmed payload observation |
| ---: | --- | --- |
| `0x01` | `0x00645194` | control update; prefix and sub-operation only at this site |
| `0x05` | `0x00645CD7` | control update; one additional byte is serialized |
| `0x0B` | `0x00646FF4` | one-byte team/event value plus a `u32` value |
| `0x0C` | `0x006480E0` | participant update after `_AddFlagEventParticipant` |
| `0x0D` | `0x00644A27` | control update; one additional byte |
| `0x0E` | `0x00645099` | control update; one additional byte |
| `0x0F` | `0x00647342` | control update; one additional byte |
| `0x11` | `0x00646D94` | control update; one additional byte; a later `0x16`/`0x17` branch is emitted for the same flow |
| `0x12` | `0x0064756E` | control update; one additional byte |
| `0x13` | `0x006479A0` | control update; one additional byte |
| `0x15` | `0x00645D35` | control update; one additional byte |
| `0x16`, `0x17` | `0x00646DE2` | branch-selected control update |
| `0x18` | `0x0064710F` | control update; one additional byte |
| `0xFF` | `0x006452C0`, `0x00645910` | terminal/reset-style update; exact semantic name unresolved |

`0x00647D40` applies the team transition. It validates team values `0` and
`1`, clears or engages the team skill effect, records a tick count, and emits
`0x34B1` updates to affected players. `0x006480E0` is the participant boundary:
it validates the player, executes `exec _AddFlagEventParticipant %d`, and then
sends sub-operation `0x0C` with a `0xFF` byte.

### `0x34D2`: Battle Arena updates

The Battle Arena manager emits `0x34D2` from the `0x00662890`–`0x006692DF`
region. The common control prefix is again `0xFF` followed by a one-byte
sub-operation in the short forms. Confirmed sub-operations are:

| Sub-operation | Representative site | Confirmed behavior |
| ---: | --- | --- |
| `0x09` | `0x00668E20` | sends a team-index byte and triggers notification `0xEF` with type `4` |
| `0x20` | `0x00663180` | serializes two computed bytes and a `u32` state field |
| `0x40` | `0x00663E00` | control/state update |
| `0x41` | `0x00663910` | sends arena info: `+0x60`, `+0x64`, player/team count, then per-team data |
| `0x80`, `0x81` | `0x006647E0` | result branch for the local team; `0x80` and `0x81` select different outcomes |
| `0x82` | `0x00664F10` | emits a failure/control update, stores a five-second retry tick, and enables a pending state |
| `0x83` | `0x006656A0` | arena state reset/update |
| `0x84` | `0x006652A0` | serializes team and arena state, then broadcasts to tracked players |
| `0x85` | `0x00664B90` | removes/invalidates a player whose team differs |
| `0x86` | `0x00664C70` | validates the opposite team and emits the same invalidation form |
| `0x87` | `0x00664600` | arena transition/control update |
| `0x88` | `0x00665990` | sends two time values derived from arena offsets `+0x8E` and `+0x90` |
| `0xF0` | `0x00662890` | periodic arena scoreboard/state broadcast, rate-limited to roughly ten one-second ticks |

The binary also emits `0x34D2` with short values `0x00` and `0x04` from
`0x006692D0` and `0x00669270`. Their surrounding callers establish control
notifications, but not stable public names for those two values.

### `0x34F2`: generic ServerEvent data and rewards

The generic server-event middleman uses `0x34F2` for GameServer→Agent event
state. Two entry-game senders are binary-confirmed:

- `0x0066F4B0` sends ServerEventData when a player enters. It writes the
  literal two-byte header values `0x02` and `0x03`, a `u32` collection count,
  and for each data record serializes the three `u32` values at record offsets
  `+0x08`, `+0x0C`, and `+0x10`.
- `0x0066F640` sends remaining reward time. It writes literal control byte
  `0x07`, a `u32` count, and for each supported class-`1` reference reward
  serializes reward ID, event ID, class, three reference values at `+0x18`,
  `+0x1C`, `+0x20`, and a remaining-time value at `+0x10`.

Inbound/continuation handling is also visible in the same family:

- `0x00670B60` copies the current message opcode into the response, reads a
  byte, a `u32`, and a byte, conditionally reads another `u32` for message
  type `2`, calls `0x00671400` to update ServerEventData, and emits a normal
  network notification.
- `0x00670CC0` resolves a ServerEvent ID and reward ID, deletes the remaining
  reward time for supported reward class `1`, and emits `0x34F2` sub-operation
  `0x08` with the two IDs.
- `0x00671230` reads a count followed by three `u32` values per entry and
  updates the remaining-reward-time records through `0x00671560`.

The reference/reward class checks, delay jobs, completion, rollback, and
reward application are all inside the GameServer. The executable does not
prove the stored-procedure transaction semantics behind those operations.

## Recovered event structures

These are field observations, not portable C++ declarations.

### Schedule objects

Every scheduled object has a base `IGame_ScheduleJob` vtable and the sizes in
the schedule table above. The Siege request end handler at `0x006794C0`
copies eight schedule values from `+0x70` through `+0x8C` into the GameServer
state at `DAT_00D6A8DC + 0x42458` through `+0x42474`. Its paired start handler
at `0x00679320` records the schedule event and announces `Start`/`End` through
the schedule logger.

### FlagWorld state

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x20` | pointer/list | tracked FlagWorld players/objects walked during broadcasts |
| `+0x34` | `u32` | event/context value used to match player context |
| `+0x58 + team*4` | pointer | per-team skill-effect object |
| `+0x8C` | pointer/entry base | team/object lookup input used by `0x00647D40` |
| `+0x90` | pointer/ID | current owner/team context; used to validate transitions |
| `+0xC4` | `u32` | last team-buff tick from `GetTickCount()` |

The player-side virtual calls used by these routines include identity/context
reads at vtable slots `+0x08`, `+0x0C`, and `+0x14`, packet allocation at
`+0x278`, packet send at `+0x27C`, and an effect operation at `+0xEC`.

### Battle Arena state

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x14` / `+0x10` | list and sentinel | tracked arena participants/objects |
| `+0x28` | byte | periodic broadcast counter; reset after ten one-second steps |
| `+0x31` | byte | arena mode/context input to `0x00669940` |
| `+0x32` | byte | adjacent arena state byte |
| `+0x54` | `u32` | last periodic update tick |
| `+0x58` | `u32` | periodic arena value serialized in `0xF0` |
| `+0x60`, `+0x64` | `u32` | arena info values sent by sub-operation `0x41` |
| `+0x8C + index*8` | entries | team/player-related references walked by the scoreboard sender |
| `+0xB4` | byte | number of team/info entries |
| `+0xD0 + team*4` | `u32` | per-team enabled/state flags |
| `+0x10C` | two `u16` values | arena level/time-like state used by participation notices |

`0x00663040` and `0x006630F0` explicitly validate team indexes for
`AddOccupy()` and `RemoveOccupy()`. RTTI strings identify
`CBattleArenaInfoFlag`, `CBattleArenaInfoPoint`, and
`CBattleArenaInfoOccupy`; the binary confirms the component types but not a
complete stable field declaration for them.

### ServerEvent data and reward records

| Record | Offset | Width | Confirmed use |
| --- | ---: | ---: | --- |
| ServerEventData record | `+0x08` | `u32` | serialized event-data value |
| ServerEventData record | `+0x0C` | `u32` | serialized event-data value |
| ServerEventData record | `+0x10` | `u32`/pointer-bearing field | serialized event-data value or linked instance, depending on path |
| RemainReward record | `+0x08` | `u32` | ServerEvent ID |
| RemainReward record | `+0x0C` | `u32` | reference reward ID |
| RefServerEventReward | `+0x15` | byte | reward class; class `1` is the supported timed reward path |
| RefServerEventReward | `+0x18`, `+0x1C`, `+0x20` | `u32` | three reward/reference values sent on entry |
| RemainReward record | `+0x10` | `u32` | remaining-time value |

The odd `+0x15` offset is intentional evidence of a packed/interior reference
record. Do not align it to `+0x14` or `+0x18` in a reimplementation.

## SQL contracts

### Fortress War

The following SQL literals are present in `SR_GameServer.exe`. The listed
addresses are the recovered builder functions.

| Builder | SQL contract |
| --- | --- |
| `0x00627CD0` | `{?=CALL _SiegeFortressRequestSiege(%d,%d,%d,%d)}` |
| `0x00627DE0` | `DELETE _SiegeFortressRequest WHERE FortressID = %d AND GuildID = %d` |
| `0x00456EC0` | `{?=CALL _AddSiegeFortressObject(%d,%d,%d,%d,%d,%f,%f,%f,%f,%d)}` |
| `0x00456EC0` | `UPDATE _SiegeFortressStruct SET OwnerGuildID=%d,RefObjID=%d,HP=%d,State=%d,MakeDate='%s' WHERE RefEventStructID=%d` |
| `0x004CD3F0` | `DELETE FROM _SiegeFortressObject WHERE [ID] = %d` |
| `0x004CFC40` | `DELETE FROM _SiegeFortressStruct WHERE RefEventStructID=%d AND RefObjID=%d` |
| `0x0061E1D0` | `{?=CALL _SiegeFortressFinished(%d)}` |
| `0x0061E280` | `{?=CALL _SiegeFortressTempFinished(%d)}` |
| `0x0061E330` | `_UpdateSiegeFortressTaxRatio`, `_SiegeFortressTaxCollect`, `_SiegeFortressHireNPC`, and `_SiegeFortressEngageBlessBuff` calls |
| `0x0061E330` | direct updates of `_SiegeFortress.Tax`, `TempGuildID`, `Introduction`, and dungeon count/time |
| `0x00628040` | `INSERT INTO _SiegeFortressItemForge VALUES (%d,%d,%d,%d,'%s','%s')` |
| `0x00628230` | `DELETE _SiegeFortressItemForge WHERE FortressID = %d AND ItemRefID = %d` |
| `0x00628470` | `INSERT INTO _SiegeFortressBattleRecord VALUES (%d,%d,%d,%d,'%s',%d)` |
| `0x00628640` | `DELETE _SiegeFortressBattleRecord WHERE FortressID = %d AND CharID = %d` |
| `0x006287D0` | `INSERT INTO _SiegeFortressStoneState VALUES (%d,%d,%d)` |
| `0x00628890` | `DELETE _SiegeFortressStoneState WHERE FortressID = %d AND GuildID = %d` |

The corresponding embedded object/reference names include `_SiegeFortress`,
`_SiegeFortressObject`, `_SiegeFortressStruct`, `_SiegeFortressRequest`,
`_SiegeFortressItemForge`, `_SiegeFortressBattleRecord`, and
`_SiegeFortressStoneState`. The binary also contains the asynchronous siege
error path `CSiegeFortress succeeded but AsyncQueryCompleted() error occurred
[AQ_Siege_Type: %d]`.

### Capture the Flag

`0x006480E0` is the only dedicated FlagWorld SQL literal recovered:

```sql
exec _AddFlagEventParticipant %d
```

It executes after the participant validity check and before the `0x34B1`
participant update. No FlagWorld table literal was found in the target.

### Battle Arena and generic events

No Battle Arena-specific SQL literal was found in the GameServer SQL inventory.
The Battle Arena lifecycle is therefore documented as stateful and
message-driven for this build, not as a proven database transaction.

The generic schedule/server-event log writers are shared infrastructure:

```sql
{CALL _AddLogServerEvent (%d,%d,'%s')}
{CALL _AddLogSchedule ('%s', %d, '%s', %d, '%s', '%02d-%02d-%02d %02d:%02d:%02d')}
```

They are built at `0x0043A6D0` and `0x0043A7F0`. The event middleman itself
loads and mutates ServerEvent data through asynchronous object paths; this
executable does not contain the stored-procedure bodies or enough evidence to
claim their transaction/isolation behavior.

## Lottery, Magic Pop, Survival Arena, and GM scope

- Lottery is represented by quest functions around `0x008926E0`–`0x00893C20`
  and `0x00892EB0`, with strings such as
  `SN_TALK_QNO_CH_LOTTERY_*`, `SN_CON_QNO_CH_LOTTERY_2`, and
  `ITEM_QNO_CH_BIGMAN_1_01`. These are quest/NPC dialogue and item paths. They
  do not establish a standalone Lottery event subsystem.
- Magic-option/alchemy code contains `MagicOptValue` diagnostics and the
  normal alchemy lifecycle. No `MagicPop` symbol, schedule definition, packet
  family, or SQL literal was found in the GameServer binary.
- No survival-arena class, RTTI string, schedule definition, event SQL, or
  dedicated message family was found. Do not map Battle Arena classes to
  Survival Arena without a different binary or capture.
- The generic ServerEvent system is suitable evidence for server/GM-authored
  events: reference event data, timed rewards, completion/rollback messages,
  skill/buff/monster reward classes, and server-event logs are all present. A
  direct `GM_COMMAND` client handler is not registered in the recovered
  GameServer client table, so GM scheduling/authorization ownership remains
  outside this binary trace.

## Remaining recovery work

1. Recover the full Agent-side parsers for `0x34B1`, `0x34D2`, and `0x34F2`
   to turn the confirmed serializer order into named wire structures.
2. Recover the GameServer↔ShardManager direction and message IDs for Fortress
   War and Battle Arena; the current evidence is strongest on local state,
   SQL, and GameServer→Agent messages.
3. Obtain matching stored-procedure definitions to prove result codes,
   transaction boundaries, and rollback behavior.
4. Obtain a binary/capture that actually contains Survival Arena or Magic Pop
   before adding those names to the GameServer parity matrix.

## Primary evidence

- [`sr-gameserver-binary-reference.md`](sr-gameserver-binary-reference.md)
- [`../analysis/decompiled/gameserver-lifecycle/00412C20_FUN_00412c20.c`](../../analysis/decompiled/gameserver-lifecycle/00412C20_FUN_00412c20.c)
- [`../analysis/decompiled/gameserver-quest-party/00676490_FUN_00676490.c`](../../analysis/decompiled/gameserver-quest-party/00676490_FUN_00676490.c)
- [`../analysis/decompiled/gameserver-sql/006480E0_FUN_006480e0.c`](../../analysis/decompiled/gameserver-sql/006480E0_FUN_006480e0.c)
- [`../.references/testin-decompilation/sql-contract-catalog.csv`](../../.references/testin-decompilation/sql-contract-catalog.csv)
- [`../.references/testin-decompilation/baseline/interesting-strings.csv`](../../.references/testin-decompilation/baseline/interesting-strings.csv)
- Ghidra project `analysis/ghidra-projects/vsro-testin-gameserver`, program `/SR_GameServer.exe`.
