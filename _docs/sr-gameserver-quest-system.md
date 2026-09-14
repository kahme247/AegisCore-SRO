# SR_GameServer and sro_Client quest-system recovery

This note records the quest architecture recovered from the pinned TestIn
`SR_GameServer.exe` and the code-equivalent vSRO 1.188 client pair. It
deliberately distinguishes values and layouts proved by executable control
flow from names found only in protocol corpora or other emulators. Broader
client initialization and decoder evidence is cataloged separately in
[`vsro188-client-binary-reference.md`](vsro188-client-binary-reference.md)
and [`vsro188-client-decoder-layouts.md`](vsro188-client-decoder-layouts.md).

## Evidence identity and confidence

- Binary: `SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Deployed client: `VSRO_Client/sro_client.exe`
- Deployed-client SHA-256: `3df2282dab84ee5439944b3e6ca8e8cfc69cb05963a45f0f7f4b8adca579c206`
- Stock code authority: `.references/sro-client/vsro-188/sro_client.stock.exe`
- Stock-client SHA-256: `58b9da3a218894f28e4c82a992b90997869cf331ab450929c8fcb7453327d114`
- Client quest handler registration: `0x0086FBB0`, `0x008980E0`
- Client detailed quest decoder: `0x008812B0`, `0x008B94D0`
- Client outbound quest builders: `0x007020C0`, `0x0081EC20`,
  `0x0081ECE0`, `0x0081EE60`, `0x008252D0`
- Quest-manager startup: `0x00412C20`, call to `0x0057FE70`
- Quest-definition projection: `0x0057FE70`
- Script-data reset: `0x00580AD0`
- Quest event-token loader: `0x0086D090`
- Lua callback registration: `0x008706B0`
- Client message filter construction: `0x00427D90`
- Player quest-state transitions: `0x00570CB0`, `0x00570F10`, `0x00571170`
- Quest persistence command builder: `0x00455F50`
- Quest dialogue constructors: `0x004EC8C0`, `0x004EC930`
- Gather/capture constructors: `0x004ED1F0`, `0x004ED240`
- Quest notification constructor: `0x004ED4A0`
- Script-progress constructor: `0x004F12B0`
- Reward-selection result path: `0x0051B370`
- Client dispatcher initialization/dispatch: `0x0050D0C0`, `0x0050EEE0`
- Quest dialogue selection handler: `0x00516670`
- Abandon request adapter/runtime path: `0x005167D0`, `0x00571B80`

In this document **confirmed** means that the value participates in decoded
instructions, control flow, serialization or query construction in the
identified binary. **Strong inference** means that two confirmed paths fit together but
their final indirect dispatch edge has not been recovered. Names from
`framework/docs/opcodes.txt`, SilkroadDoc and eSRO remain secondary evidence.

## Recovered architecture

Quest content is not a table-driven subsystem inside the GameServer alone.
The executable contains hundreds of concrete quest and mission classes
(including `CQuestBase`, `CBasicQuest`, `CQuestAType`,
`CMissionKillMonster`, `CMissionDeliverItem`, `CMissionDialog`,
`CMissionCatchMonster` and collection/talisman mission classes), plus a
generic manager and per-player runtime state.

At startup `0x00412C20` initializes ordinary world and skill systems, calls
`0x0057FE70`, and only then logs `Quest Manager Successfully initialized`.
`0x0057FE70` enumerates the reference quest collection whose count is stored
at global quest data offset `+0x8A8`. For each entry it builds a 36-byte
temporary projection with `0x00581050` and inserts it into the manager object
at manager offset `+0x14` through `0x00580E70`. It finishes with
`0x00915470`. This proves that quest definitions are materialized before
players can execute quest logic; it does not prove that the modern AegisCore
`REF_QUEST_*` schema is the legacy binary's direct source.

`0x00580AD0` is a live script-data reset path. It logs reset start/end,
clears three script-side registries, walks the quest-script declaration
array at global offsets `+0x37B4`, `+0x37BC` and `+0x37C0` (array stride
`0x1C`), resolves each quest object, and invokes virtual slots `+0x34` and
`+0x38` with the quest ID and code name. The reset therefore rebuilds quest
script registrations rather than merely clearing player progress.

## Script and Lua control layer

The executable embeds a Lua-facing quest API rather than hard-coding every
quest transition in the network handlers. `0x008706B0` registers native
function pointers and their exact Lua names through `0x00976C20`. Important
binary-confirmed registrations include:

| Lua name | Native callback | Confirmed native action |
| --- | --- | --- |
| `LuaSaveQuestNow` | `0x0086CBE0` | reads a quest ID and invokes player virtual `+0x190` (the operation-`2` transition adapter); if that reports an existing quest, it invokes `+0x1B0` and `+0x1A0`, then calls `0x00928690` (quest virtual slot `+0xF8`) |
| `LuaSaveLocalQuestNow` | `0x0086CB90` | invokes player virtual slot `+0x1A0` for the current script player |
| `LuaSetAbortQuest` | `0x0086E120` | creates/fills an abort-quest descriptor through player virtual slot `+0x184`, saves through `+0x1A0`, and updates the active quest instance through virtual helpers `+0xFC` and `+0x118` |
| `LuaQuestStart` | `0x0086E310` | reads a quest ID, resolves it in the active quest-instance collection, and invokes quest virtual slot `+0xF4` through `0x00927A30` |
| `LuaPayQuest` | `0x0086FDF0` | resolves an active quest by ID and invokes quest virtual slot `+0x104` through `0x009286B0`; the returned integer is pushed back to Lua |
| `LuaQuestSetStartHandler` | `0x0086D7C0` | registers the `UNIQUE_DEAD` start handler and stores the returned handle at handler object `+0x54` |
| `LuaQuestSetDeleteHandler` | `0x0086D700` | unregisters the `UNIQUE_DEAD` handle stored at handler object `+0x54` |

The callback names are not guesses: assembly immediately loads each native
address into `EAX`, its Lua-name string into `EDI`, and calls the common
registration routine.

### Core quest vtable

MSVC RTTI links the `CBasicQuest` type descriptor at `0x00C804F4` through its
complete-object locator at `0x00B7668C` to the vtable at `0x00B3EC04`.
`CQuestAType` has its own RTTI/vtable at `0x00B2A914` and inherits the same
implementations in the Lua-facing range. This resolves the previously opaque
virtual slots:

| Slot | `CBasicQuest` implementation | Confirmed behavior |
| ---: | --- | --- |
| `+0x80` | `0x009219A0` | event-`0x0E` countdown tick and expiry path |
| `+0x98` | `0x0092F270` | base event-`0x18` no-op; concrete quests may override it |
| `+0xF4` | `0x00922DE0` | quest start/setup path used by `LuaQuestStart` |
| `+0xF8` | `0x009235C0` | save/finalize path used by `LuaSaveQuestNow` and successful payment |
| `+0xFC` | `0x00923930` | abort/reset cleanup path selected by the abort descriptor helper |
| `+0x104` | `0x00924250` | `CBasicQuest::PayQuest`; validates/consumes required items and applies authored rewards |
| `+0x114` | `0x009259C0` | evaluates authored mission/payment conditions, resets an invalidated phase to state `1`, or promotes a satisfied quest to reward-ready state `8` |
| `+0x118` | `0x00925D20` | updates quest/NPC dialogue availability after state changes |

The start implementation establishes the current Lua player and quest ID,
allocates the player's quest descriptor, creates authored mission-event
handles, calls player virtual slot `+0x19C` with state tuple
`(questID, 1, 0, 1)`, saves through player slot `+0x1A0`, emits log/event code
`200`, and refreshes quest/NPC state.

The finalize implementation unregisters the mission-event handles created by
start, removes the quest's map mark through player slot `+0x220`, emits
log/event code `201`, and refreshes quest/NPC state. Quest-state persistence
is handled separately through the player quest manager.

The abort/reset implementation performs corresponding handle and quest-item
cleanup, writes player quest state through `+0x19C` using
`(questID, 6, 0, 1)`, saves through `+0x1A0`, and emits log/event code `202`.
The exact distinction between script `ABORTQUEST` and `RESTARTQUEST` is still
controlled by the abort descriptor and its caller, so this shared native
implementation is not labeled as only one of those events.

### Timed and repeat-cycle behavior

The common start/setup implementation `0x00922DE0` contains the
binary-confirmed timed-quest setup. Its quest-content descriptor at quest
`+0x308` (`param_1[0xC2]`) supplies a duration byte at descriptor `+0x18` and
a timer mode at `+0x1C`:

- a positive duration is split as `duration / 60` and `duration % 60` and
  sent through player virtual `+0x1C4` with `(questID, minutes, seconds, 1)`;
- the same branch registers quest event `0x0E` through player virtual
  `+0x138`, retaining the returned handle in the player quest descriptor;
- timer modes `1` and `4` send a zero-valued timer display and also register
  event `0x0E`;
- timer mode `2` registers event `0x18` instead.

The duration is populated from the declarative quest script. Registered
callback `LuaSetLimitTimeStartCondition` at `0x0086B1B0` sets policy bit
`0x08` in the current Lua build context at `+0x848` and stores its integer
argument as a 32-bit value at context `+0x858`. Quest-finalization copier
`0x00929660`, specifically instructions `0x00929E9A..0x00929EA6`, resolves
the destination descriptor through quest `+0x308` and copies that context
value into descriptor `+0x18`. The two retained declarations with value
`120` are therefore exactly the positive countdown durations consumed by
`0x00922DE0`, not a separate offer-only timeout. Their unit is seconds:
`0x00922DE0` passes `duration / 60` and `duration % 60` as the displayed
minute/second pair, so both authored values begin at `02:00`; timer packer
`0x00570650` further divides the minute quotient by `24` for its hour/day
fields.

The adjacent fields must not be conflated. `LuaSetDifferentString` callback
`0x0086B200` narrows its numeric argument to one byte at build context
`+0x85D`, and copier instructions `0x00929EA9..0x00929EB5` transfer it to
descriptor `+0x1D`. The common start routine instead reads timer mode from
descriptor `+0x1C`. Concrete native quest setup writes that distinct byte;
for example `CQNO_RM_OLDWOMAN_5` setup `0x008A30A0` writes mode `2`.
`LuaSetDifferentString` is therefore not the timer-mode source despite its
adjacent storage. `QNO_WC_SMITH_1` and `QNO_KT_ACCESSORY_1` both supply
duration `120`, and the positive-duration branch selects common event `0x0E`
independently of mode.

The other nearby policy callbacks are separated in the same way:
`LuaSetRealTime` (`0x0086BA00`) only sets build-context policy bit
`0x100000`; `LuaSetDayOrNight` (`0x0086B620`) and `LuaStartDayLimit`
(`0x0086B670`) set policy bit `0x20000` and store 32-bit values in distinct
build-context fields. None is the recovered source of descriptor `+0x18` or
the copied descriptor `+0x1D` mode.

Their operational consumers are in the shared quest object, not the player's
quest record. Offer/start predicate `0x00882C10`, specifically
`0x00883350..0x008833D4`, reads descriptor `+0x08` as a world-time selector:
value `1` rejects world hours `[4,20)` and therefore admits night, while value
`2` rejects hours outside `[4,20)` and therefore admits day. The same predicate
rejects an ordinary offer when descriptor `+0x0C` is positive and the mutable
descriptor counter at `+0x10` is zero. Common activation `0x00922DE0` initializes
that counter from `+0x0C` during quest setup (`0x0091F1F5..0x0091F220`) and
decrements it after each successful start. `LuaStartDayLimit` is consequently a
shared quest-wide start quota for the selected day/night window, not a
per-character achievement or daily-repeat limit.

Quest tick `0x0091FBC0` maintains the quota boundary. It samples the simulated
world hour through the server manager's virtual slot `+0x40` by default, stores
the last sampled hour at descriptor `+0x14`, and replenishes `+0x10` from the
authored `+0x0C` only on the `19 -> 20` transition for selector `1` or the
`3 -> 4` transition for selector `2`. Policy bit `0x100000` changes only this
tick's hour source: `0x0091FBDD..0x0091FBEE` obtains local wall-clock time with
`time(0)`, converts it through `_localtime32_s` (`0x00975580`), and reads
`tm_hour` through `0x00975500`. The offer predicate still uses simulated world
time. Thus `LuaSetRealTime` is a reset-clock modifier; by itself it adds no
offer-time exclusion predicate.

AegisCore mirrors that singleton scope with `QuestDayLimitService`. It admits
only the two exact authored descriptor shapes: night quest
`QNO_KT_SOLDIER_WE2_1` with quota `20`, and day quest
`QNO_WC_GENARAL_SW_1` with quota `5`. The service derives the current phase
from `GameTimeManager.CurrentWorldTime`, shares one counter per quest across
all characters, and rolls the window at simulated `20:00` or `04:00`.
Acceptance holds the quota lock across durable character activation and
decrements only after that activation succeeds, preserving the GameServer's
successful-start boundary. Process restart reconstructs the authored quota,
matching the pinned executable's non-persisted singleton descriptor. Standalone
`LuaSetRealTime` quests are no longer rejected because the bit has no
independent offer predicate; unsupported day-policy tuples remain fail-closed.

The shared `CBasicQuest` event-`0x0E` handler is vtable slot `+0x80`, resolved
to `0x009219A0`. For a positive authored duration it looks up the player's
quest state and decrements its countdown byte at `+0x04`. At zero it invokes
quest virtual `+0xFC` (the recovered abort/reset cleanup), refreshes through
`+0x118`, and sends the authored expiry token at quest `+0x4F0` through the
single-token `0x30EC` channel. While non-zero, every ten-second boundary sends
an updated `(questID, minutes, seconds, 0)` timer display and saves through
player `+0x1A0`. That save does not include the countdown byte: player slot
`+0x1A0` resolves through `0x004ECF80` to quest-manager save `0x00571600`,
whose fold routine `0x0050CDE0` copies only runtime progress words
`+0x11..+0x1D` into mapped `_CHARQUEST.QuestData1/2`. Countdown `+0x04`
is transient in the pinned server.

AegisCore retains the stock wire cadence and adds a durable server-side
checkpoint for the two authored positive countdowns. Migration `000098` adds
`CHAR_QUEST.TIMER_REMAINING`, initializes
active legacy rows from the recovered `120`-second policy, resets it on each
new activation, checkpoints each non-zero second, resumes the
saved value on reconnect, and clears it on expiry, manual abandon, or reward
completion. The expiry transition persists status `6`, retires the client
quest, and sends the quest-specific `SN_TALK_*` token. It never grants a fresh
two minutes merely because the player reconnects.

The base `CBasicQuest` event-`0x18` slot (`+0x98`) is the no-op
`0x0092F270`; event-`0x18` therefore requires a concrete quest override to do
work and has no universal expiration behavior. The complete 186-vtable scan
finds exactly ten overrides: `CQNO_RM_OLDWOMAN_5`, `CQNO_RM_SLAVE2_2`,
`CQNO_EU_EASTEU_1`, `CQNO_EU_EASTEU_19`, `CQNO_EU_IVY_2`,
`CQNO_CA_WAREHOUSE_1`, `CQNO_CA_WAREHOUSE_3`, `CQNO_TQ_ORNAMENT_3`,
`CQNO_CH_HWAN_2_4`, and `CQNO_EU_HWAN_2_4`. Their handlers are respectively
`0x008A0970`, `0x008A5000`, `0x008AA630`, `0x008B2A10`, `0x008BB5F0`,
`0x008C86A0`, `0x008CA6E0`, `0x008D6B10`, `0x009038D0`, and `0x00909600`.

These overrides implement authored periodic behavior rather than the shared
event-`0x0E` countdown/abort handler. The concrete `CGObjPC` producer is the
one-second player update at `0x0052AA90`. Nine handlers use runtime byte
`+0x04`; seven are ten-second ground-search rolls, two are ten-second Hwan
rolls, and `CQNO_RM_SLAVE2_2` uses the same byte as a repeating 60-second
pursuer-spawn interval:

| Quest ID / code | Start handler and trigger | Admission sphere / cap | Event-`0x18` result |
| --- | --- | --- | --- |
| `162` `QNO_RM_SLAVE2_2` | native state setup; no TypeID3/9 trigger | loaded region and actor life state other than `2` (dead); job state is not tested | every 60 ticks spawns `MOB_QT_01_WINGTRIBE` within random radius `0..20`; ordinary mission requires ten kills |
| `163` `QNO_EU_EASTEU_1` | `0x008AA700`, ref `10417` `ITEM_QNO_EU_EASTEU_3_02` | region `27211`, `(249,24,759)`, radius `200`; fewer than five result items | after 10 ticks, inclusive 50% roll grants ref `10414`; failure emits `SN_TALK_QNO_EU_EASTEU_1_12` |
| `181` `QNO_EU_EASTEU_19` | `0x008B2AB0`, ref `10441` `ITEM_QNO_EU_EASTEU_19_03` | region `25420`, `(126,-41,1863)`, radius `400`; fewer than 20 | after 10 ticks grants ref `10439` without a probability roll |
| `207` `QNO_EU_IVY_2` | `0x008BB6C0`, ref `10484` `ITEM_QNO_EU_IVY_2_03` | region `25950`, `(253,85,449)`, radius `600`; fewer than 20 | after 10 ticks, `rand()%101 >= 50` grants ref `10483`; failure emits `SN_TALK_QNO_EU_IVY_2_10` |
| `203` `QNO_CA_WAREHOUSE_1` | `0x008C87B0`, ref `10476` `ITEM_QNO_CA_WAREHOUSE_1_02` | region `27237`, `(1057,218,634)`, radius `1000`; fewer than ten | after 10 ticks, inclusive 67% roll grants ref `10475`; failure emits `SN_TALK_QNO_CA_WAREHOUSE_1_09` |
| `239` `QNO_CA_WAREHOUSE_3` | `0x008CA7B0`, ref `10506` `ITEM_QNO_CA_WAREHOUSE_3_01` | region `26219`, `(1525,152,1495)`, radius `500`; fewer than 20 | after 10 ticks, `rand()%101 >= 50` grants ref `10507`; failure emits `SN_TALK_QNO_CA_WAREHOUSE_3_08` |
| `286` `QNO_TQ_ORNAMENT_3` | `0x008D6BE0`, ref `10562` `ITEM_QNO_TQ_ORNAMENT_3_01` | any of four region-`32773` spheres, radius `300`; fewer than ten | after 10 ticks, inclusive 50% roll grants ref `10563`; failure emits `SN_TALK_QNO_TQ_ORNAMENT_3_13` |
| `357` `QNO_CH_HWAN_2_4` | `0x00903730`, ref `23367` `ITEM_QNO_CH_WHAN_2_4_01` | region `25954`, `(1672,180,232)`, radius `300`; result absent | after 10 ticks, inclusive 10% roll grants ref `23368`, sets bytes `+0x01/+0x03`, and completes; failure emits `SN_TALK_QNO_CH_HWAN_2_4_10` |
| `371` `QNO_EU_HWAN_2_4` | `0x00909460`, the same ref `23367` | the same sphere and result-absence gate | same 10% promotion, with EU success/failure talk keys |

AegisCore now assigns the distinct quest-`162` path to the global one-second
native quest manager rather than the TypeID3/9 ground-action scheduler. It
persists runtime byte `+0x04`, promotes the newly active quest on its first
periodic event, suppresses the spawn while actor life state is `2` or the
current region is not loaded, and creates transient ref character `5950`
`MOB_QT_01_WINGTRIBE`. The spawn offset mirrors `0x008A5000`: one uniform
angle over `0..2*pi` and one uniform distance over `0..20`, rather than two
independent square offsets. The transient NPC is not attached to an ordinary
spawn area, so its death cannot enqueue a normal population respawn. A failed
world admission leaves the durable countdown at zero for the next event;
successful admission confirms the recovered reset-and-trailing-decrement
value of `59`.

All TypeID3/9 rows above are ground quest-action objects, not ordinary
`0x704C` inventory-use items. This distinction prevents mode `2` from being
implemented as the positive-duration quest timer while also proving that the
player's periodic event source is shared by all ten overrides.

For quest `162`, player virtual `+0xF8` resolves through the `CGObjPC` vtable
to `0x00485EE0`, which returns the actor-state byte at the start of the object
state record. The comparison against value `2` is therefore a dead/terminal
actor gate; it is not a trade-job check. `0x008A5000` also resolves the
player's current region through `0x00404CC0` before spawning. The native
failure order is significant: it resolves `MOB_QT_01_WINGTRIBE`, constructs
and admits the NPC, then resets runtime byte `+0x04` to `60` and consumes the
current tick. A failed reference/world admission leaves zero intact for a
retry on the next player update. Agent mirrors this with a two-phase durable
tick/confirmation, creates an aggressive transient NPC at a polar random
distance `0..20`, and never binds it to an ordinary respawn nest.

Player timer-display slot `+0x1C4` resolves to adapter `0x004ED090` and quest
manager function `0x00570650`; it is not a separate timer opcode. The manager
packs the supplied time into runtime field `+0x99` (or writes `0xFFFFFFFF` for
the sentinel), then sends a detailed subtype-`2` `0x30D5` delta with flag
`0x04`, which is the conditional field already decoded by `0x005785E0`.
Consequently countdown display, periodic updates and ordinary quest progress
share the same client packet family.

The open `sro_client.exe` database (image base `0x00401000`) corroborates that
wire layout at decoder `0x008B94D0`: after reading the subtype metadata and
flags byte at object `+0x0A`, instructions `0x008B9504..0x008B9511` test flag
`0x04` and consume exactly one four-byte timer value into object `+0x0C`.
When the value is `0xFFFFFFFF`, `0x008B9524..0x008B953A` applies the client's
inactive-timer handling. This is the same field AegisCore uses for initial,
ten-second checkpoint, zero-expiry, and retirement updates.

Repeat-completion accounting is visible through `_CHARQUEST.AchievementCount`
and operation `2`. Its missing-record branch initializes `Status=4` and
`AchievementCount=1` in `0x00571170`; its existing-record caller
`LuaSaveQuestNow` invokes player `+0x1B0`, whose `0x00571520` implementation
increments the same field, before save and finalization. The asynchronous
continuation likewise writes state `4` and saves. This confirms an
achievement/completion-recording path distinct from abandon. It does not yet
establish repeat eligibility scheduling. State `4` itself is now confirmed as
the inactive/default status: the status getter returns `4` for a missing
record, login normalizes persisted status `0` to `4`, and the active-quest
counter excludes it. `AchievementCount` distinguishes a completed historical
record from no record when both report status `4`.

AegisCore now mirrors retained operation `2` for ordinary rewarded quest
completion. A new active record begins with achievement count `0`; payment
increments that count and persists status `4`, while status `5` remains the
separate close-without-achievement transition. Reconnect projects status-`4`
rows with a positive count into the completed-ID list but does not expose an
unattempted status-`4` row. Offer discovery can reactivate the inactive row
only while its count is below `LuaSetAchievementLimit`, and activation carries
the existing count into the next attempt. Dependency `all_clear` and
`one_clear` predicates likewise require status `4` plus a positive achievement
count rather than treating status `5` as a completed achievement.

There is no separate ordinary repeat-cycle scheduler in the pinned
`SR_GameServer.exe`. Registration at `0x00870B1D..0x00870B27` binds
`LuaSetAchievementLimit` to callback `0x0086A180`; that callback narrows its
argument to one byte at Lua build context `+0x825`. Finalizer `0x00929660`
copies the byte to the concrete quest object at `+0x2D`. Offer predicate
`0x00882C10` then reads the player's existing `AchievementCount` through
virtual `+0x1A4` and rejects only when the count is greater than or equal to
the non-zero limit. It performs no elapsed-time, completion-time, world-hour,
or wall-clock comparison on this branch. Consequently a normal status-`4`
completion below the authored limit is eligible again immediately; the
day/night singleton quota described above is an independent start policy.
AegisCore's offer query has the same immediate status/count predicate and
does not invent a repeat cooldown.

No separate party-quest RTTI class, party-specific quest SQL procedure, or
party-specific quest packet constructor was found in the binary string,
vtable and function-reference pass. The executable does have an independent
party manager and party EXP system, but those anchors do not establish shared
quest progress. The concrete-handler audit resolves the apparent party edge
as instance invalidation, not progress sharing. Exactly eight native quests
define `OnPartyStatusChanged`: `QNO_TQ_BAEKRYOUNG_17`,
`QNO_TQ_STONEBOARD_4`, `QNO_TQ_ORNAMENT_4`, `QNO_TQ_GENERALORDER_4`,
`QNO_CA_GORIA_7`, `QNO_EU_IVY_3`, `QNO_EU_EASTEU_21`, and
`QNO_RM_OLDWOMAN_6`. The first five share handler `0x008D7270`; the latter
three share `0x008BBB40`. On party-status value `1`, or value `3` when the
event identity equals player virtual `+0x08`, both handlers display
`UIIT_MSG_QUEST_INS_CONDITION`, invoke the player instance-exit path, and run
event cleanup `0x0063CCB0`. `0x008D7270` calls player `+0x2F8(0)`, whose
`0x004F1190` adapter falls through to `+0x2DC`; `0x008BBB40` calls `+0x2DC`
directly. The concrete `+0x2DC` adapter `0x00485B80` reaches
`0x005F7580`, raises event `0x1B`, and requests the current game-world exit.
No handler awards another party member objective progress. AegisCore keeps
quest progress character-local and implements these eight hooks at the party
transition boundary: a dismiss exits every affected instance member, while a
single leave/kick exits only the removed identity. Exit displays
`UIIT_MSG_QUEST_INS_CONDITION`, restores the persisted pre-instance position,
deletes durable membership, leaves the GameWorld scope, and uses the stock
teleport reset exchange.

`TestNativeEvent18CoverageAudit` complements the retained quest-script audit
with the complete ten-entry concrete event-`0x18` catalog. It binds quests 140
and 162 to their bespoke runtimes and quests 163, 181, 203, 207, 239, 286,
357, and 371 to the generalized catch-action catalog, checking both quest IDs
and effective handler addresses.
`TestNativePartyInstanceHooksHaveExecutableCoverage` binds all eight quest
IDs, code names, and handler addresses to the executable instance-exit catalog.

### Quest-script teleport

`INS_QUEST_TELEPORT` is an embedded quest instruction name, not a network
opcode or SQL procedure. The generic instruction builder at `0x006435F0`
looks up that name in the server instruction registry, allocates the
instruction/action object, records the destination region and coordinates,
applies the authored random offset, and submits the resulting teleport action
through the target object's virtual action path.

Concrete quest handlers including `0x008A1290`, `0x008BBF10`, `0x008C1EF0`,
`0x008D7730`, `0x008DC110` and `0x008ECEB0` use the same sequence. They first
write an authored quest step through quest/player virtual `+0x25C`, prepare a
talk token, resolve `INS_QUEST_TELEPORT`, derive the player's destination
position, add bounded random displacement, and invoke the registered
teleport action. Thus quest-step mutation precedes action submission in these
handlers. Teleport failure and post-transfer recovery are owned by the shared
movement/action subsystem; these functions do not issue `_quest_manage` or a
special quest SQL call.

The quest-side failure contract is explicit in `0x008A1290`.
`QNO_RM_OLDWOMAN_6_Step` is set to `3` before the instruction lookup. A
missing `INS_QUEST_TELEPORT` object returns `9999` without restoring that
step. If action initialization through virtual `+0x2B0` fails, the handler
invokes action virtual `+0x1F0` with `(3,255,0)` and returns `9999`; if final
submission through virtual `+0x134` fails, it destroys the temporary action
state and returns the same value. Neither branch calls a quest transition,
quest save, or compensating step write. Thus the pinned quest system has no
quest-state teleport rollback policy for Agent to mirror; only the transient
movement/action object owns failure cleanup.

The complete native quest-instance death/teleport set is six handlers. These
are not ordinary fixed-destination quest instructions: each address is the
single method in the quest class's auxiliary OnDead vtable. For example,
CQNO_RM_OLDWOMAN_6 has RTTI entry 0x00B306FC, thunk 0x008A25F0, and handler
pointer 0x00B30704 -> 0x008A1290. Media supplies the quest IDs:

| Quest ID / class | OnDead handler | Required dead ref code | Step/talk written before teleport | Instance variables after successful submission |
| --- | ---: | --- | --- | --- |
| 141 QNO_RM_OLDWOMAN_6 | 0x008A1290 | MOB_QT_01_TAHOMET | step 3; SN_TALK_QNO_RM_OLDWOMAN_6_12 | EVENT_TIMER = ElapsedTime + 60000 |
| 208 QNO_EU_IVY_3 | 0x008BBF10 | MOB_QT_01_IVY | step 3; SN_TALK_QNO_EU_IVY_3_21 | ElapsedTime = 0; EVENT_TIMER = 60000 |
| 251 QNO_CA_GORIA_7 | 0x008C1EF0 | MOB_QT_01_HUNLANCER | step 3; SN_TALK_QNO_CA_GORIA_7_15 | ElapsedTime = 0; EVENT_TIMER = 60000 |
| 287 QNO_TQ_ORNAMENT_4 | 0x008D7730 | MOB_QT_TQ_SNAKEGENERAL4 | step 3; SN_TALK_QNO_TQ_ORNAMENT_4_16 | per eligible instance member: ElapsedTime = 0, MobKillTime = 0, EVENT_TIMER = 60000 |
| 276 QNO_TQ_STONEBOARD_4 | 0x008DC110 | MOB_QT_TQ_TOMBGENERAL4 | step 3; SN_TALK_QNO_TQ_STONEBOARD_4_18 | per eligible instance member: ElapsedTime = 0, MobKillTime = 0, EVENT_TIMER = 60000 |
| 311 QNO_TQ_BAEKRYOUNG_17 | 0x008ECEB0 | MOB_QT_TQ_WHITESNAKE | step 3; SN_TALK_QNO_TQ_BAEKRYEONG_17_14 | per eligible instance member: MobKillTime = 0, increment DungeonStage; then EVENT_TIMER = ElapsedTime + 60000 |

All six resolve the dead actor's reference identity first and do nothing when
it does not match. They set the authored step and send the talk token before
resolving INS_QUEST_TELEPORT. The relocation seed is the player's current
u16-region plus float-X/Y/Z tuple: X and Z independently receive
20 + 80*(rand()/32767), Y is retained, and facing is uniformly selected from
0..360 degrees. After action initialization, the handler constructs an
instance descriptor, iterates the current game-world member collection, and
conditionally updates each member's corresponding quest record. It then
submits through instruction virtual +0x134 and finally calls instance/event
cleanup 0x0063CCB0.

This establishes a stronger implementation boundary than a generic teleport
call. Agent must first carry an instance/game-world identity, membership,
quest-owner eligibility, transient instance variables, and coordinated
member relocation. Applying these handlers through the existing ordinary
return/gate teleport path would incorrectly move only one character in the
public world and discard member iteration and dungeon state, so that shortcut
is intentionally not wired.

Agent now models this native boundary as a two-phase instance-death plan for
all six authored quest/mob pairs. Preparation validates the instance's quest,
captures a stable sorted member set, step 3, the exact talk token, and the
post-submit variable mutation without changing live state. Commit applies the
mutation only after a coordinated teleport producer reports success. The
relocation helper independently consumes three legacy `rand()/32767` rolls:
X and Z each gain 20..100 units, Y is unchanged, and heading spans 0..360
degrees. Combat now resolves a dead mob only when it shares the killer's
non-public GameWorld scope, prepares the matching plan, and submits it through
the shared teleport handler. That handler validates every captured member,
sends the authored notification to the quest actor, moves all members to the
single seeded destination, stages each session for loading, emits `0x34B5`,
and only then commits the transient variables. Submission failure does not
roll back the already-completed monster death or ordinary kill rewards.

The client half is confirmed in the open sro_client.exe database (image base
0x00401000). Registration routine 0x0086FBB0 maps opcode 0x34B5 to
0x0086E100. The handler queues response 0x34B6 and delegates to 0x0086DAB0,
which reads the destination u16, clears the prior world state, and executes
the common reset path. Coordinated quest-instance relocation therefore uses
the same per-session reset/ready/bootstrap exchange as other teleports rather
than inventing an instance-only client packet.

The shared instruction builder at 0x006435F0 and copy routine 0x00424590
also recover the submitted destination-entry ABI. Each vector element has a
28-byte stride: u32 kind at +0x00, signed ID at +0x04 (initialized to -1),
u32 value at +0x08, u16 region at +0x0C, two padding bytes, and three
32-bit coordinate fields at +0x10/+0x14/+0x18. The ordinary helper appends
kind 2 with the caller's ID, region, and X/Y/Z, then submits the containing
action through virtual +0x134. Container constructor 0x00411370 initializes
the entry vector at +0x20 and auxiliary ownership index at +0x2C; destructor
0x00418EC0 releases both. These are action-owned transient records, not rows
in the quest SQL schema.

Agent now has the first runtime prerequisite for reproducing these actions:
every world object carries a server-owned game-world scope. Scope zero is the
public world; non-zero scopes isolate quest-instance members. Both snapshot
visibility and reciprocal live reconciliation require exact scope equality,
so actors occupying identical region coordinates in different instances
cannot enter one another's known-object lists. Instance allocation,
membership transfer, transient variables, and coordinated teleport remain
separate lifecycle work; the visibility boundary prevents those later pieces
from leaking an incomplete instance into the public world.

The instance manager path further fixes identity and lifecycle semantics.
SR_GameServer.exe 0x005F7B50 allocates a pair of unsigned 16-bit IDs,
rejects world 0xFFFF, creates the world/layer object, and announces the packed
pair to the requesting player. The manager's destroy job 0x005F80B0 is
explicit: it marks the instance closing, walks every actor whose current
world/layer pair matches, evacuates those actors, and only then releases the
world. It does not prove an automatic "last member left" destroy rule.
Character membership insertion 0x00602BF0 validates the character-owned key
before persisting through _CharInstanceWorldDataUpdate; 0x00602D50 issues the
matching character/world deletion only for a retained active instance row.

Agent now represents this as a packed GameWorldScope with public sentinel zero
projecting to the stock client pair 1/1. A concurrency-safe quest-instance
registry enforces one instance per scope and at most one instance membership
per object, includes the owner as the first member, returns stable sorted
member snapshots, and exposes explicit create/join/leave/destroy operations.
Destroy returns the full evacuation set and clears member scopes; it does not
infer teardown merely from emptiness. Party member serialization now emits
the player's actual WorldID/LayerID pair instead of hard-coded 1/1, preserving
the existing public-world bytes while making instance membership visible to
the client and other party members.

The pinned SR_GameServer.exe (image base 0x00401000) registration routine at
0x008531D0 fixes the durable `_CharInstanceWorldData` field order as CharID,
DungeonKeyID, WorldID, LayerID, OpenedTime, RegionID, PosX, PosY, PosZ,
IsActivated, EnterCount, and LastEnterTime. Agent migration 99 mirrors those
twelve member/location fields in `CHAR_QUEST_INSTANCE_WORLD`, and the SQL
repository saves, loads, and deletes rows using the recovered character,
dungeon-key, and world identities. The registry's named quest stages and
timers deliberately remain instance-local memory: the GameWorld virtual
interface exposes variable lookup at +0x258, set at +0x25C, and member
collection at +0x260, while this persistence descriptor contains no named
variable columns. Persisting those transient variables would therefore go
beyond the binary evidence.

The open sro_client.exe database corroborates that party wire field.
Opcode 0x3864 dispatcher 0x00886B10 routes added/updated members through
0x00883620. When update mask 0x20 is present, that parser consumes region and
the conditional normal/dungeon coordinate widths, then reads four bytes at
member record +0x54. The client therefore retains the same adjacent
u16 WorldID/u16 LayerID pair emitted by Agent; those bytes are not padding.

The creation side is an internal service job rather than a client-authored
request. SR_GameServer.exe (image base 0x00401000) dispatches opcode 0x3C15
through 0x00414260 into 0x005F7B50. Create job 1 asks the configured world
provider for a u16 WorldID/u16 LayerID pair, rejects WorldID 0xFFFF, creates
the world, and only then publishes the packed pair. Job 2 resolves an existing
pair for entry, while job 3 invokes explicit destruction at 0x005F80B0.
Character insertion at 0x00602BF0 validates the character-owned dungeon key
before copying CharID and WorldID into `_CharInstanceWorldData` and submitting
its durable update.

The restored vSRO shard and extracted Media `gameworlddata.txt` provide the
authoritative catalog behind that provider. `_RefGame_World` columns are ID,
WorldCodeName128, Type, WorldMaxCount, WorldMaxUserCount, WorldEntryType,
WorldEntranceType, WorldLeaveType, WorldDurationTime, WorldEmptyRemainTime,
and ConfigGroupCodeName128. The six native death quests map to WorldIDs
10, 12, 13, 16, 17, and Baekryeong stage worlds 25/26/27 respectively; all
are Type 1, allow 100 layers, and allow eight users per layer.

Migration 101 and `import-reference` now maintain those rows in
`REF_GAME_WORLD` plus the quest/stage mapping. The SQL scope allocator selects
the entry-stage world, serializes allocation with a connection-owned advisory
lock, chooses the lowest free LayerID within WorldMaxCount, and durably
reserves the pair before returning it. `QuestInstanceLifecycle` then creates
owner membership and persists the recovered twelve-field row; failure destroys
membership and releases the reservation. Join likewise adds membership before
persistence and rolls it back on failure; leave removes the durable
character/world row and restores membership if deletion fails. Agent wires
this catalog-backed allocator and lifecycle at startup. The client reset
packet itself carries only the destination region; instance identity remains
in actor and party state rather than being appended to 0x34B5.

The first concrete authored entry producer is quest 251
`QNO_CA_GORIA_7`. Setup handler 0x008C39D0 binds
`INS_QNO_CA_GORIA_7` (WorldID 13), target NPC `NPC_CA_HUNTER`, required
`ITEM_QNO_CA_GORIA_7_03`, and unique `MOB_QT_01_HUNLANCER`. Media identifies
reference item 10522 as the Big Drum. Its localized quest instructions are
also explicit: the user must lead an EXP-auto-share/item-auto-share party;
members proceeding with or having completed Goria 7 may enter; and an active
trade transport prevents entry. OnEnterWorld handler 0x008C3190 initializes
Step, UniqueSpawned, ElapsedTime, and LastTime in the new GameWorld before
installing its callbacks. The stock client sends the drum through its ordinary
0x704C TypeID-classified item-use path at 0x00796660, so entry does not require
a new client opcode.

Agent now recognizes that exact item after the ordinary 0x704C shape checks,
enforces the leader/sharing/transport/quest conditions, allocates WorldID 13,
creates the leader membership, and joins only eligible connected members. The
Big Drum is consumed through the normal transactional item-use repository.
After the `0xB04C` success, every persisted member keeps their current region
and coordinates, enters the shared scope, and performs the standard
`0x34B5/0x34B6` reset/bootstrap exchange. A pre-consumption item-use failure
destroys the prepared membership rows and releases the scope reservation.

The same producer family covers three more ordinary item-use entries, but not
every instance quest. Media and the pinned server agree on the exact objects:
quest 208 uses reference 10490 `ITEM_QNO_EU_IVY_3_06` (Key of Sign), quest
276 uses reference 10550 `ITEM_QNO_TQ_STONEBOARD_4_03` (Fake Head of Hyun
Lang), and quest 287 uses reference 10567 `ITEM_QNO_TQ_ORNAMENT_4_04`
(Explosive Stone). Ivy's `ITEM_QNO_EU_IVY_3_08` is Red Hair and is a later
quest result, not an entry key. The native party-entry walkers are
0x008BEC40 for Ivy and 0x008DA980 for Ornament; each applies its quest/item
eligibility check before inserting a nearby member into the authored world.
Agent routes these three confirmed objects through the same party admission,
scope allocation, transactional consumption, and reset exchange as Goria.

Quests 141 and 311 remain separate dialogue-driven producers. Old Woman 6
enters `INS_QNO_RM_OLDWOMAN_6` through Shifle movement dialogue; its world
initializer 0x008A2750 creates the `QNO_RM_OLDWOMAN_6_*` variables consumed by
the world tick at 0x008A1940. Baekryeong 17 presents movement choice
`SN_TALK_QNO_TQ_BAEKRYEONG_17_23` from dialogue handler 0x008EFE00 and then
uses three staged worlds. Neither is included in the item-entry catalog;
Baekryeong's later stage gates still require separate recovery.

The initial movement dialogues are now recovered directly. Old Woman handler
0x008A3690 presents `SN_TALK_QNO_RM_OLDWOMAN_6_07` (or repeat prompt `_02`)
with choices `_28` and `SN_TALK_COMMON_DENY`; its accepted party walker is
0x008A3E40. Baekryeong handler 0x008EFE00 presents `_24` with movement choice
`_23`. Agent publishes those exact menus only at `NPC_RM_SLAVE2` (reference
5905) and `NPC_CH_MOONSHADOW` (reference 22867), then sends an accepted leader
and eligible party through the same lifecycle and client-reset coordinator as
the item producers. A rejected preparation consumes neither an item nor a
scope reservation.

Baekryeong's internal progression is concentrated in world tick 0x008ED660.
The executable intentionally spells its variables `QNO_TQ_BAEKRYOUNG_17_*`.
It indexes `CurrentUnique` as 0/1/2 for Flame Master, Black Snake, and White
Snake in WorldIDs 25/26/27. After a stage kill it waits 60 seconds, admits
members with the quest or `ITEM_QNO_TQ_BAEKRYEONG_17_01`, increments their
per-quest dungeon stage, and moves them to the next authored world; stage 2
uses a 1,380,000 position scalar and stage 3 uses 1,980,000 in the native
movement descriptor. This is a world-to-world membership migration rather
than a new NPC/item producer and remains a separate runtime implementation
boundary.

The two intermediate OnDead callbacks are distinct from the final handler.
Flame Master callback 0x008EC9F0 writes Step 1, speaks `_08`, copies
ElapsedTime into MobKillTime, and increments CurrentUnique plus DungeonStage.
Black Snake callback 0x008ECC50 performs the same sequence with speech `_11`.
Neither submits an immediate teleport. Agent now commits these ordered world
mutations from combat and rejects a boss whose identity does not match the
current stage; the existing White Snake rule remains the final Step-3
INS_QUEST_TELEPORT callback. Registry scope transition also preserves the
owner, sorted membership, and transient variable map for the later durable
WorldID 25-to-26-to-27 move. Firing the delayed move still depends on adding
the recovered per-GameWorld ElapsedTime tick rather than using wall-clock
timers detached from instance state.

`CBasicQuest::PayQuest` is the reward boundary. It walks the authored mission
types, verifies required inventory entries, applies configured gold/EXP/SP
components, and removes required quest items. On its confirmed successful
completion branch it writes `(questID, 4, 1, selection)` through player slot
`+0x19C`, saves through `+0x1A0`, calls the finalize slot `+0xF8`, and then
refreshes quest/NPC state through `+0x118`. Failure to consume a required item
returns without finalizing and logs the embedded `CBasicQuest::PayQuest`
diagnostic.

Reward delivery ordering is also recoverable. `CBasicQuest` virtual `+0x10C`
resolves to `0x00924CF0`. It obtains the selected reward descriptor and walks
its item entries first. Each required stack is delivered immediately through
quest virtual `+0x18C` (`0x00929460`), which calls player inventory virtual
`+0x22C` and accepts only result code `1`. Stack counts are split according to
the reference item's maximum stack size; a remainder causes one additional
insertion call. Any failed insertion returns `0` immediately.

Only after all item insertions succeed does `0x00924CF0` call quest virtual
`+0x194` (`0x009295F0`) to clear temporary per-reward-entry markers, then
apply the remaining authored reward components in this order:

1. the paired 64-bit progression values through player `+0x170`;
2. the capped currency value through player `+0x16C`;
3. the optional flag-controlled reward through player `+0x2B8`;
4. the positive scalar reward through player `+0x174`;
5. the optional string/reference reward through player `+0x324`;
6. the optional final flag reward through player `+0x314`;
7. player notification/update `+0x274` and the global reward log hook.

The failure boundary has no hidden legacy compensation phase.
`0x00924CF0` returns `0` immediately when reference lookup, maximum-stack
validation, or any individual inventory insertion fails. Items inserted by
earlier loop iterations are not removed. After the item loop, the function
writes the player quest-runtime stage byte at `+0x01` to `2`, then advances
it through `3..8` as the remaining reward components execute; those markers
do not drive a retry branch in this function. No rollback, reverse-item walk,
or quest-state restoration is present, and the confirmed callers finalize
only on its success result.

AegisCore deliberately strengthens durability while retaining the observable
ordering. Required-item consumption, all reward item stacks, balances,
progression, inventory expansion, Hwan/named points and quest closure share
one SQL transaction. Any pre-commit failure rolls all of them back. A commit
failure is surfaced as `ErrQuestOutcomeUnknown` and the reward-selection
handler sends no success or retryable failure acknowledgement; reconnect is
the reconciliation boundary. After commit, the live snapshot is updated and
item packets are published before balance/progression and quest removal,
matching the legacy item-first client order without reproducing its partial
reward loss.

The positive scalar at reward descriptor `+0x2C` is the fixed skill-point
field. It is distinct from the paired EXP/skill-EXP values at `+0x24/+0x28`,
and player slot `+0x174` receives `(amount, 1)`. AegisCore now adds this value
after skill-EXP conversion in the same transaction as the other supported
quest rewards, then sends the client's `u32` skill-point balance update.
Balances that would overflow that wire width fail closed.

The inventory-expansion branch is now resolved on the server side. The
optional byte tested before player slot `+0x2B8` reaches the `CGObjPC` vtable
entry at `0x00AF5CB4`, implementation `0x004E19D0`. That function reads the
current capacity from player offset `+0x1C2C`, adds the zero-extended authored
byte, and rejects a result above `0x70` (112). It then submits the change
through `0x0045DE20`; only a successful continuation returns result `1` to
the quest reward applier. GameServer builder `0x004E8B61` emits `0x3092` as
target byte `1` followed by the committed inventory capacity, while the
client `On3092` path applies that second byte to player inventory. AegisCore
now performs the same bounded additive update transactionally and publishes
the resulting capacity.

Player slot `+0x314` is not inventory expansion. Its vtable entry at
`0x00AF5D10` resolves to `0x004A9F40`, which stores a byte at player offset
`+0x1C58` and invokes the character-property update helpers. This is the Hwan
level branch: it consumes the authored reward byte as the new level rather
than adding it. AegisCore persists that exact level in the quest-completion
transaction, updates the live player and emits the binary-confirmed entity
Hwan-level packet. Zero retains its catalog meaning of no Hwan reward.

The script-named point reward is distinct from both fixed skill points and
the SQL academy scalar. `LuaSetAchievedPoint` is registered at `0x00870C4E`;
callback `0x00869D70` stores the amount and resolved string/reference at quest
descriptor `+0x8CC/+0x8D0`. The pinned script contains 36 variant-zero rows
and only two keys: `TEMPLE_ANUBIS` and `TEMPLE_HAROERIS`.

The `0x00924CF0` item loop is not limited to one item or quantity one. It
walks every selected ordinary reward entry, divides its authored quantity by
the reference item's maximum stack, inserts each full stack, and inserts one
remainder stack when needed. AegisCore now mirrors this for non-selectable
quest rewards, including multiple authored items. Required quest-item removal
releases its slot before reward allocation, while all database mutations stay
inside AegisCore's single completion transaction.

Player slot `+0x324` resolves to `0x004F1640`, which constructs internal opcode
`0x74DB` and serializes the keyed reward through a subsystem request. Reward
record parser `0x006AD880` independently whitelists the same two literal keys.
No direct client payload exists in selector `0x7515`; persistence is therefore
server-owned. Migration `000097` adds a per-character keyed balance, and
AegisCore applies the positive amount additively in the same transaction as
the quest completion. Unknown keys, multiple declarations, zero grants and
overflowing database writes fail closed.

The SQL `ACADEMY_POINTS` column is not an additional reward channel in this
build. The pinned catalog has 30 nonzero rows, all on the Temple quest range,
and every value exactly equals that quest's `LuaSetAchievedPoint` amount and
keyed `TEMPLE_ANUBIS`/`TEMPLE_HAROERIS` reward. Six other Temple rows retain a
Lua named-point amount while SQL stores zero. Correspondingly,
`SR_GameServer.exe` reward applier `0x00924CF0` has one keyed-point call through
player virtual `+0x324` and no second Academy-point application call. Agent
therefore accepts SQL zero or an exact SQL/Lua duplicate, grants the keyed
point once, and rejects mismatches or a standalone nonzero Academy scalar.

The selector is independently corroborated by `sro_client.exe` builder
`0x008252D0`: opcode `0x7515` is serialized as `rewardID:u32`, `count:u8`,
then exactly `count` `entryID:u32` values. `SR_GameServer.exe` handler
`0x0051B370` reads the same sequence before dispatching the selected reward.
The entry values are resolved reference-item identities, not UI row indices.

### Country-filtered selectable rewards

Reward lookup `0x00922D60` first resolves the player's quest runtime record
through player slot `+0x184`, reads the selection key from runtime offset
`+0x03`, and uses that byte to look up the reward-descriptor map at quest
offset `+0x3FC`. Payment therefore consumes an already selected authored
descriptor; it does not reinterpret the country/class/gender flags at the
end of `PayQuest`.

The pinned vSRO Media catalog establishes the supported country subset.
Quest `9` (`QNO_CH_SHAMAN_1`) lists both Chinese and European weapon rewards
with `IS_CHECK_COUNTRY=1` and selection count `1`; quest `311` likewise lists
Chinese and European ring rewards. The item references themselves carry the
country discriminator. AegisCore now locks the character's
`CHAR_REF_DATA.Country`, admits matching or universal-country (`3`) item
references, and rejects a forged foreign-country `0x7515` entry ID.

This Media set contains no class- or gender-checked quest reward rows. Its one
condition-without-country row is quest `167` (`QNO_EU_EASTEU_5`). The catalog
sets `ITEM_REWARD_TYPE=1`, raw `SELECTION_COUNT=0`, and lists exactly two
items: `ITEM_QNO_EU_EASTEU_5_02` (the one-hour Strength scroll) and
`ITEM_QNO_EU_EASTEU_5_03` (the one-hour Intelligence scroll). The localized
completion dialogue explicitly instructs the player to choose one reward.
For this proved conditional type-1 pattern, AegisCore therefore projects an
effective selection count of one, opens the ordinary reward prompt, and
validates the selected reference item through `0x7515`.

Class and gender mechanisms remain fail-closed rather than being generalized
without authored rows or executable predicates.

### Collection-item discovery and payment

The GameServer registers both collection declaration families in
`0x008706B0`: `LuaSetCollectionItemMissionData` at registration site
`0x00870B40` maps to callback `0x0086BF20`, and the `_EX` form at
`0x00870BB8` maps to `0x0086B260`. Their decoded arguments include mission
index, objective token, target NPC, required quantity, collected item, and
source-monster entries, matching the imported
`REF_QUEST_COLLECTION_ITEM_OBJECTIVE/SOURCE` catalog.

Six retained standard declarations are deliberately authored with source
chance `0` because their producer is a quest-local `UNIQUE_DEAD` Lua handler,
not `CMissionGatherItemFromMonster`'s ordinary roll. `QNO_SD_MA_029` through
`034` require, respectively, player regions `-32758` through `-32753` and
unique codes `MOB_SD_SELKIS`, `MOB_SD_NEITH`, `MOB_SD_ANUBIS`, `MOB_SD_ISIS`,
`MOB_SD_HAROERIS`, and `MOB_SD_SETH`. Each handler optionally removes one
`ITEM_<quest>_1`, then calls `LuaQuestAddItem_EXT` for one
`ITEM_<quest>_2`, the declared count-one mission item.

The native boundary corroborates the bytecode: `SR_GameServer.exe`
`0x0086D7C0` registers `UNIQUE_DEAD` and stores its handle at handler `+0x54`,
while `0x0086D700` unregisters it. `LuaQuestAddItem_EXT` is registered at
`0x00870C94` to `0x0086DA50` and performs the direct player inventory call
through vtable `+0x22C`; `LuaGetCurRegionID` at `0x0086D8F0` reads the player
region through vtable `+0xF4`; `LuaCmpCodeName` at `0x0086D990` compares the
current unique-event object's code name. Agent mirrors this as one atomic
transaction: exact quest/region/source qualification, optional phase-marker
consumption, mission-item insertion, reward-ready persistence, and inventory
plus `0x30D5` client projection. Inventory-full leaves the quest unchanged.

AegisCore admits the recovered standard variant-zero collection groups through
normal NPC quest discovery: 213 single-mission quests, eleven two-mission
quests, five three-mission quests, and two four-mission quests. Every retained
group has consecutive indices, a visible objective token per mission, and
bounded `u16` goals. The missing producer edge is recovered through RTTI.
`CMissionGatherItemFromMonster` has complete-object
locator `0x00B76574` and vtable `0x00B3EE64`. Its `0x0091C4D0` handler requires
quest content state `1`, `7`, or `8`, resolves the player's runtime quest
record, walks the authored source-monster list at mission `+0x1D/+0x21...`,
and retains the matching source index in runtime byte `+0x05`. Its
`0x0091C660` continuation checks the authored per-source numeric value at
mission `+0x14D + sourceIndex*4`; on success it calls player virtual `+0x254`
with the collected-item reference at mission `+0x241`, its quantity at
`+0x245`, and the current world-position context. `CGObjPC +0x254` resolves to
`0x004AA680`, which constructs the referenced item and admits it to the world
with placement mode `5`. Collection quest items are therefore conditional
mission drops, not global `_RefMonster_AssignedItemDrop` rows.

AegisCore now resolves active, incomplete, runtime-enabled collection
objectives and their matching `REF_QUEST_COLLECTION_ITEM_SOURCE` row at the
kill boundary, applies the authored source percentage, and materializes the
quest item through the same persisted ground-item/world-admission service as
ordinary loot. Pickup commits then advance the persisted count and `0x30D5`
task value. At payment, the required collected item quantity must still be
present: its stacks are locked, decremented or removed before reward
allocation, and the affected client inventory slots are refreshed. Collection
consumption, rewards, progression and quest closure are one database
transaction.

For standard multi-mission groups, Agent recomputes each mission from the
committed inventory stacks and packs up to four independent `u16` counts into
`CHAR_QUEST.QUEST_DATA_1`, ordered by mission index. Drop qualification reads
the corresponding packed slot, completion requires every authored goal, and
payment locks and consumes all one-to-four requirements atomically. The client
`sro_client.exe` outer decoder at `0x008B94D0` reads the mission-record count;
its per-record decoder at `0x008B6ED0` consumes the entry key, transition,
objective string, value count, and `u32` values. Agent therefore projects one
visible record and one inventory-derived value for every standard mission.

The sole extended collection quest, `QNO_WC_GENARAL_SP_1`, is also recovered.
`LuaSetCollectionItemMissionData_EX` callback `0x0086B260` parses the same
source-monster/string/chance pairs and then six control values. Its two
authored missions both require 15 items. Mission `0` supplies
`SN_CON_QNO_WC_GENARAL_SP_1` and control tuple `(1,0,0,0,0,0)`; mission `1`
supplies an empty objective token and `(0,1,0,0,0,0)`. The first tuple value
becomes the `CMissionGatherItemFromMonster` callback gate at mission `+0x249`:
`0x0091C890` emits a one-value progress record only when it is nonzero.
Mission `1` is therefore an internal companion requirement rather than a
second client objective.

Agent persists the visible mission's inventory-derived count in
`CHAR_QUEST.QUEST_DATA_1` and the companion count in `QUEST_DATA_2`. Drops use
each mission's own source probabilities, completion requires both counts, the
client continues to receive only the visible mission-0 value, and payment
locks and consumes both authored item requirements in the same reward
transaction.

### Standard delivery objectives

`CMissionDeliverItem` has complete-object locator `0x00B76528` and vtable
`0x00B3EEC4`. Its NPC/dialogue event handler is `0x0091CA00`, evaluator is
`0x0091CEB0`, and progress callback is `0x0091CFF0`. Event stage `0` only
checks the authored required and forbidden item lists through player virtual
`+0x224`, establishes pending dialogue state `10` (or walks state `40` pages),
and presents the authored type-`4` talk entry. It does not remove inventory.

The client-selected continuation re-enters `0x0091CA00` at event stage `2`.
Only pending state `10` advances to state `20`. The handler then consumes each
required stack through player virtual `+0x230` and applies the secondary item
list through virtual `+0x22C`; every mutation must return result word `1`.
After all mutations succeed it sets quest-state byte `+0x00` to `2`, persists
through player virtual `+0x1A0`, and invokes the class progress callback.
`0x0091CEB0` rechecks the inventory predicates, sets this mission's completion
bit at quest-state `+0x03`, and returns transition `2`; `0x0091CFF0` emits the
normal one-value `0x30D5` update with value `0`.

AegisCore mirrors this two-stage boundary for the imported variant-zero,
mission-zero, single-objective shape. Initial authenticated NPC contact lists
only delivery objectives whose aggregate authored stacks are present. The
chosen quest is retained in the validated NPC interaction, and only a second
confirmed `0x30D4` selector revalidates NPC range, locks the selected quest and
inventory, consumes its exact stacks, promotes it to reward-ready, and
publishes inventory plus quest updates. Multiple eligible quests are never
consumed as one batch. Other variants and multi-objective layouts remain
disabled.

### Multi-target extended kill objectives

The ordinary `LuaSetMissionData` catalog also contains 13 multi-mission
quests: twelve two-mission groups and the three-mission
`QNO_TQ_GENERALORDER_3`. These are not independent counter arrays. Every
authored mission selects `MONSTER_CLASS_NORMAL`, so
`CMissionKillMonster::0x0091BBE0` reads and increments the same runtime `u16`
at quest-state `+0x06`. The matching mission's descriptor goal at `+0x149`
caps that event, while completion is retained independently in the
mission-index bitset at quest-state `+0x03`. Thus a mission whose threshold is
already reached still needs a matching target event to set its own bit.

AegisCore enables the exact consecutive variant-zero groups. It persists the
shared normal counter in `QUEST_DATA_1`, the mission-completion bits in
`QUEST_DATA_2`, and repeats the shared value for every objective entry in the
active log and `0x30D5` projection. A matching target increments only while
the shared counter is below that mission's authored goal; reward-ready is
reached only after all two or three mission bits are set. Duplicate targets,
non-normal selectors, non-consecutive indices, and wider groups remain
invalid import shapes.

`LuaSetMissionData_EX` is registered at `0x00870C03` to GameServer callback
`0x0086B710`. The pinned quest chunk contains 77 extended kill declarations.
For the multi-target form, argument `4` is the target-list count, arguments
`5..5+count-1` are monster code names, and numeric argument `count+6` is the
shared required kill count. For example `QNO_CH_GENARAL_BO_1` names normal and
clone bandit archers and supplies shared goal `25`.

AegisCore now admits only runtime-enabled `multi_target` variant-0/mission-0
objectives. Any authored target identity advances the same persisted task
counter and `0x30D5` value.

One retained fortress quest, `QNO_FW_RB1_021`, authors two consecutive
`multi_target` missions instead: each mission names two monster variants and
requires eight kills. Agent stores their independent counters as consecutive
`u16` words in `QUEST_DATA_1`, emits both objective keys/values to the client,
and reaches reward-ready only when both counters reach eight.

The classified layout is now enabled for its authored two- and three-mission
forms. Argument `6` is the count of a
second list, argument `7` is its sole `MONSTER_CLASS_*` selector, and argument
`8` is the goal. Callback `0x0086B710` converts every numeric list/trailing
value with `__ftol2_sse`; therefore the one authored Giant value `1.5` becomes
integer goal `1`, rather than a fractional progress weight. The
`CMissionKillMonster` progress handler at `0x0091BBE0` maps selectors `0`, `1`
and `2` to separate normal, champion and giant `u16` counters at quest-state
offsets `+0x06`, `+0x0A` and `+0x0C`, increments the selected counter by one,
and compares it with the mission descriptor's integer goal at `+0x149`.
Agent preserves the same layout as three consecutive `u16` words in
`CHAR_QUEST.QUEST_DATA_1`, qualifies normal/champion/giant (including their
party base classes), and marks reward-ready only after every authored goal is
met. The active-log and `0x30D5` writer emits one objective entry per mission;
the pinned client reader `0x008B94D0` consumes the outer objective count and
`0x008B6ED0` consumes each entry's string plus `u8` value count and `u32`
values.

### Fortress reference-only rewards

The retained chunk contains 98 `QNO_FW_*` definitions with
`LuaSetDivideType(5)` and normal collection, extended-kill, delivery, dialog,
or talisman objectives, but intentionally no `LuaSetAchievedItem` call. Every
one has an active `REF_QUEST_REWARD` row. This is a stock-supported distinction,
not an absent reward: `CBasicQuest::PayQuest` at `0x00924250` consumes mission
requirements and calls reward virtual `+0x10C`; `CBasicQuest` maps that slot to
`0x00924CF0`. The latter resolves the reference reward through virtual `+0xF0`
(`0x00922D60`) and applies reference item rewards, EXP/SP EXP, gold, inventory
expansion, Hwan, and fixed SP. It does not require the Lua staging callback
`LuaSetAchievedItem` (`0x0086C2E0`).

The importer therefore admits exactly those 98 variant-zero, divide-type-5
fortress definitions only after their full objective group is runtime-safe.
It records their SQL reference reward and items with an explicit
`reference_reward` provenance marker, preserving the normal Lua/reference
equality gate for every ordinary quest. Fixed SP is mirrored into the existing
point-reward transaction so the same durable/client-balance path is used.

### Zero-mission ferry conversation

`QNO_WC_FERRY4_1` is the sole retained start definition with neither a mission
descriptor nor `LuaSetAchievedItem`. Its conversation bytecode distinguishes
`QUEST_STATUS_NODATA` from `QUEST_STATUS_ACHIEVED_BUT_NOT_PAYED`; the quest
branch has no progress phase. Its final page calls `LuaSaveQuestNow`, invokes
`LuaQuestGiveEventPoint_EXP_Gold(650,0,0)`, and adds one
`ITEM_QNO_WC_FERRY4_1_01`. The matching reference reward contains exactly the
same 650 EXP and one-item result.

GameServer registers `LuaQuestGiveEventPoint_EXP_Gold` at `0x00870C0D` to
callback `0x0086B910`; the callback applies its recovered scalar reward through
the player reward/progression virtuals and emits the normal refresh. Agent
models the stock state boundary directly: accepting this exact zero-mission
quest persists status `8` immediately, with no fabricated task counter; the
confirmed NPC payment then applies the 650 EXP/item reference reward in the
normal atomic completion transaction. `sro_client.exe` (image base
`0x00401000`) outer active-record reader `0x008B94D0` reads the objective count
at object `+0x11` and skips its entry loop when that byte is zero, proving that
the zero-objective active record is a valid client shape.

### Dialog objectives

`LuaSetDialLogMissionData` is registered in `SR_GameServer.exe` (image base
`0x00401000`) at `0x00870B4F` to callback `0x0086BB80`. Its Lua layout is
`questID, missionIndex, missionType, objectiveKey, dialogueKeyCount,
dialogueKeys...`. The callback converts the first three numeric arguments to
integers, stores the objective key, and copies at most the descriptor's ten
dialogue-key pointers into a 40-byte per-mission array. All 48 declarations in
the pinned chunk author one dialogue key.

The concrete `CMissionDialog` event handler is `0x0091D770`. On event stage
`0`, it rejects an already inactive quest, resolves the character's quest
state, and matches the incoming dialogue/NPC event against either the single
key at descriptor `+0x15` or one of up to ten keys beginning at `+0x7F`. A
match opens the authored type-`4` dialogue through player virtual `+0x150` or
the single-token continuation through virtual `+0x338`. The player's pending
dialogue state (returned by virtual `+0x158`) uses state `40` while walking
authored pages and state `10` once the final confirmation is displayed.

The client reply is not an implicit acknowledgement. `sro_client.exe` (image
base `0x00401000`) sends `0x30D4` with exactly one selected `u8`; GameServer
handler `0x00516670` stores that byte at player `+0x201D`. When a continuation
is present at player `+0x2028`, it invokes the active NPC/dialogue object's
virtual `+0x14C` with action stage `2`. `CMissionDialog::0x0091D770` then:

1. advances another authored page while pending state is `40`;
2. changes pending state `10` to `20` after the final confirmation;
3. when descriptor completion flag `+0x6B` is non-zero, sets quest-state byte
   `+0x00` to `2`, clears this mission's bit at quest-state `+0x02`, persists
   through player virtual `+0x1A0`, and invokes the common mission callback;
4. evaluator `0x0091DA60` sets the completion bit at quest-state `+0x03` and
   returns transition `2`, which feeds the normal `0x30D5` progress path.

This proves that dialog progress must be driven by the authenticated pending
dialogue continuation and its final selection, not merely by proximity or an
initial `0x7046` NPC-talk request. Migration `000095` adds the recovered
dialogue-key-to-NPC/menu route. The importer enables only variant-zero,
mission-zero, single-objective dialog quests, and Agent retains that selected
quest in the validated NPC interaction until the final `0x30D4` confirmation.
Only then does it persist the reward-ready transition and publish the typed
`0x30D5` delta. Symbolic inspection of the retained chunk finds that all 48
dialog declarations are variant `0`, mission `0`, single-objective, and carry
exactly one dialogue key. Multi-page and multi-objective handling therefore
remains outside the generalized runtime, but no retained dialog quest depends
on either shape.

### Kill-player objectives

`LuaSetKillPlayerMissionData` is registered in `SR_GameServer.exe` (image base
`0x00401000`) at `0x00870C3F` to callback `0x0086BC80`. The callback ORs the
variable filter globals into one mask, truncates the authored region to the
server's signed `u16` region representation, and stores the following scalar
as the minimum victim level. For all nine pinned declarations that scalar is
`105`; it is not a maximum.

`CMissionKillPlayer` is fixed by RTTI to complete-object locator
`0x00B765C0` and vtable `0x00B3EE04`. Event slot `+0x20` is
`0x0091BFF0`. It accepts only quest content states `1` and `7`, resolves the
runtime quest record, and calls qualifier `0x0091C130` on the killed player.
That qualifier reads the victim's level through virtual `+0x124`, active job
state through `+0x11C`, and position through `+0xF4`; it enforces the authored
level bound, membership in the descriptor's job-state byte list, and exact
signed region identity. The nine pinned declarations all use the same
`MKP_LEVEL`, `MKP_JOB`, `MKP_REGION` filter trio, minimum level `105`, region
`-32752` (`0x8010`), and job-state sets composed only of trader, robber/thief,
and hunter.

On success `0x0091BFF0` selects the normal/champion/giant `u16` counter at
runtime offsets `+0x06`, `+0x0A`, or `+0x0C` from descriptor byte `+0x35`,
increments it without exceeding the goal at descriptor `+0x31`, marks the
first progress transition as content state `7`, saves through player virtual
`+0x1A0`, and invokes the common mission callback. Evaluator `0x0091C2A0`
sets the mission completion bit; progress writer `0x0091C350` publishes the
selected counter through the ordinary `0x30D5` task-value section already
decoded by `sro_client.exe`. Client decoder `0x008B94D0` consumes that section
as `u8 valueCount` followed by exactly `valueCount` `u32` task values.

AegisCore enables only variant-zero, mission-zero, single-objective rows with
that exact three-filter vocabulary, bounded goal/mode values, and recognized
job states. Their single authored `LuaSetJobTypeStartCondition` is also
runtime-enabled: offer discovery joins the character's persisted
`CHARACTER_JOB.JOB_TYPE` to trader/robber/hunter and admits the otherwise
system-qualified quest only for that exact active job. A lethal player attack,
transfer, linked-damage hit, or abnormal damage tick records the victim's
authoritative level, active job type, and
signed navmesh region, persists matching quest progress, and sends the typed
`0x30D5` delta to the killer. Non-job victims and unrecognized authored
filters remain fail-closed.

`LuaSetJobTypeStartCondition` itself is not kill-player-specific. In
`SR_GameServer.exe` (image base `0x00401000`) it is registered at
`0x00870C21`; callback `0x00869E40` consumes the three Lua arguments and
stores the job selector and trailing mode as bytes at quest-descriptor
`+0x860/+0x861`. All 39 pinned declarations use the identical recovered
`(1, OBJ_JOBSTATE_TRADER|ROBBER|HUNTER, 1)` shape. AegisCore therefore enables
all 39 predicate rows and applies them before any otherwise-supported
objective family; unsupported objective shapes continue to fail closed at
their independent runtime gates.

This is sequential mutation, not an atomic GameServer transaction. If a later
item stack fails, earlier item insertions have already occurred; the recovered
failure path does not remove them. Likewise, `+0x194` clears temporary reward
markers and is not an inventory rollback. After `+0x10C` reports failure,
`PayQuest` invokes that marker cleanup, logs the failure and returns without
the state-`4` completion/save/finalize sequence. Database-side atomicity or
compensation outside these functions is not evidenced by the executable.

### Player quest-manager slots

Ghidra's recovered MSVC symbols identify `CGObjPC`'s primary vtable at
`0x00AF59FC` (with a secondary vtable at `0x00AF60AC`). The quest-facing player
slots used above resolve as follows:

| Player slot | Implementation | Confirmed delegation |
| ---: | --- | --- |
| `+0x184` | `0x004ECE70` | finds the quest runtime record and returns its volatile-progress block at runtime `+0x11`, or null when absent |
| `+0x188` | `0x004ECE90` | forwards the quest ID to transition `0x00570CB0`, which retains operation `0` |
| `+0x18C` | `0x004ECEB0` | forwards the quest ID to transition `0x00570F10`, which retains operation `1` |
| `+0x190` | `0x004ECED0` | forwards the quest ID to transition `0x00571170`, which retains operation `2` |
| `+0x194` | `0x004ECEF0` | returns content status `+0x20`, or default status `4` when the quest record is absent |
| `+0x198` | `0x004ECF10` | resolves a quest code name and returns the same status value |
| `+0x19C` | `0x004ECF50` | forwards state tuple arguments to `CGObjPC_QuestManager` at player `+0x1D98`, function `0x005709C0` |
| `+0x1A0` | `0x004ECF80` | forwards save-now to quest-manager function `0x00571600` |
| `+0x1A4` | `0x004ECFA0` | returns `AchievementCount +0x22` by quest ID, or zero when absent |
| `+0x1A8` | `0x004ECFC0` | copies the 16-byte `EndTime +0x34` value by quest ID, or sixteen zero bytes when absent |
| `+0x1AC` | `0x004ECFE0` | resolves a quest code name and returns its `AchievementCount`, or zero |
| `+0x1B0` | `0x004ED000` | forwards to `0x00571520`, which increments the quest content's `AchievementCount` at `+0x22` and marks it dirty |
| `+0x1B4` | `0x004ED030` | zero-extends the supplied byte into 16-bit `AchievementCount` and marks it dirty |
| `+0x1B8` | `0x004ED020` | counts ordinary active quest records, excluding statuses `4`, `5`, and `6`; returns `0xFF` without an owner |
| `+0x220` | `0x004ED660` | forwards quest-mark removal to the mark manager at player `+0x1DE8`, function `0x00579950` |

The three transition entries are consecutive data references in the primary
`CGObjPC` vtable at `0x00AF5B84`, `0x00AF5B88`, and `0x00AF5B8C`. They are
not dead helper functions. `LuaSaveQuestNow` at `0x0086CBE0` loads slot
`+0x190`, pushes the Lua quest ID and calls it, directly reaching the
operation-`2` transition. Numerous authored quest handlers call player slot
`+0x188`; operation `0` also has direct calls from `0x004E4D90` and
`0x0056F770`. The former starts referenced quests while processing a player
level increase that crosses their authored level, and the latter starts a
quest after its level gate succeeds before running its setup/notification
sequence. Operation `1` is exposed through slot `+0x18C`; concrete authored
quest handler `0x008A77A0` follows its existing-record return with the exact
state-`5` plus save sequence used by the asynchronous operation-`1`
continuation.

`0x005709C0` mutates the quest-content state byte at `+0x20` and constructs
`0x30D5` according to the transition form. State `1` registers the quest in a
player-side active collection; state `8` removes it from the corresponding
collection; states `4`, `5`, and `6` use the compact subtype-`3` update. A
state-`4` transition with its reset flag also clears all four volatile
progress words at runtime-record offsets `+0x11..+0x1D`.

The resolved save-now path `0x00571600` first calls `0x0050CDE0`, which copies
the runtime record's four volatile progress words into quest-content offsets
`+0x48..+0x54` and marks the content dirty when values changed. It then calls
the metadata-driven persistence serializer `0x009742D0` on the content object
with mode `2`. If that serializer returns a non-empty SQL string,
`0x00442120`/`0x004420C0` allocate a generic asynchronous query object, copy
the generated statement into it and submit it through the shared database
queue. It does not call `_quest_manage` directly. This is evidence that the
three retained query-object operations govern placeholder-row creation plus
post-success GameServer behavior, separately from the generic dirty-field
save and the start/end/abort state tuples.

The content mapping initializer at `0x00848140` binary-confirms the legacy
table name and mapped layout:

| `_CHARQUEST` column | Content offset | Native width | Mapping role |
| --- | ---: | ---: | --- |
| `CharID` | `+0x18` | `4` | key |
| `QuestID` | `+0x1C` | `4` | key |
| `Status` | `+0x20` | `1` | mode-2 persisted field |
| `AchievementCount` | `+0x22` | `2` | mode-2 persisted field |
| `StartTime` | `+0x24` | `16` | mode-2 persisted timestamp structure |
| `EndTime` | `+0x34` | `16` | mode-2 persisted timestamp structure |
| `QuestData1` | `+0x48` | `8` | mode-2 persisted field |
| `QuestData2` | `+0x50` | `8` | mode-2 persisted field |

The generic serializer's statement grammar is now exact. Mapping registration
`0x00848140` places `CharID` and `QuestID` in key group `0` and places all six
mutable fields in save group `2`, preserving the order shown above. During
database initialization, `0x00972B40` calls ODBC `SQLColumns`, matches each
returned column to its registered descriptor, validates its size/alignment
against the C++ offset, and records an internal value type. `0x009711D0`
selects the corresponding SQL value format:

| Internal type | Relevant ODBC type | Format text |
| ---: | --- | --- |
| `0` | `SQL_TINYINT` | `%u` |
| `1` / `2` | `SQL_SMALLINT` / `SQL_INTEGER` | `%d` |
| `3` | `SQL_BIGINT` | `%I64d` |
| `4` | floating point | `%f` |
| `6` | character data | `'%s'` |
| `7` | timestamp type `93` | `convert(datetime, '%d-%d-%d %d:%d:%d')` |

Metadata compiler `0x009732F0` constructs predicates with the exact literals
`"WHERE "`, `"%s%s=%s"`, and `"%s AND "`. It constructs assignments with
`"%s%s=%s,"`; therefore there are no spaces around `=` or after internal
commas. Runtime serializer `0x009742D0` begins with `"UPDATE %s SET "`, joins
multiple dirty mapping groups with the one-byte literal `","` at
`0x00AE71E4`, replaces the final comma with ASCII space, and appends the
preformatted predicate.

For the schema types required by the registered native layout, mode-`2`
serialization consequently renders exactly as follows before values are
substituted:

```text
UPDATE _CHARQUEST SET Status=%u,AchievementCount=%d,StartTime=convert(datetime, '%d-%d-%d %d:%d:%d'),EndTime=convert(datetime, '%d-%d-%d %d:%d:%d'),QuestData1=%I64d,QuestData2=%I64d WHERE CharID=%d AND QuestID=%d
```

This is a six-field group save, not six independently dirty column saves: once
group `2` is selected, all six values are formatted from the object. A schema
whose ODBC column types or sizes do not match the registered native offsets is
rejected by assertions during metadata initialization rather than silently
producing a different compatible query.

After successful serialization, `0x009742D0` clears the corresponding dirty
bookkeeping before the generated string is enqueued or executed. Consequently
the four 32-bit runtime progress words at `+0x11..+0x1D` are persisted as the
two adjacent 64-bit legacy values `QuestData1` and `QuestData2`, but a later
queue/database failure does not automatically re-dirty them in this path.
The table, keys, columns, exact assignment/predicate grammar, object offsets,
type formats and submission ordering are binary-confirmed.

Save-now submits this generated SQL through the generic `AQ_BrutalQuery`
object allocated by `0x00441F90`. `0x00442070` copies the statement into the
query, `0x00403600` attempts to enqueue it, and failed enqueue releases the
object. Query execution `0x00441FD0` calls the shared database executor
`0x009724C0`; result codes other than `0` or `1` log
`"AQ_BrutalQuery::DB Query Error(%d): %s"` and return failure. This query type
has no player-specific completion callback, logout, retry or compensation in
the recovered path.

The mark manager is likewise concrete. `0x005797E0` optionally removes an
existing mark, allocates a new record, stores the mark kind bytes, six authored
32-bit fields, quest/object identity, and a final flag, inserts it into the
ordered mark collection, and sends `0x30D6`. `0x00579950` finds matching mark
records by owner quest ID, sends one `0x30D7` per record, unregisters them,
unlinks the nodes, and frees their memory. The sibling `0x005798B0` performs
the same send/unregister/unlink/free sequence for one supplied runtime mark
handle instead of scanning by owner quest ID.

`0x0086D090` separately reads the quest event-token table and stores the
resolved values in the global script environment:

| Token | Global script-environment offset |
| --- | ---: |
| `CONVERSATION_SINGLE` | `+0x7C8` |
| `ABORTQUEST` | `+0x7CC` |
| `ENDQUEST` | `+0x7D0` |
| `RESTARTQUEST` | `+0x7D4` |
| `STARTQUEST` | `+0x7D8` |
| `UNIQUE_DEAD` | `+0x7DC` |

This proves that start, end, restart and abort are distinct script events and
recovers their common `CBasicQuest` state machinery. It does not map those
event tokens one-to-one onto the retained GameServer operation bytes `0`, `1`,
and `2`: the binary instead exposes those transitions through consecutive
player slots `+0x188`, `+0x18C`, and `+0x190`, while the ordinary state/save
slots perform dirty-data synchronization and mark removal separately.

## Per-player runtime structures

The three transition routines share the same insertion sequence. The
following fields are binary-confirmed even though the original C++ type names
are unavailable:

| Object/offset | Confirmed role |
| --- | --- |
| player `+0x2024` | current quest dialogue/type argument, set by both dialogue constructors |
| player `+0x2028` | dialogue/reward continuation value; later read by reward selection |
| player quest manager `+0x14` | ordered tree that receives a node keyed by quest ID |
| runtime record `+0x04` | one-byte initialized/live marker set to `1` by allocator wrapper `0x0056F0F0` |
| runtime record `+0x05` | pointer copied from the owning player/quest context |
| runtime record `+0x09` | resolved reference-quest pointer |
| runtime record `+0x0D` | quest-content/state object pointer |
| runtime record `+0x11..+0x1D` | four copied 32-bit volatile-progress values |
| runtime record `+0x21..+0x7C` | 92-byte auxiliary runtime block cleared during login hydration |
| runtime record `+0x99` | 32-bit packed quest timer/display value; `0xFFFFFFFF` is the inactive sentinel |
| quest-content object `+0x20` | one-byte persisted lifecycle status |
| quest-content object `+0x22` | mapped 16-bit `_CHARQUEST.AchievementCount` |

Lookup routines `0x0056FF50` and `0x005793B0` search the ordered collection
by quest ID. An existing record returns early with `1`. All three
missing-record branches resolve the reference quest, allocate/populate the
runtime record, insert it into the ordered tree, enqueue persistence, return
`0`, and signal the quest event path through `0x004E7350(1)`.

The runtime object is deliberately packed: its three pointers begin at
unaligned offsets `+0x05`, `+0x09`, and `+0x0D`. The four words at
`+0x11..+0x1D` are not independent database columns. Save folding
`0x0050CDE0` copies the first pair into the low/high 32-bit halves of
`QuestData1 +0x48` and the second pair into `QuestData2 +0x50`. Timer field
`+0x99` lies beyond the 92-byte block cleared explicitly by the login
hydrator, so `+0x7C` must not be mistaken for the runtime object's total
size. The exact allocator stride remains opaque behind the specialized pool,
but the binary-confirmed accessed extent reaches through `+0x9C`.

The persisted `CInstanceQuest` has generic mapped-object bookkeeping before
its SQL fields (`+0x08/+0x0C` include dirty-state bookkeeping). The mapped
payload begins at `CharID +0x18` and extends through the last byte of
`QuestData2` at `+0x57`, proving a minimum native extent of `0x58` bytes. The
16-byte gaps assigned to `StartTime` and `EndTime` are native timestamp
structures, while SQL persists them as `smalldatetime` in the companion
schema.

Mission-progress state used by the bit-`0x10` `0x30D5` section has this
binary-confirmed partial layout:

| Object/offset | Width | Confirmed role |
| --- | ---: | --- |
| mission tree node `+0x0C` | `1` | wire `entryKey` |
| progress object `+0x09` | string object | authored length-prefixed wire text source |
| progress object `+0x21` | `1` | dirty marker, cleared after serialization |
| progress object `+0x25` | `1` | `valueCount` |
| progress object `+0x26` | `4 * valueCount` | unaligned array of `u32` values |
| progress object `+0x32` | `1` | mission-evaluation transition code `0..2` |

Quest-mark records are separate allocations whose address is exposed as the
32-bit runtime mark handle:

| Mark offset | Width | Confirmed role |
| ---: | ---: | --- |
| `+0x05` | `1` | flags serialized by `0x30D6` |
| `+0x08`, `+0x0C` | `4` each | retained authored fields not emitted by the recovered `0x30D6` writer |
| `+0x10` | `4` storage, low `16` bits on wire | authored value |
| `+0x14`, `+0x18`, `+0x1C` | `4` each | floating coordinates rounded to wire `s32` values |
| `+0x20` | `4` | optional value emitted when flag bit `0x02` is set |
| `+0x24` | `4` | owning quest/object identity used by bulk removal |
| `+0x28` | `1` | mark kind byte |

The transitions differ as follows:

| Function | `Status +0x20` | `AchievementCount +0x22` | retained GameServer operation |
| --- | ---: | ---: | ---: |
| `0x00570CB0` | `4` | `0` | `0` |
| `0x00570F10` | `5` | `0` | `1` |
| `0x00571170` | `4` | `1` | `2` |

These functions are record-presence gates, not unconditional database
operations. Each begins with `0x0056FF50(questID)`. If the runtime record
already exists, it returns `1` immediately, creates no object and submits no
SQL. The caller then performs the intended continuation synchronously:

| Retained operation | Confirmed existing-record continuation |
| ---: | --- |
| `0` | authored handlers such as `0x008A77A0` call quest virtual `+0xF4`, the recovered start/setup implementation |
| `1` | `0x008A77A0` writes state `5` through player `+0x19C` and saves through `+0x1A0` |
| `2` | `LuaSaveQuestNow` increments `AchievementCount` through player `+0x1B0`, saves through `+0x1A0`, then invokes quest virtual `+0xF8` |

Only when the lookup misses does a transition build and insert the runtime
record, submit `_quest_manage`, and return `0`; its retained operation chooses
the operation-specific continuation after asynchronous database success. This
two-branch control flow explains why all three asynchronous cases execute SQL
job `0`: the database request is strictly the missing-row creation branch.

The successful async event-`5` handler now fixes their post-database effects.
For `CBasicQuest`, event dispatch `0x0091F840` selects virtual slot `+0x5C`,
which resolves to `0x00920F60`:

| Operation | Binary-confirmed successful-completion effect | Safe lifecycle role |
| ---: | --- | --- |
| `0` | calls quest virtual `+0xF4` (the recovered start/setup implementation), then player virtual `+0x150` with the authored quest data | start/accept |
| `1` | requires the player quest lookup at `+0x184`, writes state `5` through player `+0x19C`, then saves through `+0x1A0` | close/end without incrementing `AchievementCount` |
| `2` | requires the same lookup, writes state `4` through player `+0x19C`, then saves through `+0x1A0`; the missing-record constructor already set `AchievementCount=1` | record first achievement/completion and leave the quest inactive |

The effects, synchronous counterparts and dispatch addresses are
binary-confirmed. The short lifecycle labels summarize those effects and the
pre-query `Status/AchievementCount` values; they are not stored-procedure
parameter names embedded in the executable. None of these operations is the
client abandon path.

### Binary-safe status model

Status getter `0x00570C90` and active-count function `0x005714B0` provide a
stronger classifier than dialogue labels alone:

| Status | Binary-confirmed treatment | Safe interpretation |
| ---: | --- | --- |
| `0` | accepted from persistence only long enough for login hydration to rewrite it to `4` and clear both quest-data words | legacy/uninitialized persisted row |
| `1` | produced by common quest start, registered during login, counted as active, and accepted by abandon | ordinary active/start state |
| `2`, `3` | not excluded by the active counter, but not accepted by the common abandon handler; the complete `+0x19C` call-site scan found no literal writer for either value in the authored quest-code range | retained/legacy transient states whose producer and exact semantics are unresolved in this executable |
| `4` | returned when no record exists, produced after achievement recording, registered during login, and excluded from active count | inactive/default state; completion history is carried by `AchievementCount` |
| `5` | produced by retained continuation `1` and excluded from active count | closed/ended without a new achievement increment |
| `6` | produced by common abort/reset cleanup and excluded from active count | aborted/reset terminal state |
| `7` | registered during login, counted as active, and accepted by abandon; written when kill-monster, kill-player, or gather-from-monster mission progress first advances from state `1` | progressed active quest |
| `8` | registered during login, counted as active, and accepted by abandon; written by the common mission evaluator and concrete quest handlers after requirements succeed | reward-ready/turn-in phase |

The ordinary active-count path considers only reference quest IDs below
`10001`. It counts every status except `4`, `5`, and `6`; therefore statuses
`2` and `3` must not be treated as inactive merely because the generic abandon
request rejects them. Conversely, status `4` alone cannot prove prior
completion because a nonexistent quest returns the same value. Callers must
pair it with `AchievementCount` or the actual runtime-record lookup.

States `2` and `3` must not be assigned invented modern names.
`FindQuestStatusVirtualCalls.java` exhaustively inspected quest-state slot
loads in functions `0x00860000..0x0092FFFF` and recovered literal writers for
`1`, `4`, `5`, `6`, `7`, and `8`, but none for `2` or `3`.

The recovered state-`7` writers are `0x0091BBE0`
(`CMissionKillMonster`), `0x0091BFF0` (`CMissionKillPlayer`), and
`0x0091C4D0` (`CMissionGatherItemFromMonster`), plus concrete quest handler
`0x008A5000`. The three mission handlers accept state `1` or `7`, update one
of the descriptor's progress counters, and change state `1` to `7` before
saving. Common quest virtual `+0x114` (`0x009259C0`) evaluates the authored
mission set: an invalidated phase can be reset to state `1`; a satisfied phase
is changed to state `8` and saved. `CBasicQuest::PayQuest` subsequently runs
the achievement increment, state-`4` persistence, finalize, and dialogue
refresh sequence only when the runtime state byte is `8`. This establishes
`7` as progressed-active and `8` as reward-ready without claiming that every
concrete quest reaches those phases identically.

The dialogue builder at `0x004EC930` also calls player virtual slot `+0x1B8`
and replaces the standard accept label with `SN_TALK_COMMON_MAXQUEST` when
the returned count is at least `0x21`. The binary therefore enforces a
32-entry boundary for the count checked by this dialogue path.

## Binary-confirmed client-facing opcodes

| Opcode | Constructor / occurrence | Confirmed payload and behavior |
| --- | --- | --- |
| `0x3514` | `0x004EC8C0`, player virtual `+0x338` | Reward/dialogue prompt: `u32 questOrDialogueID`, `u8 mode`, then one encoded text token. Quest event paths call it with the quest ID, mode `4`, an authored token, and a continuation value; the function stores the ID and continuation at player `+0x2024/+0x2028`. |
| `0x30D4` | outbound `0x004EC930`; inbound `0x00516670` | Duplex quest dialogue. Server output begins with `u8 dialogueType` and an encoded string. Types `4` and `5` then serialize `u8 selectionCount` and that many encoded strings; type `5` additionally serializes one encoded string and a trailing `u32`. Selection count is clamped to `0..64`. The client reply on the same opcode carries one `u8 selection`. |
| `0x30D5` | `0x005703B0`, `0x00570650`, `0x005707E0`, `0x005709C0` | Quest state/progress update. Subtype `1` creates the client quest entry from a full/current snapshot, subtype `2` updates an existing entry, subtype `3` retires content states `4/5/6`, and subtype `4` is the no-broadcast state-`6` path. Every form carries the quest identity; subtypes `1` and `2` carry two quest-definition bytes followed by a flag byte and its conditional sections serialized by `0x005785E0`. |
| `0x30D6` | `0x005797E0` | Adds a quest mark. Payload is `u32 runtimeMarkID`, `u8 flags`, `u8 kind`, `u16 authoredValue`, three rounded `s32` coordinates, then an optional `u32` when flag bit `0x02` is set. |
| `0x30D7` | `0x005798B0`, `0x00579950` | Removes quest marks. `0x005798B0` resolves and removes one record by `u32 runtimeMarkID`; `0x00579950` scans for every mark whose owner field `+0x24` equals a supplied quest ID. Each removed mark emits a separate packet containing its `u32 runtimeMarkID` before unregistering, unlinking, and freeing that mark record. |
| `0x30DA` | `0x004ED1F0` | Serializes `u32` followed by `u8`. The protocol corpus calls this quest gather, but only the layout and quest call site are binary-confirmed. |
| `0x30DC` | `0x004ED240` | Serializes `u32` followed by `u8`. The illegal-base diagnostic names the related virtual operation `SendQuestCatchMobResult`. |
| `0x30EC` | `0x004ED4A0`, player virtual `+0x1CC` | Single-token UI/talk notification. Serializes one encoded string. Binary callers pass `SN_TALK_*` tokens (including `SN_TALK_COMMON_END`) and `UIIT_MSG_QUEST_*` tokens from quest condition and dialogue branches. |
| `0x3CA2` | `0x004F12B0`, player virtual `+0x310` | Adjacent global event/broadcast update, not a quest-lifecycle packet. Serializes one encoded string and one `u32`, updates player channel `0x1A`, and writes the numeric value through player stat/display slot `+0x318`. Global broadcaster `0x0064AD00` invokes it for every selected player. |
| `0x70D8` | dispatcher adapter `0x00516780` | Quest-specific interaction request. Reads one `u32 questID` and calls quest-manager path `0x00571A80`. |
| `0xB0D8` | `0x00571A80` | Quest-specific interaction response. Success is `u8(1), u32 questID`; failures are `u8(2), u8(1)` for an unknown quest and `u8(2), u8(2)` unless content state is exactly `2`. Success dispatches quest event `7`. |
| `0x70D9` | dispatcher adapter `0x005167D0` | Abandon request containing one `u32 questID`; calls quest-manager path `0x00571B80`. |
| `0xB0D9` | `0x00571B80` | Abandon response. Success is `u8(1), u32 questID`; recovered failures are `u8(2), u8(2)` for a disallowed quest-content state and `u8(2), u8(5)` while player field `+0x1CA0` is non-zero. |
| `0x70DB` | dispatcher adapter `0x00516820` | Quest local-action cancel/reset request. Reads one `u32 questID` and calls quest-manager path `0x00571970`. |
| `0xB0DB` | `0x00571970` | Quest local-action cancel/reset response. Success is `u8(1), u32 questID`; failures are `u8(2), u8(1)` for an unknown quest and `u8(2), u8(2)` unless content state is `1`, `7`, or `8`. Success dispatches quest event `0x17`. |
| `0x7515` | dispatcher handler `0x0051B370` | Reward-selection request: `u32 rewardID`, `u8 entryCount`, followed by `entryCount` `u32 entryID` values. |
| `0xB515` | `0x0051B370` | Reward-selection response. Success is the single byte `1`. Failure is byte `2` followed by a one-byte reason (`1` unknown reward ID, `2` reward-kind mismatch in the recovered branches). |

`0x00427D90` places incoming `0x30D4`, `0x70D9` and `0x7515` in the same
client-message filter class. Separately, `0x0050D0C0` initializes a contiguous
2,048-slot dispatcher and `0x0050EEE0` proves its indexing rule. The dispatcher
compares `opcode & 0x0FFF` with `opcode & 0x07FF`; values with bit `0x0800` set
take a different path. Otherwise the slot address is
`0x00CD3D08 + (opcode & 0x07FF) * 8`. This deliberately permits different
high opcode ranges to share a handler while the message filter controls which
values are admitted. It proves these request mappings:

| Request | Dispatch slot | Handler | Confirmed request payload |
| --- | --- | --- | --- |
| `0x30D4` | `0x00CD43A8` | `0x00516670` | one `u8 selection` |
| `0x70D8` | `0x00CD43C8` | `0x00516780` | one `u32 questID` |
| `0x70D9` | `0x00CD43D0` | `0x005167D0` | one `u32 questID` |
| `0x70DB` | `0x00CD43E0` | `0x00516820` | one `u32 questID` |
| `0x7515` | `0x00CD65B0` | `0x0051B370` | `u32 rewardID`, `u8 entryCount`, then `entryCount` values of `u32 entryID` |

The conventional quest-talk, abandon and reward-select names are therefore
binary-confirmed for this build.

The opcode inventory is the union of two exhaustive checks. The scalar scan
recorded every immediate occurrence of the known quest packet family in
`gameserver-quest-opcodes.log`; the full 2,048-slot dispatcher initialization
was then inspected for handlers whose request opcode is represented only by
its low slot index. That second check is what recovers `0x70D8` and `0x70DB`.
Every dispatcher-backed quest request now has a row in both the main table and
the request-slot table. The two `0x30D7` occurrences are intentionally not
duplicates: `0x005798B0` removes one mark by its wire/runtime handle, while
`0x00579950` removes all marks owned by a quest ID and emits one removal
packet per matching mark.

### Client-binary corroboration

The client side was checked against the stock vSRO 1.188 executable
`sro_client.stock.exe` (SHA-256
`58B9DA3A218894F28E4C82A992B90997869CF331AB450929C8FCB7453327D114`).
The deployed sibling `VSRO_Client/sro_client.exe` has SHA-256
`3DF2282DAB84EE5439944B3E6CA8E8CFC69CB05963A45F0F7F4B8ADCA579C206`
and differs from that stock authority only in the separately documented
40-byte ATL/DEP patch. The quest decoder code below is outside that patch and
is therefore code-equivalent in the deployed client.

`CNetProcessIn` registration at `0x0086FBB0` binds the main-channel quest
packets to these decoder entry points:

| Opcode | Client decoder | Binary-confirmed consumption / effect |
| --- | --- | --- |
| `0x30D6` | `0x008811E0` | Reads `u32 runtimeMarkID`, `u8 flags`, `u8 kind`, `u16 authoredValue`, and 12 coordinate bytes. If `flags & 2`, reads the trailing `u32`; then calls the mark manager with add mode `0`. This consumes the complete GameServer grammar. |
| `0x30D7` | `0x00881280` | Reads one `u32 runtimeMarkID` and calls the same mark manager with remove mode `1`. This exactly matches both GameServer removal producers. |
| `0x30D5` | `0x008812B0` | Reads `u8 subtype` and `u32 questID`. Subtype `1` constructs and inserts a client `SQuestInfo`; subtype `2` looks up the existing `SQuestInfo`; both then decode the same flag-controlled tail at `0x008B94D0`. Subtypes `3`/`4` have no tail and remove/retire the client entry. |
| `0xB0D8` | `0x00881450` | Reads `u8 result`; success `1` consumes a `u32 questID` and plays `snd_quest`, while failure `2` consumes a `u16 reason`. |
| `0xB0D9` | `0x008814B0` | Reads `u8 result`; success `1` consumes `u32 questID`. Failure `2` consumes `u8 reason`; reason `4` displays `UIIT_MSG_SR_ABORT_QUEST_ERROR_NOT_ALLOWED`, and reason `5` displays `UIIT_STT_QUEST_GIVEUP_1`. The client accepts more failure values than the two GameServer branches recovered above. |
| `0x30EC` | `0x00884A30` | Decodes the packet's message/token string, resolves it through the UI text system, displays it, and plays `snd_quest_end` when the resolved token is `SN_TALK_COMMON_END`. |
| `0x30DC` | `0x008A7EE0` | Reads `u32` then `u8`; value `1` triggers the catch-result UI path for the current compatible object. This consumes the complete GameServer grammar. |
| `0xB515` | `0x00881570` | Reads `u8 result`; on failure `2`, reads `u8 reason`. Reason `2` displays `UIIT_STT_QUESTREWARD_ERROR`; success has no tail. |
| `0x3514` | `0x00884CA0` | Reads the leading `u32` and `u8`, then decodes the message/token string and forwards all three values into the prompt/dialogue UI path. |
| `0x3CA2` | `0x008ABCC0` | Decodes one string and one `u32`, then forwards them to the adjacent global-event UI handler. |

The second network processor registers `0x30DA` at `0x0088D570` and `0xB0DB`
at `0x0088D5C0`:

- `0x30DA` reads exactly `u32, u8` and forwards both to the quest-item/gather
  manager.
- `0xB0DB` reads a result byte. Success `1` consumes `u32 questID` and updates
  the active quest-item operation; failure consumes a `u16` reason and
  displays `UIIT_STT_ERR_COMMON_NOT_ACCEPT`.

There is a binary-confirmed width asymmetry in the two secondary response
failures. GameServer `0x00571A80` (`B0D8`) and `0x00571970` (`B0DB`) each call
the one-byte message writer twice, producing `u8(2), u8(reason)`. Client
`0x00881450` and `0x0088D5C0` read the result as `u8` but request two bytes for
the failure reason. This is not evidence that GameServer writes `u16`: its
assembly loads writer length `1` for both fields. It is an original
producer/consumer mismatch (or a client over-read tolerated by its stream
layer), and a compatible implementation should preserve the server's
two-byte total failure payload while treating the reason's low byte as the
defined value.

This registration audit accounts for every GameServer-produced quest-family
packet in the table above. It also confirms the architecture boundary:
`sro_Client.exe` contains packet decoding, runtime/UI mutation, localization,
and sound calls, but no quest SQL or direct database persistence. All SQL in
this document belongs to `SR_GameServer.exe` or the companion database.

The client `SQuestInfo` decoder makes the complete detailed `0x30D5` wire
grammar cross-binary-confirmed:

```text
u8  subtype                 // 1=create, 2=delta, 3/4=retire; 3/4 end here
u32 questID
u8  packedQuestByte         // subtype 1 or 2
u8  questDefinitionByte     // subtype 1 or 2
u8  flags                   // subtype 1 or 2
if flags & 0x04: u32 timerValue
if flags & 0x08: u8 contentStatus
if flags & 0x10:
    u8 dirtyMissionCount
    repeat dirtyMissionCount:
        u8 entryKey
        u8 transitionCode
        u16 byteLength
        u8 textBytes[byteLength]
        u8 valueCount
        u32 values[valueCount]
if flags & 0x40:
    u8 pendingValueCount
    u32 pendingValues[pendingValueCount]
```

GameServer `0x005709C0` constructs `packedQuestByte` as the low byte of
`(quest-definition virtual +0x28 result << 4) | AchievementCount`; the
achievement value comes from quest content `+0x22`. The following byte is the
result of quest-definition virtual `+0x2C`. Their higher-level meanings are
not named because neither binary supplies a diagnostic or RTTI name for
those two virtual results. Client `0x008B94D0` stores them in `SQuestInfo` at
`+0x08/+0x09`, stores `flags` at `+0x0A`, the optional timer at `+0x0C`, the
optional content status at `+0x10`, the mission count at `+0x11`, and the
bit-`0x40` values in the vector whose begin/end/capacity pointers are
`+0x24/+0x28/+0x2C`. Its constructor at `0x008B9FC0` initializes `questID`
at `+0x04`, timer to `0xFFFFFFFF`, and the optional bytes to zero. Thus the
client consumes every conditional byte emitted by GameServer `0x005785E0`;
there is no unparsed subtype-2 tail.

The authored mission text is a localization key, not literal UI prose.
Client mission parser `0x008B6ED0` reads `transitionCode`, the length-prefixed
byte string and `valueCount`, converts the byte string to UTF-16 through
`0x004FC700`, and stores the values in the mission object at `+0x44`.
For a non-zero transition it resolves that UTF-16 key through
`0x008C9BB0 -> 0x008C9B20`. When exactly one value is present, the localized
string is passed to `swprintf_s` with that `u32`; the resulting text replaces
the mission's visible UI string. Transition zero updates stored state without
performing that visible formatting path. This proves that GameServer progress
objects at `+0x09` carry tokens such as `SN_CON_*`, and that the following
value is the template argument.

AegisCore now uses one shared mission projection for login hydration and live
`0x30D5` deltas. Every classified or multi-collection objective emits its own
entry key, localization token, transition and counter in both paths. This
fixes the former live-update drift that collapsed a multi-mission quest into
one empty mission record even though reconnect hydration contained all rows.

The client outbound builders independently confirm all request grammars:

| Opcode | Client builder | Exact payload |
| --- | --- | --- |
| `0x30D4` | dialogue/UI action in `0x007020C0` | one `u8 selection`, copied from the selected dialogue record at `+0x0C` |
| `0x70D8` | `0x0081EC20` | one `u32 questID` |
| `0x70D9` | `0x0081ECE0` | one `u32 questID` |
| `0x70DB` | operation-`1` branch of `0x0081EE60` | one `u32 questID`; the same function's operations `0` and `2` build unrelated teleport-cancel opcode `0x705B` |
| `0x7515` | `0x008252D0` | `u32 rewardID`, `u8 entryCount`, then `entryCount` `u32 entryID` values; a null selection container emits count zero |

Every builder first checks opcode support, constructs the message, and submits
it through the common client network sender `0x008418D0`. The recovered
`0x7515` builder derives its one-byte count by truncating the selected-entry
vector length; no client-side `<=255` guard is visible in this function.

The selected-entry identity is recoverable from the client UI rather than
inferred from the packet shape. `CNIFQuestReward` keeps the selected values
in the `std::vector<u32>` at object `+0x368`; `0x00605D30` returns that
vector after comparing its length with the required byte at `+0x37C`. When
a reward item is selected, `0x00607050` calls `0x008BA470` on the selected
`CNIFQuestReward_ItemWnd` subobject at `+0x398`; that getter returns the
subobject's `u32 +0x34`, and the value is appended to the vector. During
reward-window construction, `0x00606A50` initializes the same `+0x34` field
through setter `0x008BA4C0` from the reward-item record's resolved item ID at
record `+0x20`. Consequently each `0x7515.entryID` is a resolved reference
item ID (`RefItemID` / AegisCore `ITEMDATA.ItemID`), not the ordinal or the
local surrogate `REF_QUEST_REWARD_ITEM.ID`. The latter does not exist in the
Media row and was added only as AegisCore's relational primary key.

The GameServer mission-selection helper `0x009208D0` provides additional
objective evidence. Its `DAT_00D6…` globals and `CGObjPC` virtual calls place it
in `SR_GameServer.exe`; it walks the authored mission array and, when the caller's
mode mask enables it, evaluates type `3` delivery missions (mask `0x01`), type
`4` NPC-gather missions (mask `0x02`), and type `8` catch-monster missions (mask
`0x08`). For each authored item it calls the player inventory predicate at
virtual slot `+0x22C`; positive authored counts are required before returning
the dialogue result (`1` pending/available, `2` completed/confirmable). This
proves server-side availability gating for these variants, but it is not a
turn-in transaction: no SQL, inventory decrement, or quest-state commit occurs
in this helper. The corresponding client dialogue/packet transition and the
server-side completion semantics remain separate evidence requirements.

### Selectable-reward prompt lifecycle

The reward-selection flow is a two-packet lifecycle, not a server-side reward
row dump. For an active quest with a non-zero authored selection count, the
GameServer emits `0x3514` with `u32 questID`, mode `4`, and the completion
token. Client handler `0x00884CA0` opens its local `CQuestRewardData` window;
the reward rows and resolved item IDs come from the client's Media data. The
client then sends `0x7515` with the quest ID (`rewardID < 10000`) and selected
`ITEMDATA.ItemID` values. AegisCore validates count, authored-row membership,
duplicates, foreign IDs, and capacity before committing item inserts and the
`RewardReady (8) -> Closed (5)` transition in one transaction. Inventory and
quest packets are published only after commit; the legacy asynchronous ODBC
continuation chain is not recreated.

For `rewardID >= 10000`, the binary keys the lookup by event ID. The recovered
catalog is the separate `_RefEventReward` / `_RefEventRewardItems` family; it
is not `REF_QUEST_REWARD`. The surrounding server-event subsystem also loads
`_RefServerEventReward`, `_RefServerEventReward_ExpUPForPlayers`, and
`_RefServerEventReward_SpawnMonster`, with reward classes for experience,
skills, and monster spawning. The `0x7515` continuation applies the selected
entry to the currently selected quest object, but its durable economic effects
are an event-reward transaction; it does not imply a `CHAR_QUEST` close.
AegisCore keeps this branch separate from quest completion: `REF_EVENT_REWARD`
and `REF_EVENT_REWARD_ITEM` mirror the recovered catalog, resolve selected
entries through `ITEMDATA.ItemID`, validate the binary count/identity rules,
and commit gold, experience, and item insertion atomically. The handler
returns the same `B515` success/failure framing without treating an event ID
as a quest ID. No asynchronous ODBC continuation is reproduced.

The reference-data loader gives two additional structural anchors for a future
implementation: `_RefEventReward` records have a 0x30-byte object stride and
vtable `0x00B0CD90`; `_RefEventRewardItems` records also have a 0x30-byte
stride and vtable `0x00B0CDB8`. Both are loaded through the common reference
mapping framework. The source schema confirms the corresponding columns as
`EventID`, `Gold`, `Exp`, `SPExp`, `SP`, `Hwan`, `Inventory`, `ItemRewardType`,
`SelectionCnt`, and the three params, with item rows keyed by `EventID`,
`ItemCodeName`, `PayCount`, and `AchieveRatio`.

The adjacent `_RefServerEventReward` path provides a useful contrast, not a
schema substitute: its recovered continuation reads `RewardID` at `+0x08`,
`RefSkillID` at `+0x10`, reward class at `+0x15`, flags at `+0x16`, apply time
at `+0x18`, and experience/skill ratios at `+0x1C/+0x20`. Those fields drive
server-event classes (experience, skill, and spawn) and must not be merged into
the quest reward tables merely because both paths share the `0x7515` threshold.
The companion ShardManager catalog exposes the server-event association calls
`_InsertServerEventReward(ServerEventID, RewardID, AQType)`,
`_UpdateServerEventReward(ServerEventID, RewardID, AQType)`, and
`_DeleteServerEventReward(ServerEventID, RewardID)`. These are administrative
reference-association procedures, not the quest completion transaction.

The `0x70D8` and `0x70DB` request adapters were initially missed because
Ghidra did not create function symbols at `0x00516780` or `0x00516820`.
Initialization `0x0050D0C0` writes those raw code addresses to dispatcher
slots `0x00CD43C8` and `0x00CD43E0` at `0x0050E35A` and `0x0050E3B8`.
Both adapters mirror the adjacent abandon adapter: they select the same two
packet-reading helpers according to packet/session field `+0x1048`, decode
one 32-bit value, and pass it to the player quest manager at `+0x1D98`.
This makes both request/response pairs and their `u32 questID` requests
binary-confirmed even though neither full request constant appears as an
immediate scalar.

Event dispatcher `0x0091F840` maps event `7` to quest virtual `+0x70`, event
`8` to `+0x74`, and event `0x17` to `+0x94`. A scan of all 186 RTTI-backed
quest vtables sharing that dispatcher finds four event-`7` overrides, 25
event-`8` overrides across 19 distinct functions, and nine event-`0x17`
overrides:

| Event/slot | Concrete override set | Binary-confirmed behavior |
| --- | --- | --- |
| `7` / `+0x70` | `CQNO_TQ_BAEKRYOUNG_13`, `_14`, `_16` | functions `0x008E6C40`, `0x008E8C60`, and `0x008EC200` send their authored `SN_TALK_*_09` tokens through player `+0x1CC` |
| `7` / `+0x70` | `CQNO_RM_MEETROC_ECT_3` | `0x0090EE60` reads the inventory count of `ITEM_QNO_RM_MEETROC_ECT_03` and pushes progress recomputation through player `+0x1BC` for two related quest definitions |
| `8` / `+0x74` | 161 vtables inherit `0x00921700`; 25 concrete vtables use 19 override functions listed in the evidence artifact | every recovered implementation invokes abort/reset slot `+0xFC` with mode `1`; the base path conditionally refreshes through `+0x118` and advances a bounded authored repeat counter, while overrides additionally clear quest-local progress, remove authored inventory items, or reset a related quest before/after the same abort transition |
| `0x17` / `+0x94` | nine concrete quests listed in the evidence artifact | shared `0x008C8770` requires the runtime progress block, clears its byte `+0x04` (runtime record `+0x15`), and saves through player `+0x1A0` |

The event-`7` set is now wired explicitly. Media maps the three fixed-talk
overrides to quest IDs `307`, `308`, and `310`; Agent validates the locked
transient quest row, emits the exact `SN_TALK_QNO_TQ_BAEKRYEONG_*_09`
notification, then sends `0xB0D8`. Media maps `CQNO_RM_MEETROC_ECT_3` to
quest ID `376`. Its `0x0090EE60` override counts inventory ref `24175`
(`ITEM_QNO_RM_MEETROC_ECT_03`) through player virtual `+0x224`, constructs
fresh progress records for quest IDs `374/375`, and pushes both through player
`+0x1BC`; it performs no SQL mutation. Agent mirrors that as inventory-derived
`0x30D5` deltas for whichever related quests are live, before the `0xB0D8`
success response.

The event-`8` audit is also implemented in Agent's atomic abandon
transaction. In `SR_GameServer.exe` (image base `0x00401000`), base
handler `0x00921700` occupies quest-vtable slot `+0x74`; the 19 concrete
handlers first clear the runtime record (including progress/count fields) and
then invoke quest virtual `+0xFC` with mode `1`. Agent consequently clears
`ACHIEVEMENT_COUNT`, `QUEST_DATA_1/2`, and the timer when it writes status
`6`. It retains reference-table item cleanup and supplements it with the
authored item names visible immediately before calls to `0x00915530`.
The recursive overrides are `308 -> 307` at `0x008E8B80`,
`310 -> 309` at `0x008EC120`, `92 -> 91` at `0x0090C1C0`, and
`96 -> 92` at `0x0090CE00`; these related resets commit in the same SQL
transaction and are mirrored into the live player snapshot. The no-item
`CQNO_EU_IVY_3` override at `0x008BEC10` only performs an internal
bookkeeping call before delegating to the base handler, while shared
`0x008FA980` performs record-clear/abort without authored item deletion.
For recursive resets Agent publishes `0x30D5` subtype `4` after the
requesting quest's `0xB0D9` acknowledgement. Client dispatcher
`sro_client.exe:0x008812B0` reads `u8 subtype + u32 questID`; cases
`3/4` both call `0x00778D10(questID)` to remove the active entry, then
refresh it through `0x008B9910`. This keeps the related quest absent from
the client as well as SQL and the live server snapshot.

All other event-`7` and event-`0x17` slots retain base no-op `0x0092F270`;
all remaining event-`8` slots inherit the non-no-op abort handler
`0x00921700`. This
proves that `0x70DB/0xB0DB` cancels or resets quest-local action state. The
secondary corpus's fuller name “gather-cancel” is still not universal or
embedded in the executable. Likewise, `0x70D8/0xB0D8` is a quest-specific
interaction family, not a universally safe “gather” operation: its concrete
overrides include both talk notifications and inventory-driven progress.

The message-filter path is also bounded. `CGMsgFilter` construction
`0x00427C60 -> 0x00427D90` creates 17 category slots. Each configured slot
contains an opcode vector and selects either `0x00427940` (deny listed values,
allow everything else) or `0x004279A0` (allow only listed values). Lookup
`0x00427B20` returns allow immediately when the selected category slot is
unconfigured; `0x004296A0` applies the configured categories selected by the
player/session virtual state getters and rejects through player virtual
`+0x598` only when a category predicate fails. An instruction-complete scan of
the constructor finds neither `0x70D8` nor `0x70DB` in any exception vector.
They therefore use the active category's default policy rather than a hidden
quest-specific admission entry. Their dispatcher transport and filter
provenance are both recovered.

The assembly of `0x004EC8C0` fixes the `0x3514` field order independently of
decompiler argument recovery: it writes four bytes from the first argument,
one byte from the second, then encodes the third argument as a string. The
fourth argument is not serialized; it becomes player continuation `+0x2028`.
Quest handler `0x00920110` calls this player slot with `(questID, 4,
authoredToken, continuation)`, establishing its quest reward/dialogue role.

`0x30EC` is the simpler fire-and-forget text channel. Unlike `0x30D4` and
`0x3514`, it stores no dialogue continuation and carries no selection list.
Its extensive concrete quest callers make the UI/talk-notification role
binary-confirmed even though the constructor itself contains no symbolic
packet name. `0x3CA2` was initially adjacent to quest evidence but its direct
global-broadcast path and channel-`0x1A` update show that it should not be
treated as part of the quest lifecycle.

For detailed `0x30D5` updates, `0x005785E0` always writes the flag byte first
and treats bit `0x01` as illegal. Bit `0x04` adds the runtime record's `+0x99`
value; bit `0x08` adds the quest-content state byte at `+0x20`; bit `0x10`
adds a count followed by only dirty mission-progress records and clears each
record's dirty marker after serialization; bit `0x40` adds a count and a list
of `u32` values, then consumes that pending list. Bits without a recovered
branch in this function add no bytes here. This confirms that subtype `2` is
a flag-controlled delta rather than one fixed-width structure.

The bit-`0x10` mission section is now fully bounded at its outer record level:

```text
u8 dirtyMissionCount
repeat dirtyMissionCount times:
    u8 entryKey
    u8 transitionCode
    u16 byteLength
    u8  textBytes[byteLength]
    u8  valueCount
    u32 values[valueCount]
```

`entryKey` comes from the mission tree node at `+0x0C`; the transition code,
dirty marker, value count and values come from the mission-progress object at
`+0x32`, `+0x21`, `+0x25` and `+0x26`. The length-prefixed bytes are held by
the string object beginning at progress `+0x09`; `0x00425FD0` clamps the
length to remaining packet capacity and writes no terminator. After a record
is emitted, its `+0x21` dirty marker is cleared.

The `+0x32` byte is binary-confirmed as a mission-evaluation transition code,
not the authored mission type. Every concrete mission's vtable slot `+0x0C`
callback calls the player virtual at `+0x1BC` (`CGObjPC` vtable entry
`0x00AF5BB8`, function `0x004ED050`). Its eighth data argument is obtained
from that mission class's evaluator at vtable slot `+0x08`; the player wrapper
forwards it as argument nine to `0x005703B0`, which stores it at progress
`+0x32`. Across all eleven concrete evaluators the returned codes are:

| Code | Binary-confirmed evaluator outcome |
| ---: | --- |
| `0` | no new completion transition; normally the completion condition is satisfied and its bit was already set |
| `1` | condition is not currently satisfied, the relevant runtime record is missing, or the evaluator takes its non-complete/error path |
| `2` | condition becomes satisfied while its completion bit is clear; the evaluator sets the bit and invokes player virtual `+0x1A0` |

All eleven normal vtable-`+0x0C` callbacks pass the authored mission byte at
`+0x08`, plus one, as `entryKey`; pass the NUL-terminated authored string
pointer at `+0x0D` as `textBytes`; and set `valueCount` to one. Their single
value and callback gate are class-specific:

| Type | Class | Callback | Callback gate | Emitted `u32` value |
| ---: | --- | ---: | --- | --- |
| `1` | `CMissionGatherItemFromMonster` | `0x0091C890` | authored `+0x249 != 0` | current inventory count for authored item `+0x241`, queried through player virtual `+0x224` |
| `2` | `CMissionKillMonster` | `0x0091BE70` | authored `+0x14E != 0` | runtime `u16` counter at quest state `+0x06`, `+0x0A`, or `+0x0C`, selected by authored mode byte `+0x14D` |
| `3` | `CMissionDeliverItem` | `0x0091CFF0` | authored `+0x10C != 0` | `0` |
| `4` | `CMissionGatherItemFromNpc` | `0x0091D220` | authored `+0x6D != 0` | `0` |
| `6` | `CMissionDialog` | `0x0091DAF0` | authored `+0x6B != 0` | `0` |
| `8` | `CMissionCatchMonster` | `0x0091DD10` | authored `+0x71 != 0` | `0` |
| `9` | `CMissionDifferentQuestClear` | `0x0091D400` | runtime quest record exists | `0` |
| `10` | `CMissionChangeItem` | `0x0091D5E0` | authored `+0x21 != 0` plus its event/item predicate | current inventory count for authored item `+0x1D`, queried through player virtual `+0x224` |
| `11` | `CMissionTimeCheck` | `0x0091DEE0` | authored `+0x21 != 0` | `0` |
| `12` | `CMissionKillPlayer` | `0x0091C350` | authored `+0x36 != 0` | runtime `u16` counter at quest state `+0x06`, `+0x0A`, or `+0x0C`, selected by authored mode byte `+0x35` |
| `13` | `CMissionTalismanCollection` | `0x0091E110` | authored `+0x1D != 0` | `0` |

The retained `Quest.sct` does not instantiate types `9`, `10`, or `11`.
Symbolic decoding finds 126 `LuaSetMissionData` declarations and every one
uses `MISSION_TYPE_KILL_MONSTER`; all other 421 retained objective
declarations use their separately named delivery, collection, dialog,
kill-player, extended-kill, or talisman callbacks. These three native classes
therefore document executable capability but do not leave an unsupported
authored quest in the pinned vSRO catalog.

Type `13` is driven by collection-book registration of a talisman item, not
by an NPC turn-in or the ordinary ground-pickup transaction. The stock item
use dispatcher at `0x0049ACA0` selects `0x004A0780` specifically for
`TypeID3 = 12`, `TypeID4 = 5`. That handler rejects an already registered
item through player virtual `+0x348`, calls player wrapper `0x004F2A80`, and
only decrements the inventory stack after that wrapper succeeds.
The Lua binding table in `SR_GameServer.exe` (image base `0x00401000`)
registers `LuaSetTalismanMissionData` at `0x00870D0C` with callback
`0x0086BA20`. The callback reads exactly seven Lua arguments: the first two
select the quest and mission declaration, argument three is narrowed to the
mission-type byte, arguments four through six are retained as authored
references (objective key, target NPC code, and theme code), and argument
seven is retained as a dword. This independently corroborates the normalized
seven-argument script grammar; it does not supply an implicit item-count
field.

The collection manager at `0x0069E790` resolves the item code, sets the
theme/slot bit in memory, and asynchronously executes
`_AddCollectionBook_Item(CharID, ThemeID, SlotIndex)` through `0x0044D0C0`.
After accepting the registration, `0x004A0780` dispatches internal event
`243` with the item reference's theme (`+0x94`) and slot (`+0x58`). This is
the event whose quest-facing handler, `CGObjPC::OnEVENT_TALISMANITEM_PICK` at
`0x00922080`, resolves the registered item
code through the item catalog, reads its collection-book theme from item
reference `+0x94`, resolves that theme through `0x006A0DD0`, and compares the
theme code string with the mission descriptor at `+0x19`. For a matching
type-`13` mission it invokes the class callback at `0x0091E110` and the normal
quest dirty/update path. The class evaluator at `0x0091E050` separately calls
player virtual `+0x340` with the same authored theme code; this is the
collection-book "theme accomplished" predicate. On the first satisfied
evaluation it sets the mission bit in runtime quest state `+0x03` and calls
player virtual `+0x1A0` to persist that state. The authored dword at `+0x1D`
is therefore only a non-zero callback gate, not a required item count.

Character login loads durable rows with
`SELECT * FROM _CharCollectionBook WHERE CharID = ?` at `0x0043F1B0`.
`0x0069E2D0` reconstructs one per-theme slot bitmap and validates every
stored slot against the reference theme's completion count. Consequently,
inventory possession is neither the durable collection state nor a valid
substitute for it.

Ten callbacks pass a known non-zero final argument to `CGObjPC::+0x1BC`
(normally the player pointer, and `-1` in the type-`1` path). Type `10` passes
one of its callback arguments, also forwarded to its evaluator, so its value
is caller-dependent. Because `0x005703B0` sends the immediate `0x30D5` packet
only when that final argument is zero, the ten known-non-zero paths update and
dirty the progress record without sending from inside the writer; the dirty
record is then eligible for a later aggregate/delta send. The type-`10` path
can request the immediate send only when its forwarded argument is zero. The
exact client-facing interpretation of the authored `+0x0D` string remains
unresolved, but its server-side source and encoding are exact.

`0x0090CEF0` is a separate recomputation path for authored type `2`. It uses
the loop index itself as `entryKey` rather than stored ordinal plus one, emits
a per-bit boolean as both the value and an explicit `0/1` transition code,
and passes final argument `1`, likewise suppressing immediate broadcast.

### Authored mission-type catalog

The authored mission record's 32-bit field at `+0x09` is a mission type ID.
Both quest setup implementations (`0x0091E7F0` and `0x00927BA0`) subtract one
and dispatch through a jump table. Each accepted branch supplies one of the
static RTTI-backed mission factories to `0x0092B0C0`. This establishes the
following catalog directly from the binary rather than from registry order:

| Mission type | Concrete RTTI class | Setup/pay behavior visible in the shared switch |
| ---: | --- | --- |
| `1` | `CMissionGatherItemFromMonster` | validates a monster-code list and an item code |
| `2` | `CMissionKillMonster` | validates a monster-code list |
| `3` | `CMissionDeliverItem` | validates delivery-item lists and rejects sellable delivery items |
| `4` | `CMissionGatherItemFromNpc` | validates an item-code list |
| `5` | invalid/reserved | jump-table default/assert path |
| `6` | `CMissionDialog` | no extra catalog validation in the shared setup switch |
| `7` | invalid/reserved | jump-table default/assert path |
| `8` | `CMissionCatchMonster` | validates an item-code list and an optional additional item |
| `9` | `CMissionDifferentQuestClear` | no extra catalog validation in the shared setup switch |
| `10` | `CMissionChangeItem` | validates two item codes |
| `11` | `CMissionTimeCheck` | no extra catalog validation in the shared setup switch |
| `12` | `CMissionKillPlayer` | requires a non-zero authored kill count and installs its event path |
| `13` | `CMissionTalismanCollection` | validates the authored talisman code; present in `0x00927BA0` |

The first setup variant bounds its dispatch at type `12`; the second includes
type `13`. That is a real implementation difference, not evidence that type
`13` is absent from the executable. `CBasicQuest::PayQuest` uses the same set
of accepted numeric cases when it walks authored mission records.

The authored type and wire transition code are therefore independent fields:
type selects the concrete mission implementation, while the progress byte
reports the result of evaluating that implementation.

### Lifecycle coverage and fail-closed boundary

The two supplied binaries establish the following coverage boundary for the
maintained implementation:

| Family | `SR_GameServer.exe` evidence | `sro_client.exe` evidence | Runtime status |
| --- | --- | --- | --- |
| Kill-monster | `CMissionKillMonster` type `2`, evaluator `0x0091BE70`, progress callback and `0x30D5` delta | `0x30D5` parser consumes the authored progress sections | Enabled for recovered single missions and the 13 exact consecutive multi-mission groups sharing the native normal-class counter |
| Standard delivery | `CMissionDeliverItem` stage handler `0x0091CA00`, evaluator `0x0091CEB0`, inventory mutations through player `+0x230/+0x22C`, and callback `0x0091CFF0` | Authenticated `0x30D4` selection drives the stage-`2` continuation; ordinary `0x30D5` carries completion | Enabled for recovered variant-zero, mission-zero, single-objective shapes |
| Extended delivery | The sole `LuaSetDeliverMissionData_EX` row is `QNO_CH_SMITH_1`; its mission-item grant, persisted progress, NPC completion, inventory consumption, and reward path are recovered | Ordinary quest add/progress and authenticated NPC talk packets carry the complete retained flow | The sole retained extended-delivery quest is enabled; no generalized unproven shape is admitted |
| Collection / talisman | Type `1` producer/evaluator is recovered through `0x0091C4D0`, `0x0091C660`, `CGObjPC +0x254`, and `0x0091C890`; type `13` registration dispatch is `0x00922080`, theme-accomplished evaluator is `0x0091E050`, and progress callback is `0x0091E110` | Normal item visibility/pickup covers type `1`; `0x008B94D0`/`0x008B6ED0` consume repeated mission records; stock `0x704C` use, durable collection-book state, and `0x30D5` cover type `13` | All standard one-to-four-mission monster-collection groups, the exact extended companion pair, and all four normalized talisman objectives enabled |
| Dialog / special objectives | Type `6` is recovered through `0x0091D770`, evaluator `0x0091DA60`, and progress callback `0x0091DAF0`; types `9`, `10`, and `11` retain concrete evaluators (`0x0091D400`, `0x0091D5E0`, `0x0091DEE0`) | Client `0x30D4` final selection and `0x30D5` progress cover the retained single-page dialog shape | All 48 retained dialog declarations are enabled; native types `9`, `10`, and `11` have no retained declarations |
| Job / race / Hwan / time / dependency qualification | Job callback `0x00869E40` stores selector/mode at `+0x860/+0x861`; Hwan callback `0x00869E00` stores its scalar at `+0x8D4`; positive countdown setup is `0x00922DE0` and expiry handler `0x009219A0`; day-policy predicate is `0x00882C10` and shared reset tick is `0x0091FBC0` | Client decoder `0x008B94D0` consumes flag-`0x04` timer values and the inactive sentinel | All 39 job predicates, race, dependencies, four Hwan predicates, both 120-second limit-time quests, standalone real-time modifiers, and the two exact day/night quota quests are enabled |
| Skill-point / Hwan / conditional reward fields | `_RefEventReward` fields are loaded, but no `0x7515` continuation writes these values or checks the player predicates | `0x7515` carries only reward ID, count, and selected entry IDs; no field-specific reward payload exists | Rejected by `ValidateEventRewardSelection` |
| Special reward parameters | `PARAM_1..3` are mapped and persisted as reference data; no consumer semantics were recovered | No matching client field/update was found | Rejected |

This distinction is intentional: a concrete C++ mission class or a database
column proves that a field exists, not that AegisCore may safely apply it. The
runtime gate requires the objective evaluator, client transition, durable
mutation, and reward packet semantics to agree before enabling a family.

### Current recovered catalog inventory

The active shard catalog currently contains the following recovered rows. The
second column is imported evidence; the third is the explicit runtime-enabled
subset used by Agent offer/progress queries:

| Catalog family | Imported | Runtime-enabled | Binary/runtime boundary |
| --- | ---: | ---: | --- |
| Quest definitions | 1,041 | filtered per character | All definitions are retained; service, level, NPC, policy, and dependency filters apply |
| Kill-monster objectives | 126 | all recovered variant-zero shapes | Single missions use one scalar; 13 consecutive two/three-mission groups share the stock normal-class counter and retain separate completion bits |
| Standard delivery objectives | 64 | import-derived safe subset | Variant-zero, mission-zero, single-objective rows use confirmed selection, transactional item consumption, and reward-ready persistence |
| Extended delivery objectives | 1 | 1 | One exact extended shape has a recovered active-record path |
| Collection-item objectives | 260 | all recovered safe groups | 258 standard objectives form 213 one-, eleven two-, five three-, and two four-mission quests with independent packed `u16` inventory counts; the exact two-mission `QNO_WC_GENARAL_SP_1` extended shape persists its empty-token companion separately |
| Talisman objectives | 4 | 4 | Migration `000096` supplies four themes, 32 item slots, durable character registrations, transactional item consumption, theme completion, normal offer/reward projection, reconnect hydration, and zero-valued `0x30D5` quest promotion |
| Dialog objectives | 48 | 48 | Every retained row is variant zero, mission zero, single-objective, and one-key; routes are correlated to one NPC/menu entry |
| Kill-player objectives | 9 | 9 | Every retained row uses the exact `MKP_LEVEL`/`MKP_JOB`/`MKP_REGION` single-objective shape |
| Extended-kill objectives | 77 | import-derived safe subset | Variant-zero single-objective `multi_target` rows share one persisted counter; consecutive two/three-mission `classified` rows use independent packed normal/champion/giant counters |
| Job/race start conditions | 39 / 4 | 39 / 4 | All job rows share the recovered variant-zero `(1, known job, 1)` form and qualify every independently supported objective; all four `QSC_RACE` rows map China/Europe to country `0/1` and unknown keys fail closed |
| Dependency groups | 239 | offer-qualified | Authored all-clear/one-clear groups require inactive status `4` with positive achievement count; repeat attempts remain bounded by `ACHIEVEMENT_LIMIT` |
| Script point rewards | 469 | 469 | 433 variant-zero `LuaSetAchievedSkillPont` rows use the atomic fixed-SP/client-balance path; all 36 named Temple-point rows use durable keyed balances, with 30 matching SQL `ACADEMY_POINTS` duplicates validated but not double-granted |

These counts explain why the database contains far more quests than the
currently offerable set: imported rows are evidence, while runtime-enabled
rows require a complete server evaluator, client transition, durable mutation,
and reward contract.

The retained-script admission boundary is executable rather than a prose-only
claim. `inspect-vsro-quest-script --runtime-coverage` applies the same
fail-closed predicates as `import-vsro-quest-script` without opening a
database. Against the retained `Script/VIETNAM/Quest.sct`, it reports no
excluded rows: 112 kill quest groups (126 mission rows), 64 standard and one
extended delivery quest, 48 dialog quests, nine kill-player quests, all 260
collection rows, all 77 extended-kill rows, all four talisman rows, 534
completion policies, 239 dependency groups, 39 job and four race predicates,
469 point rewards, and all 435 variant-zero item-reward declarations pass
their importer admission gates. The ignored-corpus test
`TestRetainedQuestScriptHasNoUnsupportedRewardBearingQuest` independently
requires every reward-bearing retained quest to have an admitted objective;
it classifies the remaining 98 fortress scripts and the ferry script through
their recovered reference/custom reward paths. Unsupported tuple variants
remain fail-closed code paths, but none occurs in this retained script corpus.

The `0x30D6` `runtimeMarkID` is the server-side mark-record address cast to a
32-bit wire value in this 32-bit build. The three coordinates originate as
floats at record `+0x14/+0x18/+0x1C` and are converted by `0x009FBB40` before
serialization. The record stores the flags at `+0x05`, authored value at
`+0x10`, optional value at `+0x20`, owning quest/object identity at `+0x24`,
and kind at `+0x28`. Consumers reproducing this protocol must treat the wire
identifier as an opaque lifetime-bound handle, not a stable database ID.

The `0x00516670` quest-talk handler stores the selection byte at player offset
`+0x201D`. If the outbound dialogue builder left a non-zero continuation at
player `+0x2028`, the handler calls the active quest object's virtual slot
`+0x14C` with that continuation, action kind `2`, and the player. Otherwise,
selection values beginning at `5` index the player's pending dialogue-entry
collection using `selection - 5`; the selected entry ID is passed to the same
virtual slot with action kind `0`. Invalid or unavailable selections close or
clear the dialogue state through `0x005107C0`.

The `0x0051B370` handler provides the reward-selection behavior.
It reads a `u32` reward identifier and a `u8` count/kind, validates the ID
against one of two reference lookups split at ID `10000`, checks the reference
record's byte at `+0x05`, consumes that many subsequent `u32` values, applies
the selected reward through quest virtual calls, and emits `0xB515`.

The reward-selection xref corpus isolates the normal quest-reward application
continuation. `0x009287C0` materializes the selected `QuestRewardData`, and
`0x00924CF0` applies it through player virtual calls before the final quest
transition. It visibly caps the primary numeric value at `10,000,000` before
slot `+0x16C`, applies paired numeric groups through slots `+0x0C/+0x170`,
`+0x174`, and `+0x324`, conditionally invokes slots `+0x2B8` and `+0x314` for
packed reference flags, then calls final refresh slot `+0x274` and the reward
log callback. This confirms live legacy branches beyond gold/EXP/items, but
does not yet name every virtual slot, prove its SQL column update, or recover
the client delta grammar. Later player-vtable and client-packet correlation
resolved fixed skill points and Hwan, which AegisCore now applies;
conditional flags, named points, and special parameters remain rejected. The server-event
class-1/2/3 continuation at `0x0066FF00` remains separate.

The player-vtable evidence resolves two slots further: `+0x16C` targets
`0x004E4B60`, which updates a 64-bit player-side value, marks its backing
record dirty, and can emit the `0x50/0xFE/0xFE` status effect; `+0x170` targets
`0x004E5250`, which performs level/experience arithmetic, persists the updated
level block, and emits `0x3056` with level/experience fields. The adjacent
`+0x174` target is a distinct player mutation path, but its complete semantic
name is not recovered in the available decompilation. These concrete targets
strengthen the server-side application proof without justifying a
client-facing implementation for the still-ambiguous reward columns.

One field mapping is stronger than the others: the application record's
`+0x2C` is copied from the reference reward's `+0x14` field, which is the
`SKILL_POINTS` position in the recovered reward object/schema, and is passed
to `+0x174`. The player EXP routine at `0x004E5250` also calls the same slot
when converting skill-experience accumulation into skill-point gains. The
client's existing `u32` balance update and AegisCore's transactional character
mutation complete that contract. Runtime now additionally requires the
variant-zero `LuaSetAchievedSkillPont` amount to equal the normalized SQL
reward scalar before applying it.

The `0x005167D0` abandon adapter selects the appropriate packet reader and
decodes one `u32` quest ID before calling `0x00571B80`. That runtime path
requires an existing quest whose content-state byte `+0x20` is `1`, `7`, or
`8`, and rejects the request while player field `+0x1CA0` is non-zero. On
success it clears the runtime timer sentinel at record `+0x99`, sends
`0xB0D9` with the quest ID, resets its local event argument, and dispatches
quest event `8`. The inherited handler (`0x00921700`) and all 19 recovered
concrete override functions invoke the quest's abort/reset slot `+0xFC` with
mode `1`; overrides may also perform quest-specific progress, inventory, or
related-quest cleanup. The abort path performs authored
handle/item cleanup, writes state tuple `(questID, 6, 0, 1)`, saves through
player `+0x1A0`, emits log/event code `202`, and refreshes player state. No
tree unlink, runtime-record destruction, or `_quest_manage` delete is present
in this path: abandonment persists status `6` through the generic
`UPDATE _CHARQUEST` serializer and retains the row/runtime record. The exact
semantic name of player `+0x1CA0` remains unresolved.

AegisCore now mirrors the recovered event-`8` cleanup transactionally.
Abandon locks the active quest and character inventory, resolves both the
runtime-enabled objective tables and the concrete handlers' authored item
references, removes every matching quest-owned stack, clears progress fields,
and commits status `6` in the same transaction. Related resets for quests
`308`, `310`, `92`, and `96` are part of that commit. It then preserves
the legacy wire order: successful `0xB0D9` acknowledgement first, related
`0x30D5` abort removals, then one stock `0xB034` operation-`0x0F`
slot-removal update for each deleted stack. After the transaction, Agent
applies the same quest and inventory changes to the session-owned `Player`
snapshot before later actions can observe stale state.

### Values that are not binary-confirmed here

The checked-in protocol corpus assigns semantic names to additional quest
requests and packets. Constants without a decoded constructor, populated
GameServer dispatch slot or anchored handler may be constructed indirectly,
owned by AgentServer, absent from this version, or mislabeled in secondary
material. They must not be presented as GameServer-confirmed solely from the
corpus. `0x70D8/0xB0D8` and `0x70DB/0xB0DB` are no longer in this category:
their dispatcher adapters, request fields, response structures, state gates,
and internal event dispatches are confirmed as described above.

## Persistence and SQL calls

### Normal runtime persistence

`0x00455F50` is the binary-confirmed quest-lifecycle stored-procedure builder;
dirty field updates separately use the generic `_CHARQUEST` serializer
described above. Its only binary references are the missing-record branches
of `0x00570CB0`, `0x00570F10`, and `0x00571170`. Those callers pass a
continuation byte in `BL`; values above `2` abort.
For operations `0..2` it constructs exactly:

```sql
exec _quest_manage 0, <resolved-persistence-value>, <quest-id>
```

The format string is `exec _quest_manage %d,%d,%d`. Assembly at
`0x00455F9D` pushes the quest ID, a value returned by virtual slot `+0x380`
on the owning quest/player context, literal zero,
and the format string. The operation byte is retained in the asynchronous
query object at offset `+0x34`, but it is **not** substituted into the first
SQL placeholder by this function. This is an important binary fact: the
three operations select asynchronous completion behavior while the emitted
SQL text always begins with `0` in this build. The query object also retains
the three supplied arguments at `+0x28`, `+0x2C`, and `+0x30`.

### Character-login load and hydration

Quest persistence participates in `AQ_CharDataLoader`, whose RTTI type
descriptor `0x00C68344` resolves to vtable `0x00AE3510`. Its database-work
entry is `0x0043D980`. That function constructs `WHERE CharID = %d` and sends
mapped character tables through generic selector `0x009728E0`; the latter
appends the predicate to the table metadata's compiled base `SELECT` and
dispatches to the mapped ODBC row loader.

For `CInstanceQuest`, metadata compiler `0x009732F0` emits
`SELECT * FROM %s` when the mapped-field count equals the introspected table
column count; otherwise it emits an explicit mapped-column list. The binary
maps all eight `_CHARQUEST` columns, and the restored TestIn table has exactly
those eight columns. The binary-plus-companion-database query for this backup
is therefore exactly:

```sql
SELECT * FROM _CHARQUEST WHERE CharID = %d
```

The generic loaders `0x00971980` and `0x00971D90` execute the supplied SQL,
bind columns from the compiled mapping, allocate one `CInstanceQuest` per
row, convert each ODBC value into its mapped object offset, and append the
objects to the character-load collection. They close and free the statement
handle after success, no-data, or reported ODBC failure. The exact selector
between these two near-equivalent loader layouts is metadata-controlled.

Player creation `0x004E0DC0` passes the quest-row collection at character-load
bundle `+0x84` to quest-manager hydrator `0x0056F4F0`. The manager itself is
at player `+0x1D98`. For every loaded row, the hydrator:

1. clears the content object's dirty bookkeeping at `+0x08/+0x0C`;
2. resolves content `QuestID +0x1C` against the global reference-quest
   collection at `+0x8A0/+0x8A4`;
3. allocates a runtime record and stores owner, reference quest, and content
   pointers at runtime `+0x05`, `+0x09`, and `+0x0D`;
4. normalizes persisted status `0` to status `4` and clears both 64-bit
   `QuestData` values for that legacy/uninitialized case;
5. copies content `QuestData1 +0x48/+0x4C` and
   `QuestData2 +0x50/+0x54` into runtime volatile words `+0x11..+0x1D`;
6. zeroes the remaining runtime state block `+0x21..+0x7C`, inserts the
   record into the ordered quest-ID tree, and registers reference-quest state
   for persisted statuses `1`, `4`, `7`, and `8` through `0x00580630`.

Rows whose quest ID no longer resolves to a reference definition are not
materialized into the runtime tree. After consuming the input collection,
the hydrator empties it and invokes player virtual `+0x294` to refresh the
dependent quest/NPC state. This load path proves that `QuestData1/2` are the
durable backing for the four volatile runtime progress words and that status
zero is repaired during login rather than treated as an active state.

### Companion TestIn database procedure

The procedure body is not embedded in `SR_GameServer.exe`, but companion
database evidence is available in the supplied
`VSRO_OLDDB/SRO_VT_SHARD.bak`. The backup identifies its source as server
`JellyBitz\VSRO_TESTIN`, database `SRO_VT_SHARD`, database version `957`, and
finished at `2025-04-08 18:18:28`. The restored procedure was created at
`2006-04-07 11:49:02.740` and last modified at
`2012-12-26 17:22:26.270`:

```sql
create procedure _quest_manage
    @job tinyint,
    @charid int,
    @questid int
as
    if (@job = 0)
    begin
        insert _CharQuest values (@charid, @questid, 0, 0, getdate(), getdate(), 0, 0)
    end
    else if (@job = 1)
    begin
        delete _CharQuest where charid = @charid and questid = @questid
    end
```

The restored `_CharQuest` columns, in insertion order, are:

| Column | SQL type |
|---|---|
| `CharID` | `int` |
| `QuestID` | `int` |
| `Status` | `tinyint` |
| `AchievementCount` | `smallint` |
| `StartTime` | `smalldatetime` |
| `EndTime` | `smalldatetime` |
| `QuestData1` | `bigint` |
| `QuestData2` | `bigint` |

Thus companion-database job `0` inserts a placeholder quest row with zeroed
status, count, and data plus current start/end timestamps. Job `1` deletes the
row. A hypothetical job `2` matches neither branch, performs no mutation, and
can still return successful procedure execution. The procedure contains no
explicit transaction, result set, return-status assignment, or local error
handling.

This database contract must remain separate from the binary call-site
contract. `0x00455F50` always serializes literal SQL job `0`, even when the
query object retains GameServer operation `1` or `2` at `+0x34`. The reason is
now binary-confirmed: every caller first proves that the quest runtime record
is missing, constructs it, and only then invokes the builder. The retained
byte controls the post-insert GameServer continuation, not the stored
procedure argument. The procedure proves what database jobs `0` and `1` mean,
but the pinned executable has no reference that sends job `1` or `2` through
this builder. In particular, retained operation `2` is not an invocation of
`_quest_manage @job = 2`.

### Asynchronous completion

The `_quest_manage` query object is an `AQ_QuestDataManager` instance. Its
vtable begins at `0x00AE44CC`; `0x00455FE0` submits the SQL string stored at
object `+0x35`, while `0x00456040` and `0x00456080` are its database-success
and database-failure callbacks. Both resolve the player from object `+0x28`
and call player virtual slot `+0x5D4`, passing the query object and respectively
`1` or `0`.

For `CGObjPC`, slot `+0x5D4` resolves to `0x004E9440`. That adapter first calls
the query object's virtual slot `+0x2C` with the player; the
`AQ_QuestDataManager` implementation is the no-op `0x009BF500`. It then calls
`0x004E73D0` on success or `0x004E9110` on failure. The success dispatcher
selects asynchronous type `7`, branches on the retained operation byte at
query `+0x34`, loads the retained quest ID from `+0x30`, and calls
`0x005804B0` with event kind `5` and the same operation value (`0`, `1`, or
`2`). `0x005804B0` resolves the quest definition (using separate lookup paths
below and above ID `10000`) and invokes its virtual slot `+0x08`.

The failure dispatcher has no quest-specific recovery case for asynchronous
type `7`; it reaches the generic `"AQ Failed! Log out!! [AQType: %d]"`
diagnostic and calls `0x004DF050(4)`. Thus this build confirms fail-closed
session handling for a failed `_quest_manage` request. It does not confirm a
database rollback procedure inside `_quest_manage`.

### Aggregate/session save participation

Quest manager function `0x0056FDF0` is the bulk counterpart to save-now. It
walks every runtime quest record, calls `0x0050CDE0` to fold volatile progress
into the mapped content object, invokes the generic mode-`2` serializer
`0x009742D0`, and appends every non-empty generated `_CHARQUEST` update to a
caller-provided SQL statement collection.

Player aggregate-save dispatcher `0x004E63F0` calls this bulk quest serializer
when save-mask bit `0x10` is set. Other mask bits select unrelated player
subsystems. This proves that quest dirty state participates in the shared
batched player-save mechanism in addition to explicit `LuaSaveQuestNow` and
transition saves.

The graceful external callers are now resolved. Normal player removal
`0x004177D0` detaches the character from the live user indexes and invokes
async save builder `0x00437BC0`. That builder maps the player's close-state
byte to reason `2`, `3`, or `4`, then calls the player aggregate virtual
`+0x500` through `0x00441260` with save mask `-1` and context `0`; mask `-1`
necessarily includes quest bit `0x10`. Full `CGame` destruction follows
`0x00412860 -> 0x004129F0 -> 0x00417E10`, walks every remaining live player,
and calls the same virtual with `(statementCollection, -1, 0)`. AgentServer
and ShardManager loss close users through the graceful player-removal path
described in the binary lifecycle reference. Therefore ordinary logout,
world/server removal and orderly shutdown all include bulk quest persistence.
The watchdog's dump-and-`_exit(0)` path remains deliberately outside this
contract; a hard process termination cannot synchronously flush runtime-only
state.

### Administrative SQL exposed by the binary

`0x0051DE90` contains operator/GM command branches that build four additional
ODBC call strings. The exact shared fragments are `"', '"` between two
quoted arguments and `"' )}"` at the end. Three branches append the target
character name returned by player slot `+0xEC`, a parsed quest-code token,
and the terminator; reset appends only the character name and terminator:

```sql
{CALL _InsertQuestinCharByName ( '<character-name>', '<quest-code>' )}
{CALL _InsertEndQuestinCharByName ( '<character-name>', '<quest-code>' )}
{CALL _ResetQuestByCharName ( '<character-name>' )}
{CALL _RemoveQuestByCharName ( '<character-name>', '<quest-code>' )}
```

These are administrative command paths, not the normal player lifecycle, and
must not be substituted for `_quest_manage` in runtime persistence. The
builder uses legacy string concatenation rather than parameter binding; the
preceding GM command parser supplies the appended tokens, but SQL-side
escaping behavior is not added by these concatenation calls.

### Exhaustive binary SQL inventory

`SearchDefinedStrings.java` was run over the complete defined-data inventory
for `_CHARQUEST`, `_CharQuest`, `_quest_manage`, `QuestinChar`, and
`QuestByChar`. It recovered exactly six identifier-bearing executable strings
and their only direct references (the shared punctuation fragments used by
the GM builders are described immediately above):

| String/address | Direct binary reference | Role |
| --- | --- | --- |
| `0x00AE44AC` `exec _quest_manage %d,%d,%d` | `0x00455FA1` in `0x00455F50` | missing-row lifecycle insert request |
| `0x00B11F4C` `_CHARQUEST` | `0x00848197` in `0x00848140` | mapped table registration used by generic select/update generation |
| `0x00AF6E5C` `_InsertQuestinCharByName` prefix | `0x0051EA9F` in `0x0051DE90` | GM insert/start command |
| `0x00AF6E90` `_InsertEndQuestinCharByName` prefix | `0x0051EC2B` in `0x0051DE90` | GM insert-ended command |
| `0x00AF6EB8` `_ResetQuestByCharName` prefix | `0x0051ED7D` in `0x0051DE90` | GM reset command |
| `0x00AF6ED8` `_RemoveQuestByCharName` prefix | `0x0051EED5` in `0x0051DE90` | GM removal command |

The generic login `SELECT` and dirty-row `UPDATE` are synthesized from the
mapped table/field metadata, which is why their complete statements do not
exist as standalone binary strings. Within the pinned executable there is no
additional literal quest-table `INSERT`, `DELETE`, or lifecycle procedure
format hidden outside the paths documented above. This exhaustiveness claim
is limited to SQL text embedded or metadata-generated by this executable; it
does not enumerate SQL internal to external stored procedures.

A broader case-sensitive `Quest`/`QUEST` sweep was also reviewed for names
that do not contain the table or known procedure substrings. Its only extra
SQL-looking token, `@ResetQuestData` at `0x00AECD98`, is compared by
`0x004B1750` through string helper `0x0041B600`; it selects an in-process
command/reset branch and is never submitted to an ODBC executor. Other hits
are RTTI names, diagnostics, script tokens, UI text, and quest code names.

Quest reward code also contains explicit failure diagnostics for adding or
deleting quest items and `fail pay quest` paths. Those prove that item reward
and item-consumption failures are checked, but their inventory transaction
ordering is outside the recovered SQL contract.

## Confirmed lifecycle boundary

The recoverable high-level sequence is:

1. Startup projects reference quests into the quest manager and registers
   their script callbacks.
2. `AQ_CharDataLoader` selects the character's `_CHARQUEST` rows; player
   creation hydrates them into the ordered runtime tree, restores volatile
   progress, normalizes status zero, and registers applicable persisted
   states. GameServer snapshot path `0x005707E0` emits `0x30D5` subtype `1`;
   the client constructs `SQuestInfo`, decodes its flag-selected state, and
   inserts it into the client quest manager.
3. NPC/dialogue logic constructs `0x30D4` or `0x3514` and records continuation
   state on the player. The client resolves the encoded tokens into the
   dialogue/reward UI and retains the displayed selection records.
4. The client builder returns the selected record's one-byte value in
   `0x30D4`; the GameServer dispatcher invokes
   the active quest/dialogue object with action kind `2` for an explicit
   continuation or action kind `0` for a selected pending entry.
5. A quest transition resolves the quest ID. If its runtime record exists, it
   returns `1` and the caller performs the start, complete, or achievement
   continuation synchronously. If absent, it creates the per-player runtime
   record, sets the content state and inserts it into the ordered collection.
6. Only the missing-record branch posts `_quest_manage` with literal SQL job
   `0`, retaining GameServer continuation `0`, `1` or `2` separately in its
   asynchronous query object.
7. On database success, asynchronous type `7` re-dispatches event kind `5`
   to the quest definition with the retained operation value. Database
   failure takes the generic AQ failure/logout path.
8. Mission implementations mutate volatile progress and use gather/capture,
   notification, or script-progress packet constructors as applicable. A
   `0x30D5` subtype-`2` delta updates the existing client `SQuestInfo`; the
   client parser consumes all timer, status, dirty-mission, and pending-value
   sections selected by the flags. A
   save-now call folds the four volatile words into `QuestData1/QuestData2`,
   generates a dirty-field `UPDATE _CHARQUEST SET ...` statement and submits
   it through the generic asynchronous database queue.
9. Reward selection validates reference metadata, applies selected entries,
   and sends `0xB515`; the client request builder emits the selected `u32`
   entry vector, and its response handler displays the recovered failure-2
   reward error. Quest-specific reward paths check item addition, deletion
   and payment failures.
10. The client `0x70D9` builder emits one `u32 questID`. GameServer accepts
    abandon only for quests in content states `1`, `7`,
    or `8`, sends `0xB0D9`, and dispatches event `8`. The inherited handler
    and every recovered concrete override run abort cleanup, change the
    retained quest record to status `6`, and persist it with the generic
    `_CHARQUEST` update; the transition does not delete or
    unlink the row. The client response handler consumes success or the
    one-byte failure reason and presents the corresponding UI text.
11. Script-data reset unregisters and re-registers quest callbacks globally;
    it is distinct from resetting one character's quest state.

## Remaining recovery boundary

### Native seven-stage search: `QNO_RM_OLDWOMAN_5`

`QNO_RM_OLDWOMAN_5` (quest ID `140`) is one of the native-only classes with
no retained Lua objective/reward declaration, so it must not be synthesized
as an ordinary collection row. Its `SR_GameServer.exe` vtable is
`0x00B3053C` (image base `0x00401000`). Initializer `0x008A00D0` requires the
preceding `QNO_RM_OLDWOMAN_4`, binds start NPC `NPC_RM_SLAVE2`, creates a
seven-item objective using `ITEM_QNO_RM_OLDWOMAN_7_03`, and creates the
associated gather descriptor for `MOB_RM_ROCKY` and
`ITEM_QNO_RM_OLDWOMAN_5_08`.

The interaction handler at `0x008A0AA0` accepts persisted states `7` and `8`
and validates its action object's virtual `+0xE8` reference identity against
`ITEM_QNO_RM_OLDWOMAN_5_08` (Media ref object `5940`). The retained Media row
classifies it as `TypeID 3/3/9/0`, Silkroad's quest-action item family, so this
is a ground quest-object interaction rather than ordinary inventory use.
Client item-use classifier `0x00796660` does not admit TypeID3 `9` through
its recovered `0x704C` ETC branches. Initializer `0x008A00D0` independently
declares the `MOB_RM_ROCKY` gather producer for item `5940`, corroborating the
ground-object source. Agent therefore intercepts that reference at the normal
ground-pickup boundary before persistence/removal. The handler selects only
the stage immediately following the runtime success counter. Each stage
requires the player to be within its authored world sphere and to hold fewer
than seven result items:

| Stage | Region | X | Y | Z | Radius | Countdown | Success chance |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 24180 | 1081 | 5775 | 1631 | 270 | 5 | 25% |
| 1 | 24427 | 1282 | 5590 | 285 | 280 | 10 | 30% |
| 2 | 23664 | 44 | 4767 | 1898 | 300 | 15 | 40% |
| 3 | 23153 | 1825 | 4689 | 1871 | 270 | 20 | 50% |
| 4 | 22893 | 1322 | 4378 | 1761 | 270 | 25 | 60% |
| 5 | 22132 | 1023 | 5785 | 1845 | 270 | 30 | 75% |
| 6 | 22379 | 1013 | 5910 | 85 | 280 | 40 | 100% |

After the player-local action admission succeeds, `0x008A0AA0` stores the
selected stage in runtime byte `+0x02` and its countdown in byte `+0x04`.
Action-event handler `0x008A0970` decrements that byte. At zero it rolls the
stage-specific probability, grants one `ITEM_QNO_RM_OLDWOMAN_7_03`, emits
the native progress callback, and increments runtime byte `+0x00`; failure
emits `SN_TALK_QNO_RM_OLDWOMAN_5_18`. Event-`0x17` handler `0x008C8770`
clears the countdown byte `+0x04` and invokes player save slot `+0x1A0`.
Player virtual `+0x184` returns the four-word progress block beginning at
runtime record `+0x11`; save-now folds that block into `QuestData1/2`.
Consequently the success, selected-stage, and countdown bytes occupy
`QuestData1` byte offsets `0`, `2`, and `4`, and the cancel event durably
clears byte `4`. Completion helper `0x008A0E60` runs after each successful
grant. When incrementing byte `0` advances the normal sequence from `7` to
`8`, it sets bytes `1` and `3` to `1`, saves through player virtual `+0x1A0`,
and invokes the quest completion callback. The adjacent
setup/reset path `0x008A08E0` clears runtime bytes `+0x02/+0x04` for states
`1`, `7`, and `8`.

The open `sro_client.exe` database confirms the cancel response at
`0x0088D5C0`: `0xB0DB` success consumes `u32 questID` and resets the active
quest-item operation; failure reads the legacy two-byte reason. Completing
this native quest therefore requires a durable seven-stage search model plus
session-local selected-stage/countdown state and world-position admission;
mapping it to the generic packed collection counters would lose executable
semantics.

Agent now retains the exact seven-stage constants and the three-field
`QuestData1` codec as a dedicated native-search model. The quest action
dispatcher registers quest `140` event `0x17`; an authenticated `0x70DB`
request locks an active state-`1/7/8` row, clears only `QuestData1` byte `4`
with a compare-and-swap update, commits, and then emits the stock `0xB0DB`
success response. Unknown quests and invalid states preserve recovered failure
reasons `1` and `2`. The event-`0x18` action producer and grant path remain
separate from this now-complete cancel/reset transition.

The event-`0x18` entry boundary is a player periodic-update event, separate
from the positive `LuaSetLimitTime` quest timer.
`0x008A0AA0` calls player virtual `+0x228` to reserve/validate an inventory
insertion result and player virtual `+0x1D4`, whose concrete adapter is
`0x004ED240`, to emit `0x30DC(u32 questID, u8 countdown)`. Client decoder
`0x008A7EE0` consumes those exact fields; value `1` has an additional UI
effect, while the authored values `5..40` are still fully consumed. The
event-`0x18` handler `0x008A0970` is reached through the GameServer's generic
quest-event subscription callbacks (`0x0056AFB0/0x0056B080/0x0056B0D0`). The
concrete producer is `CGObjPC` periodic routine `0x0052AA90`: it dispatches
player virtual `+0x144(eventID=0x18, target=0, player)`. Its vtable entry is
`0x00AF6078` (`CGObjPC` base `0x00AF59FC`, slot `+0x67C`); wrapper
`0x0052D9B0` invokes that slot and public-instance thunk `0x006055F0` invokes
the wrapper at player slot `+0x680`. The same routine decrements neighboring
player counters by one per invocation. Agent therefore advances the native
search once per one-second player update, independently of further pickup
requests. Agent's native transition
model now preserves all neighboring bytes, rejects invalid stage/countdown
states, applies the inclusive `rand()%101 <= chance` boundary, and performs
the recovered byte-`1`/byte-`3` reward-ready promotion after stage seven.
Agent also has an atomic start-admission transition that locks quest and
inventory state, requires step `1..7`, selects `step-1`, rejects an existing
countdown, validates the recovered stage sphere, enforces fewer than seven
result items plus one free slot, and stores the selected stage/countdown
without performing ordinary pickup/removal of trigger object `5940`. Quest
vtable `0x00B3053C + 0x158` resolves to `0x00922340`; it calls player virtual
`+0xF4` for the packed `u16 region + float X/Y/Z` position and copies those
fields unchanged. This is the same coordinate tuple Agent persists and uses
for its recovered sphere check. The event-`0x18` transaction atomically
decrements progress, inserts result item `9312` only on probability success,
and promotes the final status to reward-ready; inventory-full rolls the entire
action back. After admission Agent schedules the recovered periodic event
until the countdown resolves, stopping on durable invalidation or failure.

The normal pickup path does not establish that connection. Action-`6` handler
`0x00526090` reaches transfer boundary `0x004B29A0`, which dispatches the
ground object's virtual `+0x4D8`. The generic item class uses vtable
`0x00AE9384` (`CGItem`, corroborated by constructors `0x0048F750` and
`0x0048F800`), so that slot resolves to `0x0048FC70`. The concrete transfer
implementation performs inventory attachment and world removal but does not
invoke common event virtuals `+0x140/+0x144` (`0x00485F90/0x00485F60`). Agent
therefore must not interpret repeated `0x7034` pickup requests as event-`0x18`
frames without another recovered native dispatch edge.

The apparent `0x00516270..0x00516670` candidate is not this edge: the pinned
opcode table assigns those functions to stall requests `0x70B3..0x70B6`, so
their byte-stream state cannot be used as catch-action timing evidence.
Opcode-`0x7046` handler `0x00510250` also has a generic action-`10` branch
that raises event `0x18` on the request's selected target object. That is a
separate object-scoped interaction and must not be wired as the Old Woman
countdown producer: the quest registers the recurring player-scoped event
raised by `0x0052AA90`, and using both paths would double-decrement progress.
Agent's dedicated event-`0x18` consumer performs the atomic transition,
commits the live quest/inventory snapshot, emits the server-add inventory
operation and `0x30D5` progress on success, or emits
`SN_TALK_QNO_RM_OLDWOMAN_5_18` on a resolved probability failure. Ordinary
pickup performs only admission and `0x30DC`; the scheduled player-update
callback owns all subsequent decrements.

The following are not yet exact enough to implement as legacy-compatible
behavior:

- whether another GameServer version uses the companion procedure's delete
  job `1`; the pinned executable's complete builder-reference set does not;
- any database/service-side retry policy outside the recovered
  `AQ_BrutalQuery` object after execution failure;
- precise semantic names for every runtime-record field and every concrete
  mission class.

Until those edges are recovered, AegisCore should treat its current quest
schema and handlers as an independently designed implementation informed by
legacy evidence, not a claim of full `SR_GameServer` parity.

## Reproducible artifacts

The supporting decompilations and instruction listings are under the outer
workspace's ignored reverse-engineering tree:

```text
analysis/decompiled/gameserver-quest/
analysis/decompiled/gameserver-quest-core/
analysis/decompiled/gameserver-quest-engine/
analysis/decompiled/gameserver-quest-dispatch/
analysis/decompiled/gameserver-quest-async/
analysis/decompiled/gameserver-quest-lua/
analysis/decompiled/gameserver-quest-lua-native/
analysis/decompiled/gameserver-quest-rtti/
analysis/decompiled/gameserver-quest-vmethods/
analysis/decompiled/gameserver-player-rtti/
analysis/decompiled/gameserver-player-quest-slots/
analysis/decompiled/gameserver-quest-async-completion/
analysis/decompiled/gameserver-quest-save/
analysis/decompiled/gameserver-quest-wire/
analysis/decompiled/gameserver-quest-mission-wire/
analysis/decompiled/gameserver-quest-mission-types/
analysis/decompiled/gameserver-quest-mission-registry-refs/
analysis/decompiled/gameserver-quest-progress-writers/
analysis/decompiled/gameserver-quest-mission-progress-discriminators/
analysis/decompiled/gameserver-quest-operation-dispatch/
analysis/decompiled/gameserver-quest-reward-order/
analysis/decompiled/gameserver-quest-opcode-gaps/
analysis/decompiled/gameserver-quest-variants/
analysis/decompiled/gameserver-quest-session/
analysis/decompiled/gameserver-quest-opcode-audit/
analysis/decompiled/gameserver-quest-opcode-completeness/
analysis/decompiled/gameserver-quest-internal-actions/
analysis/decompiled/gameserver-quest-abandon-event8/
analysis/decompiled/gameserver-quest-sql-exact/
analysis/decompiled/gameserver-quest-mapping-builder/
analysis/decompiled/gameserver-quest-database/
analysis/decompiled/gameserver-quest-transition-refs/
analysis/decompiled/gameserver-quest-existing-continuations/
analysis/decompiled/gameserver-quest-load/
analysis/decompiled/gameserver-quest-load-query/
analysis/decompiled/gameserver-quest-state-getters/
analysis/decompiled/gameserver-quest-status-writers/
analysis/decompiled/gameserver-quest-structure-audit/
analysis/decompiled/gameserver-quest-timer-mode2/
analysis/decompiled/gameserver-quest-sql-audit/
analysis/decompiled/gameserver-quest-opcodes.log
analysis/decompiled/client-quest-complete/
analysis/decompiled/client-quest-outbound/
analysis/decompiled/client-quest-30d5/
analysis/decompiled/gameserver-quest-client-crosscheck/
```

They were generated from the existing `vsro-testin-gameserver` Ghidra project
with `ExportStringReferenceFunctions.java`, `FindScalarConstants.java`,
`ExportFunctionsAndCallers.java`, `ExportFunctionListings.java`,
`ExportReferences.java`, `ExportFunctions.java`, and
`ExportMultipleVtables.java`; focused literal audits also use
`ReadCStringAt.java`, `SearchDefinedStrings.java`, and
`FindQuestEventOverrides.java`; quest-state writer coverage additionally uses
`FindQuestStatusVirtualCalls.java`. Client label handlers that Ghidra had not
bounded as functions were recovered with `ExportFunctionsAtLabels.java` from
the `vsro-188-stock` project. The two `vsro-client-3df2282d*` projects are
empty metadata shells and are not cited as analyzed evidence.
