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

The shared `CBasicQuest` event-`0x0E` handler is vtable slot `+0x80`, resolved
to `0x009219A0`. For a positive authored duration it looks up the player's
quest state and decrements its countdown byte at `+0x04`. At zero it invokes
quest virtual `+0xFC` (the recovered abort/reset cleanup), refreshes through
`+0x118`, and sends the authored expiry token at quest `+0x4F0` through the
single-token `0x30EC` channel. While non-zero, every ten-second boundary sends
an updated `(questID, minutes, seconds, 0)` timer display and saves through
player `+0x1A0`.

The base `CBasicQuest` event-`0x18` slot (`+0x98`) is the no-op
`0x0092F270`; event-`0x18` therefore requires a concrete quest override to do
work and has no universal expiration behavior. The complete 186-vtable scan
finds exactly ten overrides: `CQNO_RM_OLDWOMAN_5`, `CQNO_RM_SLAVE2_2`,
`CQNO_EU_EASTEU_1`, `CQNO_EU_EASTEU_19`, `CQNO_EU_IVY_2`,
`CQNO_CA_WAREHOUSE_1`, `CQNO_CA_WAREHOUSE_3`, `CQNO_TQ_ORNAMENT_3`,
`CQNO_CH_HWAN_2_4`, and `CQNO_EU_HWAN_2_4`. Their handlers are respectively
`0x008A0970`, `0x008A5000`, `0x008AA630`, `0x008B2A10`, `0x008BB5F0`,
`0x008C86A0`, `0x008CA6E0`, `0x008D6B10`, `0x009038D0`, and `0x00909600`.

These overrides implement authored one-shot behavior rather than a shared
countdown: observed effects include progress mutation plus save, promotion to
state `7`, and quest-specific `SN_TALK_*` notifications. None delegates to
the common event-`0x0E` countdown/abort handler. This distinction prevents a
mode-`2` timer from being incorrectly implemented as the normal countdown and
closes the executable-wide override inventory without assigning one semantic
to ten quest-specific scripts.

Player timer-display slot `+0x1C4` resolves to adapter `0x004ED090` and quest
manager function `0x00570650`; it is not a separate timer opcode. The manager
packs the supplied time into runtime field `+0x99` (or writes `0xFFFFFFFF` for
the sentinel), then sends a detailed subtype-`2` `0x30D5` delta with flag
`0x04`, which is the conditional field already decoded by `0x005785E0`.
Consequently countdown display, periodic updates and ordinary quest progress
share the same client packet family.

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

No separate party-quest RTTI class, party-specific quest SQL procedure, or
party-specific quest packet constructor was found in the binary string,
vtable and function-reference pass. The executable does have an independent
party manager and party EXP system, but those anchors do not establish shared
quest progress. Party sharing may be implemented by concrete mission event
handlers over generic party membership; it remains unresolved and must not
be inferred merely from the presence of the party subsystem.

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

All other event-`7` and event-`0x17` slots retain base no-op `0x0092F270`;
all remaining event-`8` slots inherit the non-no-op abort handler
`0x00921700`. This
proves that `0x70DB/0xB0DB` cancels or resets quest-local action state. The
secondary corpus's fuller name “gather-cancel” is still not universal or
embedded in the executable. Likewise, `0x70D8/0xB0D8` is a quest-specific
interaction family, not a universally safe “gather” operation: its concrete
overrides include both talk notifications and inventory-driven progress.

The focused message-filter constructor at `0x00427D90` still does not
enumerate `0x70D8` or `0x70DB`; their dispatcher transport is exact, while the
separate admission filter or shared route that permits them remains outside
this recovered edge.

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
| Kill-monster | `CMissionKillMonster` type `2`, evaluator `0x0091BE70`, progress callback and `0x30D5` delta | `0x30D5` parser consumes the authored progress sections | Enabled for the recovered variant-zero, single-objective shape |
| Standard delivery | `CMissionDeliverItem` type `3`, setup validation and evaluator `0x0091CFF0` | Generic quest state/progress packets are decoded, but no complete delivery turn-in response grammar is proven | Imported; runtime-disabled except separately proven delivery path |
| Extended delivery | `CMissionDeliverItem` extended declarations and item-entry catalog | No complete client turn-in transition was recovered | Runtime-disabled pending packet/commit evidence |
| Collection / talisman | Types `1`, `4`, `8`, and `13`; talisman validation is present at `0x00927BA0` and evaluator `0x0091E110` | Progress transport exists, but collection inventory-consumption and completion sequencing are not fully paired to a client flow | Runtime-disabled pending end-to-end evidence |
| Dialog / special objectives | Types `6`, `9`, `10`, and `11` have concrete evaluators (`0x0091DAF0`, `0x0091D400`, `0x0091D5E0`, `0x0091DEE0`) | No single generic client grammar proves their authored side effects | Runtime-disabled unless an exact family-specific path is enabled |
| Job / race / Hwan / time / dependency qualification | Fields and evaluators exist in the reference/runtime objects; timer modes are handled by `0x00570650` and `0x00571B80` | Timer packets `0x30D4`/`0x30D5` are decoded, but qualification and repeat/expiry persistence are family-specific | Fail-closed in offer selection until each predicate and persistence transition is proven |
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
| Kill-monster objectives | 126 | 99 | Variant-zero, mission-zero compatible shapes only |
| Standard delivery objectives | 64 | 0 | `CMissionDeliverItem` is confirmed, but full turn-in commit/packet pairing is incomplete |
| Extended delivery objectives | 1 | 1 | One exact extended shape has a recovered active-record path |
| Collection-item objectives | 260 | 0 | Inventory collection and completion transition remain unpaired end-to-end |
| Talisman objectives | 4 | 0 | Type `13` setup/evaluator is confirmed; turn-in and item-consumption semantics remain incomplete |
| Dialog objectives | 48 | 0 | Concrete evaluator exists; generic completion side effects are not proven |
| Kill-player objectives | 9 | 0 | Type `12` evaluator exists; player-event and persistence path is not fully paired |
| Extended-kill objectives | 77 | 0 | Objective catalog is recovered; supported runtime shape is not established |
| Job/race start conditions | 39 / 4 | 0 | Predicate mapping to character state and offer packets remains fail-closed |
| Dependency groups | 239 | 0 | Authored all-clear/one-clear groups are retained; generic runtime evaluation is not enabled |
| Script point rewards | 469 | 0 | Skill-point/named-point semantics and client deltas are not fully proven |

These counts explain why the database contains far more quests than the
currently offerable set: imported rows are evidence, while runtime-enabled
rows require a complete server evaluator, client transition, durable mutation,
and reward contract.

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
the client delta grammar. AegisCore therefore still rejects skill points,
Hwan, conditional flags, and special parameters until those mappings are
paired with persistence and `sro_client.exe` evidence. The server-event
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
when converting skill-experience accumulation into skill-point gains. This is
server-side confirmation that the skill-point branch is real; it still does
not establish the exact atomic SQL/update packet contract needed to enable it
in AegisCore's event/quest transaction.

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
transition saves. The recovered call graph does not identify every external
reason that supplies mask `0x10`, so it is not evidence that all disconnect,
crash or migration paths synchronously flush quests.

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

The following are not yet exact enough to implement as legacy-compatible
behavior:

- whether another GameServer version uses the companion procedure's delete
  job `1`; the pinned executable's complete builder-reference set does not;
- the separate admission filter or shared ingress route for the confirmed
  `0x70D8` and `0x70DB` dispatcher handlers;
- any database/service-side retry policy outside the recovered
  `AQ_BrutalQuery` object after execution failure;
- the client localization lookup and rendering behavior for encoded text
  tokens; the GameServer field order and producer roles are recovered;
- the client-facing semantics of each mission progress record's authored,
  length-prefixed text value;
- durability and external compensation behavior after a mid-reward failure;
  the in-process item-first mutation order and lack of local rollback are
  recovered;
- party-shared progress, repeat-cycle eligibility scheduling, teleport
  failure recovery, and the external conditions that
  select aggregate-save mask `0x10` during disconnect or migration;
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
