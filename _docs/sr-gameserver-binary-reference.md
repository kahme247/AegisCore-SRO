# SR_GameServer binary reference

This document is the unified, binary-first reference for the pinned TestIn
`SR_GameServer.exe`. It records the executable lifecycle, packet-dispatch
contract, recovered object layouts and database calls. It does not use emulator
names as proof. Focused combat, aggro and quest notes remain authoritative for
their detailed algorithms and are linked below.

The feature-focused COS/pet recovery is documented separately in
[sr-gameserver-pet-systems.md](sr-gameserver-pet-systems.md), including the
binary-confirmed command subcommands, transport state, COS inventory SQL, and
the unresolved revival path.

## Evidence identity and method

- Binary: `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`
- Size: `8,925,184` bytes
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Architecture: 32-bit Windows x86
- Saved analysis: Ghidra project `analysis/ghidra-projects/vsro-testin-gameserver`
- Primary program object: `/SR_GameServer.exe`

Here, **confirmed** means an immediate value, memory access, branch, call,
serialization operation or SQL string exists in the pinned executable.
**Derived** means deterministic arithmetic was applied to confirmed data (for
example, converting a dispatcher slot address to an opcode). **Unresolved**
means a conventional name may exist elsewhere but is not proved by this binary.

The executable is not the vSRO 1.274 binary also present in the workspace. Its
SHA-256 is `5193de0a32fc43e7c7d603d615c18305e78f3a7dfb2aa35eabfeb352d2010fd0`;
addresses and layouts in this reference must not be transferred to that build.

## Process and game lifecycle

### Startup

The main recovered game initialization routine is `0x00412C20`. Its success
path is ordered as follows:

1. Capture local time and validate locale against China, Korea and Taiwan;
   unknown locale data can abort initialization.
2. Resolve the current directory, load `AbuseFilter.txt`, create the object
   factory and load GameServer configuration at `0x00427050`.
3. Allocate two global work buffers with confirmed capacities of `10,000,000`
   and `10,000` bytes/entries (the element semantics are unresolved).
4. Initialize the siege-fortress manager, game-world instance manager and
   initial resources. Failure in any step leaves the success path.
5. Create the overlapped-message manager and require at least one loaded
   region.
6. Register scheduled jobs with periods visible in the initializer: `1000`,
   `300`, `600000`, `300` and `10000` milliseconds.
7. Initialize, in order, the skill system (`0x005893E0`), security manager,
   party manager, flea-market manager, guild manager, training-camp manager,
   Silk Mall and server monitors.
8. Project reference quests into the quest manager with `0x0057FE70`.
9. Run post-world siege initialization, instantiate the town-guard object,
   initialize optional flag-world/battle-arena paths and log
   `InitializeGame() Succeeded!`.
10. Initialize the final server-event middleman when its configuration byte is
    enabled.

The order matters. In particular, scheduled jobs exist before the skill,
security and social managers, and the quest manager is built only after server
monitors.

### Steady state

`0x00413590` registers four monitor channels:

| Monitor ID | Embedded label |
| --- | --- |
| `0x3020001` | `Obj Count` |
| `0x3020002` | `DBQuery Count` |
| `0x3020003` | `Game Update` |
| `0x3020004` | `Msg Dispatch` |

The watchdog at `0x00401390` samples the global update tick once per second.
It normally uses a 30-second lag threshold, tightens the threshold to 10
seconds after its first detected stall and keeps a ten-sample history. Outside
a debugger, repeated stalls launch `userdump\\userdump.exe SR_GameServer.exe`,
wait for the dumper and terminate the process with `_exit(0)` after more than
three trace cycles. This is a hard fail-stop, not graceful degradation.

The game loop exposes separate `PumpNetMsg` and `Update` exception diagnostics.
Their embedded text records the latest update thread ID, latest processed
message ID and object code name, proving that network pumping and world-object
updates are separately monitored stages.

### Shutdown boundary

Server-session loss is dispatched at `0x00416F80` by comparing the disconnected
peer's registered name against `SR_GameServer`, `AgentServer`, `FarmManager`
and `SR_ShardManager`.

| Lost peer | Confirmed response |
| --- | --- |
| `SR_GameServer` | call `0x00530910(serverID, 0)` and log the disconnect |
| `FarmManager` | log only in the recovered branch |
| `AgentServer` | call `0x00417B30` for Agent-scoped user closure |
| `SR_ShardManager` | call `0x00417A80` for global user closure/save, then clear global `0x00C825D0` |
| unknown | log ID and registered name; no recovered teardown call |

`0x00417A80` is the confirmed global disconnect/save path. It walks the live
user collection at owner offsets `+0x2294` (container/root) and `+0x2298`
(count). For every entry it invokes player virtual slot `+0x274` with
`(0xFFFFFFFF, 0)`, then calls `0x004DF050(4)`. After all users it calls
`0x006351F0` to save siege state and finally `0x00411CC0` on the manager at
global `0x00D6A9A8`. The last call's semantic name remains unresolved.

`0x00417B30` applies the same player virtual call but collects only entries
whose `0x004E00A0` result equals the disconnected AgentServer identifier. It
then saves siege state, logs
`Disconnected to AgentServer, all user connections are closed!!`, processes
the collected entries with `0x004DF050(4)`, and removes the Agent registration
through `0x00437FC0(global 0x00D6A900, agentID)`.

The siege-save routine `0x006351F0` walks fortress records. For eligible live
records it calls `0x006212B0`, then logs the persisted current tax read from
the nested fortress object at `+0x04`, tax value `+0x28/+0x2C` (`u64`).

These paths prove user-close-before-final-manager-call ordering and explicit
siege persistence on ShardManager/AgentServer loss. They do not yet prove that
all asynchronous DB queues have drained before the calls return. The watchdog
path remains a separate hard dump-and-`_exit(0)` terminal path and does not run
this recovered graceful sequence.

## Network dispatch and opcodes

### Incoming client dispatcher

`0x0050D0C0` first fills a contiguous 2,048-entry table with a default handler,
then replaces selected entries. The table covers `0x7000..0x77FF`; every entry
is eight bytes (handler plus a zero field):

```text
slotAddress = 0x00CD3D08 + (opcode - 0x7000) * 8
opcode      = 0x7000 + (slotAddress - 0x00CD3D08) / 8
```

A mechanical pass over the initializer finds **163 non-default slots**. This
count and the address-to-opcode mapping are binary-confirmed. Most handler
semantics are deliberately unnamed until their parsers are traced.
The complete machine-readable mapping is
[`evidence/sr-gameserver-testin-opcodes.csv`](evidence/sr-gameserver-testin-opcodes.csv).
`LAB_*` entries identify handlers whose entry point has not yet been promoted
to a Ghidra function; they are still exact code addresses.

Confirmed anchors useful to implementation are:

| Opcode | Slot | Handler | Confirmed behavior |
| --- | --- | --- | --- |
| `0x7045` | `0x00CD3F30` | `0x0050FE50` | object/player target selection; reads one `u32 targetUID`, resolves the live object, then invokes its selection virtual; see [player-right-click-target-selection.md](player-right-click-target-selection.md) |
| `0x7046` | `0x00CD3F38` | `0x00510250` | adjacent teleport/dialogue request family |
| `0x704C` | `0x00CD3F68` | `0x00510980` | combat/action request path used by the recovered combat work |
| `0x7202` | `0x00CD4D18` | `0x005169D0` | dedicated potion/skill-restoration request; reads `u32,u32,u8` |
| `0x7203` | `0x00CD4D20` | `0x00516BB0` | sibling dedicated potion/skill-restoration request; reads `u32,u32,u8` |
| `0x705A` | `0x00CD3FD8` | `0x00513590` | teleport request family |
| `0x70D9` | `0x00CD43D0` | `0x005167D0` | reads one `u32 questID`, then runs the abandon path |
| `0x7515` | `0x00CD65B0` | `0x0051B370` | reads `u32 rewardID`, `u8 count`, then `count` `u32` entry IDs |

Registered opcode ranges are sparse, not continuous. The confirmed populated
groups are `0x7005..0x716A`, `0x7202..0x725A`, `0x7302..0x730D`, `0x7402`,
`0x7420`, `0x7470..0x74E0` and `0x7501..0x751D`. A missing entry in those
ranges still resolves to the default handler.

This table is only the client request dispatcher. It does not establish that
all outbound `0xBxxx`, `0x3xxx` or inter-server messages have a corresponding
entry here.

### Confirmed outbound constructors

| Opcode | Constructor/runtime | Confirmed layout |
| --- | --- | --- |
| `0x3514` | `0x004EC8C0` | `u32`, `u8`, encoded string |
| `0x30D4` | `0x004EC930` | dialogue type and encoded string; types 4/5 add a bounded selection list; type 5 adds string + `u32` |
| `0x30DA` | `0x004ED1F0` | `u32`, `u8` |
| `0x30DC` | `0x004ED240` | `u32`, `u8` |
| `0x30EC` | `0x004ED4A0` | one encoded string |
| `0x3CA2` | `0x004F12B0` | encoded string, `u32` |
| `0xB515` | `0x0051B370` | success `u8(1)`; failure `u8(2), u8(reason)` |
| `0xB0D9` | `0x00571B80` | success `u8(1), u32 questID`; failure `u8(2), u8(reason)` |

The executable's message filter at `0x00427D90` groups `0x30D4`, `0x70D9`
and `0x7515`, but that fact alone does not make `0x30D4` an inbound request.

## Recovered structures

All offsets below are build-specific. They are field observations, not stable
C++ ABI declarations.

### Player quest state

| Object/offset | Width | Confirmed use |
| --- | ---: | --- |
| player `+0x1CA0` | 4 | non-zero blocks recovered quest-abandon path; semantic name unresolved |
| player `+0x2024` | 4 | current quest dialogue/type continuation |
| player `+0x2028` | 4 | dialogue/reward continuation consumed by selection |
| player quest manager `+0x14` | 4 | root/container for ordered quest-ID records |
| runtime record `+0x05` | 4 | owning player/context pointer |
| runtime record `+0x09` | 4 | resolved reference-quest pointer |
| runtime record `+0x0D` | 4 | quest content/state pointer |
| runtime record `+0x11..+0x1D` | 16 | four copied volatile-progress words |
| quest content `+0x20` | 1 | lifecycle/type state |
| quest content `+0x22` | 2 | completion/reset discriminator |

The odd offsets show that the decompiler is observing a packed or interior
record. They must not be rounded to aligned offsets in a reimplementation.

### Quest globals and manager

| Object/offset | Confirmed use |
| --- | --- |
| global quest data `+0x8A8` | number of projected reference quests |
| quest manager `+0x14` | ordered definition collection |
| script globals `+0x37B4/+0x37BC/+0x37C0` | declaration-array bounds/count, stride `0x1C` during reset |
| async quest query `+0x28/+0x2C/+0x30` | three persistence arguments |
| async quest query `+0x34` | operation byte `0..2` retained for completion behavior |

### Skill/threat observations

The skill parser at `0x00587630` stores authored values at these offsets:

| Tag | Skill offset | Confirmed role |
| --- | ---: | --- |
| `alop` | `+0x304` | normal aggro suppression/block behavior |
| `2tnt` | `+0x3CC` | flat and percentage threat increase |
| `tntd` | `+0x3D0` | threat-reduction operands |
| `agkl` | `+0x47C` | aggro absorption/link behavior |

See [sr-gameserver-aggro.md](sr-gameserver-aggro.md) and
[sr-gameserver-combat-variance.md](sr-gameserver-combat-variance.md) for the
confirmed arithmetic, x87 rounding and mitigation order.

## SQL and asynchronous database work

### Inventory method

A string-reference pass over the saved program selected static strings
containing `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `EXEC` or `CALL`. It
exported 329 referencing functions and found 142 distinct SQL-bearing string
literals. This is an auditable lower bound, not a claim that the GameServer has
only 142 queries: `0x009732F0` and `0x009742D0` also build generic SQL from
table/column metadata, and stored-procedure bodies live outside the executable.

The binary routes database work through asynchronous query (`AQ_*`) objects.
Query construction and completion behavior are separate: for quests, for
example, the operation byte is retained in the AQ object but is not the first
formatted SQL argument.

### Confirmed direct SQL families

| Domain | Confirmed tables/procedures | Representative builder(s) |
| --- | --- | --- |
| Character inventory | `_Inventory`, `_Items`, `_BindingOptionWithItem`, `_RentItemInfo`, `_InventoryForAvatar`, `_InventoryForMagicCube` (table vocabulary) | `0x0043D2C0`, `0x0043FB20`, `0x004548E0`, `0x00730120` |
| Account/guild/COS storage | `_Chest`, `_GuildChest`, `_InvCOS`, `_InventoryForLinkedStorage`, `_STRG_*`, `_GET_USER_GOLD`, `_GET_GUILD_GOLD`, `_UPDATE_*_GOLD` | `0x0043D2C0`, `0x0043D980`, `0x0043EEC0`, `0x00459F70..0x0045E080` |
| Item-mall package operation `0x18` | `_STRG_ADD_PACKAGEITEM`, `_STRG_ADD_PACKAGEITEM_WITH_STOCKQUANTITY` | `0x0050FA50` -> `0x0045E080` -> `0x0045E620` |
| Cash-item insertion and magic-parameter item creation | `_STRG_BUY_CASHITEM_EXP`, `_STRG_BUY_CASHITEM_EQUIP_NORMAL`, `_STRG_ADD_ITEM_MAGIC` | `0x0045A5D0`, `0x0045ADA0` |
| COS lifecycle | `_AddNewCOS`, `_DeleteCharCOS`, `_ChangeCOSState`, `_COS_Naming`, `_CharCOS` | `0x00440120`, `0x00440CB0` |
| Quest state | `_quest_manage`, `_InsertQuestinCharByName`, `_InsertEndQuestinCharByName`, `_ResetQuestByCharName`, `_RemoveQuestByCharName` | `0x00455F50`, `0x0051DE90` |
| Skills | `_skill_manage` | `0x004587A0` |
| Open market | `_OpenMarket`, `_OpenMarket_Insert`, `_OpenMarket_Update` | `0x004535A0..0x004548E0` |
| Item exchange completion | `_STRG_MOVEITEM_NEW`, plus conditional `_Char.RemainGold` update | `0x0043FB20` |
| Fortress/siege | `_SiegeFortress*`, `_AddSiegeFortressObject`, `_UpdateSiegeFortressTaxRatio`, `_SiegeFortressTaxCollect`, `_SiegeFortressHireNPC`, `_SiegeFortressEntryDungeon` | `0x00456EC0`, `0x004CD3F0`, `0x0061E330`, `0x00627DE0..0x00628890` |
| Trade job | `_AddTrijobReward`, `_DeleteOldTrijobData`, `_InsertCharTrijobSafeTradeData`, `_DeleteCharTrijobSafeTradeData` | `0x00465220..0x00465F20`; lifecycle/opcodes/structures are documented in [vsro188-job-system.md](vsro188-job-system.md) |
| Timed jobs | `_TimedJob`, `_TimedJobForPet` deletes | `0x00650AF0`, `0x006514A0`, `0x0065FDC0` |
| Guild history/memo | `_GPHistory`, `_DeleteGPHistory`, `_Memo_Delete` | `0x005D05E0`, `0x005D70A0` |

### Exact high-value calls

The following strings are reproduced because their argument order affects a
compatible implementation:

```sql
exec _quest_manage %d,%d,%d
exec _skill_manage %d, %d, %d, %d
{?=CALL _AddNewCOS (%d,%d,%d,%d,%d,%d,1,%d,%d,%d,%d,'%d-%d-%d %d:%d:%d')}
{?=CALL _DeleteCharCOS (%d,%d,%d)}
{?=CALL _ChangeCOSState (%d,%d)}
{?=CALL _COS_Naming (%d,'%s')}
{?=CALL _STRG_MOVE_ITEM (%d,%d,%d,%d,%d,%d,%d,%d)}
{?=CALL _STRG_MOVEITEM_NEW (%d,%d,%d,%d,%d,%d,%d,%d,%d)}
{?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}
{CALL _STRG_ADD_SPECIALTY (?, ?, %d,%d,%d,%d,%d, '%s')}
{CALL _STRG_ADD_ITEM_MAGIC (?, ?, %d, %d,%d, %d,%d,%I64d,%d, %d, ...)}
{CALL _STRG_BUY_CASHITEM_EXP (?, ?, %d,%d,%d, %d,%d, ?,?,?)}
{CALL _STRG_BUY_CASHITEM_EQUIP_NORMAL (?, ?, %d,%d,%d, %d,%d,%I64d, %d, ?,?,?)}
{?=CALL _UPDATE_USER_N_CHAR_GOLD (%d,%I64d, %d,%I64d)}
{?=CALL _UPDATE_GUILD_N_CHAR_GOLD (%d,%I64d, %d,%I64d)}
{?=CALL _UPDATE_CHAR_GOLD_FOR_GUILDCHEST (%d,%I64d)}
EXEC _AddTrijobReward %d, %d
EXEC _DeleteOldTrijobData %d
```

At `0x00455F50`, the emitted quest call always supplies literal `0` as the
first argument. The operation selector `0..2` is stored at AQ offset `+0x34`.
Conflating those values would be a compatibility bug.

Direct ad-hoc statements also exist. Confirmed examples update character gold,
character `RefObjID`/scale, item `RefItemID`, siege state/tax/introduction and
delete expired siege/timed-job rows. The GameServer therefore does not use
stored procedures exclusively.

### Transaction boundary not present in the binary

Procedure names and call formatting are confirmed; stored-procedure bodies,
isolation levels, locks, rollback behavior and result-set contracts are not.
Those require the matching database procedures. A call string must not be used
to infer atomicity.

## Focused binary notes

- [SR_GameServer alchemy subsystem](sr-gameserver-alchemy.md)
- [SR_GameServer quest-system recovery](sr-gameserver-quest-system.md)
- [SR_GameServer aggro recovery](sr-gameserver-aggro.md)
- [SR_GameServer combat damage variance](sr-gameserver-combat-variance.md)

## Remaining work

- Recover the process entry point through service/console bootstrap and the
  final manager destruction and asynchronous-query drain semantics.
- Trace and name all 163 registered request handlers from their packet reads;
  registration alone proves only opcode-to-address ownership.
- Export a normalized machine-readable SQL catalog from the Ghidra project so
  future binary hashes can be diffed mechanically.
- Recover network session, world object, character, inventory and asynchronous
  query layouts beyond the fields currently exercised by confirmed paths.
- Obtain the exact stored-procedure set matching this executable and prove
  transaction/result semantics independently.
