# vSRO 1.188 client decoder layouts

This is the field-level companion to [`vsro188-client-binary-reference.md`](vsro188-client-binary-reference.md). It describes what the pinned client reads at the decoder entry points, not what a server implementation hopes the packet means.

Authority: stock client SHA-256 `58B9DA3A218894F28E4C82A992B90997869CF331AB450929C8FCB7453327D114`. The requested executable is the compatibility-patched copy, which differs only at the documented ATL/DEP patch sites.

## Notation and read semantics

| Notation | Meaning |
| --- | --- |
| `u8`, `u16`, `u32` | unsigned little-endian integer of the stated width |
| `i16`, `i32` | signed little-endian integer; the client often reads unsigned then sign-extends |
| `f32` | four-byte IEEE-754 value copied into a float |
| `str` | client string extraction: `u16 byteLength` followed by that many raw bytes |
| `remain` | bytes still available in `CMsgStreamBuffer` after the entry point's reads |
| `opaque_X` | width/order is confirmed, semantic name is not |

`CMsgStreamBuffer::Read` at `0x004F7220` accepts a read only when the requested width fits the available logical payload. It copies across linked 4 KiB nodes and advances the read cursor. The focused handlers do not all enforce an exact final `remain == 0`; some delegate the remaining bytes to a second decoder or intentionally ignore/flush them.

## Entity movement decoders

## Group-spawn `0x3019` parser

The registered `0x3019` entry at `0x008AC170` is a two-stage parser. Its
entry wrapper allocates/marks a message object and jumps to `0x00833380`,
which copies all remaining stream bytes without interpreting them. The
message is then marked `0x3019` and passed to `0x008A6D20` by the grouped
spawn dispatcher at `0x008AC3D0`.

The second-stage envelope is binary-confirmed:

```text
u8 groupFamily

if groupFamily == 1:
    u16 recordCount
    repeat recordCount:
        parsePrimaryGroupRecord(stream, mode = 0)

if groupFamily == 2:
    u16 recordCount
    repeat recordCount:
        parseObjectLeftRecord(stream)

otherwise:
    assertion/error path
```

`parsePrimaryGroupRecord` at `0x008A6BD0` begins with a `u32`
object/reference identifier. In the grouped-list call, mode is zero: the
identifier is classified through the client object catalog, the resolved
object byte at `+0xEC` is set to `1`, and the remaining message pointer is
passed to the resolved object's vtable slot `+0x64`.

For the recovered world-object RTTI/vtable set, those `+0x64` targets do
not read message bytes:

| Runtime class family | vtable `+0x64` target | Stream effect |
| --- | ---: | --- |
| `CICPlayer`, `CICMonster`, `CICNonuser`, `CICCos`, `CICGuard`, `CICUser`, `CICharactor` | `0x009C75A0` | object/global-state predicate; no stream read |
| `CICMonsterNPC`, `CICATStruct` | `0x009C75A0` | object/global-state predicate; no stream read |
| `CICNPC` | `0x009D1220` | NPC/resource-state update; no stream read |
| `CICRide` | `0x009C2440` | empty routine; no stream read |
| `CICScriptObj` primary vtable | `0x009E2300` thunk to `0x009DB550` | empty routine; no stream read |
| `CICScriptObj` secondary vtable (`0x00DE33E4`) | `0x009DBC30` | common script-object setup, then conditional byte tail |

For the no-tail rows, the family-1 record grammar is exactly `u32
objectOrReference`, followed only by the vtable side effect. The
`CICScriptObj` secondary-vtable row is the qualified exception. Its
`0x009DBC30` routine first calls `0x009D1220` for common object/script state
(that helper performs no stream read), then reads:

```text
u8 scriptByte0       -> object + 0x794

if !object.IsSame(CICPlayer):
    u8 scriptByte1   -> global 0x00EF1BB8

if !object.IsSame(CICPlayer):
    u8 scriptByte2   -> object + 0x824

if !object.IsSame(CICPlayer):
    u8 scriptByte3   -> global 0x00EF1BB9
```

`CObj::IsSame` is the runtime-class comparison at `0x00B94B70`, and
`0x00EF1B44` is the pinned `CICPlayer` runtime-class object. The byte names
are intentionally semantic-neutral; the binary proves the destinations and
the repeated class gate, not English field names. Therefore a script-object
record is not safe to model as a bare four-byte identifier.

The helper at `0x008A6BD0` also has alternate call modes `-1` and `-2`.
Mode `-1` consumes an additional `u16` flag block and, when
`flags & 0x1C == 0x14` and `flags & 0x60 == 0x40`, selects the current
player's special object. Mode `-2` consumes a `u32` plus `u16` special
block, but the focused stock listing does not show a separate object-catalog
resolution for it. These alternate helper modes are not additional complete
`0x3019` record grammars.

Family 2 dispatches directly to `0x008A6CC0`, which the pinned DevKit symbol
and client export identify as `CNetProcessIn::OnObjectLeft`. It reads a
`u32 objectUID`, checks the current-player UID and performs the associated
cleanup, resolves the object through the catalog, then jumps through that
object's vtable slot `+0x4C`. Its fixed prefix is therefore exactly
`u32 objectUID`; the suffix is the object-left handler, not a second generic
spawn parser.

The separate `0x008A6CA0` object-entered wrapper calls
`0x008A6BD0(stream, 1)`. That nonzero mode reaches the helper's conditional
vtable `+0x68` follow-up and must not be merged with the `0x3019`
grouped-list path.

Raw address evidence is the pinned `sro_client.exe` at `0x008AC170`,
`0x008AC3D0`, `0x008A6D20`, `0x008A6BD0`, and `0x008A6CC0`; the RTTI
and vtable entries are recorded in the Ghidra scan log and pinned DevKit
symbol map.

### `0xB021` at `0x008A5750`, helper `0x008A5640`

The helper reads a movement union and the outer handler then reads one trailing control byte. The direct client decoder contract is:

```text
u32 entityUID
movement:
    u8 mode
    if mode == 0:
        u8 source
        u16 heading
    if mode == 1:
        u16 region
        if region & 0x8000:
            u32 x
            u32 y
            u32 z
        else:
            i16 x
            i16 y
            i16 z
u8 control
```

The direct branch lengths are therefore:

| Mode | Body after `entityUID` | Total decoder prefix |
| ---: | --- | ---: |
| `0` | `mode:u8, source:u8, heading:u16, control:u8` | 9 bytes |
| `1`, ordinary region | `mode:u8, region:u16, x/y/z:i16, control:u8` | 14 bytes |
| `1`, high-bit region | `mode:u8, region:u16, x/y/z:u32, control:u8` | 20 bytes |

The function stores the ordinary three coordinates after sign extension and preserves the high-bit region branch as three full-width values. The high-bit branch is a confirmed wire discriminator; its world-plane meaning is not assigned here.

The focused disassembly shows no source-region/source-position tail after `control` in this decoder entry. AegisCore's destination-response writer and golden tests now emit the binary-confirmed 14-byte ordinary form (or 20-byte high-bit-region form); the retained `Source*` structure fields are server-side context only and are deliberately not serialized.

### `0xB023` at `0x008A4F80`

The authoritative position decoder is fixed-width:

```text
u32 entityUID
u16 region
f32 x
f32 y
f32 z
u16 heading
```

The decoded body is 20 bytes. This is a position/correction update, not the normal movement union: the server-side TestIn evidence independently reaches the same serializer role, while the client parser confirms the widths/order.

### Related turn/control forms

The exact client constructor evidence records two-byte heading requests for `0x7023` and `0x7024`, each:

```text
u16 heading
```

Those values are distinct from the `0x7021` destination-flag request and from the server-to-client `0xB023` position correction.

## Action and combat decoders

### `0x7074` request family

The stock request forms recovered from the action dispatcher are:

```text
stop/cancel:
    u8 phase

basic/entity target:
    u8 phase = 1
    u8 actionKind = 4
    u32 targetOrCandidateID
    u8 terminator = 0

entity skill:
    u8 phase = 1
    u8 actionKind = 4
    u32 skillID
    u8 targetMode = 1
    u32 targetUID

position skill:
    u8 phase = 1
    u8 actionKind = 4
    u32 skillID
    u8 targetMode = 2
    i16 region
    i32 x
    i32 z
    i32 y

remove own buff:
    u8 phase = 1
    u8 actionKind = 5
    u32 skillID
    u8 terminator = 0
```

The byte names above are descriptive. The binary proves the widths, order, and action-kind branches; it does not embed the English names for every numeric kind. `0x7074` is a generic entity-action request, not a skill-only opcode.

### `0xB070` at `0x008A5FD0`

The client entry point has a result gate and a delegated success parser:

```text
u8 result

if result != 1:
    u16 failureOrResultCode
    error/UI path

if result == 1:
    u16 resultFamily       // accepted entry values: 0x3000 or 0x3002
    u32 opaque_0
    u32 opaque_1
    u32 opaque_2
    delegated remainder    // branch-specific; passed to 0x00A54BE0/0x00A54F00 paths
```

The entry function diagnoses any success-family value other than `0x3000` or `0x3002`. The three direct `u32` fields are the complete fixed prefix; the target UID is not part of this prefix and begins only in the delegated remainder. Their current Go labels (`SkillID`, `SourceUID`, and `CastUID`) are implementation labels supported by surrounding call sites, while the binary-confirmed contract is their width and order.

The independently recovered TestIn serializer gives the following deeper result-block shape for the common success family. It is a server-side corroboration of the client's delegated remainder, not a replacement for a client capture:

```text
u8  success = 1
u16 resultFamily
u32 referenceSkillOrAction
u32 sourceActor
u32 executionOrCastID
u32 delegatedField0       // client 0x00A54BE0 reads this as f32
u8  resultFlags

if resultFlags & 0x01:
    u8  hitCount
    u8  targetCount
    repeat targetCount:
        u32 targetUID
        repeat hitCount:
            u8 attackStateAndKind
            if (attackStateAndKind & 0x7F) is 0, 4, 5, or 7:
                u32 packedDamageAndDamageState
                u32 effectOrAbnormalMask
            if kind is 4 or 5:
                displacement endpoint fields
            if kind is 7:
                u16 barrierRemaining
                u16 barrierMaximum

if resultFlags & 0x0A:
    compact two-field result block

    if serverInvocationFlags & 0x40:
    f32 additionalValue
```

The hit serializer packs a 24-bit damage value and one damage-state byte as `(damage24 << 8) | damageState`. Kinds 4/5 carry knock-over/knockback endpoint data; kind 7 carries barrier accounting. They are not interchangeable ordinary hit records.

### `0xB071` at `0x008A4960`

The recovered wrapper sets the action-state flag and calls `0x00A56680`. That helper consumes the fixed entry prefix:

```text
u8 result

if result != 1:
    u16 failureCode
    u32 actionOrCastID

if result == 1:
    u32 actionOrCastID
```

On result `1`, the remaining bytes begin with the delegated continuation
prefix `u32 delegatedValue` (read as `f32` by `0x00A54BE0`) and
`u8 continuationFlags`, then continue through the action-state success path
(`0x00A54BE0`, then `0x00A53090`). Result `2` takes an alternate action-state
branch; other non-success values take the generic state/error path. A target
UID is not part of the fixed B071 prefix; any target/position bytes belong to
the selected delegated action path when that path supplies them.

The client helper now confirms the common continuation masks and position/hit-list reads below; uncommon action-state subclasses remain qualified until their direct branch writers are matched.

### Delegated B070/B071 suffixes: server-writer corroboration

The matching vSRO server serializer provides an exact wire grammar for the
common delegated branches. It is independent server-binary evidence, so the
runtime class names below remain descriptive rather than client-source names.

The common B070 success suffix begins after the fixed prefix
`success:u8, family:u16, direct0:u32, direct1:u32, direct2:u32`:

```text
u32 delegatedField0       // client 0x00A54BE0 reads this as f32
u8  resultFlags

if resultFlags & 0x01:
    u8 hitCount
    u8 targetCount
    repeat targetCount:
        u32 targetUID
        repeat hitCount:
            u8 resultKindAndAttackState
            kind = resultKindAndAttackState & 0x7F

            if kind in {0, 4, 5, 7}:
                u32 packedDamageAndDamageState
                u32 effectOrAbnormalMask

            if kind in {4, 5}:
                u16 region
                u32 x
                u32 y
                u32 z

            if kind == 7:
                u16 barrierRemaining
                u16 barrierMaximum

if resultFlags & 0x0A:
    u32 compactField0
    u32 compactField1

if serverInvocationFlags & 0x40:
    f32 additionalValue
```

`invocationFlags` is the server-side condition that selects this optional
float; it is not an additional field in the fixed B070 client prefix, and
the matching client branch is action-state dependent rather than universal.

The ordinary packed value is `(damage24 << 8) | damageState`. Kinds `2`,
`8`, `1`, and `3` are state-only variants and emit no packed damage/effect
pair; their first kind/state byte is still present. Kinds `4/5` are
displacement records and kind `7` reports barrier consumption rather than
ordinary target damage.

The B071 server completion serializer has two exact branches after the
result byte:

```text
u8 result
if result == 1:
    u32 completionOrCastID
    u32 delegatedValue       // common writer emits zero
    u8  continuationFlags
    // client 0x00A54BE0 consumes delegatedValue as f32, then this byte
else:
    u16 failureCode
    u32 actionOrCastID
```

The mode-1 success writer emits the completion ID, a four-byte delegated
value (zero in the common writer), and then a zero continuation byte; other
modes emit the failure/action fields or the alternate action value. This
explains why a target UID must not be inserted into the fixed B071 prefix.
The client-side fixed reads are from `0x008A4960`/`0x00A56680`; the
server corroboration is `0059E8F0`, `00585730`, `005856D0`, `005855F0`, and
`0059AE10`.

### Client-side delegated continuation reads

The stock action-state helper at `0x00A54BE0`, reached from the B070/B071
delegation paths, confirms a continuation prefix that is not visible in the
top-level B070/B071 wrappers:

```text
f32 delegatedValue
u8  continuationFlags

if (continuationFlags & 0x01) == 0 and the action-state condition permits it:
    f32 optionalStateValue

if continuationFlags & 0x01:
    u8 hitCount
    u8 targetCount
    repeat targetCount:
        u32 targetUID
        repeat hitCount:
            u8 kindAndState
            kind = kindAndState & 0x7F
            if kind in {0, 4, 5}:
                u32 packedDamageAndDamageState
                u32 effectOrAbnormalMask
            if kind in {4, 5}:
                u16 region
                u32 x
                u32 y
                u32 z
            if kind == 7:
                u32 packedDamageAndDamageState
                u16 barrierRemaining
                u16 barrierMaximum

if continuationFlags & 0x08:
    u16 region
    u32 x
    u32 y
    u32 z

if continuationFlags & 0x02:
    u16 region
    u32 x
    u32 y
    u32 z

if resultFlags & 0x01:
    u8 hitCount
    u8 targetCount
    delegated hit/target records
```

The hit/target helper at `0x00A548E0` reads the two count bytes and then
iterates the target and hit collections; its state-machine bounds checks are
client-side validation, not extra wire fields. `0x00A55E00` confirms the
kind/state and kind-specific reads shown above. This directly confirms the
same optional position blocks and hit-list discriminator used by the
matching server B070 serializers. The `continuationFlags` name and the
labels in this continuation are descriptive; the binary proves the masks,
widths, order, and environment-gated branch.

## Item-result decoders

### Entity bootstrap/control packets: `0x3017`, `0x3019`, `0x3018`, `0x3015`, `0x3016`

The registration table in `0x0086FBB0` binds the bootstrap sequence to
`0x3017 -> 0x3019 -> 0x3018`. The exact control reads are split across the
following stock-client addresses:

```text
0x3017 at 0x008AC0E0:
    u8  groupAction       // 1 = spawn, 2 = remove
    u16 objectCount
    // creates/finds the object-list accumulator and appends the two fields

0x3019 at 0x008AC170:
    // consumes the accumulated object-list bytes in chunks of at most 0x1000
    // and parses each record through the object-type dispatch path

0x3018 at 0x008A6F50:
    if interface bootstrap flag +0x7CC is set:
        no stream reads; copies the internal buffer end pointer
    else:
        u8  endMode       // client clamps unexpected values to 1
        u8  endState      // accepted endMode values are 1..3
        // publishes endMode/endState through the data-manager virtual path

0x3015 wrapper at 0x008A6CA0:
    dispatch one spawn record through 0x008A6BD0(mode=1)

0x3016 at 0x008A6CC0:
    u32 uniqueId
    // resolves the object, clears the local-player selection if it matches,
    // then invokes the object vtable removal slot
```

`0x008A6BD0` begins the single-object record with `u32 objectId`. Values
`-1` and `-2` select special event/object forms and consume additional
TypeID-related bytes; an ordinary ID resolves through `0x009C1FA0`. The
record then calls the resolved object's vtable slots `+0x64` and, for a
non-null secondary context, `+0x68`. Therefore `0x3015` is not a
four-byte-only spawn packet: the object record after that ID remains
TypeID-dependent.

For `CIItem`, the ordinary single-entry tail is now exact. Initial parser
`0x009E1D00` consumes the TypeID-dependent item-data prefix, calls base
placement parser `0x009C3A70` for
`u32 UID + u16 region + 3*f32 position + u16 heading`, then reads
`u8 hasMarker`, an optional marker `u32`, and one item-state byte. Because
`0x008A6CA0` passes mode `1`, dispatcher `0x008A6BD0` immediately invokes
update parser `0x009E1A30`, which reads exactly one more state byte; only
values `5` and `6` consume a following `u32`. Ordinary ground entry uses
state `0`. Leaving an unconditional `u32` after that byte produces four
unread bytes and reaches the client's deliberate packet assertion.

The group packet sequence is consequently `3017(action,count)`, one or more
`3019` record chunks, then `3018`; removal uses `3017(action=2,count)` with
`3019` containing only the `u32` IDs, then `3018`. AegisCore's group writer
matches the proven `3017` prefix and ordering, while its `3019` body must
continue rejecting unsupported object families rather than emitting a
short record that shifts the next object. The fixed-prefix codec and golden
vectors are [`entity_spawn_codec.go`](../protocol/vsro188/entity_spawn_codec.go)
and [`entity_spawn_codec_test.go`](../protocol/vsro188/entity_spawn_codec_test.go).

Raw evidence: [`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-quest-complete/0086FBB0_FUN_0086fbb0.c), [`008A6CC0_CNetProcessIn__OnObjectLeft.c`](../../analysis/decompiled/client-quest-30d5/008A6CC0_CNetProcessIn__OnObjectLeft.c), [`008AC170_CNetProcessIn__OnObjectListData.c`](../../analysis/decompiled/group3019-script/008AC170_CNetProcessIn__OnObjectListData.c), and the address-range disassembly around `0x008AC0E0`/`0x008A6F50`.

### Alchemy result prefixes: `0xB150`, `0xB151`, and `0xB16A`

The stock client has separate handlers at `0x008729E0`, `0x00872CD0`, and
`0x00872FA0`. Their fixed prefixes are binary-confirmed; the item body is
the variable `0x008BE4F0` parser and is not interchangeable with a fixed
`CSOItem` memory image.

```text
0xB150 (elixir/compound response):
    u8 result
    if result == 2: u16 errorCode; stop
    if result == 1:
        u8 phase
        if phase == 1: stop (cancelled)
        if phase == 2:
            u8 status
            u8 itemKindRaw       // client subtracts 0x0D for inventory UI
            if status == 0:
                u8 recordMarker
                if recordMarker == 0: variable item record

0xB151 (reinforcer/stone response):
    u8 result
    if result == 2: u16 errorCode; stop
    if result == 1:
        u8 phase
        if phase == 1: stop (cancelled)
        if phase == 2:
            u8 status
            u8 itemKindRaw
            if status == 0: variable item record

0xB16A (socket response):
    u8 result
    u8 status
    if result == 2: u16 errorCode; stop
    if result != 2 and status == 1: u8 followupStatus; stop
    if result != 2 and status in {2,3,4}:
        u8 itemKindRaw
        variable item record
```

`0xB151` has a special client branch for error `0x5423`; other errors are
sent through the normal UI error path. The handlers call
`0x008BE4F0(param_1, 1)`, which first consumes the common rent prefix at
`0x008BB130`, then reads `u32 referenceItemId` into the runtime object at
`+0x34`, and uses the reference item's packed TypeID and category helpers to
select optional reads. Its `CSOItem` destination is
`0x1D0` bytes, but that ABI size is not a wire length. The parser includes
TypeID-dependent durability/quantity, magic options, advanced attributes,
socket/rent/COS data, and other optional branches; a partial record is
therefore a crash/desynchronization risk.

Raw evidence: [`008729E0_CNetProcessIn__OnB150.c`](../../analysis/decompiled/alchemy-client/008729E0_CNetProcessIn__OnB150.c), [`00872CD0_CNetProcessIn__OnB151.c`](../../analysis/decompiled/alchemy-client/00872CD0_CNetProcessIn__OnB151.c), [`00872FA0_CNetProcessIn__OnB16A.c`](../../analysis/decompiled/alchemy-client/00872FA0_CNetProcessIn__OnB16A.c), and [`008BE4F0_FUN_008be4f0.c`](../../analysis/decompiled/cos-rental/008BE4F0_FUN_008be4f0.c).

The common mode-1 item prefix is resolved at `0x008BB130`:

```text
u32 rentFlags
if rentFlags & 0x03:
    u16 rentField0
    if rentFlags & 0x01:
        u32 rentField1
        u32 rentField2
    if rentFlags & 0x02:
        u16 rentField3
        u32 rentField4
u32 referenceItemId
```

For the equipment TypeID branch, `0x008BE4F0` then reads
`u8 optionLevel, u64 variance, u32 durability/value`, followed by
`0x008BCDF0`: `u8 magicCount` (maximum `12`) and `magicCount * u64` entries.
`0x008BB070` performs two binding sections, each
`u8 bindingType, u8 count`, followed by `count * (u8 slot, u64 value)`.
The type-3 rent order is `rentField0, rentField1, rentField2, rentField3,
rentField4`; AegisCore's `WriteRentInfo` and its golden test preserve it.

The remaining `0x008BE4F0` TypeID2 branches have these direct reads:

| Packed TypeID classification | Direct mode-1 reads after `referenceItemId` |
| --- | --- |
| TypeID2 `1` (equipment) | `u8 optionLevel, u64 variance, u32 value/durability`, then magic and binding sections above |
| TypeID2 `2` (container/COS family) | state- and subtype-gated container record; the initial state is `u8`, and active subtypes add `u32` reference/name/lifetime/buff data selected by the client predicates |
| TypeID2 `3`, TypeID3 `5`, TypeID4 other than `1` | `u32` amount branch (gold-like item) |
| TypeID2 `3`, TypeID3 `11`, TypeID4 `1` or `2` | `u16` quantity, then `u8` assimilation |
| TypeID2 `3`, TypeID3 `14`, TypeID4 `1` | `u16` quantity only |
| TypeID2 `3`, TypeID3 `14`, TypeID4 `2` | `u16` quantity, then `u8 magicCount` and `magicCount * u64` entries (maximum `12`) |
| other TypeID2 `3` expendables | `u16` quantity |

The masks are directly tested by `0x00538410`, `0x00871DE0`,
`0x0056FCE0`, `0x0056FCA0`, and `0x00666FC0`; they are not inferred from
item names. AegisCore's `WriteExpendableItem` emits the same gold,
stone/assimilation, gacha, and ordinary-quantity widths and order.

For the container path, the direct common sequence recovered from
`0x008BE4F0` is:

```text
u8 containerState
if containerState != 1:
    u32 containerReferenceId
    subtype-selected name/string path
    if the ability/lifetime predicate at 0x00666C40 is true:
        u32 remainingLifetime
        u8 buffCount
        repeat buffCount:
            u8 buffKind
            u32 buffValue0
            u32 buffValue1
            if buffKind == 5:
                u32 buffValue2
                u8 buffValue3
```

The alternate container predicate at `0x00666CB0` selects a shorter
`u32`-field branch. These predicates are evaluated against the resolved
reference TypeID; they are not packet flags. A partial active-COS record is
particularly dangerous because the next `buffCount` or next packet is then
interpreted as a client object field.

The remaining alchemy-adjacent result wrappers are fixed-width:

```text
0xB155 / 0x3156 at 0x00872810:
    u8 result
    if result == 2: u16 errorCode; stop
    if result == 1:
        u8 phase
        if phase == 2:
            u8 status
            if status == 2: u32 value

0xB157 at 0x00872940:
    u8 result
    if result == 2: u16 errorCode; stop
    if result == 1: stop (success/action completion)
```

The `B155` handler recognizes phase `2` and status `2` before consuming the
four-byte result value; it does not consume that value for status `1` or
other status bytes and returns with those bytes still in the message buffer.
The recovered TestIn writer nevertheless emits the four-byte value after
the status for its successful material path, so this is an intentional
client-side ignored tail rather than permission to move the field. AegisCore
preserves the writer order; its tests assert the complete emitted prefix and
tail.

Raw evidence: the registration at `0x0086FBB0` and the static instruction
range `0x00872810..0x008729D1`.

### `0x3040` inventory/item state update at `0x0088BF30`

The exact client decoder begins with two bytes and then consumes fields in
flag order. It is separate from the `0xB034` polymorphic item-record helper:

```text
u8 itemKind
u8 flags

if flags & 0x01: u32 field01
if flags & 0x02: u8  field02
if flags & 0x04: u64 field04
if flags & 0x08: u16 field08
if flags & 0x10: u32 field10
if flags & 0x20: u8 magic-option count, then count * u64 magic-option entries

if itemKind >= 0x0D and flags & 0x40: u8  field40
if itemKind >= 0x0D and flags & 0x80: u32 field80
```

The field names are intentionally neutral: the handler routes values through
item/UI helpers but does not establish universal English meanings. The
`itemKind >= 0x0D` gates are mandatory; reading the final two fields for all
kinds consumes the next packet's bytes for kinds below `0x0D`. The matching
AegisCore codec and exact-byte golden test are
`protocol/vsro188/item_update_codec.go` and
`protocol/vsro188/item_update_codec_test.go`.

The `flags & 0x20` branch delegates to `0x008BCDF0`, whose stock body reads
the option count and then one eight-byte entry per option (with the source
assertion `m_btMagicParamNum <= MAX_MAGPARAM_PER_ITEM`; the observed maximum
is `12`). This is a wire field and must not be omitted when reconstructing
magic-option updates.

Raw client evidence: [`0088BF30_CNetProcessIn__On3040.c`](../../analysis/decompiled/client-item-use/0088BF30_CNetProcessIn__On3040.c)
and [`008BCDF0_FUN_008bcdf0.c`](../../analysis/decompiled/client-item-use/008BCDF0_FUN_008bcdf0.c).

The matching server writer at `0x004EFA00` emits the same flag-selected
sequence for opcode `0x3040`; that corroborates byte order and placement but
does not promote the neutral field names to item semantics.

### `0x305C` item-used/entity notification at `0x008735D0`

The stock client decoder reads exactly two four-byte values:

```text
u32 firstValue
u32 secondValue
```

It resolves `firstValue` through the client entity lookup, returns early when
`secondValue` equals the local entity value, and otherwise applies a
TypeID-driven status/UI side effect. The decoder then reads the referenced
item's packed TypeID and selects the observed TypeID4/status branches. The
wire fields are exact; names such as `entityId` or `itemId` remain descriptive
until a typed capture proves which runtime virtual supplies each value.

Raw client evidence: [`008735D0_CNetProcessIn__On305C.c`](../../analysis/decompiled/client-item-use/008735D0_CNetProcessIn__On305C.c).

### Dedicated potion/skill-restoration requests: `0x7202` and `0x7203`

The stock client has separate outbound senders at `0x00820E30` and
`0x00820F10`. They select `0x7202` and `0x7203`, respectively, and serialize
the same exact width sequence:

```text
u32 itemRefID              // server-correlated ITEMDATA reference ID
u32 targetSkillOrMasteryID
u8  targetLevel
```

The matching client response registrations are `0xB202 -> 0x00880DB0` and
`0xB203 -> 0x00880E90`. The server-side handlers identify the accepted item
names as `ITEM_QSP_ALL_POTION_1_01`,
`ITEM_MALL_SKILL_RESTORATION_POTION`, and
`ITEM_QNO_RM_OLDWOMAN_2_02`; this is an item-name allow-list, not proof that
all three names share one semantic item class. The server emits the failure
body `u8 2, u16 resultCode` when its internal operation does not return
`0x7800`.

The client response grammars are now exact:

```text
0xB202:
    u8 result
    if result == 2: u16 errorCode; display error
    if result == 1: u32 resultingSkillID; update the client skill state

0xB203:
    u8 result
    if result == 2: u16 errorCode; display error
    if result == 1: u32 masteryID, u8 level; update the client mastery state
```

The success field names above are qualified by the matching server operation
mapping and focused mastery report; the client handler itself proves their
widths, lookup calls, and UI-state writes. `0xB203`
also checks that the referenced mastery is learned before applying its byte
value. The server-side non-`0x7800` response is therefore a failure-form
envelope, while the success forms are distinct and wider than that envelope.

Raw sender evidence: [`00820E30_FUN_00820e30.c`](../../analysis/decompiled/client-pickup-refs/00820E30_FUN_00820e30.c),
[`00820F10_FUN_00820f10.c`](../../analysis/decompiled/client-pickup-refs/00820F10_FUN_00820f10.c),
registration evidence [`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c),
and response handlers [`00880DB0_FUN_00880db0.c`](../../analysis/decompiled/client-skill-restoration/00880DB0_FUN_00880db0.c)
and [`00880E90_FUN_00880e90.c`](../../analysis/decompiled/client-skill-restoration/00880E90_FUN_00880e90.c).

### Client `0x704C` construction at `0x00796660`

The exact client item-use constructor is in the recovered `IFSlotWithHelp`
path. It selects `0x704C` after the local item/category and target checks have
passed. The common prefix is:

```text
u8 inventorySlot = sourceSlot + 0x0D
u16 packedItemTypeID = FUN_0053D460(current-client-state)
u8 targetOrAction
```

The constructor has two request families. The ordinary branch writes the slot,
the client object/reference value, and the target/action byte. Category-specific
branches append one or more of the following blocks:

```text
u32 durationOrValue
u32/fixed position prefix
u16 regionOrHeading
u32 x
u32 y
u32 z
serialized object/item state
```

The client binary also exposes the classifier that selects these branches. At
`0x00796660`, the item’s packed 16-bit TypeID is tested with the following
confirmed masks before the `0x704C` writer is reached:

| TypeID field | Mask/test | Confirmed role |
| --- | --- | --- |
| bit `1` | `(value & 2) == 0` | normal ETC-use admission bit |
| TypeID1 | `(value & 0x1C) == 0x0C` | value `3` |
| TypeID2 | `(value & 0x60) == 0x60` | value `3` |
| TypeID3 | `(value >> 7) & 0x0F` | normal branch accepts `1`/`2`; global scroll uses `3` |
| TypeID4 | `(value >> 11) & 0x1F` | local action/check selector |

The global-message-scroll path at `0x007901C0` pins the narrower TypeID
`(3,3,3,5)` and writes `u8 slot+0x0D`, `u16 TypeID`, and the encoded message
after selecting `0x704C`. The TypeID4 switch in `0x00796660` has explicit
local branches for `6`, `7`, `8`, `9`, `0x0B`, `0x0C`, `0x0F`, and `0x10`;
TypeID4 `9` reaches a target form that appends `u32` plus `u8` after the
common slot/TypeID prefix. The remaining forms append state-dependent
position or serialized-object data, so they must not be collapsed into one
fixed item-use struct.

The position form is emitted in the order `u32`, `u16`, then three `u32`
coordinates. The constructor also has explicit equipment, consumable, COS, and
special-item admission/error branches. These are client-side gates; they do not
establish a complete display-code-to-scroll classifier. In particular, the
serialized object state is copied from the client inventory/equipment object,
not reconstructed as a universal `{itemID, slot, quantity}` struct.

For the catalog rows `ITEM_ETC_SCROLL_RETURN_01`,
`ITEM_ETC_SCROLL_RETURN_02`, and `ITEM_EVENT_RETURN_SCROLL_HIGH_SPEED`, the
client-visible packed TypeID is `0x09EC` (`3,3,3,1`). The reverse-return row
`ITEM_EVENT_REVERSE_RETURN_SCROLL` is `0x19EC` (`3,3,3,3`). The client
constructor selects the shared `0x704C` writer for this item-use family, but
the recovered client function contains no branch on those display names or
reference IDs. Its UI resource initialization does contain the `last return`
and `last death` reverse-portal labels; those labels do not establish the
opaque reverse-selection values or a separate reverse-scroll opcode.

The same function emits local `0xB04C` error packets for client-side rejection
cases. Those packets are `u8 result = 2` followed by a `u16` error value, which
matches the server response envelope below. Raw evidence:
[`00796660_FUN_00796660.c`](../../analysis/decompiled/cos-rental/00796660_FUN_00796660.c)
and [`007901C0_FUN_007901c0.c`](../../analysis/decompiled/item-use-client-exact/007901C0_FUN_007901c0.c).

The matching server-side expendable callback is a second classifier after
this request leaves the client: `0x0049ACA0` dispatches on TypeID3 and
`0x0049B9F0` dispatches the TypeID3=`3` scroll/event family on TypeID4. The
complete binary-confirmed callback matrix is recorded in the
[GameServer item lifecycle report](sr-gameserver-items-inventory-mall.md#expendable-typeid-fan-out-beyond-the-generic-0x704c-handler).

The separate UI-use helper `CIFSlotWithHelp::UseItem` at `0x00676570` adds a
qualified semantic anchor: its TypeID4 switch has a `0x11` branch that
selects the stock labels `ITEM_MALL_INVENTORY_ADDITION` or
`ITEM_MALL_WAREHOUSE_ADDITION`, followed by
`UIIT_STT_STORAGE_EXPANSION_INVENTORY_ADDITION_ITEM_USE` or
`UIIT_STT_STORAGE_EXPANSION_WAREHOUSE_ADDITION_ITEM_USE`. This confirms a
storage-expansion item/UI family in the client. The same helper's non-`0x11`
path contains `ITEM_MALL_SKILL_RESTORATION_POTION` and a quest-related item
key, but those literals do not establish a universal TypeID4-to-item-name
table or a distinct server opcode. They are therefore UI evidence, not a
replacement for the exact `0x704C` request grammar above.

Raw UI-use evidence: [`00676570_CIFSlotWithHelp__UseItem.c`](../../analysis/decompiled/cos-rental/00676570_CIFSlotWithHelp__UseItem.c)
and reverse-portal UI resource evidence [`00644C90_FUN_00644c90.c`](../../analysis/decompiled/client-quest-30d5/00644C90_FUN_00644c90.c).

### `0xB04C` at `0x0087A7B0`

The top-level result contract is exact:

```text
u8 result

if result != 1:
    u16 errorCode
    error/UI path

if result == 1:
    u8  encodedSlot       // the handler derives encodedSlot - 0x0D for the inventory UI
    u16 packedTypeID      // the handler uses its TypeID bit fields for action/UI branches
    u16 actionOrState      // only the observed zero/nonzero split is established
    inventory/item state transitions
```

The listing proves three reads after the success byte: one byte, then two
16-bit values. The first is used as an encoded inventory slot, the second is
consumed as a packed TypeID, and the final value controls the observed
zero/nonzero action/UI split. These are wire roles, not proof that the final
field is universally an amount, item ID, or action code. The entry point also
dispatches UI/inventory updates based on TypeID bit fields.

Raw evidence: [`0087A7B0_CNetProcessIn__OnB04C.c`](../../analysis/decompiled/client-quest-30d5/0087A7B0_CNetProcessIn__OnB04C.c)
and [`0087A7B0_listing.txt`](../../analysis/decompiled/item-use-client-exact/0087A7B0_listing.txt).

### `0xB0C5` COS item-use result at `0x00872590`

The stock client consumes the COS-specific result envelope in this exact
order:

```text
u8 result
u8 command
if result == 2:
    u16 errorCode
u32 payload
if command == 8:
    u32 secondaryPayload
```

The `u32 payload` is read for every result/command combination. Command `2`
with result `2` routes the error to the item notification path and clears the
resolved COS object state. Command `8` consumes the additional `u32` and
calls the COS state helper with selector `1` on result `1`, or with the
decoded error value otherwise. Other command values do not establish a
universal semantic name for the payload. This grammar is separate from both
PC `0xB04C` and polymorphic item-operation `0xB034`.

Raw client evidence: [`00872590_CNetProcessIn__OnB0C5.c`](../../analysis/decompiled/client-cos-response/00872590_CNetProcessIn__OnB0C5.c).

### `0xB034` at `0x0087F370`

The response envelope is:

```text
u8 result

if result == 2:
    u16 errorCode
    error/UI path

if result == 1:
    decodeItemRecord(0x0087EB50)
```

Other result values leave through the default error path without a recovered additional field. The success branch calls the item-record helper; it is not a fixed five-byte response.

### Item-record helper `0x0087EB50`

The helper begins with a one-byte item-record discriminator and then selects a family-specific layout. The recovered switch groups are:

| Discriminator family | Confirmed branch behavior |
| --- | --- |
| `0x00`, `0x10` | base fields; `0x10` additionally reads a `u32` field at record offset `+0x30`; `0x00` may repeat nested records using a count byte |
| `0x01`, `0x1D` | reads scalar fields at record offsets `+0x03`, `+0x04`, `+0x06`; mode `1` additionally reads a `u32` field at `+0x0C` |
| `0x02`, `0x03` | reads scalar fields at `+0x03`, `+0x04`, and a mode-dependent `u32` field at `+0x0C` |
| `0x04`, `0x05` | `0x05` reads `+0x03`; `0x04` reads `+0x03` in mode `1`, and `+0x03/+0x04` in mode `0` |
| `0x06`, `0x11`, `0x1C` | reads type-specific `u32` data at `+0x30` or `+0x34`; mode `1` adds `+0x0C`, while mode `0` reads `+0x04` |
| `0x07`, `0x12`, `0x17` | reads optional `u32` data and a scalar at `+0x03`; `0x17` adds a repeated child-record list |
| `0x08`, `0x13`, `0x2B` | reads two scalar fields at `+0x02/+0x03`, a count byte, repeated child records, and a trailing 16-bit field; `0x2B` adds a second repeated list |
| `0x09`, `0x14` | reads scalar fields at `+0x03`, `+0x06`, and a `u32` field at `+0x0C`; `0x14` also reads a `u32` field at `+0x30`, and `0x09` has an additional `+0x10` scalar |
| `0x0A..0x0D`, `0x20`, `0x21` | reads an eight-byte value and dispatches it through the item/UI subsystem |
| `0x0E`, `0x15` | reads scalar `+0x04`, optionally scalar `+0x08` |
| `0x0F`, `0x16` | reads scalar `+0x03`, optionally scalar `+0x08` |
| `0x18` | reads scalar fields around `+0x02/+0x03/+0x14`, a `u16` field at `+0x12`, an eight-byte value, and optional repeated child records |
| `0x1A`, `0x1B` | reads `u32` `+0x0C`, then scalar `+0x03/+0x04` |
| `0x1E`, `0x1F` | both modes read scalar `+0x03/+0x04`; mode `1` additionally reads `u32` `+0x0C` |
| `0x22` | reads scalar fields at `+0x04` and `+0x10`; mode zero adds a typed field at `+0x06` |
| `0x23`, `0x24` | reads scalar `+0x03/+0x04`; mode zero adds typed `+0x06` and a count-prefixed child-record list |
| `0x27` | shared tail: scalar `+0x03/+0x04`, then typed `+0x06` |
| `0x28` | reads `u32` `+0x44`, then the `0x27` tail |
| `0x29` | mode zero reads scalar `+0x03` and `u32` `+0x44`, then the shared tail |
| `0x2A` | reads scalar `+0x04` |

The helper's `0x004F7440`, `0x004F7460`, `0x004F74A0`, and `0x004F74C0` calls are typed primitive reads around the common message buffer. The export proves their destinations and branch order, but not a complete semantic type for every destination. The raw evidence is [`0087EB50_FUN_0087eb50.c`](../../analysis/decompiled/shop-client-exact/0087EB50_FUN_0087eb50.c) and the caller [`0087F370_FUN_0087f370.c`](../../analysis/decompiled/shop-client-exact/0087F370_FUN_0087f370.c).

Do not flatten this helper into one `itemID, slot, quantity` structure. The discriminator, count loops, and optional branches are the client's actual item-record grammar.

The primitive helper bodies remove an important ambiguity in the switch table:

| Helper | Exact wire read |
| --- | --- |
| `0x004F7460` | `u8` |
| `0x004F7440` | `u16` |
| `0x004F74A0` | `u16` |
| `0x004F7480` | `u32` |
| `0x004F74C0` | `u32` |

The following exact read grammar applies to the response mode (`param_3 == 0`)
used by `0x0087F370`; `m=1` records are the alternate internal/UI mode passed
by the same helper family. `@+N` denotes the destination field inside the
client's item object, not a wire offset.

| Discriminator | Exact reads in order |
| --- | --- |
| `0x00` | `u8@+3,u8@+4,u16@+6`; then `u8 childCount`, repeated `u8,u8,u8,u16` child records |
| `0x10` | `u32@+0x30,u8@+3,u8@+4,u16@+6` |
| `0x01`,`0x1D` | `m=0: u8@+3,u8@+4,u16@+6`; `m=1` adds `u32@+0x0C` |
| `0x02`,`0x03` | `m=0: u32@+0x0C,u8@+3,u8@+4`; `m=1: u8@+3,u8@+4,u32@+0x0C` |
| `0x04`,`0x05` | `0x05: u8@+3`; `0x04: m=1 -> u8@+3, m=0 -> u8@+3,u8@+4` |
| `0x06`,`0x11`,`0x1C` | `0x11: u32@+0x30`; `0x1C: u32@+0x34`; `m=1: u32@+0x0C`; otherwise `u8@+4` |
| `0x07`,`0x12`,`0x17` | `0x12: u32@+0x30`; `0x17: u32@+0xF0`; then `u8@+3` |
| `0x08`,`0x13`,`0x2B` | `m=1: u8@+2,u8@+3,u16@+6,u32@+0x0C`; `m=0: u8@+2,u8@+3,u8 childCount, child bytes,u16@+6`; `0x13` additionally starts with `u32@+0x30`; `0x2B` has a second count-prefixed `u8,u16` child list |
| `0x09`,`0x14` | `0x14: u32@+0x30`; then `u8@+3,u16@+6,u32@+0x0C`; `m=0` adds `u8@+0x10` |
| `0x0A..0x0D`,`0x20`,`0x21` | `u64` then UI/item-state dispatch |
| `0x0E`,`0x15` | `u8@+4`; `m=0` adds `u8@+8` |
| `0x0F`,`0x16` | `u8@+3`; `m=0` adds `u8@+8` |
| `0x18` | `m=0: u16@+0x12,u8@+0x14,u8@+2,u8@+3,u8 childCount, child bytes,u16@+6`; `m=1: u16@+0x12,u8@+0x14,u8@+2,u8@+3,opaque helper@+0xF4,u16@+6,u64,u32@+0x0C` |
| `0x1A`,`0x1B` | `u32@+0x0C,u8@+3,u8@+4` |
| `0x1E`,`0x1F` | both modes: `u8@+3,u8@+4`; `m=1` additionally reads `u32@+0x0C` |
| `0x22` | only `m=0`: `u8@+4,u8@+0x10,u16@+6` |
| `0x23`,`0x24` | `m=1: u8@+3,u8@+4`; `m=0` adds `u16@+6` and a count-prefixed `u8,u8,u8,u16` child list |
| `0x27` | `u8@+3,u8@+4,u16@+6` |
| `0x28` | `u32@+0x44`, then the `0x27` tail |
| `0x29` | only `m=0`: `u8@+3,u32@+0x44`, then the `0x27` tail |
| `0x2A` | `u8@+4` |

The binary does not contain stable English field names for these destination
slots. The complete semantic-neutral grammar above is stronger than guessed
labels such as `quantity`, `durability`, or `itemID`; those names require a
typed stock capture or a selected runtime class rather than destination
offsets alone.

There is a narrower, qualified semantic map for fields reached through a
known `CSOItem` instance. These names are valid for that runtime object only;
they do not rename every destination used by `0x0087EB50`:

| `CSOItem` offset | Binary-confirmed semantic name |
| ---: | --- |
| `+0x28` | validity byte (`m_blValid`) |
| `+0x34` | reference-object item ID (`m_refObjItemId`) |
| `+0x8C` | option level (`m_OptLevel`) |
| `+0x98` | current durability (`m_CurrDurability`) |
| `+0x9C` | quantity/stack count (`m_quantity`) |
| `+0xC4` | blue-option map (`bluemap`) |
| `+0xE0` | physical attack maximum (`m_PhyAtkPwrMax`) |
| `+0xE4` | physical attack minimum (`m_PhyAtkPwrMin`) |
| `+0xE8` | magical attack maximum (`m_MagAtkPwrMax`) |
| `+0xEC` | magical attack minimum (`m_MagAtkPwrMin`) |
| `+0xF0` | blocking rate (`m_BlockingRateValue`) |
| `+0xF4` | maximum durability (`m_MaxDurability`) |
| `+0xF8` | physical defense (`m_PhyDefPwrValue`) |
| `+0xFC` | parry rate (`m_ParryRateValue`) |
| `+0x100` | physical absorption (`m_PhyAbsorption`) |
| `+0x108` | critical value (`m_CriticalValue`) |
| `+0x10C` | magical defense (`m_MagDefPwrValue`) |
| `+0x110` | magical absorption (`m_MagAbsorption`) |
| `+0x114` | physical reinforcement minimum (`m_PhyReinforcementMin`) |
| `+0x118` | physical reinforcement maximum (`m_PhyReinforcementMax`) |
| `+0x11C` | magical reinforcement minimum (`m_MagReinforcementMin`) |
| `+0x120` | magical reinforcement maximum (`m_MagReinforcementMax`) |
| `+0x124` | physical reinforcement value (`m_PhyReinforcementValue`) |
| `+0x128` | magical reinforcement value (`m_MagReinforcementValue`) |
| `+0x130` | attack rate (`m_AttackRateValue`) |

The item helper's apparent overlap with these offsets is not enough to claim
identity: some branches write an operation record, some write a temporary
UI record, and some pass a child or reference object. A decoder may use the
semantic names above only after the surrounding call chain proves that its
destination is the corresponding `CSOItem`; otherwise the `@+N` notation and
opaque labels remain authoritative.

### Server-correlated item-operation semantics

The matching TestIn server binary provides a qualified semantic cross-check
for the same polymorphic operation families. `004F76E0` is the server's
operation serializer, and `004E9480` emits the `0xB034` response after calling
that serializer. This evidence supports the following operation-level names
and effects, but does not rename every client destination slot in the table
above:

| Discriminator | Server-correlated operation meaning/effect |
| ---: | --- |
| `0x02` | resolves the operation reference at `+0x04` and dispatches player action `0x3B` |
| `0x03` | resolves the operation reference at `+0x04` and dispatches player action `0x20` |
| `0x06`, `0x1C` | item/object transition; a valid item dispatches player action `9`, then performs the related-object transition using `+0x34` |
| `0x0E` | item validation followed by player action `9` |
| `0x11` | additional player-state virtual call, then the shared item/object transition |
| `0x15` | derives an operation context/reference and dispatches player action `0x15` |
| `0x07`, `0x0A`, `0x12` | object/ownership transition using operation fields `+0x3C` and `+0x40` |
| `0x08`, `0x13`, `0x18`, `0x2B` | shared operation/finalization path; `0x08` and `0x2B` first perform the extra object/action step, then all four reach the common `0x004E9AE0` continuation |
| `0x09` | derives an operation context/reference and dispatches player action `0x15` |
| `0x2A` | no operation-specific post-action is recovered in `0x004E9480`; it reaches the emitter's generic output-write path |

The serializer's mode argument is not a universal inverse selector for the
client reader's `param_3`: in particular, `004F76E0(operation, 1)` and the
`0x0087EB50` response-mode reads do not line up branch-for-branch for every
discriminator. The safe promotion is therefore operation semantics plus the
client's exact width/order grammar, not a claimed ABI identity between the
server temporary record and the client runtime item object. Raw evidence is
[`004F76E0` item-operation serializer](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c)
and [`004E9480` B034 operation path](../../analysis/decompiled/gameserver-quest-async/004E9480_FUN_004e9480.c).

There is one useful discriminator-specific correlation. For operation `0x18`,
the server's `004F76E0(operation, 1)` branch writes the response body in this
order:

```text
u16 operation +0x12
u8  operation +0x14
u8  operation +0x02
u8  operation +0x03
u8  count = (operation +0x6C) - (operation +0x68), or zero when +0x68 is null
u8  itemByte[count]       // contiguous values beginning at operation +0x68
u16 operation +0x06
```

The stock client `0x0087EB50` response-mode (`param_3 == 0`) `0x18` branch
reads the same widths and order: `u16`, three `u8` values, a count-prefixed
byte sequence, and a trailing `u16`. This is a direct wire-width/order
correlation for the package-purchase response; the child-byte values still
have no stable semantic name in the executable. Other discriminators remain
subject to the non-universal mode warning above.

### Shared item/storage transition helper `0x0087BA20`

The response side does more than decode a record. The shared helper branches on
the item-operation family and updates the client inventory/equipment/COS UI.
The recovered branches prove these transition classes:

| Branch family | Confirmed client-side effect |
| --- | --- |
| normal expendable/non-expendable item | reads the item state, updates the slot/object, and formats the corresponding item-gained or item-state message |
| storage/chest transfer | resolves source/target storage and invokes storage-to-inventory or inventory-to-storage UI transitions |
| equipment/avatar | refreshes equipment/avatar state and emits the matching item-state UI path |
| COS inventory | resolves the COS owner/inventory and refreshes the COS item slot state |
| gold/result-only | reads a four-byte amount and formats the gain-gold message |
| error | selects the storage/item error UI path without treating the result as a normal item record |

The helper receives the operation discriminator and several opaque bytes from
the response dispatcher. Its branches therefore corroborate that `0xB034` is a
polymorphic item-operation response, but they do not turn every branch into a
single fixed packet layout. Raw evidence:
[`0087BA20_FUN_0087ba20.c`](../../analysis/decompiled/cos-rental/0087BA20_FUN_0087ba20.c).

### Item-mall purchase senders

The same client also has a broader storage-operation sender at
`0x007950B0`. Its outbound boundary is exact:

```text
IsOpcodeSupported(0x7034, 0)
select opcode 0x7034
serialize with 0x007923A0(record, 0)
submit with 0x008418D0(record)
```

For the normal-inventory operation `0x00`, the sender encodes the two UI
indices as wire slots by adding `0x0D`; the mode-0 serializer writes
`u8 record+0x03`, `u8 record+0x04`, and `u16 record+0x06`. If the operation
has a child vector, each child repeats that same `u8,u8,u16` shape. This is a
client/server width correlation with the server relocation descriptor, not a
claim that every `0x00` caller has one universal move/swap/merge meaning.

Raw client evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
and [`007923A0_disasm.txt`](../../analysis/decompiled/item-mall-client/007923A0_disasm.txt).

The client-side mall click path is distinct from the response decoder above,
but it uses the same item-operation opcode family. Both recovered senders,
`0x007978A0` and `0x00826460`, first check
`IsOpcodeSupported(0x7034, 0)`, select opcode `0x7034`, serialize a local
record through `0x007923A0(record, 0)`, and submit it with
`0x008418D0(record)`. Each contains the exact diagnostic string:

```text
SendNetMsgItemMall : ShopID(%d), Tab(%d), Index(%d)
```

This proves the three selector inputs and the request opcode for the pinned
client. The older sender also checks its local mall-dealing counter and builds
client-side `0xB034` rejection envelopes with observed error values `0x1807`
and `0x1880`. It does not prove their UI text meanings.

The serializer's mall-relevant discriminator is now width-resolved for mode
zero. The sender writes operation value `0x18`; the branch emits:

```text
u16  record +0x12
u8   record +0x14
u8   record +0x02
u8   record +0x03
blob record +0xF4       // u16 length, then length bytes
u16  record +0x06
u64  record +0xB0
u32  record +0x0C
```

The blob is serialized by `0x0078F480`, whose inline/heap branch only changes
where the bytes are read from. This is a complete width/order statement for
the recovered mode-0 branch, not a semantic naming of the fields. The server
binary now reaches a conditional `0xB034` response and a three-value
balance-state update for operation `0x18`. The matching client balance
handlers are recovered below; they identify the wire widths and the UI reason
labels, but do not by themselves prove which account column produced each
balance value.
Primary
raw evidence is [`007978A0_FUN_007978a0.c`](../../analysis/decompiled/client-quest-30d5/007978A0_FUN_007978a0.c),
[`00826460_FUN_00826460.c`](../../analysis/decompiled/client-quest-30d5/00826460_FUN_00826460.c),
the bounded serializer listing
[`007923A0_disasm.txt`](../../analysis/decompiled/item-mall-client/007923A0_disasm.txt),
and the primitive-width helpers
[`mall-serializer-width-helpers.txt`](../../analysis/decompiled/item-mall-client/mall-serializer-width-helpers.txt).

### Silk balance notifications: `0x3153` and `0x3154`

The registered stock client handlers provide exact payload boundaries:

```text
0x3153:
    u32 balanceValue0
    u32 balanceValue1
    u32 balanceValue2
    -> FUN_00778E50(balanceValue0, balanceValue1, balanceValue2)

0x3154:
    u8  reason
    u8  cashType
    u32 displayedValue
```

`0x3153` is consumed by `0x00882DE0`; its handler forwards the three values
to `0x00778E50`, which stores them at client-object offsets `+0x4AC`,
`+0x4B0`, and `+0x4B4`, respectively. These are exact client destination
offsets, but the executable export does not assign stable account-column
names to them. The server writer `0x004F0940` emits the same three `u32`
values from player state offsets `+0x2188`, `+0x218C`, and `+0x2190` when its
notification flag is zero. The operation-`0x18` continuation passes flag `1`,
so that branch updates server state without directly emitting `0x3153`.

`0x3154` is consumed by `0x00882E50`. The handler reads `reason`, then
`cashType`, then `displayedValue`; it selects a localized status template by
`reason`, formats the `u32` value into that template, and displays it. The
binary contains these exact reason-to-template labels:

| Reason | Localized stock key |
| ---: | --- |
| `0` | `UIIT_STT_SILK_BUY_RESULT` |
| `1` | `UIIT_STT_SILK_BUY_CANCLE` |
| `2` | `UIIT_STT_SILK_BUY_RECEIVE` |
| `3` | `UIIT_STT_SILK_SEND_RESULT` |
| `4` | `UIIT_STT_SILK_POINT_BUY_RECEIVE` |
| `5` | `UIIT_STT_SILK_POINT_USE_RECEIVE` |
| `0xFF` | `UIIT_MSG_STRGERR_TRADE_FAILURE` |

The server-side `0x004F0750` writer validates cash type `0..2` and reason
`0..5`, then writes the bytes in the `0x3154` order above. The `0xFF` client
branch is therefore a client-recognized special display value, not one of the
normal reason values accepted by that writer. Neither handler proves whether
`cashType` selects Silk, gift Silk, or Silk points; the server's semantic names
and stored-procedure result mapping remain unresolved.

Raw handler evidence: [`00882DE0_FUN_00882de0.c`](../../analysis/decompiled/client-silk-handlers/00882DE0_FUN_00882de0.c),
[`00882E50_FUN_00882e50.c`](../../analysis/decompiled/client-silk-handlers/00882E50_FUN_00882e50.c),
[`00778E50_FUN_00778e50.c`](../../analysis/decompiled/client-silk-handlers/00778E50_FUN_00778e50.c),
and the registration table at
[`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c).

## Quest and dialogue decoders

### `0x30D4` at `0x00889D90`

The direct client read order is:

```text
u8  dialogueType
str primaryText

if dialogueType == 4 or 5:
    u8 selectionCount
    repeat selectionCount:
        str selectionText
```

The focused client export proves the common type-4/type-5 count loop. It does not by itself prove the meaning of every final UI/data call. The TestIn serializer additionally emits one `str` and one `u32` after the type-5 selection list; keep that suffix tagged as server-side corroboration until the corresponding client continuation is recovered.

### `0x30D5` at `0x008812B0`

The stable entry prefix is:

```text
u8  subtype       // accepted values 1..4
u32 opaqueValue
```

Values outside 1..4 take the default exit. Subtypes 3 and 4 converge on the same recovered branch body. The current client export does not justify a universal semantic name for `opaqueValue` or additional subtype-specific fields; those should be promoted only after the direct callees or a capture identify them.

### `0x3CA2` at `0x008ABCC0`

```text
str primaryText
u32 opaqueValue
```

The decoded values are passed into the subsystem rooted at `0x008AB590`. The client parser does not establish this as a quest-only packet; the TestIn evidence classifies it as an adjacent global event/display update.

## Special length-sensitive extensions

### `0xB0BD` local timed-buff extension

The stock handler consumes the fixed tuple:

```text
u32 targetUID
u32 skillID
u32 token
```

The local DevKit wrapper recognizes `remain == 16`, temporarily reads an appended `u32 remainingMilliseconds`, restores the read cursor, invokes the stock handler, and flushes the four extension bytes. This is not stock behavior and must not be enabled against an unmodified client.

### `0xBF00` local ping extension

The local wrapper accepts exactly:

```text
u32 tickToken
```

and rejects/flushes any other remaining length. It is a GO-SRO/DevKit extension, not a binary-confirmed stock request/response.

## Promotion rules

The recovered RTTI/vtable set closes the family-1 `0x3019` catalog used by
the focused scan, including the qualified `CICScriptObj` secondary-vtable
tail above. The qualified `CSOItem` anchors above are named, but item-helper
destinations remain semantic-neutral whenever the surrounding object identity
is not proven.
Uncommon B070/B071 action subclasses likewise remain semantic-neutral where
the binary provides widths/branches but no typed name.

The following are safe to implement from static evidence: field widths/order
stated above, branch discriminators, exact fixed lengths, count-loop
placement, the `0x3019` list envelope, the item helper's complete read
grammar, the recovered `0x3019` dispatches, and the common B070/B071 suffix
branches corroborated by the matching server serializer. Remaining capture-
or runtime-dependent claims are English names for unqualified item-helper
destinations and uncommon B070/B071 action subclasses not reached by the
exported common serializers.

Primary raw evidence:

The `0x3019` qualifier above supersedes the earlier generic "virtual-parser
tail" shorthand: the recovered no-tail RTTI/vtable classes take no stream
tail on the family-1 `+0x64` call, while the recovered `CICScriptObj`
secondary vtable has the explicit conditional tail documented above.

- [`008A5640/008A5750` movement decompilation](../../analysis/SRO_CLIENT_DEVKIT_FULL_REPORT.md)
- [`0x7021` movement constructor](../../analysis/decompiled/client-movement-parity/009EBBF0_FUN_009ebbf0.c)
- [`0xB070`/`0xB071` RetDec handlers](../../analysis/decompiled/movement-combat-retdec/movement_combat_handlers.c)
- [`0xB070`/`0xB071` delegated callees](../../analysis/decompiled/movement-combat-retdec/movement_combat_callees.dsm)
- [`0x3019` client parser addresses](../../analysis/ghidra-user/settings/ghidra/ghidra_12.1.2_PUBLIC/script.log-2026-07-18-1.log)
- [`0x3019` script-object decompilation](../../analysis/decompiled/group3019-script/009DBC30_FUN_009dbc30.c)
- [`0x3019` RTTI/vtable scan output](../../analysis/ghidra-user/settings/ghidra/ghidra_12.1.2_PUBLIC/script.log)
- [`0087A7B0` item-use listing](../../analysis/decompiled/item-use-client-exact/0087A7B0_listing.txt)
- [`0087F370` item-result export](../../analysis/decompiled/shop-client-exact/0087F370_FUN_0087f370.c)
- [`0087EB50` item-record helper](../../analysis/decompiled/shop-client-exact/0087EB50_FUN_0087eb50.c)
- [`0xB070` server serializer](../../analysis/decompiled/gameserver-combat/0059E8F0_FUN_0059e8f0.c)
- [`0xB071` completion serializer](../../analysis/decompiled/gameserver-combat/0059AE10_FUN_0059ae10.c)
- [`0xB070` target/hit serializers](../../analysis/decompiled/gameserver-combat/00585730_FUN_00585730.c)
- [`quest-retdec/FINDINGS.md`](../../analysis/decompiled/quest-retdec/FINDINGS.md)
- [`movement_codec.go`](../protocol/vsro188/movement_codec.go) and [`combat_codec.go`](../protocol/vsro188/combat_codec.go) for current AegisCore comparison
