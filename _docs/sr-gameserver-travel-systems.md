# SR_GameServer travel systems: binary-confirmed reference

This is the travel-specific companion to
[`sr-gameserver-binary-reference.md`](sr-gameserver-binary-reference.md). It
covers the pinned TestIn `SR_GameServer.exe` and the AegisCore travel paths:
ferry, caravan/transport, teleport network, return scroll and reverse return
scroll.

The evidence vocabulary is intentional:

- **Binary-confirmed** means the pinned executable contains the dispatcher
  entry, immediate, memory access, branch, serializer call or SQL literal
  being described.
- **Derived** means the fact is calculated from binary-confirmed data, such as
  converting a dispatch-table slot into an opcode.
- **Implementation contract** means behavior currently implemented in
  AegisCore. It is not automatically behavior of the stock executable.
- **Unresolved** means the evidence is insufficient to assign a conventional
  semantic name or wire layout.

## Evidence identity

The primary binary is:

`VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`

It is a 32-bit x86 executable, 8,925,184 bytes, SHA-256
`0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`.
Addresses in this document are valid only for that hash. The complete
dispatcher inventory is
[`evidence/sr-gameserver-testin-opcodes.csv`](evidence/sr-gameserver-testin-opcodes.csv).

The executable's reference-data strings include `CRefTeleport`,
`CRefOptionalTeleport`, `_REFTELEPORT`, `_REFOPTIONALTELEPORT` and
`AppointedTeleport`. These prove that the binary has separate normal and
optional teleport reference concepts; they do not prove the database schema
or the meaning of every field.

## Lifecycle recovered from the binary

The travel request enters the normal client-message pipeline:

```text
client packet
    -> 0x0050D0C0 dispatcher
    -> 0x00CD3D08 + (opcode - 0x7000) * 8
    -> travel handler
    -> world/player checks and transition helper
    -> response or failure
```

`0x0050D0C0` initializes 2,048 eight-byte slots for opcodes `0x7000..0x77FF`.
The travel entries below are therefore binary ownership facts, even where the
handler body still has unresolved helper calls.

The broader server lifecycle is recovered in the companion reference:
startup is `0x00412C20`, the update and message-dispatch monitors are separate,
and the graceful Agent/Shard disconnect paths close players before final
manager work. No travel-specific alternate server lifecycle was found.

### Teleport request transition

The strongest recovered path is the normal teleport request at `0x705A`:

1. The dispatcher selects `0x00513590`.
2. The handler selects the active world/context accessor according to the
   message context at `param_2 + 0x1048`, then calls `0x004A8E10` with selector
   `0x30`. That helper resolves the selected object, checks its object state,
   checks the selection/range predicate and returns a small result code.
3. It rejects unavailable or forbidden states. The player virtual calls used
   for these checks are confirmed; their semantic names are unresolved.
4. On a successful transition branch it writes state `0x7CE6` into the
   world/message object at `+0x1050`, sends the transition event through the
   recovered world manager, and calls the common transition routine
   `0x00430990`.
5. The related transition routine `0x004F2B50` contains a direct `0xB05A`
   response construction. Its success payload is binary-confirmed as one byte
   `01`; its failure branches return through the common error path.

The selection helper's complete object-type predicate, the world manager
methods, and the meaning of the selector values are not sufficiently recovered
to claim a complete stock wire structure. In particular, the binary evidence
does not justify treating every current Go field name as a C++ field name.

The current Agent implementation adds a complete loading handshake around
this binary-compatible family: stop movement, flush the last position,
commit the destination, send `0xB05A`, send `0x34B5` with the destination
region, accept `0x34B6`, then bootstrap the new area. That is an
**implementation contract** in
[`agent-server/handler/character/teleport.go`](../agent-server/handler/character/teleport.go),
not a claim that all of those steps were recovered from SR_GameServer.

The maintained wire contract also admits `0x7059` with an exact
`selected-object-u32` payload. On a validated teleport-building object it
persists the selected point and returns `0xB059 [01]`; the SQL transaction
rechecks character ownership and the enabled `TELEPORT_POINT` row before
updating `CHAR.RETURN_TELEPORT_ID`. The one-byte success response is
binary-confirmed; the request-field name and the AegisCore persistence
transaction remain implementation/documentation boundaries.

## Client request opcodes owned by SR_GameServer

| Opcode | Dispatcher slot | Handler | Binary-confirmed interpretation |
| --- | --- | --- | --- |
| `0x7059` | `0x00CD3FD0` | `0x00513410` | teleport-designation request; resolves a selected object with selector `0x30`, validates a two-word designation record, then sends `0xB059` success |
| `0x705A` | `0x00CD3FD8` | `0x00513590` | teleport-use request family; transition path described above |
| `0x705B` | `0x00CD3FE0` | `0x00513EA0` | teleport-cancel request; removes the active internal action type `0x0B` unless its action parameter is `6`, then resets the state machine |
| `0x705D` | `0x00CD3FF0` | `0x0051A5B0` | siege-return request, not ordinary return-scroll use |
| `0x705E` | `0x00CD3FF8` | `0x00519E60` | siege-action request, not caravan movement |

`0x705D` and `0x705E` are included because their names are commonly confused
with return/caravan systems. The `0x00519E60` body dispatches a siege action
selector over cases `0..0x20` and emits `0xB05E` with failure status when the
action cannot be accepted. That is siege control flow, not proof of a return
scroll implementation.

### Confirmed response evidence

The focused SR_GameServer decompilation directly observes `0xB059`, `0xB05A`
and `0xB05D` response constructors. The successful designation sequence in
`0x00513410` is:

```text
message = 0xB059
payload = u8(1)
send(message)
```

The successful teleport sequence in `0x004F2B50` is:

```text
message = 0xB05A
payload = u8(1)
send(message)
```

The siege-return failure sequence in `0x0051A5B0` selects response `0xB05D`,
writes status `u8(2)` and serializes an additional four-byte error/status
value. Observed internal error values include `1`, `2`, `3`, `0x7C08` and
`0x7C09`. The protocol catalog names `0xB05B` as the cancel response, and the
stock-client analysis contains a handler for it, but the focused GameServer
handler cancels through its internal action queue and does not expose a direct
`0xB05B` constructor in the exported body. Keep the complete cancel payload
unresolved.

### Matching stock-client response handlers

The pinned client registers this response family in `FUN_0086FBB0`:

| Response | Client handler | Confirmed read/branch |
| ---: | ---: | --- |
| `0xB059` | `0x00883520` | reads `u8 status`; status `1` loads the resource at `0x00DDB9C0`, performs local UI work and calls `0x007781B0(5, string)`; other status values take the error/read path |
| `0xB05A` | `0x008809D0` | reads `u8 status`; status `2` reads a `u16` value and calls `0x00778190(0x0D, value, 0xFFDBC99B, 0, 1)`; status `1` returns without another payload read |
| `0xB05B` | `0x008727A0` | reads `u8 status`; status `1` loads the resource at `0x00DD9638` and calls `0x007781B0(0, string)`; status `2` reads a `u16` value and returns |

The status width and success/error branches are binary-confirmed. The
resource addresses are client-local string/resource keys, not server packet
fields. `0x007781B0` routes a local notification through
`0x008B51F0`, `0x007B85F0` and `0x007B89E0`; this proves the local-notice
route but not the human-readable localization text. A targeted export of
`0x00778190` shows the same broader notification family: it receives a
category byte and a `u16` code, validates the UI context through
`0x008B51F0(0x44, 1)`, and routes accepted notices through `0x007B89E0`.
The category/code-to-localization mapping remains unresolved; `0x0D` is only
the category observed for the `0xB05A` error branch.

Raw client evidence: [`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c),
[`00883520_FUN_00883520.c`](../../analysis-tmp/client-targeted-responses/00883520_FUN_00883520.c),
[`008809D0_FUN_008809d0.c`](../../analysis-tmp/client-targeted-responses/008809D0_FUN_008809d0.c),
[`008727A0_FUN_008727a0.c`](../../analysis-tmp/client-targeted-responses/008727A0_FUN_008727a0.c),
[`007781B0_FUN_007781b0.c`](../../analysis/decompiled/client-silk-handlers/007781B0_FUN_007781b0.c),
and the targeted notification router [`00778190_FUN_00778190.c`](../../analysis-tmp/client-targeted-responses/00778190_FUN_00778190.c).

## System-by-system findings

### Teleport network

**Binary-confirmed:** the server owns the designation/use/cancel request
family (`0x7059..0x705B`), has teleport reference-data class/table strings, and
contains `0xB059` and `0xB05A` success constructors. The focused binary also contains
teleport gate/object virtual checks and the `TELEPORT`/teleport-building
reference vocabulary.

**Implementation contract:** AegisCore validates the selected NPC/gate and
interaction, quotes a service link, rechecks the character under a transaction,
charges gold, writes the destination, and then performs the loading reset.
The gate catalog and link repository are in
[`agent-server/model/teleport_repository.go`](../agent-server/model/teleport_repository.go)
and [`agent-server/model/teleport_gate.go`](../agent-server/model/teleport_gate.go).

### Ferry

The binary strings include ferry reference names such as
`NPC_CH_FERRY`, and AegisCore exposes ferry NPC talk options. No separate
ferry opcode or ferry-specific SQL call was found in the pinned GameServer
dispatcher/SQL inventory. The safe conclusion is that ferry selection belongs
to the generic NPC/teleport reference path. AegisCore admits ferry NPCs as
teleport service objects and routes their `0x7059`/`0x705A` actions through
the same validated selection, link quote, gold/position transaction and
loading reset as city gates. Ferry routes are reference-data rows in
`TELEPORT_POINT` and `TELEPORT_LINK`; the checked-in minimal catalog still
needs the matching reference-data import for a particular shard's full ferry
map. The stock binary does not establish a second ferry opcode or a
ferry-specific SQL call.

Do not use an eSRO/other-version transport opcode as proof for this TestIn
GameServer. The checked-in secondary headers are corroboration only and have
different version/evidence status.

### Caravan / transport

The binary evidence reviewed here does not establish job-caravan packet
ownership. The `0x705D/0x705E` entries are siege entries, not caravan entries,
and transport opcodes found in secondary protocol headers are not present as
confirmed slots in the TestIn client dispatcher inventory.

AegisCore implements both the ride-transport mount state and the job-transport
COS lifecycle: validated item creation/activation, owner-scoped world spawn,
job-COS inventory serialization, movement/follow/command handling,
state checkpointing, and unsummon/termination. Ride transport remains
deliberately separate: only `TypeID4=1` activates the player's mounted flag
and transport-UID spawn segment, while `TypeID4=2` is a job caravan COS with
its own owner/inventory spawn data. The binary evidence does not establish
job goods, escort, attack, reward, or caravan-specific opcode ownership, so
those are not claimed as recovered stock behavior.
The stock player spawn notes also carry job type, job level, PVP state,
transport flag/unique ID and scroll mode. These are spawn-structure fields;
they do not prove job-trade caravan lifecycle, goods, attack, escort or
terminate semantics.

### Return scroll

The pinned binary now provides a complete item-use-to-return-scroll dispatch
chain through the expendable-item virtuals. The shared request is `0x704C`;
the server handler is `0x00510980`; `CGItemExpendable` vtable
`0x00AEB2B4 + 0x4C8` points to `0x0049ACA0`; TypeID3 `3` selects the
`+0x4CC` handler `0x0049B9F0`; and TypeID4 `1` selects `0x004A0380`.
The catalog rows `ITEM_ETC_SCROLL_RETURN_01`, `ITEM_ETC_SCROLL_RETURN_02`
and `ITEM_EVENT_RETURN_SCROLL_HIGH_SPEED` all have tuple `3,3,3,1` and
therefore share this executable branch. Their distinct reference rows may
provide different item-derived timing values, but the static branch does not
distinguish them by name.

`0x004A0380` performs player-state and location/effect gates, emits the
binary-observed event call `0x00466F90(0x0B, mode, 0)`, invokes player virtual
`+0x264`, updates player virtual `+0x4B0` with an item-derived value, and
queues internal action type `0x0B` through `0x004E0B50` with subtype `3`.
The queued record stores a positive float delay, an action parameter and an
opaque pointer. The function itself does not directly call `0x00430990`.

After the callback returns success, the common item-use caller consumes one
stack unit and sends `0xB04C`, then `0x305C`. The recovered deletion path
continues through `_STRG_DEL_ITEM` when the stack reaches zero. No
travel-specific SQL literal was recovered, and the binary does not prove a
separate destination-persistence procedure at this call site.

AegisCore's ordinary return-scroll contract is:

1. lock the character and inventory item;
2. resolve `CHAR.RETURN_TELEPORT_ID` to an enabled `TELEPORT_POINT`;
3. save the pre-use position as the reverse-return origin;
4. update the character position to the bind point and consume the item;
5. after the reference cast delay, enter the same loading/reset/bootstrap path
   used by gate teleport.

This is implemented in
[`agent-server/model/item_use_repository.go`](../agent-server/model/item_use_repository.go)
and [`agent-server/handler/inventory/item_use_handler.go`](../agent-server/handler/inventory/item_use_handler.go).

### Reverse return scroll

The catalog row `ITEM_EVENT_REVERSE_RETURN_SCROLL` has tuple `3,3,3,3`
(`0x19EC`). The stock item virtual chain selects `0x004A00C0` for TypeID4
`3`. That routine performs separate state/location checks, resolves a
location through `0x004E0130` or `0x004E01C0`, updates the player timing/state
virtual, queues internal action type `0x0B` with subtype `3`, and emits
`0x00466F90(0x0B, 4, action-parameter)`. The resolver's opaque selection
values and the exact stock request fields remain unresolved.

The matching client UI strings name `last return` and `last death` choices,
but that evidence does not establish the following AegisCore request as the
stock wire grammar. The reverse-return request format below is therefore an
AegisCore implementation contract:

```text
option 2                  -> saved pre-return position
option 3                  -> saved death position
option 7 + u32 map ID     -> enabled optional teleport destination
```

The repository validates the option, locks the selected destination row,
writes the character position in the same item-use transaction, and schedules
the same delayed loading transition. The schema is deliberately explicit:

- `CHAR_REVERSE_RETURN_POSITION` stores the position captured by ordinary
  return scroll use;
- `CHAR_DEATH_POSITION` stores the last valid position before death;
- `REF_OPTIONAL_TELEPORT` stores imported optional destinations and level
  bounds.

These tables and option values are not binary-confirmed SR_GameServer SQL
contracts. They are AegisCore extensions that reproduce the intended gameplay
surface while keeping the destination decision atomic. Item use refuses to
commit any teleport scroll unless the runtime loading coordinator is
configured, and a failed post-commit handoff aborts the loading phase rather
than leaving the session stranded in `CharacterLoading`.

Raw item-use evidence: [`0049ACA0_FUN_0049aca0.c`](../../analysis/decompiled/item-use-extra-refs/0049ACA0_FUN_0049aca0.c),
[`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/item-use-extra-refs/0049B9F0_FUN_0049b9f0.c),
[`004A0380_FUN_004a0380.c`](../../analysis/decompiled/gameserver-quest-async/004A0380_FUN_004a0380.c),
[`004A00C0_FUN_004a00c0.c`](../../analysis/decompiled/item-use-extra-refs/004A00C0_FUN_004a00c0.c),
and [`004E0B50_FUN_004e0b50.c`](../../analysis/decompiled/gameserver-cos-state/004E0B50_FUN_004e0b50.c).

### Queued `AJStateChanger` completion

The return callbacks do not themselves perform the final player-position
transition. They enqueue an `AJStateChanger` record on the character's action
queue. The recovered chain is:

```text
0x004E0B50
  -> allocate record through 0x00405F10
  -> type      @ +0x0C = 0x0B
  -> subtype   @ +0x0D = 3
  -> delay     @ +0x10 = positive float
  -> parameter @ +0x14 = resolver/action selector
  -> opaque    @ +0x18 = selector-dependent pointer
  -> insert through 0x004066A0

CGObjChar update wrapper 0x004A6990
  -> queue update 0x004065D0
  -> AJStateChanger vfunc +0x08 = 0x00405F70
  -> on completion, AJStateChanger vfunc +0x0C = 0x00405F30
```

`0x004065D0` advances the queue using a `GetTickCount()` delta. Its virtual
update call retains a record while the delay remains positive; a zero result
invokes the record's completion/cleanup virtual and removes it from the list.
The RTTI/vtable data at `0x00ADF20C`/`0x00ADF210` identifies the record class
as `AJStateChanger`; the name is binary RTTI evidence, while the gameplay
name is still a documentation label.

For action type `0x0B`, `0x00405F70` performs the final transition work after
the delay and player-state gate. Its parameter selector is binary-confirmed
as follows:

| Action parameter (`+0x14`) | Binary operation |
| ---: | --- |
| `0` | calls `0x004DF290(0)` and retains the action (`return 1`) |
| `1` | resolves a compact location through `0x00407760` using `+0x18` |
| `2` | resolves through `0x004E0130` |
| `3` | resolves through `0x004E01C0` |
| `4` | copies the owner player's current region/position fields |
| `5` | reads a location record from the opaque pointer at `+0x18` |
| `6` | calls owner virtual `+0x2DC`; the cancel handler preserves this selector |
| `7` | resolves through `0x004076F0` using `+0x18` |

When a nonzero destination region is resolved, the method calls owner virtual
`+0x378` with the region and three coordinate values plus mode `2`, then calls
`0x004E0B50(0, 0xFF, 0, 0)` to clear the active state. It returns zero so the
queue removes the action; `0x00405F30` then releases selector-owned data for
parameter `5` and decrements the allocator's live-record count. This is the
binary-confirmed completion edge from ordinary/reverse item use into the
player loading/transition virtual. It does not directly call `0x00430990`.

The `0x705B` handler's exact predicate is also now bounded: it looks up type
`0x0B` with `0x00406750`, and when no record exists or its `+0x14` parameter is
not `6`, removes matching type/subtype records through `0x004066C0` and resets
the state through `0x004E0B50`. The stock cancel response payload remains
unresolved because this handler does not construct `0xB05B` directly.

Raw queue evidence: [`00405F70_FUN_00405f70.c`](../../analysis-tmp/action-record/00405F70_FUN_00405f70.c),
[`00405F30_FUN_00405f30.c`](../../analysis-tmp/action-record/00405F30_FUN_00405f30.c),
[`004065D0_FUN_004065d0.c`](../../analysis-tmp/action-queue-callers/004065D0_FUN_004065d0.c),
[`004A6990_FUN_004a6990.c`](../../analysis-tmp/action-update-direct/004A6990_FUN_004a6990.c),
and [`AJStateChanger-vtable.txt`](../../analysis-tmp/action-vtable-dump.txt).

## Structures and field confidence

The following are the useful structure facts currently safe to publish:

| Structure/field | Status | Evidence |
| --- | --- | --- |
| Dispatcher base `0x00CD3D08`, slot width `8`, range `0x7000..0x77FF` | Binary-confirmed | `0x0050D0C0` initializer |
| Travel handlers listed above | Derived from slots and confirmed function addresses | `evidence/sr-gameserver-testin-opcodes.csv` |
| Message context `+0x1048` selects one of two parser families | Binary-confirmed; meaning unresolved | `0x00513590`, `0x004F2B50` |
| `0x004A8E10` selector `0x30` | Binary-confirmed object-selection/range validation path; exact object type name unresolved | `0x00513410`, `0x00513590` |
| Designation record pair `(+0x90,+0x94)` below `player[0xD]+0x18` | Binary-confirmed two `u32` comparisons; semantic names unresolved | `0x00513410` |
| Internal action lookup type `0x0B`; cancel handler preserves parameter `+0x14 == 6` | Binary-confirmed cancel guard; semantic action name unresolved | `0x00513EA0`, `0x00406750` |
| `AJStateChanger` record: owner `+0x08`, type `+0x0C`, subtype `+0x0D`, delay `+0x10`, parameter `+0x14`, opaque pointer `+0x18` | Binary-confirmed observed fields; selector semantics are branch-specific | `0x00405EE0`, `0x00405F70`, `0x004E0B50` |
| Character action queue `+0x1D8`; update wrapper `0x004A6990` | Binary-confirmed | `0x004A6990`, `0x004065D0` |
| Transition/state field `+0x103C` and paired field `+0x103E` | Binary-confirmed memory accesses; semantic name unresolved | travel handlers clear/copy this pair on reject/complete |
| World/message field `+0x1050` receives `0x7CE6` on one complete branch | Binary-confirmed; semantic name unresolved | `0x00513590` |
| Player vtable slots `+0x120`, `+0x128`, `+0x244`, `+0x278`, `+0x27C` | Binary-confirmed calls; method names unresolved | travel handlers |
| `CRefTeleport` / `CRefOptionalTeleport` and `_REFTELEPORT` / `_REFOPTIONALTELEPORT` | Binary-confirmed strings/RTTI vocabulary | TestIn string inventory |
| `TeleportDestination{ID, region, x, y, z, world, fee, min-level}` | Implementation contract | AegisCore SQL repository, not a recovered C++ layout |
| `option 2/3/7` reverse-return request | Implementation contract | AegisCore item-use handler |

The siege-return helper `0x00407760` also recovers a compact location record:
`+0x20` is read as a `u16` region identifier, `+0x26`, `+0x28` and `+0x2A`
are read as signed 16-bit coordinate components and converted to float, and
`+0x48` is read as a `u16` heading. These fields are confirmed in the helper
used by `0x0051A5B0`; the record's class/name and whether it is a fortress
gate or another siege location remain unresolved.

The odd/interior offsets in decompiler output are not Go protocol layouts and
must not be aligned or renamed without another binary confirmation pass.

## SQL call inventory

### Stock GameServer result

The checked-in SR_GameServer SQL catalog contains 142 distinct SQL-bearing
string literals as an auditable lower bound. Filtering those literals for
`teleport`, `return`, `ferry`, `caravan` and `transport` finds no
travel-specific SQL statement or stored-procedure call. The binary does prove
many generic character/inventory/gold calls, but a generic `_Char` or gold
update must not be labeled as a return-scroll call without a travel call chain.

Therefore the binary-confirmed travel SQL inventory is currently:

```text
no travel-specific destination SQL recovered
```

This is a result, not an omission. Travel reference data appears to be part of
the executable's reference-data system; the database procedure bodies and any
external data-loading queries are not present in the static SQL-string pass.
The return-scroll item path is a qualified exception at the generic inventory
boundary: after a successful TypeID-specific callback, `0x00510980` reaches
`_STRG_DEL_ITEM` when the consumed stack reaches zero. That proves item
consumption persistence, not persistence of the bind/reverse destination or
the queued travel action.

### AegisCore SQL contract (not stock-binary proof)

| Operation | SQL objects/calls |
| --- | --- |
| Teleport-network quote | `TELEPORT_LINK` joined to enabled `TELEPORT_POINT` owner/target |
| Teleport commit | `SELECT CHAR ... FOR UPDATE`; repeat link lookup; `UPDATE CHAR` for gold, region and position |
| Teleport designation | `SELECT CHAR ... FOR UPDATE`; enabled `TELEPORT_POINT` lookup by NPC reference; `UPDATE CHAR.RETURN_TELEPORT_ID` |
| Ferry catalog | migration `000074_ferry_teleport_catalog` adds enabled ferry `TELEPORT_POINT` rows and bidirectional `TELEPORT_LINK` rows |
| Bind-city return | `CHAR.RETURN_TELEPORT_ID` joined to `TELEPORT_POINT`; insert/update `CHAR_REVERSE_RETURN_POSITION`; `UPDATE CHAR` |
| Reverse option 2 | `SELECT CHAR_REVERSE_RETURN_POSITION ... FOR UPDATE`; `UPDATE CHAR` |
| Reverse option 3 | `SELECT CHAR_DEATH_POSITION ... FOR UPDATE`; `UPDATE CHAR` |
| Reverse option 7 | `REF_OPTIONAL_TELEPORT` joined to region/game-server catalogs with service and level filters; `UPDATE CHAR` |
| Death capture | insert/upsert `CHAR_DEATH_POSITION` from the last valid `CHAR` position |
| Item consumption | decrement `ITEM.STACK_COUNT` or delete the `INVENTORY` link |

These calls are parameterized AegisCore SQL and are not claims about the stock
executable's SQL, transaction boundaries or stored-procedure bodies.

## Open binary work

The remaining travel-specific reverse-engineering work is narrow:

1. recover the complete `0xB059`, `0xB05A`, `0xB05B` and loading-reset payloads
   from GameServer constructors rather than client handlers;
2. recover the complete client-side `0xB05A`/`0xB05B` handler payload behavior
   and correlate it with the server's response constructors;
3. identify whether the TestIn build has a distinct ferry/transport handler
   outside the `0x7000..0x77FF` client table or routes those actions through
   generic NPC/teleport handlers;
4. correlate any external reference-data loader SQL with the matching
   database build before adding a travel SQL claim;
5. keep job caravan, ride transport and ferry semantics as separate domains.
