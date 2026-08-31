# SR_GameServer alchemy subsystem

This is the binary-first reference for the alchemy subsystem in the pinned
TestIn `SR_GameServer.exe`. It covers the client request boundary, the delayed
operation lifecycle, the recovered object/state layouts, response opcodes and
the SQL boundary. Names are used only where MSVC RTTI/vftable symbols or
embedded strings identify them.

## Evidence identity and limits

- Binary SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Size: `8,925,184` bytes; 32-bit x86 PE
- Saved Ghidra program: `analysis/ghidra-projects/vsro-testin-gameserver:/SR_GameServer.exe`
- Focused exports: `analysis/decompiled/alchemy-gameserver/`
- Client corroboration: `.references/testin-decompilation/notes/vsro-client-3df2282d-alchemy-7150.md`
- Analysis date: 2026-07-19

**Confirmed** below means an instruction, memory access, branch, vftable,
serialization call or literal string exists in this binary. **Derived** means
only deterministic address/arithmetic interpretation was added. Names from
third-party bots and emulators are corroboration, not binary proof.

This pass proves the executable probability branches and their reference-data
lookups, but it does not assign a public semantic name to every error code.
The generic database message's write contract was independently traced through
the pinned `SR_ShardManager.exe` and matching restored shard procedures. A
decrypted live capture would corroborate packet timing and error labels, but is
not required for the binary-confirmed algorithms below.

## Dispatcher and opcode map

The GameServer client table at `0x0050D0C0` covers `0x7000..0x77ff`, with an
eight-byte slot at `0x00cd3d08 + (opcode - 0x7000) * 8`. The alchemy entries
are therefore exact, not opcode-name guesses:

| Direction | Opcode | GameServer handler | Confirmed role |
| --- | ---: | ---: | --- |
| C -> S | `0x7150` | `0x00506e20` | general reinforcement/elixir request |
| S -> C | `0xb150` | constructed at `0x00506740`, `0x0052aa90` | reinforcement result/cancel/error |
| C -> S | `0x7151` | `0x00506ee0` | magic/attribute stone request |
| S -> C | `0xb151` | constructed at `0x00506740`, `0x0052aa90` | stone result/cancel/error |
| C -> S | `0x7155` | `0x0050a100` | make-stuff/material family |
| S -> C | `0xb155` | constructed in `0x00509030`, `0x00509fa0` | make-stuff/material result |
| C -> S | `0x7157` | `0x0050c160` | dismantle/equipment-resolution family |
| S -> C | `0xb157` | constructed at `0x0050c160` | dismantle result |
| C -> S | `0x716a` | `0x00506fa0` | socket operation multiplexer |
| S -> C | `0xb16a` | constructed at `0x005076a0`, `0x0052aa90` | socket result/cancel/error |
| C -> S | `0x34a7` | `0x00507910` | avatar magic-option operation |
| S -> C | `0x34a8` | constructed at `0x00506740`, `0x0052aa90` | avatar option result |
| C -> S | `0x34a9` | `0x005079c0` | magic-option grant request |
| S -> C | `0x34aa` | constructed at `0x005079c0` | magic-option update |

`0x7150`, `0x7151`, `0x7155` and `0x7157` also occur in the state-specific
allow lists built at `0x00427d90`. Repeated occurrences there are permission
table population, not duplicate dispatchers.

## Request structures

All fields below are byte-sized unless stated otherwise. The packet reader at
`0x00404f80` advances one byte.

### General enchant (`0x7150` and `0x7151`)

`0x00506e20`/`0x00506ee0` read `action`; action `2` reads `type` and enters
`0x00507cf0`. That function reads `slotCount` and then exactly that many
inventory-slot bytes:

```text
u8 action             // 2 = start/fuse; the other handled path is cancel
u8 type
u8 slotCount
u8 slots[slotCount]
```

Each slot must be in the inclusive inventory encoding range `0x0d..0x6f` and
must resolve through the character inventory lookup (`vfunc +0x578`). This
confirms why client code stores inventory indices as `wireSlot - 0x0d` and
serializes them as `index + 0x0d`.

The first slot is validated by a type-specific virtual operation; subsequent
slots are added through the shared material path. Empty lists and any invalid
slot abort, reset the packet cursor to its end and clear the alchemy action.

Confirmed type-to-operation mapping:

| Type | Constructed class | Size | Response |
| ---: | --- | ---: | ---: |
| `3` | `CEnchant_Reinforce` | `0x44` | `0xb150` |
| `4` | `CEnchant_EnchantMagicStone` | `0x44` | `0xb151` |
| `5` | `CEnchant_EnchantAttrStone` | `0x44` | `0xb151` |
| `6` | rejected/default branch | - | `0xb150` error path only |
| `7` | `CEnchant_EnchantAvatarItem` | `0x60` | `0x34a8` |
| `8` | `CEnchant_UpperReinforce` | `0x48` | `0xb150` |
| `9` | socket insert operation (separate path) | `0x48` | `0xb16a` |

Types 3/8 sharing `0xb150`, 4/5 sharing `0xb151`, and type 7 using `0x34a8`
are selected directly by the switch at `0x00506740`.

### Socket (`0x716a`)

The handler reads two leading bytes and dispatches three RTTI-confirmed socket
operation classes:

```text
u8 action
u8 socketSubType
... action-specific body
```

| Action | Subtype | Remaining request body | Constructed class | Execution |
| ---: | ---: | --- | --- | --- |
| `1` | echoed in cancel response | none confirmed | none | cancels an active socket insert |
| `2` | `9` | `u8 targetSlot, u8 stoneSlot, u8 socketIndex` | `CEnchant_SocketStoneInserter` | delayed through type-9 character state |
| `3` | `8` | `u8 targetSlot, u8 socketIndex` | `CEnchant_SocketMaker` | immediate |
| `4` | `10` | `u8 targetSlot, u8 socketIndex` | `CEnchant_SocketStoneEraser` | immediate |

The action-2 parser at `0x00507420` proves the three trailing bytes. Both
inventory slots must be in `0x0d..0x6f`, resolve to live items and pass the
common item validator. It stores `socketIndex` at character offset `+0x2210`,
sets alchemy type 9 and enters the delayed state machine. Completion at
`0x005076a0` constructs `0xb16a` with leading bytes `1,2`, applies the target
item change accumulator and increments the operation count.

Actions 3 and 4 are handled by `0x005070c0`. Both validate the target slot and
construct a `0x48`-byte operation with `socketIndex` at object offset `+0x44`.
Socket creation additionally searches inventory slots `0x0d..inventoryEnd`
for a qualifying material with the confirmed type-mask tail `0x5000`; erasure
searches for the corresponding material with tail `0x4800`. On success the
server emits `0xb16a` leading bytes `1,3` or `1,4`, applies the target item
change accumulator and completes immediately. The searched material's public
item name is not proved by this binary pass.

The matching client handler `CNetProcessIn::OnB16A` reads the target slot after
successful actions 2 through 4 and then invokes the inventory-item decoder on
the packet remainder. Their bodies are therefore `1, action, targetSlot,
itemRecord`. Target and material changes also travel through the generic
inventory accumulator.

Rejected create/erase requests return `0xb16a` with leading result `2`, the
operation action and a two-byte error code. Confirmed errors include `0x6c06`
for an out-of-range target slot and `0x6c08` when the required inventory
material is absent. Insert failures use the same response family but still
need a complete numeric error-name map.

### Make-stuff (`0x7155`)

`0x0050a100` first reads the standard action byte. Action `2` reads a make-stuff
selector and calls `0x0050a1b0`. The complete common variable-list form is:

```text
u8 action             // 2 = start; other values cancel/reset
u8 selector           // 1 = resolve material; 2 = compound
u8 slotCount
u8 slots[slotCount]
```

Selector 1 constructs the `0x94`-byte `CMakeStuff_ResolveMaterial`; selector 2
constructs the eight-byte `CMakeStuff_Compound` and forces the operation count
to one. Slots are again constrained to `0x0d..0x6f`. The result family is
`0xb155`.

For selector 1, `slotCount` becomes the required-operation count. Duplicate
wire slots are rejected by `0x00509290`. The first accepted component controls
which component class subsequent slots must match; each accepted slot is
stored in the character vector at `+0x2226..+0x222a` as a 12-byte record
containing wire slot, item pointer and item quantity/operation count. Selector
2 requires exactly one operation even though it uses the same parser.

`CMakeStuff_ResolveMaterial` automatically scans the inventory for
`ITEM_ETC_ARCHEMY_RONDO_01` at `0x005085d0`. Absence returns error `0x5806`.
Its execution path emits one `0xb155` result per produced/resolved component,
decrements that component record's remaining count, and resets the phase so
the tick state machine can continue until `completed == required`. Confirmed
validation errors include `0x5807`, `0x5808` and `0x5809`; their public labels
remain unresolved.

The successful `0xb155` body is exactly seven bytes:

```text
u8  result = 1
u8  action = 2
u8  resolveRondoCountOrComponentSlot
u32 producedValue
```

For `CMakeStuff_ResolveMaterial`, the byte is the required Rondo count truncated
to the serialized byte and the dword comes from the source component's virtual
accessor `+0x4e8`. The same value multiplies the per-component yields and the
Rondo formula `(sourceValue / 1000 + 1) * operationValue`.
For `CMakeStuff_Compound`, the byte is the first staged component's wire slot
and the dword is literal `1`. These producer descriptions are binary proof;
calling the dword a reference-object ID or quantity in both variants would be
an unsupported unification.

### Dismantle (`0x7157`)

`0x0050c160` reads `slotCount`, then `slotCount` wire slots. Each must be
`0x0d..0x6f`, resolve to an inventory item and pass the virtual resolver. A
successful operation returns `0xb157`; rejected resolver statuses explicitly
include `2`, `0x5813`, `0x5815` and `0x5817`.

The operation is RTTI-confirmed as `CMakeStuff_ResolveEquip`. Its constructor
allocates `0x74` bytes: owner pointer followed by nine 12-byte
`slot/item/auxiliary` triplets, each initialized with slot sentinel `0xff`.
Consequently the binary can stage at most nine dismantle inputs in one object.
The response itself is deliberately minimal: result byte `1` on successful
completion; result byte `2` followed by a `u16` error on failure. Generated
items and consumed equipment are delivered through the generic inventory
change accumulator rather than embedded directly in `0xb157`.

`0x0050c070` proves several rejection conditions: one item-state predicate
returns `0x5816`, another returns `0x5817`, and existing item properties whose
four-byte tags are `0x6e726570` or `0x00726570` return `0x5813` or `0x5815`.
Semantic names for those predicates/tags require their property registry.

### Dismantle yield and consumption algorithm

`CMakeStuff_ResolveEquip::execute` at `0x0050be60` performs this confirmed
sequence:

1. Calculate required `ITEM_ETC_ARCHEMY_RONDO_02` count as
   `itemVirtualValue(+0x3a8) / 20000 + 1` (`0x0050a8c0`). Sum matching Rondo
   stacks from the staged material vector and reject if insufficient.
2. Build a `0xc4`-byte local yield plan (`0x0050b4e0`). The equipment's
   reference byte at `ref+0x2c` scales two floating ranges used by the random
   count generator.
3. Seed two pairs of output selectors from two independent `rand() % 12`
   choices over the fixed 24-value table
   `{0,1,0,2,0,3,1,0,1,2,1,3,2,0,2,1,2,3,3,0,3,1,3,2}`.
4. Generate two counts for each selector pair through the CRT random helper.
5. Select up to two outputs from type-mask tail `0x1000`; select up to two more
   from tails `0x0800` or `0x3800`. Candidate subtypes are chosen from packed
   reference bytes by `0x0050ae10`.
6. Optionally resolve one `_RefAlchemyMerit` record. Its lookup key is a
   five-byte structure derived from `(ref+0x1a8 + 2) / 3`, the calculated item
   grade, equipment category (mapped to 1 or 2), and the reference TypeID at
   `ref+0x80` (`0x006bf190`, `0x006bf4d0`).
7. Verify inventory capacity, material/item creation, and the proposed change
   set (`0x0050aba0`, repeated independently by `0x0050baf0`).
8. Apply the generic item-change accumulator, log operation `0x89`, consume
   the computed number of Rondo units across stacks, then consume exactly one
   equipment item (`0x0050bd30`).

The yield arithmetic is also recoverable. Let `V` be equipment vfunc `+0x3a8`,
`C` be reference byte `+0x2c`, `M` be reference byte `+0x58`, and `T[G]` the
grade divisor selected by `0x004775a0`:

```text
T = [75,140,215,290,365,458,551,660,776,903,1035,1180]

for each of two selector pairs:
  amountA = round((((rand() % 6) + 15) / 100 + C * 0.025) * V / T[G]) + 1
  amountB = round((((rand() % 6) + 10) / 100 + C * 0.020) * V / T[G]) + 1

typeTail_0x1000_count =
  round(((rand() % 6) + 5) * V / 100 /
        [18750,34950,53700,72340,91150,114280,
         137690,164830,193830,225240,258180,294706][G])

typeTail_0x0800_or_0x3800_count =
  round(((rand() % 6) + 5) * M * V / 100 /
        [18750,34950,53700,72340,91150,114280,
         137690,164830,193830,225240,258180,294706][G])
```

`round` here is the helper at `0x009fbb40`; its normal path implements the
binary's x87-to-integer rounding behavior. The constants are binary doubles:
`0x00b46170 = 0.025`, `0x00b45ec8 = 0.020`, and `0x00b45af0 = 100.0`.

The per-degree selector is weighted, not uniform. `0x0072e140` performs
`rand() % totalWeight`; the restored pinned `_RefDropItemAssign` rows prove
the weights used to populate the canonical stone references:

- magic: STR 30, INT 60, DUR 30, HR 100, EVADE_BLOCK 30,
  EVADE_CRITICAL 30, ER 100, HP 30, MP 60, FROSTBITE/ESHOCK/BURN/POISON/
  ZOMBIE 100 each;
- protection-family: SOLID 300 and LUCK 300; ASTRAL and ATHANASIA have zero
  relative weight and are absent from random dismantle selection;
- attribute: PA/PASTR/MA/MAINT/PD/PDSTR/MD/MDINT/PAR/MAR 60 each, ER/HR 100
  each, and CRITICAL/BR 30 each.

Only canonical `ITEM_ETC_ARCHEMY_{MAGICSTONE,ATTRSTONE}_..._{degree}` rows
with positive relative weight participate. Event/premium aliases and APE are
not silently added as duplicate candidates.

### Dismantle merit and advanced-elixir output

`0x004f2250` constructs seven RTTI-named merit operations in group order:
`RarityGroupOp`, `OptLvGroupOp`, `ATTRGroupOp`, three `AlchemyMeritOp`
instances for groups 3-5, and `ClassGroupOp`. `0x004f2640` combines their
restored `_RefAlchemyMerit` coefficients as follows:

```text
rarity = {normal:1, rare:8, normal-set:3, rare-set:8}
class  = ((ItemClass - 1) % 3) + 1
plus   = 2 for +0..+4, 4 for +5..+6, 6 for +7 and above

score = rarity * class + plus + varianceMerit + magicOptionMerit
grade = 0 when roundToEven(score) < 10
        1 when 10 <= roundToEven(score) < 20
        2 when roundToEven(score) >= 20
```

`varianceMerit` extracts each five-bit variance bucket, divides by 31 and by
100, multiplies it by the group-2 category/stat coefficient, then by the
group-3 correction (`0.6/0.7/0.6/0.6` for weapon/armor/accessory/shield).
The analogous magic-option branch first integer-divides current value by its
maximum and multiplies by the reference magic-option weight and group-4
correction. Every `_RefMagicOpt.Weight` row in the pinned TestIn shard is zero,
so that branch is present in the executable but dormant for this data set.

Nonzero grades map through the five-byte key built by `0x006bf190` to exactly
one canonical recipe. Grade 1 is `A`, grade 2 is `B`; rarity 2 inserts
`RARE_`; the packed TypeID selects `WE`, `SH`, `AR`, or `AC`. The resulting
code form is:

```text
ITEM_ETC_ARCHEMY_UPPER_REINFORCE_RECIPE_{WE|SH|AR|AC}_{RARE_?}{A|B}_{degree}
```

One recipe item is appended to the atomic dismantle output plan.

## Per-character state and lifecycle

The alchemy state lives inside the character/player object:

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x2211` | `u8` | active alchemy type (`3..9`) |
| `+0x2212` | `u32` | operation-active flag; compared with `1` |
| `+0x2216` | `u32` | animation/delay phase counter |
| `+0x221a` | `u32` | required result/material-operation count |
| `+0x221e` | `u32` | completed result/material-operation count |
| `+0x2226` | pointer | begin of make-stuff component vector |
| `+0x222a` | pointer | end of make-stuff component vector |

The confirmed state machine is:

1. **Parse and validate.** The handler selects an operation class, resolves
   every inventory slot and invokes class-specific validation.
2. **Enter action state.** GameServer calls character vfunc `+0x2ac` with
   action/status `0x0f,2`, stores type, sets `active=1`, zeros `phase` and
   initializes required/completed counts.
3. **World ticks.** `0x0052aa90`, the character update path, observes the
   active flag. While the character is in the disallowed body state it emits
   the operation-family cancellation response and clears the action.
4. **Delay.** On each active update, `phase` increments. When it equals `3`,
   the operation executes. This is a tick count, not proof of a fixed number
   of wall-clock seconds.
5. **Resolve.** Types 3-8 execute through `0x00506740`; type 9 uses
   `0x005076a0`; make-stuff selectors use `0x0050a460`.
6. **Serialize result.** The matching client handlers prove that `0xb150` and
   `0xb151` read `result=1, action=2, outcomeBit, targetSlot`, then decode the
   target item record from the packet remainder. When the `0xb150` outcome is
   zero it first reads one additional destruction bit and omits the item only
   when destruction is true. Item mutation/destruction is also carried by the
   separate generic change path.
7. **Apply/persist.** The global change accumulator at `0x00cd3a80` is cleared,
   populated and applied via character vfunc `+0x498`. The operation object is
   told its result through vfunc `+8`.
8. **Count and leave.** `completed` increments and `phase` resets to zero. When
   `completed == required`, the operation state is cleared and character
   action `0x0f,0` is emitted.

### Cancellation and errors

For `0x7150`/`0x7151`, any action other than start reaches the cancellation
branch only when an operation is active and has not reached phase 3. It clears
the state and emits the matching response with leading result byte `1` and the
request action byte. Invalid construction/validation clears the state with
action `0x0f,0`; several paths return a two-byte error value, but this pass
does not assign semantic names to those numeric codes.

The update loop also cancels an active operation if the character body-state
vfunc returns `2`. This proves that alchemy is interruptible and coupled to
world state rather than being an immediate inventory RPC.

## Recovered operation-object layout

The three basic `CEnchant` derivatives have the same `0x44`-byte initialized
shape. Constructors at `0x00505b80`, `0x00503600` and `0x00502950` write:

```text
+0x00  vftable
+0x04  owner/player pointer
+0x08  u32 slot0 = 0xff
+0x0c  u32 value0 = 0
+0x10  u32 aux0 = 0
... repeated five times with 0x0c stride ...
+0x38  u32 slot4 = 0xff
+0x3c  u32 value4 = 0
+0x40  u32 aux4 = 0
```

The slot sentinel is stored as a 32-bit `0xff`, not merely an uninitialized
byte. `CEnchant_UpperReinforce` appends one `u32` at `+0x44` (total `0x48`).
`CEnchant_EnchantAvatarItem` uses the same five triplets and appends avatar
state through `+0x5f` (total `0x60`).

`CMakeStuff_ResolveMaterial` is `0x94` bytes: owner pointer at `+4`, six scalar
words at `+8..+0x1c`, followed by four embedded `0x1c`-byte records beginning
at `+0x20`. `CMakeStuff_Compound` is only vftable plus owner pointer.

### Item mutation anchors

- `0x00501dc0`, identified by its diagnostics as
  `CEnchant::SetEnchantAttrToAttrAssim`, checks the item pointer and rejects
  negative slot/value inputs before applying an attribute-assimilation value.
- `0x00502040`, `CEnchant::SetEnchantMagicToAttrAssim`, checks the item, magic
  option count and option slot; it rejects non-magic-option slots and reports
  a failed magic-option value update.
- `0x00503810` is the magic-stone `_BlessItem` path and looks up the magic
  option in reference data before applying it.
- `0x00504170` is the equivalent avatar-item blessing path.

These functions prove mutation of the in-memory item/magic-option structures;
they do not by themselves identify probability or persistence contracts.

### Avatar enchant and NPC grant details

`0x34a7` reads only the standard action byte. Action `2` forces shared
enchantment type `7`, so the remaining body is `u8 slotCount` followed by
exactly that many normal-inventory wire slots. Other actions cancel an active
pre-phase-three operation and return `0x34a8` beginning with `1, action`.

The type-7 object is `CEnchant_EnchantAvatarItem` (`0x60` bytes, constructor
`0x00503d60`). Its first component must pass the item's avatar predicate. The
base validator at `0x00501cd0` returns `0x5406` when the target is absent and
`0x5407` when the secondary component is absent; the derived validator at
`0x00503f90` deliberately maps both success and `0x5407` to success. A
secondary material is therefore optional, not packet-required. Execution is
delayed through the same phase-three lifecycle as types `3..8`. Its success
ratio is clamped to `5..100`; the binary diagnostic states that this avatar
path is intended to be 100-percent success. The blessing routine at
`0x00504170` resolves a reference magic option, enforces the reference item's
magic-option capacity (maximum 12), and either appends the option or advances
an existing option value. The generic item-change accumulator carries the
actual item mutation.

The two non-avatar predicates used by its component adder are exact packed
TypeID tests. `0x0042e710` accepts `3/3/11/1`; `0x0046c370` accepts
`3/3/11/7`. Thus the optional secondary component is a magic or protection
stone, not an avatar-inventory item or opaque token. A second component of the
same category is rejected because its staged triplet is already occupied. The
blessing resolves through the target's `_RefMagicOptAssign` catalog rather
than applying the optional stone's ordinary option name directly; a supplied
stone is consumed through the generic item-change path. The pinned assignment
rows are TypeID4-specific: hat (`1`) has six avatar options, dress (`2`) has
twenty, attachment (`3`) has HR/HPRG, and Devil's Spirit (`4`) has
`MATTR_{STR,INT}_AVATAR`.

`0x34a9` is a separate immediate NPC blessing request:

```text
u8 targetSlot         // inclusive 0x0d..0x6f
string optionCode     // packet string read by 0x00404f50
```

The handler reads the slot through `0x00404b60`, then reads the option string
through `0x00404f50`. The slot is resolved from normal inventory and the item
must pass vfunc `+0x454`. The validation method copies the supplied string into
the operation before execution. The constructed `CBless_AvatarItemWithNPC` object is `0x2c` bytes
(`0x005053f0`, vftable `0x00af69e4`). Its execute function at `0x005056e0`
clamps the total ratio to `5..100` but asserts if the roll can fail, with the
literal diagnostic that this operation is 100-percent success. Its blessing
routine at `0x00505810` resolves a reference magic option, enforces the
reference magic-option capacity, appends or advances its value, and logs item
operation `0x94`.

Successful `0x34aa` is exactly:

```text
u8 result = 1
u8 mutationFlag
```

The second byte is written by the operation's execute method. The generic
item-change accumulator is applied only when it equals `1`; no item body is
embedded in `0x34aa`. Invalid slot/item/object paths do not construct a
family-specific failure packet in `0x005079c0`; they enter the common request
rejection path.

## Probability, reinforcement and protection

### Basic reinforcement (`CEnchant_Reinforce`)

The execution function is `0x00506090`. After validation it obtains a chance
from vfunc `+0x1c` (`0x00506020`), clamps that chance to `10..100`, and tests:

```text
success = (rand() % 100) < clamp(chance, 10, 100)
```

The chance is not a hard-coded curve. `0x00505f00` reads one byte from the
primary elixir reference object selected by:

```text
plus       = target.ref[+0x2c]
tier       = clamp(floor(plus / 4) + 1, 1, 3)
column     = 3 - min(plus, 3)
baseChance = byte(elixir.ref.matrix[tier, column])
```

If a probability-up material exists at character `+0x226a`, the same lookup
is performed on its reference object and added to `baseChance` unless the
current sum is already at least 100. Finally, target magic property
`MATTR_LUCK` (raw tag `0x6c75636b`) adds exactly five percentage points and
consumes one property charge. Clamping happens after all additions.

Success increments the target option level by exactly one, capped by the
mutation helper at 250, sets outcome flag `0x01`, and records operation log
`0x5a`. Failure begins with outcome flag `0x08` and records log `0x5b`.

### Reinforcement failure decision tree

Let `P` be the option level before the attempt. `0x00506ce0` implements:

```text
flags = 0x08                         // ordinary failed attempt

if P >= 5:
    if rand() % 100 < 50:
        if target.vfunc(+0xbc) == 0:
            flags |= 0x10            // destruction
    else:
        flags |= 0x02                // maximum-durability penalty

if flags has 0x10 and MATTR_ATHANASIA exists:
    consume one ATHANASIA charge
    flags &= ~0x10

if flags has 0x02 and MATTR_SOLID exists:
    consume one SOLID charge
    flags &= ~0x02

if flags does not have 0x10:
    if flags has 0x02:
        add/increment MATTR_DEC_MAXDUR

    if flags has 0x08:
        if P == 0: flags &= ~0x08
        else if P < 4: set plus = 0
        else if MATTR_ASTRAL exists:
            consume one ASTRAL charge
            set plus = 4
        else:
            set plus = 0
```

Thus this binary does **not** decrement option level by one on ordinary
failure. Levels `+1..+3` reset to zero; `+4` and above reset to zero unless
Astral preserves `+4`. At `+5` and above the independent 50/50 branch can also
destroy the item or add a maximum-durability penalty. Athanasia and Solid
prevent those two branches respectively. The raw four-byte tags are confirmed
by the executable and the full names are corroborated by the pinned reference
data (`MATTR_ATHANASIA`, `MATTR_SOLID`, `MATTR_ASTRAL`, `MATTR_LUCK`).

Outcome flag `0x10` suppresses normal item serialization because the target is
destroyed. Other nonzero flags run the item-change path. The response serializer
therefore uses a bitmask, not a single success boolean.

### Advanced elixir (`CEnchant_UpperReinforce`)

Type 8 does not enter the basic reinforcement probability roll.
`0x005065e0` validates the target and advanced elixir, invokes the class's
blessing function directly, applies its binding/change object and emits log
operation `0xa0`. The binding is produced through `0x00506430`; this is why the
advanced increase belongs to the binding-option structure rather than the
ordinary plus-level probability matrix.

### Magic and attribute stones

`CEnchant_EnchantMagicStone` (`0x00503680`) and
`CEnchant_EnchantAttrStone` (`0x005032a0`) share this top-level roll:

```text
chance  = clamp(stoneBaseChance + characterAlchemyBonus, 5, 100)
roll    = rand() % 100
failure = chance < roll
success = chance >= roll
```

The inclusive comparison is important: for a clamped value below 100, the
effective number of successful residues is `chance + 1`. The stone base comes
from stone-reference offset `+0x300` in both class-specific accessors
(`0x00503270` attribute, `0x00503650` magic). The character bonus is returned
after emitting operation selector `0xac` and x87 rounding at `0x00503d30`.

On the outer-roll failure the stone is consumed, the target is unchanged and
the server emits failure log `0x83` for magic stones or `0x86` for attribute
stones. On success the class-specific blessing mutates the target, runs the
generic item-change query path and sets result `1`.

Magic-stone blessing (`0x00503810`) resolves the option name stored in the
stone reference, verifies item degree, resolves the corresponding magic-option
record and increments or creates the target property. Properties with raw tags
`atha`, `luck`, `soli`, `astr` and `ape` are limited to five charges;
`rep`-family properties are limited to six; other properties use the option
record's virtual maximum, clamped to `1..1700`. The first four raw tags map to
Athanasia, Luck, Solid and Astral in the pinned reference data; `ape` maps to
`MATTR_APE` and is the assimilation-protection property used below.

Attribute-stone blessing first selects an equipment-compatible attribute from
the confirmed lists:

- shields: `NATTR_DUR`, `NATTR_PDSTR`, `NATTR_MDINT`, `NATTR_PD`, `NATTR_MD`,
  `NATTR_ER`;
- weapons: `NATTR_DUR`, `NATTR_PASTR`, `NATTR_MAINT`, `NATTR_HR`, `NATTR_PA`,
  `NATTR_MA`, `NATTR_CRITICAL`;
- armor: `NATTR_DUR`, `NATTR_PDSTR`, `NATTR_MDINT`, `NATTR_BR`, `NATTR_PD`,
  `NATTR_MD`;
- accessory: `NATTR_PAR`, `NATTR_MAR`.

`0x00501ea0` chooses the target percentile bucket from
`[0,23,43,63,83,100]`, using up to five or six `rand()%6` comparisons depending
on the reference type, then maps it by `(bucket * 31) / 100`. After applying
that primary attribute, `0x00503430` draws `roll = rand()%101`. Assimilation is
attempted exactly when `roll < stoneAssimilationThreshold`; equality does not
trigger it. If `MATTR_APE` exists, one charge is consumed (or the property is
removed at its last charge) and no secondary option changes. Otherwise the
function selects uniformly across the target's normal-attribute and magic-slot
counts. A selected normal attribute is replaced only when it differs from the
primary attribute; a selected magic slot is routed through the class's magic
replacement virtual. Zero available slots or selection of the primary normal
attribute leaves only the primary change.

### Common enchant response serialization

On phase-3 resolution, `0x00506740` opens the response chosen by type
(`0xb150`, `0xb151` or `0x34a8`) and writes this common prefix:

```text
u8 result = 1
u8 action = 2
u8 success = outcomeFlags & 1
u8 targetSlot
```

When `success != 1`, it additionally writes
`u8 destroyed = (outcomeFlags >> 4) & 1`. A destroyed target suppresses the
generic item-edit application; a surviving target is submitted to that change
path. The flags used by reinforcement are the same `0x01/0x02/0x08/0x10`
flags described above, but only bit 0 and bit 4 become fixed response bytes at
this layer. Cancellation writes the selected family's result/action prefix
without executing the operation. Validation failures use result `2` plus the
operation's `u16` error code.

## SQL and persistence boundary

### Generic item mutation through ShardManager

The ordinary target-item and material changes do not call an alchemy-specific
procedure. GameServer mutates the in-memory objects, builds its generic item
change set, and submits that set to ShardManager. The matching ShardManager
consumer is `0x004883e0` in the pinned `SR_ShardManager.exe` (SHA-256
`00751081e8dc4d13d928bd6c0d58979dbc63788f269cd2932e881334093504df`,
size `4,558,848`). Its item branch reads a fixed `0x143`-byte item record and
selects one of three procedures by change action:

```sql
exec _SMC_DEL_ITEM  0, '%s', %d
exec _SMC_EDIT_ITEM 0, '%s', %d, %I64d, %d, %d, %I64d, %d, '%s', %d,
                        %I64d, %I64d, %I64d, %I64d, %I64d, %I64d,
                        %I64d, %I64d, %I64d, %I64d, %I64d, %I64d
exec _SMC_ADD_ITEM  0, '%s', %d, %d, %I64d, %d, '%s', %d,
                        %I64d, %I64d, %I64d, %I64d, %I64d, %I64d,
                        %I64d, %I64d, %I64d, %I64d, %I64d, %I64d
```

The leading `0` is the character-inventory storage selector. The same binary
uses `1` for account chest, `2` for guild chest and `3` for avatar inventory,
but normal alchemy inventory changes use selector 0. The restored pinned
database supplies the exact parameter order represented by those format
strings:

```text
_SMC_DEL_ITEM:
  TargetStorage, OwnerName, Slot

_SMC_EDIT_ITEM:
  TargetStorage, OwnerName, Slot, ItemIDToEdit,
  RefItemID, OptLvl, Variance, Data, Creater,
  MagParamNum, MagParam1 .. MagParam12

_SMC_ADD_ITEM:
  TargetStorage, OwnerName,
  RefItemID, OptLvl, Variance, Data, Creater,
  MagParamNum, MagParam1 .. MagParam12
```

`Data` is polymorphic: for equipment it is current durability and for
stackable items it is stack quantity. Consequently one generic edit persists
reinforcement level (`OptLvl`), durability/quantity (`Data`), variance and up
to twelve packed magic parameters. `_SMC_EDIT_ITEM` validates that the item ID
still occupies the named slot, then updates `_Items`. `_SMC_ADD_ITEM` allocates
an item, writes `_Items`, and links the first free inventory slot in one SQL
transaction. `_SMC_DEL_ITEM` clears the storage slot, removes rent metadata if
present and frees the item in one transaction. These database-side effects are
confirmed by the procedure bodies in the pinned restored shard database; the
call selection and argument order are independently confirmed by the
ShardManager executable.

The GameServer operation log numbers (`0x5a`, `0x5b`, `0x82`, `0x83`, `0x85`,
`0x86`, `0x89`, `0x8a`, `0xa0`) describe the gameplay reason for the change;
they are not ShardManager packet opcodes or separate persistence procedures.

### Binding options: sockets and advanced elixirs

Binding-option changes are the exception to the generic ShardManager item
path. `SR_GameServer.exe` directly embeds and invokes:

```sql
{?=CALL _Bind_Option_Manager (%d, %I64d, %d, %d, %d, %d, %d, %d, %d)}
```

The generic caller at `0x0044b600` passes, in order:

```text
job, itemDBID, optionType, slot, optionID, optionLevel,
optionValue, parameter1, parameter2
```

The database procedure defines jobs `0=insert`, `1=update`, `2=delete` and
`3=insert all`; option type `1` is a socket and type `2` is the special/advanced
elixir binding. Socket slots must be unique. The advanced-elixir slot is
allocated by the procedure and only one may exist per item. The insert-all
path at GameServer `0x0045bfd0` iterates both binding-option types and submits
job 3 for each record. Thus socket and advanced-elixir persistence must not be
folded into `_SMC_EDIT_ITEM`: that procedure has no binding-option parameters.

No alchemy execution path or either binary contains `_AlchemyFuse`,
`_EnchantItem` or another alchemy-specific item-write procedure.

### Binary-confirmed alchemy-item SQL strings

The GameServer does directly embed these alchemy-stone storage contracts:

```sql
{CALL _STRG_ADD_ALCHEMYSTONE (?, ?, %d,%d, %d,%d,%d,%d)}
{CALL _STRG_RESTORE_SOLDITEM_ALCHEMYSTONE (?, ?, %d,%d, %d,%d,%d,%d, %I64d)}
```

The first is referenced at `0x0045acfc` inside `0x0045a9a0`; the second at
`0x0045bf32` inside `0x0045bb60`. They are subtype branches of generic storage
add and restore-sold-item functions. The two `?` values are output parameters
`NewItemID` and `LatestItemSN`. The formatted arguments are, in order,
`Operation`, `CharID`, `Slot`, `RefItemID`, `OptLevel`, `Data`; restore appends
`RemainGold`. The matching procedure bodies confirm that `Data` is the stone's
stack quantity and that both allocation/linking operations are transactional.

Related item persistence visible elsewhere in the same binary includes:

```sql
UPDATE _ITEMS SET REFITEMID = %d WHERE ID64 = %I64d
```

That statement proves a generic item-reference mutation contract, but no
binary call-graph edge recovered in this pass ties it specifically to an
alchemy outcome.

### Reference-data SQL tables

Alchemy behavior is driven by loaded reference objects whose embedded table
names include `_REFMAGICOPTGROUP`, `_REFMAGICOPT`, `_REFMAGICOPTASSIGN`,
`_REFDROPCLASSSEL_REINFORCE`, `_REFDROPCLASSSEL_ALCHEMY_TABLET`,
`_REFDROPCLASSSEL_ALCHEMY_MAGICSTONE`,
`_REFDROPCLASSSEL_ALCHEMY_ATTRSTONE`, `_REFMAGICOPTBYITEMOPTLEVEL` and
`_REFALCHEMYMERIT`. These are startup/reference-data inputs, not per-attempt
write queries.

## Implementation consequences for AegisCore

- The maintained parsers cover `0x7150`, `0x7151`, `0x7155`, `0x7157`,
  `0x716a`, `0x34a7`, and `0x34a9` with the family/type selection above.
- Delayed operations advance from AegisCore's authoritative game update and
  execute at phase three; death/body-state interruption and explicit cancel
  clear the per-character state without a wall-clock sleep.
- `0x716a` is a multiplexer: insert has a five-byte body
  (`2,9,target,stone,index`), while create and erase have four-byte bodies
  (`3,8,target,index` / `4,10,target,index`); all three are implemented.
- Manufacture, compound, dismantle, avatar enchant and NPC avatar grant are
  implemented alongside reinforcement, stones and sockets.
- AegisCore persists each resolved operation atomically in MySQL and treats a
  failed commit as an unknown outcome by closing the session. Those queries
  are AegisCore's persistence design, distinct from the original GameServer's
  separately documented ShardManager procedure boundary.

## Coverage audit and remaining semantic labels

The requested implementation-facing surfaces have independent executable
anchors:

| Requirement | Evidence establishing it | Status |
| --- | --- | --- |
| Client opcodes and family selection | dispatcher `0x0050d0c0` plus handlers listed above | confirmed |
| Request and response structures | packet reads in the handlers; packet writes in `0x00506740`, `0x005076a0`, `0x00509030`, `0x00509fa0`, `0x0050c160` | confirmed |
| Delayed lifecycle and cancellation | player fields `+0x2211..+0x222a` and update `0x0052aa90` | confirmed |
| Runtime operation classes/layouts | MSVC RTTI/vftables and constructors listed below | confirmed |
| Reinforcement probability/protection | `0x00505f00`, `0x00506090`, `0x00506ce0` | confirmed |
| Stone probability/assimilation | `0x00503270..0x00503d30` | confirmed |
| Manufacture/dismantle/socket algorithms | their handlers, constructors and execution functions above | confirmed |
| Ordinary item persistence | ShardManager `0x004883e0` and `_SMC_*_ITEM` procedure bodies | confirmed |
| Socket/advanced binding persistence | GameServer `0x0044b600`, `0x0045bfd0` and `_Bind_Option_Manager` | confirmed |
| Alchemy-stone storage SQL | GameServer `0x0045a9a0`, `0x0045bb60` and matching procedure bodies | confirmed |

What remains unnamed is deliberately narrower than an implementation blocker:
some numeric validation errors have no recovered localization label; socket
create/erase material public names are represented only by confirmed type-mask
tails; and a tick phase is not converted to wall-clock seconds because the
world update cadence is external to the operation. Implement against the
numeric codes, type predicates and phase count documented here rather than
inventing those presentation names or a fixed sleep duration.

## Reproduction anchors

The focused decompilations are generated evidence, not maintained source. The
most useful entries are:

- dispatch initializer: `0x0050d0c0`
- request handlers: `0x00506e20`, `0x00506ee0`, `0x00506fa0`, `0x0050a100`,
  `0x0050c160`
- common parser/state entry: `0x00507cf0`
- tick/interrupt/completion: `0x0052aa90`, `0x00506740`, `0x005076a0`
- constructors: `0x00505b80`, `0x00503600`, `0x00502950`, `0x00503d60`,
  `0x00506280`, `0x00508490`, `0x0050a680`
- storage SQL: `0x0045a9a0`, `0x0045bb60`
- binding-option SQL: `0x0044b600`, `0x0045bfd0`
- ShardManager generic item consumer: `0x004883e0`; item-record reader:
  `0x0048c460`; log builder: `0x0048bb10`

The bounded opcode-list disassembly is
`analysis/decompiled/alchemy-gameserver/registration.asm`; it demonstrates
state allow-list population and should not be confused with the client
dispatcher table initialized by `0x0050d0c0`.
