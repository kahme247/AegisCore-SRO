# SR_GameServer party systems

This is the binary-first recovery for the pinned TestIn `SR_GameServer.exe`.
It covers the server-side party core, EXP and item-distribution evidence,
party matching, automatic matching boundaries, and the feature the client UI
usually calls academy (the executable calls it **Training Camp**).

The document intentionally separates facts from names inferred from the
client-facing opcode catalog:

- **Confirmed** means an instruction, memory access, call, embedded string or
  packet-construction operation is present in the executable.
- **Derived** means a deterministic calculation from confirmed values, such as
  the opcode for a dispatcher slot.
- **Corroborated** means the current emulator or client naming agrees with the
  binary, but the name is not independently proved by the binary path.
- **Unresolved** means the behavior or field meaning still needs a packet
  capture, stored-procedure body, or a deeper call trace.

## Evidence identity

- Binary: `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Size: `8,925,184` bytes
- Architecture: 32-bit Windows x86
- Saved program: `analysis/ghidra-projects/vsro-testin-gameserver`
- Primary dispatcher initializer: `0x0050D0C0`
- Game initialization: `0x00412C20`

Addresses and offsets below are specific to this hash. They must not be
transferred to the vSRO 1.274 executable in the repository.

## Lifecycle

### Startup

`0x00412C20` creates the social/gameplay managers after configuration and
world initialization. The relevant confirmed sequence is:

1. `0x00427050` loads runtime settings, including the party EXP and item-drop
   factors described below.
2. The object factory and world managers are initialized.
3. Scheduled jobs and their executor are registered.
4. The skill system and security manager are initialized.
5. The **Party Manager** is created and logs `Party Manager is successfully
   created`.
6. The flea-market manager is created.
7. The **Guild Manager** is created.
8. The **Training Camp Manager** is created and logs `Training Camp Manager is
   successfully created`.
9. Remaining server monitors, quest data and optional event systems are
   initialized.

The ordering is important: party and training-camp objects are live-world
managers, not database-only services. The binary does not show a separate
`AcademyManager`; `CTrainingCamp`, `CTrainingCampManager` and the
`_TRAININGCAMP*` tables are the names that are actually present.

### Runtime and teardown

The party object is a live `CParty` instance. Its recovered destructor at
`0x005BB300` checks a one-byte live flag at object offset `+0x04`, clears it,
then calls two cleanup routines. A second destruction attempt logs
`why do you about to delete already deleted !!! [CParty]` and enters the
binary's assertion/fail path.

`0x005BB770` is the recovered local-member broadcast path. It walks the
intrusive member list rooted at party offset `+0x18`; each node's third word is
the member object pointer. Null member pointers are removed and logged. For a
non-null member the path:

1. optionally excludes the supplied member ID;
2. resolves the member's client context;
3. queues the message for each local client;
4. posts the accumulated local count to the message facility;
5. synchronizes the player message-state fields at `+0x103C` and `+0x103E`;
6. calls the party-manager virtual at slot `+0x4C`.

This proves local fan-out and the member-list ownership boundary. It does not
prove that a party is restricted to one game world or one AgentServer; those
checks occur in callers and in the world/session objects.

## Party configuration and distribution modes

`0x00427050` initializes four relevant global values. Each optional setting is
read as an integer, divided by the executable's global scale at
`0x00B45AF0`, and clamped back to a default when below the accepted range.

| Setting string | Global | Confirmed role |
| --- | ---: | --- |
| `ExpRatio` | `0x00C82518` | Individual EXP gain factor |
| `ExpRatioParty` | `0x00C8251C` | Party EXP gain factor |
| `DropItemRatio` | `0x00C82520` | Item-drop factor |
| `DropGoldAmountCoef` | `0x00C82524` | Gold-drop amount factor |

The executable logs either the configured factor or `system default` for the
first three. This is configuration evidence, not the full award algorithm.

### EXP distribution

The exact award path is `0x004EA6A0`; its party-member pass is
`0x004FCB00`. Both call the same formula helpers, `0x00410110` for EXP and
`0x004106B0` for SP. The helpers return the base values from the supplied
progression parameter, and the award path applies this confirmed
fixed-point/rounding sequence (the x87 helpers are `0x009FBB40` and
`0x009FBB76`):

```text
if awardMultiplier > 0:
    E0 = round(BaseEXP(P) * 0x00B45CE0 * awardMultiplier)
    S0 = round(BaseSP(P)  * 0x00B45CE0 * awardMultiplier)
else:
    E0 = round(BaseEXP(P) * 0x00B45CE0)
    S0 = round(BaseSP(P)  * 0x00B45CE0)
```

The signed input is first converted to float; negative inputs receive the
binary's `0x00B45BA4` correction before entering both formula helpers. If the
resulting base pair is non-positive, the award path skips the main award and
continues to the party-member pass.

The selected rate is `0x00C8251C` when the player has a party object at player
offset `+0x72E`, otherwise `0x00C82518`. With award flag bit `2`, the rate is
compared with threshold `0x00B46178`. The lower-rate branch subtracts
`0x00B45AC0` from the selected rate, multiplies the already scaled EXP/SP
pair, and rounds both results. The other rate branch leaves the base pair as
the main award. This is an instruction-confirmed rate boundary; the binary
does not give the globals friendly unit names.

Optional adjustment values are read through `0x004B3740` from offsets
`0x100`, `0x102`, `0xBA`, `0xCA`, and, when the corresponding award flags are
set, `0xC2`, using the player-state base at `player + 0x1EC`. The
`0x100/0x102/0xBA/0xCA` values are combined as
EXP/SP adjustment pairs, divided by `0x00B45AF0`, multiplied by the base
award and rounded. The `0xC2` pair is added as a further signed adjustment.
The binary clamps every negative intermediate EXP/SP pair to zero before
summing the main pair and adjustments. Two later optional terms are gated by
the floating-point globals at `0x00C82568` and `0x00C8256C` and are likewise
rounded before addition.

The final award is a call through player virtual slot `+0x170`. In the party
pass, `0x004FCB00` walks the party list rooted at `EAX + 0x5C`; each member
node's player is at member offset `+0x0C`. It repeats the base EXP/SP formula,
selects the party rate, applies the same threshold and optional adjustment
branches, clamps negative values, and calls that member player's virtual
slot `+0x170`. This proves the lifecycle and arithmetic boundary, but not
the semantic names of every optional adjustment or the client display packet.

The binary also contains `IGObj::GetPartyInfo` and the group/restriction
strings `GROUPMODE_PARTY`, `GROUPMODE_PARTY_SETUP`,
`NOTEXIST_PARTY_RESTRICTION_EXP` and `TARGET_PLAYER_PARTY_MASTER`. The exact
eligibility predicate that chooses members before `0x004FCB00`, quest-EXP
callers, and equal/shared UI terminology remain separate questions; they are
not inferred from the formula itself.

The maintained Agent preserves the separate rate boundary in
`GAMEPLAY_PROGRESSION_RATES.PARTY_EXPERIENCE_RATE_PERCENT` (migration `000075`)
and uses a deterministic remainder-conserving split. That is emulator
behavior informed by the binary rate boundary, not a claim that the original
executable uses that split algorithm.

The repository now also contains an isolated `BinaryPartyExperience` calculator
in `agent-server/service/binary_party_experience.go`. It implements the
recovered award-stage constants, rounding, rate branch, state-offset additions,
and clamping, while accepting the outputs of `0x00410110`/`0x004106B0` as
explicit inputs. Combat does not silently substitute `NPC.ExperienceReward()`
for those missing base-helper inputs; enabling the compatibility path requires
the remaining reference/player formula boundary to be supplied.

### Item distribution

`DropItemRatio` is confirmed as a global drop factor. The binary also contains
`NOTEXIST_PARTY_RESTRICTION_ITEM` and party-aware target/group strings, proving
that item eligibility can depend on party state. The pinned executable's
recovered party files do not identify the item-owner rotation or random
selection state. The external vSRO packet reference corroborates the
server-to-client distribution notification `0x3068`:

| Branch | Payload after `memberInfo.JID:u32, RefItemID:u32` |
| --- | --- |
| `TypeID1 == 3, TypeID2 == 1` | `OptLevel:u8` |
| `TypeID1 == 3, TypeID2 == 2` | no additional bytes |
| `TypeID1 == 3, TypeID2 == 3` | `Quantity:u16` |

The branch body is packet-reference corroboration, not a claim that the
`0x3068` constructor has been found in the pinned GameServer.

No party-specific SQL literal was found in the saved SQL-string inventory.
The likely persistence boundary is the normal character/storage item path,
not a `_PARTY` table; that is an implementation hypothesis until the monster
drop and inventory commit callers are traced together.

The maintained Agent exposes the party-side decision boundary as
`PartyService.SelectItemRecipient` and `AssignItemOwner`, and its
`GroundItemService` emits the corroborated `0x3068` packet to connected party
members after ownership is assigned. They preserve live-member party order
and rotate a cursor after a successful assignment; item-share mode deliberately
returns an open-ownership result. This is deterministic emulator behavior, not
a claim that the pinned binary's unresolved drop-owner algorithm is round-robin.

## Client request dispatcher and opcodes

The dispatcher at `0x0050D0C0` initializes a 2,048-entry table for opcodes
`0x7000..0x77FF`. A slot is eight bytes and begins at `0x00CD3D08`:

```text
slot = 0x00CD3D08 + (opcode - 0x7000) * 8
opcode = 0x7000 + (slot - 0x00CD3D08) / 8
```

The following party entries are exact binary slot-to-entry-point mappings.
`LAB_*` is Ghidra's label because that entry has not been promoted to a
function in the saved project; it is still an exact code address.

| Opcode | Slot | Handler entry | Binary recovery label |
| ---: | ---: | ---: | --- |
| `0x7060` | `0x00CD4008` | `0x005143B0` | party create |
| `0x7061` | `0x00CD4010` | `0x00514530` | leave; resolves player ID and calls `0x005144E0(2)` when party exists |
| `0x7062` | `0x00CD4018` | `0x00514560` | party invite |
| `0x7063` | `0x00CD4020` | `0x005146D0` | kick; validates master/requester and calls `0x005BE1A0(party,4)` |
| `0x7069` | `0x00CD4050` | `0x00514750` | matching register |
| `0x706A` | `0x00CD4058` | `0x005147C0` | matching operation; validates/submits party-job type `11` through `0x005BF500` |
| `0x706B` | `0x00CD4060` | `0x00514810` | matching operation; submits party-job type `12` through `0x005BF530` |
| `0x706C` | `0x00CD4068` | `0x00514860` | matching operation; submits party-job type `13` through `0x005BF560` |
| `0x706D` | `0x00CD4070` | `0x005148B0` | matching join/response; parses input and reaches `0x005BF740`/`0x005BF6F0` |
| `0x706E` | `0x00CD4078` | `0x00514900` | matching completion/player-join path |

The client-facing aliases in `framework/network/opcode/party.go` corroborate
the names for `0x7060..0x706D`, but the names in the last column are only
binary-confirmed where the recovered function contains the corresponding
operation or embedded diagnostic.

Academy/Training Camp request slots are:

| Opcode | Slot | Handler entry | Binary recovery label |
| ---: | ---: | ---: | --- |
| `0x7470` | `0x00CD6088` | `0x00518FD0` | create |
| `0x7471` | `0x00CD6090` | `0x00519120` | result/disband path; success calls `0x005DF140`, error `0x681D` emits `0xB471` |
| `0x7472` | `0x00CD6098` | `0x00519230` | invite; validates target type `0x26`, PC/level/camp restrictions, calls `0x005DF8B0` |
| `0x7473` | `0x00CD60A0` | `0x005193A0` | kick |
| `0x7474` | `0x00CD60A8` | `0x00519510` | leave; calls `0x005DEEB0(1)` |
| `0x7475` | `0x00CD60B0` | `0x00519600` | member/grade operation; validates level bounds and calls `0x005DEF20` |
| `0x7476` | `0x00CD60B8` | `0x00519770` | member operation; guards levels `<0x29`/`<0x3D`, calls `0x005DEF20(iVar2,0,1)` |
| `0x7477` | `0x00CD60C0` | `0x00519900` | comment/action; builds temporary records and calls `0x005DF070` |
| `0x7478` | `0x00CD60C8` | `0x00519AD0` | honor/ranking path |
| `0x747A` | `0x00CD60D8` | `0x00519B80` | wrapper around `0x005DF1B0` |
| `0x747B` | `0x00CD60E0` | `0x00519BD0` | wrapper around `0x005DF1E0` |
| `0x747C` | `0x00CD60E8` | `0x00519C20` | wrapper around `0x005DF210` |
| `0x747D` | `0x00CD60F0` | `0x00519C70` | wrapper around `0x005DF240` |
| `0x747E` | `0x00CD60F8` | `0x00519CC0` | wrapper around `0x005DF260` |
| `0x747F` | `0x00CD6100` | `0x00519D10` | matching completion/response wrapper; calls `0x004168F0(0xB47E)` |

The `0x7479` slot is not populated in the recovered initializer. The
adjacent `0x747F` slot is populated and its wrapper is recovered, but the
direction and complete payload of the resulting `0xB47E` response still need
capture evidence; the external protocol catalog's `0x347F` name must not be
copied onto this incoming dispatcher entry.

Confirmed outbound constructors seen in these paths, plus separately labeled
packet-reference corroboration, include:

| Opcode | Runtime path | Confirmed observation |
| ---: | ---: | --- |
| `0xB06D` | `0x00514900` | matching completion path emits a response; exact payload branches are unresolved |
| `0x3068` | external packet reference; maintained `GroundItemService` | party item distribution notification; branch suffixes are corroborated, pinned constructor not recovered |
| `0xB470` | `0x00518FD0` | create/error path emits a response with three serialized local values |
| `0x3C80` | `0x005DDBE0` | member deletion/deactivation path emits three values after a member transition |

The `0x306E` and `0x3C81` aliases used by the current network package are not
entries in this client-request table. They may be server/world notifications
or a direction-specific alias; captures are required before treating them as
requests or responses for this binary.

The maintained Agent keeps that direction distinction explicit: `0x706D` is
the corroborated server-to-master approval prompt, while `0x306E` is the
client approval answer. It also accepts the pinned TestIn dispatcher alias
`0x706E` at the same guarded answer boundary. The answer handler ignores the
echoed opaque `u32` and resolves the applicant from the pending server-side
request, preventing a client from substituting another character ID. This is
a maintained safety correction around the corroborated client behavior, not a
claim that the original `0x00514900` payload has been fully recovered.

## Recovered packet and object structures

These are field observations, not stable C++ declarations.

### Party invite/create paths

`0x005143B0` (slot `0x7060`) first calls `0x00513F90` for request validation.
It selects a packet reader based on player offset `+0x1048`, then resolves a
target with `0x004A8E10(..., 0x26)`. The target must pass the PC virtual check
at vtable slot `+0x1C`; a successful path calls `0x005BE280` with the target
and a parsed local value. Failure synchronizes `+0x103C` from `+0x103E` and
passes a 16-bit error to `0x004E7250`.

`0x00514560` (slot `0x7062`) reads the current party object at player array
offset `0x72E`. It checks the inviter/master relation and the settings byte at
party offset `+0x10`. The binary confirms these capacity branches:

- settings bit 0 clear: the allowed member-count field at `+0x1C` must be 4;
- settings bit 0 set: the allowed member-count field at `+0x1C` must be 8.

The success path calls `0x005BE2F0` with the target and the settings byte.
The failure paths emit error values `0x2C13` or `0x2C14`. The meaning of
settings bits 1 and 2 is not proved by this path; current emulator names such
as item-share and guest-invite are corroborating labels only.

### Party-matching record

`0x005BAE80` serializes a party-matching record from a live player/party
object. The destination record fields observed by the decompiler are:

| Record offset | Width | Confirmed source/use |
| ---: | ---: | --- |
| `+0x04` | 4 | source virtual slot `+0x08` |
| `+0x1C` | string/object | copied from source name virtual slot `+0xEC` when source type is 4 |
| `+0x24` | string/object | copied from source nickname virtual slot `+0xF0` for the other source type |
| `+0x40` | 4 | source virtual slot `+0x04` |
| `+0x44` | 1 | source virtual slot `+0x124` |
| `+0x45` | 1 | source virtual slot `+0x11C` |
| `+0x46` | 1 | packed two nibbles from two current/max stat ratios |
| `+0x48` | 2 | copied from source word index `0x21` |
| `+0x4C,+0x50,+0x54` | 4 each | values returned by `0x009FBB40`; semantic name unresolved |
| `+0x58` | 4 | first word returned by source virtual slot `+0x14` |
| `+0x5C,+0x60` | variable | populated by `0x005A1890` |

The packed byte at `+0x46` is derived as two rounded ratios multiplied by
the global factor at `0x00B45C60`: the first ratio is written to the low
nibble and the second to the high nibble. The exact statistic names are not
recoverable from this function alone.

`0x00514750` (slot `0x7069`) calls `0x005BF4D0` to register this data. It
performs a PC check and returns the standard session error path on failure.
The embedded message `OnRegisterPartyReq:: Is not a PC[%s]` confirms the
operation's object-domain check.

### Matching completion

`0x00514900` (slot `0x706E`) parses a completion result with `0x004EA280`.
On the success branch it reads the current player/party state, builds a
matching record through `0x005BAE80`, serializes a `0xFF` marker through
`0x00470BA0`, and emits `0xB06D`. The failure branch emits the same opcode
without the success record. This proves a completion/transition response, but
not the exact client payload because the shared packet writer helpers remain
unnamed.

The asynchronous party job behind the matching operations is also recovered.
RTTI identifies `.?AUtrs_sm_partyjob@@` at type descriptor `0x00C68A98` with
vtable `0x00AE6534`. Builder/handler `0x0046D4B0` stores the query type at job
offset `+0x28` and accepts types `0`, `2`, `10`, `11`, `12`, and `13`;
completion callback `0x0046D3A0` dispatches through `0x005BE680`. Failed
submission enters the job's virtual cleanup path.

The completion sends and state mutations are exact:

| Query type | Completion path | Confirmed result |
| ---: | ---: | --- |
| `0` | `0x005BE710` | resolves two party objects, sends `0xB060` to one and `0xB067` to the other, then clears both stored references |
| `2` | `0x005BE940` | resolves the second party object and sends `0xB067` |
| `10` | `0x005BF7D0` | sends `0xB069`, updates the player state byte, and stores the result at player `+0x734` |
| `11` | `0x005BF870` | sends `0xB06A` |
| `12` | `0x005BF900` | sends `0xB06B`, clears the player state byte and player `+0x734` |
| `13` | `0x005BF980` | sends `0xB06C` and performs the additional state update |

The result opcode names are therefore binary-confirmed even where the
matching payload fields are not. The type names and completion semantics are
not evidence of a particular stored-procedure name; the job is an
inter-server/ShardManager boundary.

### Auto Party Match boundary

The saved server evidence contains `CParty`, `CPartyManager`, the matching
request slots, and the `trs_sm_partyjob` job type. It does **not** contain a
server RTTI/class name equivalent to the client-side `CIFPartyMatchAuto`, nor
does any recovered handler prove an automatic queue-selection or timer
algorithm. Therefore “Auto Party Match” is currently a client/UI feature name
plus an unresolved part of the matching transition, not a binary-confirmed
server subsystem. A packet capture around `0x706E` and the ShardManager party
job completion is required to identify it.

For the maintained Agent, `PartyService.AutoMatch` provides a bounded local
fallback: it chooses the lowest-numbered compatible formed listing whose
purpose and level range contain the applicant, then creates the same pending
master-approval request used by manual matching. It is intentionally isolated
from opcode handlers until the binary's asynchronous completion payload is
recovered, so this fallback must not be described as the original
`trs_sm_partyjob` implementation.

The maintained opcode handlers now enforce the matching lifecycle around the
recovered request shapes: form uses the injected `PartyService`, update only
mutates an existing listing owned by the authenticated master, delete removes
only that master's listing, and list uses the handler's configured service
with bounded page metadata. The response envelopes remain the corroborated
vSRO 1.188 forms; the pinned executable's unresolved label bodies still do not
justify renaming these implementation fields as original C++ structures.

The maintained response adapter also follows the corroborated result details:
successful `0xB06D` carries a little-endian `joinResult:u16` (`2 = accepted`,
`1 = denied`), and successful `0xB06B` carries the deleted matching ID after
its result byte. Academy matching-list serialization keeps the durable leader
`CHAR.id` separate and resolves the required live leader UID only when
building `0xB47D`.

### Training Camp member and buff records

The binary's RTTI and SQL metadata prove these server-side record/table names:

- `CTrainingCamp`, `CTrainingCampManager`, `CTrainingCampCommon`;
- `TrainingCampMember`, `TrainingCampMemberBuffData`;
- `CInstanceTrainingCamp`, `CInstanceTrainingCampMember`;
- `CInstanceTrainingCampHonorRank`, `CInstanceTrainingCampBuffStatus`;
- `_TRAININGCAMP`, `_TRAININGCAMPMEMBER`, `_TRAININGCAMPHONORRANK`,
  `_TRAININGCAMPBUFFSTATUS`.

`0x005E16A0` confirms the honor-ranking completion object fields at offsets
`+0x2C` (current owner/manager), `+0x30` (rank-record list) and `+0x34`
(rank count). Counts above `0xFF` enter an assertion path. Each rank record is
resolved through `0x004DDDE0` and applied through `0x005DD1C0`.

## Academy / Training Camp lifecycle

The request paths at `0x00518FD0`, `0x00519120`, `0x00519230`, `0x005193A0`, and `0x00519770` share a
common shape:

1. confirm that the player has a live training-camp object at player array
   index `0x733`;
2. load the packet payload through the player/world packet reader;
3. resolve the camp with `0x005E42C0`;
4. perform a member/camp-specific operation;
5. return a 16-bit error through `0x004E7250` on failure;
6. destroy temporary encoded strings/objects before returning.

`0x00518FD0` is the create path. A failed precondition emits `0x680B` or
`0x6816`; a failed camp resolution emits `0x6815`; later failures include
`0x6820`. One error branch emits `0xB470` and three serialized values.

`0x005193A0` is a member-removal path: it checks the camp, resolves a member
operation, and calls `0x005DEEB0(2)` after the camp/member checks pass. The
current opcode alias calls this kick, which is consistent with the branch but
the original C++ symbol is not present.

`0x00519770` performs another camp member operation and adds two level/range
guards (`< 0x29` and `< 0x3D`) before calling `0x005DEF20(iVar2, 0, 1)`. Its
UI name is unresolved in the binary and must not be silently called grade,
comment, or invite.

Member deletion has a second lifecycle path at `0x005DDBE0`: it emits the
`0x3C80` update, then calls `0x005E1B10(..., 9, 0, memberID, ...)` to
deactivate the mentor's EXP buff. If that transition fails the binary logs
`CTrainingCamp::MemberDeleted Mentor's ExpBuff Deactivate Failed!!`.

The recovered constructor writes the following confirmed member-transition
shape (little-endian packet fields):

```text
0x3C80
  updateType:u32 = 3
  memberID:u32   = MemberDeleted member argument
  state:u8       = MemberDeleted state argument
```

The external packet reference independently documents another `0x3C80`
branch, `updateType == 5`, as a variable-length honor-balance update beginning
with a character ID and ending with a new honor balance. That branch is useful
wire corroboration, but its complete field widths and the pinned binary's
callers were not recovered; it must not be substituted for the binary-proven
member-deletion shape above.

### Training Camp `0x3C81` serialization boundary

The normal member-state sender is `0x004DF9E0`. It resolves the live camp
through `0x005E1860`, prepares the member state with `0x005DCDE0`, allocates
packet opcode `0x3C81` through the player virtual slot `+0x278`, invokes the
camp virtual slot `+0x10`, and sends the resulting packet through player slot
`+0x27C`. It then emits the related `0xB483` buff-status packet when active
camp buffs exist.

RTTI identifies `CInstanceTrainingCamp` at type descriptor `0x00C7AEE4` and
vtable `0x00B0D3E0`. Its slot `+0x10` is `0x0042EFB0`; that method validates
the embedded camp/member state through its `+0x0C` accessor and dispatches the
next virtual operation (`0x00824D30`).

`0x005DCDE0` confirms the prepared member fields before the packet call:

| Object/offset | Width | Confirmed write |
| --- | ---: | --- |
| camp/member `+0x38` | pointer | player pointer |
| player `+0x733` | pointer | member object |
| member `+0x08` | `u32` | value returned by the player's virtual `+0x14` accessor |
| member `+0x0C` | `i16` | player's recovered level-related word |
| member `+0x0E`, `+0x10`, `+0x12` | `u16` each | three values returned by `0x009FBB40` |

This proves the `0x3C81` sender, validation/serialization call chain, and
prepared state. The absolute wire field order, widths of the values emitted
by `0x00824D30`, and complete packet payload are still unresolved; no field
layout is guessed from the opcode number.

## SQL and inter-server calls

### Party

The party-specific database boundary recovered in this corpus is an
asynchronous/inter-server job object named `trs_sm_partyjob`, logged by
`0x0046C620` with a one-byte query type at job offset `+0x28`.

The string is **not** a SQL statement. It identifies a GameServer to
ShardManager/SM overlapped job. No `_PARTY` stored-procedure literal was found
in the executable's party-related SQL inventory. Party creation, membership,
matching registration and local broadcast are therefore confirmed as
in-memory operations in this recovery; any persistent side effects need the
ShardManager path or the matching stored-procedure set.

### Training Camp

The database job is `AQ_TrainingCampQuery`. Its completion function
`0x005E13C0` distinguishes query types:

| Query type | Confirmed completion meaning |
| ---: | --- |
| `0` | update/reload honor ranking |
| `1` | update EXP save-up data |
| `2` | update EXP buff data |

The query builder at `0x004414C0` stores the query type at AQ offset `+0x28`,
the member/camp context at `+0x38`, a second argument at `+0x3C`, and a mode
byte at `+0x40`. It then uses the generic SQL builder `0x009742D0` with the
generated text at AQ offset `+0x60`:

- type `1` resolves camp/member/buff-save-up state through `0x005E3A00` and
  builds an EXP save-up update;
- type `2` resolves camp/member/buff state, selects the donor/current buff
  path based on the mode byte, calls `0x005E1E90` and `0x0044A330`, optionally
  calls `0x0044A300` when the donor ID is not `-1`, and builds an EXP-buff
  update;
- type `0` is completed by `0x005E16A0`, which reloads ranking records and
  applies them to the live manager.

Confirmed SQL metadata includes the four `_TRAININGCAMP*` tables and these
where-clause fragments:

```sql
WHERE CampID = %d
WHERE CampID = %d AND RecipientCharID = %d ORDER BY BuffSlotIdx ASC
```

The preserved ShardManager SQL-string recovery adds the following exact
queries/procedure calls to that boundary:

```sql
declare @CampID int;
exec @CampID = _GetCharTrainingCamp '%s';
select [ID], Rank, GraduateCount, EvaluationPoint
from _TrainingCamp with (nolock)
where @CampID > 0 and [ID] = @CampID;

declare @CampID int;
exec @CampID = _GetCharTrainingCamp '%s';
select CampID, CharName, MemberClass, HonorPoint
from _TrainingCampMember with (nolock)
where @CampID > 0 and CampID = @CampID;

exec _SMC_DEL_TRAINING_CAMP_MEMBER %d, '%s';
```

The same recovery logs `Update TrainingCamp Member Data(CampID: %d,
Member: %s)` immediately around the member-update path. These strings are
binary/ShardManager evidence, not SQL used by the maintained normalized
`ACADEMY` repository; translating them into the latter schema requires a
separate compatibility migration and must not silently be treated as an exact
stored-procedure port.

The executable also contains generic templates such as `UPDATE %s SET ...`
and `SELECT * FROM %s`; the exact column list is built from metadata and is
not present as one literal. Stored-procedure bodies, transactions, locks and
result-set schemas are therefore unresolved.

### Atomicity boundary

Procedure/table names and query-type dispatch are confirmed, but this binary
does not prove transaction isolation, rollback, advisory locking, or whether
the GameServer waits for all queued SQL before a member transition is visible.
Those claims require the matching database procedures and the ShardManager
completion path.

### Maintained Agent party-to-loot handoff

`PartyService.AssignItemOwner` is the policy boundary for a newly materialized
ground item. `GroundItemService.MaterializeAndSpawn` can now take a
`GroundItemSpec` through the injected SQL `ITEM` materializer, then selects
the next connected member in deterministic party order, emits `0x3068`, and
publishes the item. The normal player pickup handler enforces that runtime
owner before entering the transactional `ITEM`/`INVENTORY` claim path
documented in `sr-gameserver-drops-loot.md`. This is an emulator-side
lifecycle contract, not evidence that the pinned executable's original drop
factory has been fully connected to its SQL worker.

## What remains unresolved

- The maintained Agent now has a binary-shaped direct/random-group/gold
  selector and a post-death `MonsterDropService` hook. Hydrating that selector
  from the pinned TestIn reference tables and reproducing the original
  GameServer SQL-worker completion still requires the missing loader/query
  evidence; the SQL `ITEM` materializer and party ownership/`0x3068` handoff
  are implemented.
- The maintained normalized `ACADEMY` repository implements durable create,
  join, leave, kick, graduate, comment, matching, and ranking operations. The
  exact Training Camp member snapshot/buff notification layouts remain
  suppressed until their stored-procedure and wire schemas are recovered.
- Trace the monster-death caller that selects eligible party members before
  `0x004FCB00`; the award arithmetic and `ExpRatioParty` rate boundary are
  recovered, but the caller's level-distance/filter policy is not.
- Trace item-drop ownership/rotation and its inventory/storage SQL completion.
- Capture matching registration, update, list, join and auto-match packets to
  recover variable-length strings and response layouts.
- Recover the Training Camp stored-procedure bodies and exact SQL column
  bindings.
- Recover the full absolute `0x3C81` packet field layout and capture its
  direction-specific relationship to `0x3C80`/`0x306E`.
- Correlate `0x306E`, `0x3C80`, `0x3C81` and `0xB06D` with direction-specific
  captures before changing the current opcode aliases.

Primary artifacts used for this recovery:

- `analysis/decompiled/gameserver-quest-party/`
- `analysis/decompiled/gameserver-sql/004414C0_FUN_004414c0.c`
- `analysis/decompiled/gameserver-sql/005DDBE0_FUN_005ddbe0.c`
- `analysis/decompiled/gameserver-sql/005E13C0_FUN_005e13c0.c`
- `analysis/decompiled/gameserver-sql/005E16A0_FUN_005e16a0.c`
- `analysis/decompiled/alchemy-shardmanager/sql-strings/00486A90_FUN_00486a90.c`
- `analysis/decompiled/alchemy-shardmanager/sql-strings/004883E0_FUN_004883e0.c`
- `.references/online-opcode-research/SilkroadDoc.wiki/AGENT_PARTY_DISTRIBUTION.md`
- `_docs/evidence/sr-gameserver-testin-opcodes.csv`
- `framework/network/opcode/party.go`
