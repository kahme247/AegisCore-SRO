# SR_GameServer and sro_client drops and loot lifecycle

This document records what is directly recoverable from the pinned TestIn
`SR_GameServer.exe` and the pinned vSRO 1.188 `sro_client.exe`; it is
deliberately not a generic Silkroad drop-system description. In particular, a
literal opcode in either executable proves a packet construction/use site, but
does **not** by itself prove its direction or full wire layout. Claims below
are labelled **confirmed**, **bounded inference**, or **unrecovered**
accordingly.

## Evidence identity

- Binary: `.references/testin-decompilation/binaries/SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Verified file size: `8,925,184` bytes
- Client binary: `go-sro/VSRO_Client/sro_client.exe` (vSRO 1.188
  compatibility-patched image)
- Client SHA-256: `3df2282dab84ee5439944b3e6ca8e8cfc69cb05963a45f0f7f4b8adca579c206`
- Client verified file size: `11,485,184` bytes; its stock comparison image is
  `.references/sro-client/vsro-188/sro_client.stock.exe`, SHA-256
  `58b9da3a218894f28e4c82a992b90997869cf331ab450929c8fcb7453327d114`
- Drop-reference construction: `0x00724A00` (assigned/direct) and `0x00724E30` (random-group)
- Generation-to-placement handoff: `0x004C12B0` calls the coordinator at `0x004C143D` and consumes its result list at `0x004C1456`
- Drop-data decode/validation: `0x006AA810`
- COS-pet pickup/action handler: `0x004D4F60`; it constructs `0x3036` at `0x004D51DF`
- Client `0x7034` serializer: `0x007923A0`; action dispatch bytes at
  `0x00792CE4`, target table at `0x00792C80` (action `0x06` ->
  `0x00792673`)
- Binary opcode occurrences: `0x3015 @ 0x00533FA7`, `0x3016 @ 0x0053407F` (direct disassembly; omitted by the literal scanner), `0x3017 @ 0x00533EDE`, `0x3018 @ 0x00533F3E`, `0x3019 @ 0x00533F23`, `0x3036 @ 0x004BDAC6, 0x004D51DF, 0x0052643D, 0x00918046`, `0x304D @ 0x004901DE`, `0x304E @ 0x004E3FBF`, `0xB034 @ 0x004E95D1`, `0xB0C5 @ 0x004D52D2, 0x004D5362, 0x004D5384, 0x004D5455`, and `0x375C @ 0x0086A4A8, 0x0092ABAD`

The source evidence is preserved in `analysis/decompiled/` and the raw opcode sites in `.references/testin-decompilation/binary-opcode-candidates.csv`.

## Maintained Agent materialization boundary

The maintained Agent now exposes an explicit producer-to-world boundary in
`agent-server/service/ground_items.go`. `GroundItemSpec` is materialized by
the injected SQL repository before party ownership, `0x3068` notification,
and world admission run:

```text
hydrated drop selector (injected catalog)
  -> INSERT SRO_SHARD.ITEM (FK_REF_ITEM, VARIANCE, STACK_COUNT, OPT_LEVEL,
                             DATA, RENT_*)
  -> PartyService.AssignItemOwner
  -> party 0x3068 notification, when applicable
  -> WorldService.AddVisibleObject
  -> SpawnManager single-item 0x3015 serialization
```

This is emulator implementation structure, not a claim that the pinned
executable issues that exact SQL from `0x004C12B0`. The binary proves the
in-memory description list and runtime factory path below; its generic
reference-loader SELECT is recovered in the reference-loading section, while
the specific startup submission edge and SQL-worker completion remain
unrecovered. The normal pickup path subsequently claims the existing `ITEM` row by inserting its
`INVENTORY` link in a transaction.

The maintained selector implementation is
`agent-server/service/monster_drops.go`. It now reproduces the recovered
direct and random-group boundaries: inclusive byte count ranges,
two-`rand()` 30-bit-to-million-point chance checks with the item factor,
including the inclusive `roll <= DropRatio * factor * 1,000,000` boundary,
bounded group retries,
`Overlap == 1` candidate removal, binary gold `%101` chance with 15-bit
interpolation and drop-class multipliers, and the
`<1000 / <10000 / >=10000` gold reference split. A hydrated
`MonsterDropCatalog` is now loaded through the maintained Agent's explicit
`MonsterDropCatalogLoader` boundary, and combat invokes `MonsterDropService`
once after a confirmed mob death. The SQL repository reads the normalized
`REF_DROP_GOLD`, `REF_DROP_ITEM_GROUP`,
`REF_MONSTER_ASSIGNED_ITEM_RND_DROP`, and
`REF_MONSTER_ASSIGNED_ITEM_DROP` tables introduced by shard migration
`000078` plus the `REF_MAGIC_GROUP_ID` extension in `000080`; this is
emulator composition, not a claim that those exact SELECTs are the pinned
executable's unrecovered startup submission.

The direct catalog's `RentCodeName` and random-group `RefMagicGroupID` are
carried through `DropRecord`/`DropGroupCandidate`, `GroundItemSpec`, and the
runtime `Item` as unresolved source tokens. The
maintained schema has no recovered rent-reference or magic-option-group
catalog, so it does not invent `RentInfo` or option values from those tokens;
resolving those metadata catalogs is a separate follow-up boundary.

The retained SQL reference contains no `_SupportGoldConfig`, so the
profile-driven generator (`0x00725FD0`) has no rows and its predicate is false
for this deployment. The named exception generator (`0x00726020`) is implemented
for the resulting class-`3/8` path. The profile data itself is no longer an
unknown: the pinned server
registers `_REFDROPITEMASSIGN`, `_REFDROPOPTLVLSEL`, and these ten concrete
class-selection tables:

- `_REFDROPCLASSSEL_EQUIP` and `_REFDROPCLASSSEL_RAREEQUIP`;
- `_REFDROPCLASSSEL_RECOVER`, `_CURE`, `_AMMO`, and `_SCROLL`;
- `_REFDROPCLASSSEL_REINFORCE`;
- `_REFDROPCLASSSEL_ALCHEMY_TABLET`, `_ALCHEMY_MAGICSTONE`, and
  `_ALCHEMY_ATTRSTONE`.

Their retained CSV exports are present under
`framework/docs/db/table_data/` with explicit headers. Class-selection rows
are keyed by monster level and contain `ProbGroupN` columns;
`_RefDropItemAssign` contains `Service`, `RefItemID`, relative and absolute
probabilities, assigned group, and drop count; `_RefDropOptLvlSel` contains
option level, probability, and required online time. These are the missing
normalized runtime inputs, not speculative profile records.

At image base `0x00401000`, coordinator `0x007245C0` calls
`0x00725FD0` only when the monster reference has a matching profile. That
function walks category indices `0..15`, stopping at the already-derived
inventory cap. Helper `0x00725990` first resolves the category from the
monster reference's vector rooted at `+0x2A8`; each entry is 32 bytes, with
the category discriminator at `+0x00` and the selected count range at
`+0x0C/+0x10`. Only configured categories reach its switch. Recovered
concrete bodies are category `0` (equipment plus per-entry magic-option
rolls), `6` (one of four reinforcement-recipe codenames), `10` (assigned
item selection), and `14` (the separate special producer at `0x00725820`);
categories `1`, `11`, `12`, and `13` explicitly return without output.
After this profile pass, `0x007245C0` still runs random-group and direct
assignments, then cycles the eight generic class selectors through
`0x00726900` and `0x00726A70`. Implementing this branch therefore requires
the twelve retained tables plus the reference-item classification/option
builders; treating it as one extra drop-ratio row would change both output
families and PRNG consumption.

Migration `000100` and `import-drop-catalog` now provide that lossless
normalized input. `REF_DROP_CLASS_SELECTION` stores every level/group cell;
`REF_DROP_ITEM_ASSIGN` preserves source-row identity because the retained file
contains legitimate duplicate item/group tuples and signed
`AssignedGroup = -1` sentinels; `REF_DROP_OPT_LEVEL_SELECTION` retains all
thirteen option rows. Agent startup hydrates these tables into an immutable
`DropProfileCatalog`, classifying selectable assignments by the retained item
TypeIDs and rarity. Its assignment primitive mirrors `0x00724120`: source
rows contribute their `Prob_Relative` weight, one candidate is selected from
that weighted population, and a second `rand()%100 <= Prob_Absolute` roll is
inclusive and is consumed even when the absolute value is 100. Class-group
selection now mirrors `0x006AA6D0`/`0x007244F0`: positive probabilities above
`0.000001` are accumulated, multiplied by one million and truncated; the
two-15-bit random value is reduced by `1,000,000 / classMultiplier`, receives
the native item-factor transform, and lower-bounds that cumulative map to a
zero-based group. Assignment sentinel `-1` resolves as `ItemClass-1`, while
an explicit positive group is decremented once, matching `0x006AA810`.

The maintained coordinator now runs random-group assignments, direct
assignments, and then the recovered repeated generic selector cycle. Gold,
recovery, cure, reinforcement recipes, alchemy tablets, ammunition/scroll,
and magic/attribute-stone pairs use the native cap, pass count, class
multiplier, family choice, country filter, weighted assignment, absolute
gate, and source drop count before ordinary world materialization. Gold is
inside that repeated cycle: low drop-class nibble `4`, for example, performs
five gold/class passes. Equipment and rare-equipment now additionally build
option level, variance, durability, and magic options through
`0x00724400`/`0x007276A0` before ordinary world materialization. The retained
class-`3/8` named exceptions run before random/direct rolls and the generic
cycle, matching coordinator `0x007245C0`.

The first two equipment builders are now closed. `_RefDropOptLvlSel` stores
cumulative probabilities: `0x006AA620` converts each probability to a
million-point integer and stores source `OptLevel+1`; `0x00724400` consumes
two 15-bit draws, selects the first cumulative threshold, clamps the stored
level to `1..8`, and returns `0..7`. Base variance dispatch
`0x00727590` selects six 5-bit fields for armor and eligible shields, seven
for weapons, and two for accessories. Each field is
`floor(min(rand()/32767, rand()/32767, rand()/32767) * 31)` through
`0x00729280`. Non-accessory durability uses field zero to interpolate the
reference `Dur_L..Dur_U` range; accessories store data value `1`. Agent now
hydrates the required TypeIDs/durability bounds and emits these base states
together with the option catalog described below.

`0x007276A0` consumes
the item-family candidate lists assembled from `magicoptionassign.txt`, magic
definitions from `magicoption.txt`, including its trailing family/minimum-class
pairs. Migration `000102_drop_magic_option_catalog` normalizes 537 retained
assignment entries and 765 applicability entries while extending
`REF_MAGIC_OPTION` with the native drop weight. `refmagicoptgroup.txt` remains
the separate named/set-option group catalog, and
`refmagicoptbyitemoptleveldata.txt` remains the preauthored item-option value
catalog; neither drives this ordinary equipment-drop branch. Normal equipment has a 30-percent admission
branch before option-count selection, while rarity-2 equipment bypasses that
gate. Four independent `rand()%6+1` draws choose the minimum requested option
count, bounded by the item's `MaxMagicOptCount` and finally by the twelve-slot
wire limit. Candidate selection (`0x0072F410`) concatenates two 15-bit
`rand()` results, reduces them modulo one million, and lower-bounds a
cumulative-weight vector. Candidate eligibility (`0x0072F2D0` / `0x0072FF60`)
then accepts either an exact item reference or an encoded equipment-family
record with country and minimum-item-class constraints. The builder rejects
duplicates against its temporary option list. `MATTR_NOT_REPARABLE` consumes
the attempted slot without adding a normal magic parameter, which is why the
attempt count and emitted count can differ. The maintained generator now
performs these transitions and persists the resulting options with the ground
item.

The coordinator at `0x007245C0` fixes the generic eight-slot order as gold,
equipment, recovery, cure, reinforcement, tablet, ammo/scroll, and
magic/attribute stone. The equipment slot calls `0x00726A70` once: in its
default path `rand()%1000 == 1` switches that single selection from the normal
assignment vector to the rarity-2 vector. Normal and rare equipment are not
independent drop attempts; Agent mirrors this branch before its class roll.

## Lifecycle

```text
reference tables / decoded drop records
              |
              v
 gold range/ID branch (0x00724330 -> 0x00726900) -+
 profile generation (0x00725FD0 -> 0x00725990) ---+
 hard-coded exception generator (0x00726020) ------+
  assigned direct rolls (0x00724A00) --------------+
                                                     +--> in-memory drop-item list
  random-group rolls (0x00724E30) -----------------+         |
                                                    v
                       0x004C12B0 passes the list into 0x004B7680
                       -> CGNPCInventoryOP source storage (mob +0x1C10)
                       -> factory (0x00489730) -> UID index + source context binding
                                                    |
                         +--------------------------+-------------------------+
                         |                          |                         |
                         v                          v                         v
                source-storage teardown       shared update loop       visibility entry
                0x004A72E0 -> 0x004B75D0      0x304D / expiry           0x3015 boundary
                -> 0x00485E50 -> 0x00417090
                (confirmed removal)           (confirmed events)        item +0x39C = 0x00485740
                                                                        -> 0x004858E0
                                                                        -> CMsgBlockObject -> 0x3015
                                                    |
                                                    v
 player 0x7034 { u8 action=6, u32 targetObjectUID } --+
 COS    0x7034 { u8 action=0x11, u32 fieldA, u32 targetObjectUID } -+
       -> actor-specific action handler -> validation / item-transfer virtual
                                                    |
                         +--------------------------+--------------------------+
                         v                                             v
           active-update / attachment leave             outer manager removal
           (+0x364 -> dynamic +0x0C)                    -> UID-index erase / pool return
                         |                                             |
                         +--------------------------+------------------+
                                                    v
                                       pickup animation 0x3036
                                       -> AQ_SlotUpdater queue / SQL worker
                                       -> player owner callback: 0xB034
                                       (COS owner: 0x004D4AB0 hook)
```

The diagram's downstream SQL label is reached by both confirmed action
handlers after successful transfer/animation: COS/action-`0x11` continues
from `0x004D4F60` through `0x0050FA50`, while player action `6` continues from
`0x0050F5C0` through `0x0050F1F0` and the same updater/queue family. The
successful record-`+0x38 == 1` branch is `AQ_SlotUpdater` (`0x0045D9E0`),
whose metadata-driven slot `UPDATE` remains downstream of the animation. The
ordinary/player SQL-worker success callback then reaches `0x004E73D0` case `8`
and `0x004E9480`, which builds `0xB034`; COS uses its separate
`0x004D4AB0` owner hook. The common transfer virtual itself still does not
own this persistence call.

The two roll functions build item descriptions into a list owned by their caller (`param_4` in both decompilations). They do not themselves create a visible world object, send a client packet, persist an item, or remove a ground object. The next caller is now recovered: `0x004C12B0` calls the coordinator `0x007245C0` at `0x004C143D`, then passes the produced list to `0x004B7680` from `0x004C1456`.

`0x004B7680` is a reusable storage-population helper: it initializes a caller-supplied storage context, iterates candidates from the supplied list, and invokes the runtime-object construction chain before returning success or failure. At `0x004B787A` it calls `0x00485E70`, a wrapper around the global manager at `0x00D6A960`; that wrapper dispatches to `0x00489730`. The latter resolves a factory from the source reference, calls that factory's virtual slot `+0x350`, and returns the constructed runtime object. Back in `0x004B7680`, the result passes type/state validation, is appended to the caller storage, and receives a virtual `+0x504` call at `0x004B7995` with the storage-owner context. For the `typeID1/typeID2 == 3/3` item family, that vtable slot resolves to `0x0048F990`: it stores the context pointer at object `+0x14C` and copies the context's `+0x08` value to object `+0x154`. In the recovered monster-drop caller, `0x004C12B0` pushes its runtime receiver (`EBX`) as `0x004B7680`'s third stack argument at `0x004C1463`; `0x004B7995` passes that same argument to `+0x504`. Thus the monster-drop item receives the initiating runtime-object pointer at `+0x14C`, not a world-layer pointer. This confirms construction, global UID-index registration, source-storage retention, and source-context binding before the helper returns. The subsequent item `+0x2B0 -> CGObj::EnterWorld -> item +0x39C -> 0x004858E0` path provides the direct world-layer attachment and `0x3015` visibility edge documented below.

The three immediate successor calls in `0x004C12B0` are ruled out as materialization: `0x004077F0` is a bounds-checked vector element getter, `0x00432820` destroys/clears a list of wrapper nodes, and `0x00695450` clears/frees a temporary list. The seven recovered item RTTI classes and the structure subset below do not constitute a complete C++ declaration; the final service identity selected by the connection table, complete packet grammar, source-level names for several operation-context pointers, and COS completion-hook response semantics remain unrecovered.

### Loot opcode reference

This matrix records only wire fields and direction/builder boundaries that
are recoverable in the pinned executable. It deliberately does not turn a
repository protocol label into a claim about a complete packet layout.

| Opcode | Direction/binary boundary | Confirmed payload or role | Lifecycle scope |
|---|---|---|---|
| `0x7034` | registered inbound action family | player `{ u8 action=0x06, u32 targetObjectUID }`; COS `{ u8 action=0x11, u32 fieldA, u32 targetObjectUID }` | pickup request; COS `fieldA` semantics unrecovered |
| `0x3015` | `CGObj::EnterWorld -> item +0x39C = 0x00485740 -> 0x004858E0 -> CRgnObject +0x18 = 0x00538A00 -> CMsgBlockObject +0x08 = 0x00534340 -> 0x00533F70` | polymorphic item serialization; variable item-data body, with conditional item-state extension | socket handoff is confirmed through the C8258C `+0x50` route; selected service identity and complete body grammar remain unresolved |
| `0x3016` | `CMsgBlock` family `+0x0C -> 0x005344B0 -> 0x00534050` | exactly `{ u32 objectUniqueID }` | emitted when a prior CMsgBlockObject region attachment is removed; successful pickup reaches this only through the conditional `+0x364 -> 0x00485150` leave edge, while expiry remains unrecovered/conditional |
| `0x3017` | `CMsgBlock` family pre/post-recipient helper `0x00533E70` | exactly `{ u8 mode (1 enter / 2 leave), u16 groupedRecordCount }`; count is returned by `0x00533B90` | generic message-block control/update adjacent to 3015/3016; not loot-specific proof |
| `0x3018` | `CMsgBlock` family recipient virtual `+0x278`/`+0x27C` from `0x00533E70` | opcode construction is confirmed; no explicit payload field is recovered in the server branch | generic message-block finalization/control; body semantics unrecovered |
| `0x3019` | `CMsgBlock` family helper `0x00533E70` via `0x00430A20` | grouped bytes are built from selected objects' virtual `+0x498(mode=2)` serializers and chunked with recipient/object context; complete record grammar is unrecovered | generic message-block object/control update; not a ground-item-specific packet |
| `0x3036` | `0x004D51DE`, `0x0052643C` -> `0x00484D90` | exactly `{ u32 collectorUniqueID, u8 heading }` | successful pickup animation |
| `0xB034` | player owner completion `0x004E9440 -> 0x004E73D0` case `8` -> `0x004E9480 -> 0x004F76E0` | leading `{ u8 status=1 }`; action `0x06` adds `u8 record+0x04`; action `0x11` adds `u32 record+0x30` then `u8 record+0x04` (bounded fragment) | ordinary/player success response after SQL-worker success; complete grammar unrecovered |
| `0xB0C5` | COS handler `0x004D4F60` failure/result branches | parser-confirmed `{ u8 mode, u8 subcommand, [u16 modeValue], u32 objectOrContextID, ... }`; subcommand `8` may add `u32 amountOrCount` | COS command/result response; not a proven successful-pickup persistence acknowledgement |
| `0x304D` | `0x004901C0 -> 0x00484D90` | exactly `{ u32 objectUniqueID }` | timed ownership-release marker clear; not despawn |
| `0x304E` | `CGObjPC` amount-credit writer `0x004E3FB0` | exactly `{ u8 selector, u64 value, u8 trailing }` for recovered pickup call shape | player amount-credit/gold state update |

### Client peer handlers and SQL boundary

The client evidence below is tied to the compatibility-patched vSRO 1.188
image identified above. The preserved stock comparison establishes that its
only byte differences are the ATL/DEP compatibility sites and code cave; the
following handler addresses remain valid for this exact patched image. These
are client receive-side effects, not a claim that a particular server removal
path emitted each packet.

| Opcode | Client registration/handler | Confirmed client read and effect | Cross-binary implication |
|---|---|---|---|
| `0x3015` | registration `0x0086FBB0`; `OnObjectEntered 0x008A6CA0 -> 0x008A6BD0(stream, 1)` | first reads a `u32` object/reference discriminator; `-1` reads a following `u16`, `-2` reads `u32` then `u16`, and other values resolve through `0x009C1FA0`. The resulting client object receives virtual `+0x64(stream)` and `+0x68(stream)` calls. For CIItem, the RTTI-confirmed secondary subobject (`offset 0x54`) maps `+0x64` to `0x009E1D00` (initial parser) and `+0x68` to `0x009E1A30` (state-update parser). | confirms a dedicated polymorphic client single-enter route and the CIItem parser dispatch, but not a fixed ground-item body or complete factory-to-packet edge |
| `0x3016` | `OnObjectLeft 0x008A6CC0` | reads exactly `u32 objectUniqueID`, resolves it through `0x009C31A0`, then tail-dispatches the resolved object's virtual `+0x4C` with zero | independently corroborates the server's one-UID removal wire shape and proves client object-side removal handling |
| `0xB034` | registration in `0x0086FBB0`; `CNetProcessIn::OnB034 0x0087F370` -> parser helper `0x0087EB50` | success form reads status `1`, operation byte, then action `0x06` selector byte; action `0x11`/`0x1C` reads a four-byte `+0x30`/`+0x34` value followed by the selector byte; complete branch grammar is not recovered | independently corroborates the server `0x004F76E0` field widths/order for the player success response |
| `0x3017` | registration `0x0086FD74`; handler `0x008AC0E0` | reads a `u8` then a `u16` through `0x004F7220`, requires a nonzero first value on the normal path, and updates a client message/state record; no ground-item lookup is performed | corroborates the server's 1-byte/2-byte control shape and accepts the server's enter/leave values `1`/`2`, but does not itself assign those semantics |
| `0x3018` | registration `0x0086FDB6`; handler `0x008AC3D0` | dispatches through `0x008A6D20`, which reads `u8 batchKind` (`1` = repeated `0x3015`, `2` = repeated `0x3016`), then `u16 count`, and loops the corresponding handler; local state is stamped with opcode `0x3019` | generic visibility batch/control packet; server-side writer of the batch body remains indirect |
| `0x3019` | registration `0x0086FD8B`; handler `0x008AC170 -> 0x00833380` | allocates/forwards a generic buffered message path; a fixed object/ground-item payload is not recovered | generic message-block object/control update; not proof of a ground-item spawn body |
| `0x3036` | `On3036 0x008A78B0` | reads `u32 collectorUniqueID`, then `u8 heading`; resolves the actor through `0x009C3220`, converts the heading to the client rotation representation, and issues local action `10` when its predicate allows | independently corroborates the server pickup-animation layout; it is an animation/update, not the item-removal packet |
| `0x304D` | `On304D 0x008A75C0` | reads exactly `u32 objectUniqueID`, resolves the object, clears fields at `+0x258` and `+0x254`, then calls `0x008AF420` with the object/UID context | independently corroborates the server ownership-release UID payload and shows a client state-clear effect; field names remain unrecovered |
| `0x304E` | registration in `0x0086FBB0`; handler `0x008A70D0` | reads a selector byte and then selector-specific data: `1 -> u64 value, u8 status`; `2 -> u32 value, u8 status`; `3 -> u16 value`; `4 -> u8 value, u32 value`; `0x10 -> u32 value`. Selector `1` updates the client gold/inventory UI and uses the status byte to gate the gain message | independently corroborates the server amount-credit writer's selector-1 `{ u64 value, u8 trailing }` body; the server's trailing zero is the recovered call shape |

Client registration also proves that all eight listed values are inbound handlers in
`CNetProcessIn`. This does not turn server packet construction alone into a
delivery guarantee, but it eliminates a direction ambiguity for this exact
client profile.

The client gives `0x3018` a stronger generic visibility meaning than the
server's indirect writer alone exposes. `0x008AC3D0` routes its stream to
`0x008A6D20`; that parser reads a one-byte batch kind and a two-byte count,
then invokes `0x008A6BD0` (the `0x3015` body handler) once per entry for kind
`1`, or `0x008A6CC0` (the `0x3016` body handler) once per entry for kind `2`.
This proves a client-side enter/leave batch envelope, but does not connect the
envelope to a particular ground-item spawn or pickup removal on the server.

The client has a broad `0x7034` construction family. Generic dispatcher
`0x007950B0` and the specialized callers at `0x007978A0` and `0x00826460`
test opcode support, construct a local request, serialize it through
`0x007923A0`, and submit it through `0x008418D0`. The serializer itself first
writes exactly one byte from request-record `+0x00`, then switches on that
same byte to serialize action-specific tails. This confirms the client emits
an action discriminator at the start of its `0x7034` payload. The two
specialized callers are item-mall workflows, not a recovered ordinary
ground-pickup gesture.

The serializer switch is now mapped rather than inferred from call-site
literals. The byte at `0x00792CE4 + action` selects an index into the pointer
table at `0x00792C80`; the table covers action bytes `0x00..0x2B`. Action
`0x06` selects target `0x00792673`. That target has no extra action-6
specific write: it falls through to the common tail, which writes either one
byte from request-record `+0x04` when the serializer's second argument is
`1`, or four bytes from `+0x0C` otherwise. Thus the client binary contains a
real action-6 serializer branch, but this branch alone does not establish
which record form the gameplay pickup path supplies. Action `0x11` selects
the same target and, before the common tail, writes four bytes from `+0x30`;
when the second argument is not `1`, its common tail then writes four bytes
from `+0x0C`, giving the same two-four-byte width/order as the server-side COS
layout. That is only serializer capability: the `fieldA`/target semantics
remain independently confirmed by the server parser, and no gameplay caller
that constructs action `0x11` was recovered.

The action-byte and pointer-table bytes at these addresses are identical in
the pinned compatibility-patched client and the stock vSRO 1.188 comparison
image, so this serializer result is not introduced by the compatibility patch.

For auditability, the complete action-to-target map is:

```text
00:2507  01:25C1  02:25FE  03:25FE  04:2639  05:265E  06:2673  07:26BB
08:26F4  09:29AC  0A:2A9E  0B:2A88  0C:2B3E  0D:2A9E  0E:2A27  0F:29FC
10:2507  11:2673  12:26BB  13:26F4  14:29AC  15:2A27  16:29FC  17:26BB
18:286F  19:2C75  1A:2AB4  1B:2AB4  1C:2673  1D:2AC8  1E:2B0C  1F:2B0C
20:2B3E  21:2B3E  22:2A52  23:2B54  24:2B54  25:2C75  26:2C75  27:2C21
28:2C16  29:2C43  2A:2C2E  2B:26F4
```

The values are function RVAs relative to `0x00790000` (for example `06:2673`
means `0x00792673`). The no-op target `0x00792C75` is the common epilogue.
Client evidence therefore now proves that action 6 is representable by this
serializer, but still does **not** prove that a recovered gameplay caller
constructs it, nor does it replace the server-confirmed player
`{ 0x06, targetUID }` and COS `{ 0x11, fieldA, targetUID }` layouts.

This negative boundary is stronger than a name search. A complete direct-call
scan of the pinned image finds exactly 15 `call 0x007923A0` sites:
`0x007952B1`, `0x00795584`, `0x00795641`, `0x00795794`, `0x00795909`,
`0x00795C97`, `0x007961B0`, `0x007963EE`, `0x007967DD`, `0x00797CE6`,
`0x008260EB`, `0x008261D9`, `0x00826271`, `0x00826339`, and `0x008265C3`.
The generic branches visibly initialize other action records (including
`0x1E`, `0x1B`, `0x14`, `0x23`, and `0x2A`), while the specialised branches
belong to the already-bounded mall workflows. No direct serializer caller
statically constructs an action-`6` record. This is not proof that a client
can never send pickup: a dynamically selected path, another serializer, or
runtime-modified code remains possible. It does prove that the currently
recovered `0x7034` serializer call graph contains no ordinary pickup sender.

The opcode boundary was also checked independently of the serializer. The
literal instruction form `push 0x7034` occurs 66 times in the client `.text`
section, always in the bounded construction regions `0x00795261` through
`0x00797CAE` or `0x0082602B` through `0x00826586`. These are paired
support-check/buffer-construction uses around `0x00841780` and
`0x0053CEA0`; there is no additional literal-`0x7034` sender elsewhere in
the executable. Within the main dispatcher, all immediate writes to the
local action-record byte include `0x27`, `0x28`, `0x24`, `0x09`, `0x02`,
`0x1E`, `0x1B`, `0x13`, `0x08`, `0x2B`, `0x03`, `0x01`, `0x1D`, `0x1F`,
and `0x14`, but not `0x06`. This covers ordinary immediate opcode and action
construction; it does not exclude a packet whose opcode or action is loaded
from data at runtime.

UID-based interaction tracing does not produce a hidden pickup sender. The
three closest object-lookup/send clusters are `0x00794726 -> 0x009C3220`,
`0x008A62CE -> 0x009C3220`, and `0x008AFC70 -> 0x009C3220`. In each case the
subsequent packet builder is an unrelated interaction: the first performs an
opcode-`0x70C5` support check and submits through `0x008418D0`, while the
latter two explicitly test and allocate opcode `0x70C5` at `0x008A6301` and
`0x008AFD11` before the same submit routine. The `0x007A0B6D` occurrence of
`0x3016` is inside client-side removal handling/construction logic, not an
outbound `0x7034` sender. These clusters demonstrate that the shared object
lookup routine is used by other client interactions and cannot be used as a
proxy for ground pickup. No cluster connecting `0x009C3220` to a literal or
statically selected `0x7034` action-6 record was recovered.

The client executable has no direct ODBC, MySQL, SQLite, or SQL imports and
no matching SQL/database query literal in the pinned image's ASCII/UTF-16
scan. Client receipt of spawn, ownership, pickup-animation, and removal
packets therefore has no direct SQL execution boundary in this binary. Durable
item and inventory SQL remains server-side or otherwise external to the
client process.

`0x375C` is an immediate offset, not an opcode, at both scanner hits in this
build; `0x3138` has no hit. Neither may be substituted for a pinned-binary
loot packet.

### Source dispatch above the drop producer

The call into `0x004C12B0` is virtual rather than a standalone direct
call. MSVC RTTI identifies primary vtable `0x00AEC0A4` as `CGObjChar`; its
virtual `+0x350` is `0x004A6F40`. That routine first requires its
`0x0048B4A0` gate to succeed, then invokes the receiver's virtual `+0x574`
and `+0x580`, both with its final stack argument, before performing its
following cleanup/state work. Primary vtable `0x00AEDDAC` is identified as
`CGObjMob`; its `+0x574` entry is exactly `0x004C12B0` and its `+0x580`
entry is `0x004A6910`.

This establishes the executable dispatch chain:

```text
CGObjChar virtual +0x350 (0x004A6F40)
  -> gate 0x0048B4A0
  -> CGObjMob virtual +0x574 = 0x004C12B0 (drop-list generation/placement)
  -> CGObjMob virtual +0x580 = 0x004A6910
  -> caller's subsequent lifecycle cleanup/state work
```

The two virtual hooks must both report success for that caller to continue.
This is the strongest recovered source-side attachment for the loot
producer. The source-level semantic name of `CGObjChar +0x350`, its input
argument, and the exact gameplay event that reaches it remain unrecovered;
it must not be relabelled as a death callback merely because the mob hook
creates drops.

The sibling `CGObjMob +0x580` hook is not a second drop construction path.
`0x004A6910` is a small forwarder: it takes the same one stack argument,
addresses `mob + 0x1C34`, and calls `0x004B8000`. That dispatcher resolves a
registered subordinate handler and invokes its virtual `+0x14` with the
argument. The forwarder itself has no call to the drop coordinator/factory,
global object-manager admission, visibility writer, packet builder, or SQL
queue. Its subordinate-handler semantics remain outside the recovered loot
path.

### Runtime factory selection

The factory resolution inside `0x00489730` is `0x00489DE0`. It reads the packed type word from the selected `_RefObjCommon` record at `+0x80` and dispatches through a hierarchy of factories; it is not one generic item constructor. The first three decoded selectors are:

```text
typeID1 = (packedType >> 2) & 0x07
typeID2 = (packedType >> 5) & 0x03
typeID3 = (packedType >> 7) & 0x0f
```

The first selector chooses a top-level factory family. Subsequent branches select factory routines by the second and third selectors, including the item-family branches used by gold and ordinary item references. The final subtype selector is also inspected in later branches. This matches the `_RefObjCommon.TypeID1..TypeID4` hierarchy and rules out an implementation that creates all loot as a single undifferentiated ground-item structure. Exact branch-to-C++-class names and subtype-specific field layouts remain unrecovered.

For the item top-level branch (`typeID1 == 3`), the executable further proves this routing:

| Type selector | Factory |
|---|---|
| `typeID2 == 1` | `0x004959F0` |
| `typeID2 == 2`, `typeID3 == 1` | `0x00492B70` |
| `typeID2 == 2`, `typeID3 == 2` | `0x00493A10` |
| `typeID2 == 2`, `typeID3 == 3` | `0x004A2D70` |
| `typeID2 == 3` | `0x0049A840` |

`0x0049A840` obtains an object from an object pool, invokes its virtual reinitialization slot, and marks the pooled instance active at `+0x140`. The reference row for `ITEM_ETC_GOLD_01` is `TypeID1/2/3/4 = 3/3/5/0`, so gold uses this last factory path. The same path covers any other `typeID1 == 3 && typeID2 == 3` reference; the binary does not yet distinguish which references may use a further subtype-specific implementation.
MSVC RTTI identifies the resulting vtable `0x00AEB2B4` as `CGItemExpendable`; this establishes the class name for the recovered gold path without proving that every `CGItemExpendable` instance is gold.

### Active-world registration

The `0x004B7680` construction call supplies final argument `1` to
`0x00485E70 -> 0x00489730`. With that flag, `0x00489730` allocates a new
unique ID from the manager's counter structure at `manager + 0x04` through
`0x00489560`, passes the ID to the selected factory's `+0x350` initializer,
then calls `0x004117E0` with the returned object, the global object manager
at `0x00D6A8E8`, and that ID. `0x004117E0` inserts the object into the
manager's `+0x22B4` index. The target resolver `0x00485D90`, used by the
confirmed `0x7034` pickup handler, queries precisely that same
`0x00D6A8E8 + 0x22B4` index. This is a direct construction-to-resolvability
edge: a successfully placed drop is live in the global object registry
before it can be selected for pickup.

The successful pickup path erases the object from this same index in
`0x00417090 -> 0x00489FC0`, closing the confirmed registry lifecycle.
Neither insertion nor erasure directly calls the per-recipient
`0x005344B0` visibility-removal routine, so this evidence must not yet be
expanded into a claim that registration itself emits `0x3015`, or that
pickup removal itself emits `0x3016`.

This registry lifecycle is also distinct from the executable's generic
map/layer entry implementation. `0x00485360` is the `CGObj::EnterWorld`
routine identified by its diagnostics; it resolves world and layer state,
performs a move operation, and queues world-context events. The recovered
`CGItemExpendable` factory initializer is virtual `+0x350 = 0x0048F930`;
it calls `0x00484B80` to initialize the pooled item and its unique ID, then
returns to `0x00489730` for global-index insertion. That initializer itself
does not call `0x00485360`, but the recovered mob route has a subsequent,
direct world-entry edge.

After candidate marker initialization, `0x004C3F60` invokes the candidate
item's virtual `+0x4DC` at `0x004C425C`. All seven recovered ground-item
vtables map that slot to the same `0x0048FDF0` implementation. That routine
sets the item's placement fields `+0x7C` through `+0x90`, performs its
placement/randomization work, clears the source context through virtual
`+0x504(0)`, and calls the item's virtual `+0x2B0` at `0x004900A4`. The
concrete target is `0x0048FA30`, which calls `CGObj::EnterWorld`
(`0x00485360`), resets the lifetime accumulator at `+0x3C`, and appends the
item to the active-update list through `0x00417D30`. Therefore the recovered
mob route has this confirmed post-factory lifecycle:

The positional call shape of the placement helper is also recovered, without
inventing names for its unknown fields:

```text
0x0048FDF0(item,
           param2, param3, param4, param5, float param6,
           param7, float param8, u32 param9)
  item + 0x7C = 0
  item + 0x80 = param4
  item + 0x84 = param5
  item + 0x88 = param6
  item + 0x8C = param7
  item + 0x90 = param8
  item + 0x2B0(..., param9)
```

The mob caller constructs this shape with `param9 = 5`. The other arguments
include the source-provided world/position words and randomized placement
offsets; their gameplay names are not recovered. The item +2B0 call retains
the same final value, which is the value later observed by `+0x39C` as its
mode byte.

```text
CGObjMob +0x574 (0x004C12B0)
  -> 0x004C3F60 candidate placement
  -> item +0x4DC = 0x0048FDF0
  -> item +0x2B0 = 0x0048FA30
  -> CGObj::EnterWorld (0x00485360)
  -> active-update insertion (0x00417D30)
```

This is a factory-to-world-entry and update-registration edge. The argument
construction is now exact:
the `push 5` at `0x004C4203` is the final `param_9` of `0x0048FDF0`; that
routine passes `param_9` as the final argument of item `+0x2B0`, and
`0x0048FA30` forwards the same value through `CGObj::EnterWorld` to the
seventh argument of item `+0x39C`. `0x00485740` compares that byte with
`7`, so this recovered mob-drop placement route supplies mode `5` and takes
the non-`7` branch. The successful branch converges at `0x004857F5` on
`0x004858E0`, then reaches the selected `CMsgBlockObject` attachment and its `0x3015` writer as
described below.

The world-entry completion call is now more specific. At `0x00485457`,
`CGObj::EnterWorld` resolves its world entry through global
`DAT_00D6A9A4` and `0x005F6B80`; after `MoveTo`, its
`0x0048568E..0x0048569F` slice calls `0x005F7330`. RTTI identifies the
returned region-manager body as `CRegionManagerBody` (vtable
`0x00B431FC`, constructor `0x0098A560`), whose virtual `+0x38` is
`0x00529760`. That function is only `mov eax,1; ret 0x0c`: it reports
region-manager admission success and contains no call to `0x004858E0`,
`CMsgBlockObject`, `0x00534340`, `0x005344B0`, or a packet allocator. This
callback is status-only, but `CGObj::EnterWorld` then invokes the object's
virtual `+0x39C`; for every recovered ground-item vtable that is
`0x00485740`, whose successful mode-5 branch calls `0x004858E0`. The
separately recovered `CRgnObject +0x18 -> CMsgBlockObject` attachment path
is therefore the continuation after the status callback, not an inference
from the callback body itself. Evidence:
`analysis/decompiled/world-enter-events/CRegionManagerBody_enterworld_handoff.txt`.

`CGObj::EnterWorld` also emits event-bus notifications around the movement
step. In the recovered `0x00485360` assembly, the object event source at
`object + 0x40` is queried with `0x0063D4A0(..., eventID=3)` and, when not
already handled, dispatches `0x0063D190(category=0, eventID=3, args={object})`
before `MoveTo`; after a successful move it repeats the query and dispatches
`0x0063D190(category=2, eventID=3, args={object})`. These calls construct no
`0x3015`/`0x3016` packet and have no direct call to the `CMsgBlock` writers.
They provide an independent event-bus boundary; the concrete item
`+0x39C -> 0x00485740 -> 0x004858E0` path is the recovered visibility
handoff. The subscriber registration and callback that might add additional
loot behavior to event ID `3` remain unrecovered.

The event infrastructure itself is now bounded. `CGObj` construction at
`0x00484800` passes `object + 0x40` to `0x0063CE10`; that initializer installs
the `CEventHandlers` vtable at the source, constructs three `0x0C`-byte event
records with `0x0063D5A0`/`0x0063CEA0`, and initializes a secondary structure at
`source + 0x28`. The base `CGObj` vtable's `+0x2D4` slot is concrete:
`0x00485B60` returns `object + 0x40`, so both the removal path and the generic
world-event setup use the same object event source. `0x0063D4A0` is the
presence test used by `EnterWorld`, while
`0x0063D190` walks the selected record tree and invokes a registered record's
object virtual `+0x04` with the event argument. The dispatch routine returns
the concrete status values `0`, `2`, `3`, or `4` on its recovered branches;
those values are framework results, not loot opcodes.

`0x005F1DB0` is one recovered source-registration workflow. It obtains the
source through virtual `+0x2D4`, allocates several `FLAG_WORLD_EVENT_HANDLERS`
records with `0x0063D070`, and conditionally includes the RTTI-confirmed
`OnDropItems` record (`0x005F2000`, vtable `0x00B00064`). Its callback
`0x005F0BC0` only resolves the object and returns `2` or `3` according to the
object's virtual `+0x28` state; it does not construct `0x3015` or `0x3016` and
does not call the `CMsgBlock` family. This proves that an object can have an
event subscriber named `OnDropItems`, but it is not the missing observer-layer
visibility bridge.

The registration helper at `0x0063D070` is not a packet builder. Its machine
code initializes the subscriber record at the preserved object pointer, stores
the incoming category/index and callback/context words at record offsets
`+0x10`, `+0x14`, `+0x18`, and `+0x1C`, selects one of the three source slots,
then inserts the record through the tree helper `0x0063D750` and links the
secondary context through `0x0063C330`. Direct call sites at `0x004C2476` and
`0x004C25D4` construct records whose vtables are
`0x00AEE404` (`PANDORA_BOX_100LV::OnSummonedMobDead`) and `0x00AEE410`
(`PANDORA_BOX_100LV::OnPandoraDead`). A complete direct-reference scan found
no `0x0048xxxx` item-constructor or `0x0053xxxx` message-block caller of
`0x0063D070`; indirect registration remains possible, but the recovered
registration evidence does not connect event ID `3` to `0x00534340`,
`0x00533F70`, or `0x00534050`.

The non-`7` branch is not an empty fall-through. At `0x00485829` it invokes
the item's common virtual `+0x1C` and `+0x2C` predicates, then calls the
runtime `CRegionManagerBody` method `0x0098B300` through global
`0x00CC387C`'s vtable `0x00B431FC` slot `+0x30`. That method compares and
updates six-word region/position records and can return flag `0x10000000`.
When that flag is returned, item virtual `+0x250` resolves to
`0x00485FD0`; this formats a `MoveTo...` diagnostic and queues a movement
record through `0x00568090` on global `0x00D6A980`. On the successful branch,
`0x00485740` converges at `0x004857F5` on `0x004858E0`; this is the direct
mob-drop transition edge. `0x004858E0` resolves `CRgnObject +0x18` at
`0x00538A00`, obtains the selected `CMsgBlockObject` region attachment,
and reaches its `+0x08 = 0x00534340 -> 0x00533F70 -> 0x3015` writer.
The region-manager admission call itself is not the packet writer, but the
successful item `+0x39C` continuation is the recovered initial visibility
handoff. Evidence: `analysis/decompiled/world-enter-events/00485740_item_transition_bridge.asm`.

The binary has a separate generic object-manager admission route at
`0x00417240`. On its successful branch it calls object virtual `+0x2B0` at
`0x00417564`, then later calls `0x004117E0` at `0x0041767B`. Every recovered
ground-item vtable maps `+0x2B0` to `0x0048FA30`. That shared item method
calls `CGObj::EnterWorld` (`0x00485360`), resets the lifetime accumulator at
item `+0x3C`, and inserts the object in the manager's active-update list
through `0x00417D30` (manager `+0x2234`). This is a binary-confirmed generic
admission-to-update/registry sequence:

```text
object manager 0x00417240
  -> item virtual +0x2B0 = 0x0048FA30
  -> CGObj::EnterWorld 0x00485360
  -> active update-list insertion 0x00417D30
  -> global UID-index insertion 0x004117E0
```

It explains how an item admitted by that manager begins receiving the shared
virtual `+0x34C` updates used by ownership release and expiry. The separate
mob path above reaches the same `+0x2B0` implementation without a direct
call to `0x00417240`; both paths then enter `CGObj::EnterWorld`, whose item
virtual `+0x39C = 0x00485740` reaches `0x004858E0` on successful admission.
Thus the direct initial `0x3015` edge is recovered for the seven item
families, while the exact recipient/session selected by the message-block
transport remains unresolved. The distinct mob argument construction still
prevents the manager route's bounded mode values from being applied to a
drop.

A complete direct-call scan of the pinned server image finds exactly three
calls to `CGObj::EnterWorld` (`0x00485360`): `0x0048FAE4` inside the recovered
ground-item `0x0048FA30` implementation, plus `0x004A82D7` and `0x0052EF88`.
The latter two occur in other virtual families (`0x004A8260` and
`0x0052EF10`, respectively); neither target appears in any of the seven
recovered ground-item vtables. This rules out an additional statically direct
ground-item `EnterWorld` caller hiding an alternate observer-layer handoff in
this image. It does not rule out an indirect callback or a caller outside the
pinned executable.

The absence of the layer transition from this admission route is now bounded
by its actual virtual control flow, rather than only by a missing direct-call
search. Item `+0x2B0` (`0x0048FA30`) calls `CGObj::EnterWorld`
(`0x00485360`), whose item vtable `+0x39C` target is `0x00485740`.
That shared hook calls `0x004858E0` only when its seventh argument byte is
exactly `7`. The generic manager obtains the corresponding admission-mode
byte from `0x00437110`: its `request + 0x140` switch produces only `1`, `2`,
`3`, or `4` (the `2` case selects `2` or `3` from a subordinate predicate).
`0x00417240` passes that result as its earliest `+0x2B0` stack argument;
`0x0048FA30` forwards it from its `+0x40` argument to `CGObj::EnterWorld`,
which forwards it as the seventh argument to `+0x39C`. Therefore this generic
manager admission path reaches the non-`7` branch of `0x00485740`, not the
`0x004858E0` layer-transition branch. That conclusion is limited to the
manager path: it cannot be substituted for the distinct `0x0048FDF0` mob
placement call, whose mode is independently confirmed as `5`. Neither path
currently proves layer association, attachment creation, `0x3015`, or
`0x3016` for a newly admitted item.

### Source storage and source teardown

The `0x004C12B0` factory argument at `CGObjMob + 0x1C10` is not an opaque
drop-only work buffer. `CGObjMob` inherits the `CGObjChar` construction path
at `0x004A6AD0`, which initializes `+0x1C10` with the `CGStorage` vtable
`0x00AED54C` (and initializes a second `CGStorage` at `+0x1C34`). During the
recovered drop call, `0x004B7680` changes this first storage from its reset
state (`+0x18 = -1`) to `+0x18 = 1`, records the originating mob at `+0x14`,
stores its loop/capacity values at `+0x1C/+0x20`, and appends successfully
materialized item pointers to the storage vector at `+0x04`. This proves
that the source mob retains the constructed candidates in its embedded
`CGStorage`. The storage-kind resolver `0x004A6A50` maps kind `1` to the
global operation object at `0x00C66AAC`; its vtable `0x00AEC85C` has MSVC
RTTI type `CGNPCInventoryOP`. Thus this is an NPC-inventory operation policy,
not the `CGPCInventoryOP` selected by the player collector route. It still
does not establish a client-visible NPC inventory or initial layer admission.

The helper's storage role is independently confirmed by `0x004D6F70`: the
COS setup path calls the same `0x004B7680` with `COS + 0x704` and storage kind
`2`; failure immediately logs the binary string `Create Pet Inventory
Failed!! COSID[%d]`. This is a concrete non-drop caller that uses the same
factory/append logic to create pet inventory contents. It rules out describing
`0x004B7680` itself as a dedicated ground-placement or visibility routine.
For the mob route, only the storage kind and operation policy change (`1` /
`CGNPCInventoryOP`); no direct layer, observer, or packet operation appears
in either helper use.

The relevant `CGStorage` subset is directly visible in its initializer
`0x004B7500`, the factory helper, and the teardown loop:

| Offset | Confirmed use in the recovered source-drop route |
|---|---|
| `+0x00` | vtable; reset initializer installs `0x00AED54C` |
| `+0x04` | vector of runtime-item pointers; `0x004B7680` appends materialized candidates and `0x004B75D0` iterates it |
| `+0x14` | source/owner pointer; factory writes the originating `CGObjMob` |
| `+0x18` | storage kind; reset to `-1`, then factory writes `1`, resolved by `0x004A6A50` to `CGNPCInventoryOP` |
| `+0x1C` | caller-provided loop/count value stored by `0x004B7680` |
| `+0x20` | factory capacity/selection bound; must be zero before setup and is then set from the computed bound |

The complete recovered `CGStorage` kind dispatcher at `0x004A6A50` is
bounded to values `-1..6`. Its global operation objects and RTTI names are:

| Kind | Operation object | RTTI implementation |
|---:|---:|---|
| `-1` | `0x00C66A98` | `CGStorageOP` |
| `0` | `0x00C66AA0` | `CGPCInventoryOP` |
| `1` | `0x00C66AAC` | `CGNPCInventoryOP` |
| `2` | `0x00C66AB8` | `CGCOSInventoryOP` |
| `3` | `0x00C66AD0` | `CGChestOP` |
| `4` | `0x00C66ADC` | `CGGuildChestOP` |
| `5` | `0x00C66AC4` | `CGAvatarInventoryOP` |
| `6` | `0x00C66AE8` | `CGMagicCubeStorageOP` |

There is a distinct source-teardown removal route. Storage reset/destruction
`0x004B75D0` iterates that vector and calls `0x00485E50` for every non-null
item. The wrapper dispatches directly to global object-manager removal
`0x00417090`. The object teardown routine `0x004A72E0` calls this storage
destructor for both embedded `CGStorage` members at `+0x1C10` and `+0x1C34`
when the required global managers exist. Thus items still retained in the
source's first storage have a confirmed `source teardown -> 0x004B75D0 ->
0x00485E50 -> 0x00417090` removal route, separate from successful pickup,
reference-threshold expiration, and the `0x304D` ownership-release event.
Character cleanup `0x004DEC10` ends by calling `0x004A72E0`, providing a
concrete cleanup caller for this embedded-storage teardown. The
caller/event semantics of `0x004DEC10` are not used here to relabel the
source generation hook as a death callback, and the pickup transfer slice
still does not prove immediate removal from the source vector. See
`analysis/decompiled/pickup-transfer/004DEC10-source-storage-teardown.txt`.

`0x00417090` itself has a recoverable event-before-erase prefix. It builds a
`CEventArg` containing the object pointer and a zero word, obtains the same
event source through the target virtual `+0x2D4` (`0x00485B60` for the
recovered item families), dispatches through `0x0063D190`, and only then calls
`0x00489FC0` for UID capture/index removal. The dispatch is therefore a real
removal notification opportunity, but the recovered prefix contains no
`0x3016` construction or `CMsgBlock` call; the callback-to-visibility edge
remains indirect/unrecovered. The pickup attachment-leave helper
`0x00485150` separately dispatches event `(0,4)` and, when manager state
permits, `(2,4)`; a direct-reference audit finds no loot-specific subscriber
that turns either event into a recovered SQL or `0x3016` writer.

### Context and timed state

The item-family vtable at `0x00AEB2B4` confirms a shared base-state layout used by the pooled object. Its `+0x504` method (`0x0048F990`) stores its supplied context pointer at `+0x14C` and, when it is non-null, copies the context's `+0x08` value into `+0x154`. The actual `CGItemExpendable` reset path `0x0048F8A0 -> 0x004848E0` clears the following related item state:

```text
+0x140  state byte       = 0
+0x144  runtime field    = 0
+0x148  auxiliary value  = 0
+0x14C  context pointer  = 0
+0x150  ownership-release marker = 0
+0x154  context-derived value = 0
+0x158  ownership-release accumulator = 0.0
```

`0x00437030` is deliberately excluded from the item chain. Its sole direct caller is `0x004374C0`, the `PrepareForUserLogin` path that allocates and constructs a `LobbyEntry` (`0x00436F20` installs `LobbyEntry::vftable`). It copies a login/lobby record, increments the referenced session object, and stores `KERNEL32!GetTickCount()` at that *LobbyEntry* object's coincidentally numbered `+0x150`. Therefore neither `0x00437030` nor `0x00436F20` proves the item marker's producer, initial value, or timestamp semantics.

The source context for the recovered monster-drop route is exact. `0x004C12B0` retains its `CGObjMob` receiver in `EBX` and calls `0x004B7680` at `0x004C146A`, passing that same pointer as the latter's stack argument at `+0x10`. After `0x004B7680` has materialized a candidate with `0x00485E70`, it calls the new item's virtual `+0x504` at `0x004B7995` with that argument. For `CGItemExpendable`, this is `0x0048F990`, so the item receives `+0x14C = originating CGObjMob` and `+0x154 = originating CGObjMob(+0x08)`. This proves an origin/source association for the mob drop; it does not itself establish the marker value used by a later placement path.

The marker setter is now directly recovered as `0x004901B0`. With the item
in `EAX` and a supplied `u32` in `ECX`, it performs exactly
`item + 0x150 = ECX` and `item + 0x158 = 0.0f`. There are four direct
callers. At `0x004C41F3` and `0x004AAA88`, source-item placement code first
obtains a value through the source object's virtual `+0x08`, puts that value
in `ECX`, and applies the setter to the candidate item. This is the direct
initialization route that associates a source-derived marker with a placed
item; the virtual getter's semantic name and the exact source event remain
unrecovered. At `0x004B2B73`, an independent item operation passes
`ECX = 0xFFFFFFFF`, proving that `-1` is an intentional marker sentinel and
not an accidental uninitialized value. At `0x004E98AD`, a non-null internal
action-record field at `+0x118` is supplied as the marker. These call sites
prove the setter and its value domain, but do not justify renaming every
nonzero marker as an exclusive player owner.

The recovered mob generation path reaches this setter directly. The
`CGObjMob +0x574` producer `0x004C12B0` calls `0x004C3F60` at
`0x004C1DE3`; its candidate loop resolves entries from the receiver's
embedded storage at `mob + 0x1C10` and reaches the source-derived setter call
at `0x004C41F3`. This closes the machine-code edge from the documented mob
drop producer to marker initialization. The source parameter consumed by the
virtual `+0x08` getter is distinct from the factory's retained mob context,
so it must not automatically be called the mob, killer, or ownership player.

The item update at `0x0048F9B0` invokes its diagnostic/assert path when item `+0x14C` is non-zero, but then proceeds to the ownership-release marker check. When item `+0x150` is neither zero nor `-1`, it accumulates the supplied delta in float field `+0x158`, compares it against the binary's exact `30.0` threshold at `0x00B45E58`, and invokes `0x004901C0` when the threshold is reached. That helper first requires virtual predicate `+0xC4` to succeed, then clears `+0x150`, constructs `0x304D`, writes one `u32` from object `+0x08`, and broadcasts the packet through `0x00484D90`.

This is binary-confirmed **timed ownership removal**: it clears the marker and notifies observers of the object's ID, but the routine does not destroy the object, remove it from the world registry, or persist inventory state. This gives the existing `EntityRemoveOwnership` label a concrete lifecycle anchor and rules out treating `0x304D` as the ground-item despawn packet. The central object-manager update traversal at `0x004139B0` iterates the active-object list at manager `+0x2234` and calls each object's virtual `+0x34C` with its caller-supplied float delta; every recovered item-family vtable maps that slot to `0x0048F9B0`. Thus the accumulator is fed by the shared object-update loop, not by the pickup handler. The threshold is exactly `30.0` in the binary. The setter supplies the timer reset, but the physical unit of the update delta and any data-driven source for the duration remain unrecovered, so `_RefObjCommon.DecayTime` must not yet be claimed as that source.

The same shared update has a distinct expiration branch after that ownership
work. `0x0048F9B0` always delegates the delta to `0x00484D10`. For every
recovered item-family vtable, virtual `+0xF8` is `0x00485EE0` (the byte at
the item state object `+0x30`) and virtual `+0x368` is `0x00484CF0`.
`0x00484D10` accumulates the delta at item `+0x3C`, compares it with the
float at `item->data(+0x34)->ref(+0x18)+0x84`, and on reaching that separate
reference-controlled threshold calls `+0x368`. That implementation calls
virtual `+0x1F0` with action byte `3`; the common item implementation
`0x00484A20` stores that byte in the state object.

On return from virtual `+0x34C`, the central traversal reads `+0xF8`; when
it sees state `3`, it unlinks the current update-list node and calls
`0x00417090` on the object-manager path (`0x00413A97`--`0x00413ABD`). Thus
reference-threshold expiration is a binary-confirmed ground-object removal
route, separate from the 30.0 ownership-clear event and from successful
pickup removal. The expiry virtuals themselves do not invoke `+0x364`,
`0x00485150`, or the CMsgBlockObject `0x3016` writer; any client visibility
removal would have to be an indirect subscriber/removal edge after
`0x00417090`. The field at reference `+0x84`, the unit of its threshold, and
that later client visibility-removal delivery remain unrecovered. See
`analysis/decompiled/pickup-transfer/expiry-vs-3016-boundary.txt`.

### Ground-item runtime structure (recovered subset)

The following offsets are relative to the concrete pooled item runtime
object. They are an evidence-backed subset, not a full C++ declaration:

| Offset | Width | Confirmed use |
|---|---:|---|
| `+0x08` | `u32` | unique object ID; allocated by `0x00489560`, inserted into the global `+0x22B4` index, and emitted by `0x304D`/`0x3016` |
| `+0x34` | pointer | receiver assigned from the concrete `+0x350` initializer's factory-context slot; item-family methods dereference it as permanent/item-data-like state (`+0x38` is the amount consumed by the amount-credit route), while teardown treats it as an `OwnerTable`-compatible object; exact runtime class/role is unresolved |
| `+0x3C` | `float` | common item lifetime accumulator; `0x00484D10` adds the shared update delta and compares it with `data->ref(+0x18)+0x84` |
| `+0x128` | pointer | current world-layer association; cleared by base reset `0x004848E0`, set by virtual `+0x3AC = 0x00482520`, required non-null by `0x0048FC70`, and passed as the prior-layer argument during `0x004858E0` layer transition logic |
| `+0x12C` | pointer | dynamic attachment interface; the generic successful layer-transition path obtains a selected `CMsgBlockObject` region entry through `CRgnObject +0x18 = 0x00538A00`, then reaches `+0x3B0 = 0x00485BB0` and `+0x4AC = 0x00484040` to store it here. The normal leave path invokes attachment virtual `+0x0C` with the item, then clears the field through the same setter; other producers/attachment identities remain unrecovered |
| `+0x140` | `u8` | pooled-object active flag; set by factory `0x0049A840`, cleared by release `0x0049A880` |
| `+0x144` | `s32` | runtime field reset to zero by `0x004848E0` |
| `+0x148` | `u32` | reset to zero by `0x004848E0`; the `CGItemExpendable` factory initializer later stores the result of reference-driven lookup `0x00486140 -> 0x004883B0` here; semantic name unrecovered |
| `+0x14C` | pointer | caller-supplied context pointer; the recovered mob-drop factory stores its originating `CGObjMob` here via virtual `+0x504` |
| `+0x150` | `u32` | ownership-release marker; setter `0x004901B0` writes its caller-supplied value and resets `+0x158`; `0x0048F9B0` tests it and `0x304D` clears it |
| `+0x154` | `u32` | copied from context `+0x08` when context is bound; source-object value on the recovered mob-drop route |
| `+0x158` | `float` | accumulated ownership-release update delta; reset by `0x0048F8A0` |

The vtable is at object `+0x00`. Important recovered slots are `+0x354`
(release/destruction), `+0x498` (generic visibility serialization),
`+0x4D8` (pickup transfer), and `+0x504` (context binding). Their meaning
is established by executable behavior, not by externally supplied class
names.

MSVC RTTI now supplies the concrete names for the seven item vtables already
observed in the pickup path. This table identifies classes only; it does not
assign unproven gameplay semantics to every subtype.

| Vtable | RTTI class | `+0x354` release | `+0x364` leave | `+0x39C` world-entry hook | `+0x4F0` pickup-mutation slot |
|---|---|---|---|---|---|
| `0x00AE9384` | `CGItem` | `0x009DD3AD` | `0x0048FB30` | `0x00485740` | `0x00677A00` |
| `0x00AE9934` | `CGItemContainer` | `0x009DD3AD` | `0x0048FB30` | `0x00485740` | `0x00677A00` |
| `0x00AE9E5C` | `CGItemCOSSummoner` | `0x00492BB0` | `0x0048FB30` | `0x00485740` | `0x00677A00` |
| `0x00AEA394` | `CGItemMonsterCapsule` | `0x00493A50` | `0x0048FB30` | `0x00485740` | `0x00677A00` |
| `0x00AEA9EC` | `CGItemEquip` | `0x00495A30` | `0x0048FB30` | `0x00485740` | `0x00677A00` |
| `0x00AEB2B4` | `CGItemExpendable` | `0x0049A880` | `0x0048FB30` | `0x00485740` | `0x0049A9B0` |
| `0x00AEB824` | `CGItemStorage` | `0x004A2DB0` | `0x0048FB30` | `0x00485740` | `0x00677A00` |

Every row maps `+0x2D4` to `0x00485B60` and `+0x4D8` to `0x0048FC70`.
`0x00677A00` is a literal `return 1` stub for its one-argument call; only
the `CGItemExpendable` mutation implementation is currently shown to alter
item quantity data. The identical `+0x39C` entry proves that the seven
recovered item families do not supply a type-specific alternative to the
mode-`7` transition analyzed above. The unresolved initial-visibility edge
is therefore not explained by choosing a different one of these item
families; the mob route's caller and mode producer are recovered as
`0x004C3F60` and mode `5`. The unresolved initial-visibility edge is the
missing handoff from that mode-5 world entry to a layer/container observer,
not a different item-family implementation.

Release is now classified for five of these concrete pooled families. In
addition to `CGItemExpendable +0x354 = 0x0049A880`, the corresponding
implementations are `CGItemCOSSummoner` `0x00492BB0`,
`CGItemMonsterCapsule` `0x00493A50`, `CGItemEquip` `0x00495A30`, and
`CGItemStorage` `0x004A2DB0`. Each first clears the active byte at object
`+0x140`, invokes virtual cleanup `+0x358`, returns the object through its
family-specific helper (`0x00493F50`, `0x00494180`, `0x00499520`, and
`0x004A3370`, respectively), and clears its owner-pool slot. Those helpers
enqueue the released pointer in distinct global free collections and
decrement the corresponding active count. Thus pickup/expiry removal is a
confirmed pool return for these five concrete families, not merely for gold.
The `CGItem` and `CGItemContainer` `+0x354` entries both point to
`0x009DD3AD`, which has no recoverable item-specific release body in this
analysis; their release behavior remains unclassified.

## 1. Reference loading and record validation

### Confirmed

`0x006AA810` reads a six-`uint32` record from a bounded stream. The second word is resolved through the `_RefObjCommon` lookup (`0x004260B0`). A missing reference logs exactly:

```text
this item is to drop but can't find in _RefObjCommon.
check it out its existance or service on/off [RefID: %d]
```

The fifth word is a requested quantity. A negative value is replaced with the referenced object's byte at `+0x1A8`; the resulting value must be at least one and is then decremented before being stored. This establishes a zero-based/internal quantity convention at this boundary, but the meaning of the other four words is not yet named by the binary. The decoder advances an in-memory bounded stream (`stream + 0x18/+0x1C/+0x20`) and increments its field/read counter at `+0x28` for every primitive consumed. Its only recovered direct caller is `0x0072719C`, in the reference-materialization traversal. Neither this decoder nor that direct decode call invokes a database API. Thus the roll routines consume hydrated reference records; table names below corroborate their provenance but are not evidence of a per-roll SQL read or of the original loader query.

The binary's own diagnostics and the exported reference data align with these families:

- `_RefMonster_AssignedItemDrop` supplies direct, per-monster drops;
- `_RefMonster_AssignedItemRndDrop` supplies random group assignments;
- `_RefDropItemGroup` supplies group members and their selection weights;
- `_RefDropGold` supplies level-indexed gold probability/range.

The direct-assignment hydration-to-roll attachment is also concrete. Loader
`0x006B66A0` allocates a `0x34`-byte node, invokes the assigned-item parser
`0x006A61B0` for its `+0x04` payload, resolves the parser's returned monster
reference ID through `0x004260B0`, and appends that payload to the resolved
monster reference at `+0x288` through `0x006BFC00`. The direct roller then
iterates records from `dropData + 0x28C` with its confirmed `0x30` stride.
This is a binary-confirmed loader-to-direct-roll in-memory attachment; the
four-byte allocation prefix accounts for the `0x34` node versus the `0x30`
payload. The parallel random-assignment loader `0x006B68F0` follows the same
shape: it allocates a `0x34`-byte node, parses it through `0x006A6AA0`, then
appends node `+0x04` to the resolved monster reference at `+0x298` through
`0x006BFCC0`; the group roller reads from the matching `dropData + 0x29C`.
The assigned parser validates both monster and item references and validates
its packed magic-option values before attachment.

### Generic reference-loader SQL

The original SQL boundary is not a per-drop read. Metadata discovery at
`0x00972B40` calls ODBC `SQLColumns` for the registered table and fills the
typed record descriptors. The generic loader at `0x009732F0` then builds the
query from that registration. Its binary-confirmed no-column-list SELECT
branch is:

```sql
SELECT * FROM <registered_table> WITH (NOLOCK)
```

The same routine also has metadata-driven `SELECT <columns>`, `INSERT INTO`,
and `UPDATE` branches. The table name and column list are runtime values; no
single hard-coded query literal contains the full schema. The four drop
catalog registrations and their recovered registration/parser edges are:

| Registered table | Schema registration | Table-registration path | Parser/attachment path |
| --- | ---: | ---: | --- |
| `_REFMONSTER_ASSIGNEDITEMDROP` | `0x0082BE90` | `0x00745A50` | `0x006B66A0` / `0x006A61B0` |
| `_REFMONSTER_ASSIGNEDITEMRNDDROP` | `0x0082C370` | `0x00746250` | `0x006B68F0` / `0x006A6AA0` |
| `_REFDROPITEMGROUP` | `0x0082C5E0` | `0x00746050` | group registration path `0x00746050` |
| `_REFDROPGOLD` | `0x008312C0` | `0x007402A0` | level-indexed lookup `0x00724330` |

The exported reference CSVs corroborate the column order used by the
metadata-driven descriptors:

```text
_RefMonster_AssignedItemDrop:
RefMonsterID; RefItemID; DropGroupType; OptLevel; DropAmountMin;
DropAmountMax; DropRatio; RefMagicOptionID1; CustomValue1; ...;
RefMagicOptionID9; CustomValue9; RentCodeName

_RefMonster_AssignedItemRndDrop:
Service; RefMonsterID; RefItemGroupID; ItemGroupCodeName128;
Overlap; DropAmountMin; DropAmountMax; DropRatio; param1; param2

_RefDropItemGroup:
Service; RefItemGroupID; CodeName128; RefItemID; SelectRatio;
RefMagicGroupID

_RefDropGold:
MonLevel; DropProb; GoldMin; GoldMax
```

This establishes the original generic SQL call contract and the four
reference families. It does not recover stored-procedure bodies or a
per-monster SQL call during the roll routines; the rollers consume the
hydrated in-memory collections described above. The exact startup caller is
also recovered: `0x00401B70` connects the database through `0x00731540`, calls
`0x00731630` (`LoadReferenceData`), then calls `0x007389F0` and `0x00402830`
before local-data completion. The surrounding initialization is therefore
bounded:
`0x00731630` selects a service-mode catalog branch, registers the descriptors,
`0x00829080` allocates table nodes and calls `0x009725C0(0)` for each table,
`0x006BE970` builds typed reference data, and `0x006E13E0`/`0x006E1580` attach
assigned/random records to monster data. In broad mode (`config +0x04 == 2`)
the drop registration sites include `_REFDROPGOLD` (`0x007402A0`),
`_REFMONSTER_ASSIGNEDITEMDROP` (`0x00745A50`), `_REFDROPITEMGROUP`
(`0x00746050`), and `_REFMONSTER_ASSIGNEDITEMRNDDROP` (`0x00746250`). The
reduced mode (`config +0x04 == 1`) includes only the assigned-drop family
among those four sites; the group, random-assigned, and gold sites do not
execute in that branch. Other mode values take the error path. `0x009725C0` appends a descriptor
WHERE clause when present, then calls the table object's virtual `+0x10`.
See `analysis/decompiled/drop-generation/reference-loader-init-chain.txt`.
The service-mode branch and failure behavior are preserved in
`analysis/decompiled/drop-generation/00731630-service-mode-catalog-init.txt`.
The startup sequence is preserved in
`analysis/decompiled/drop-generation/00401B70-reference-startup-caller.txt`.

## 2. Assigned/direct item rolls - `0x00724A00`

This function iterates an array at `dropData + 0x28C`, with a confirmed stride of `0x30` bytes. For every enabled record it chooses a count uniformly from the inclusive byte range at `+0x06..+0x07`, then performs one chance roll for each prospective item.

```text
roll = ((rand2 << 15) | rand1) % 1,000,000
success when roll <= record.chance * globalItemDropFactor
```

Both comparisons and the multiplication are floating-point in the recovered routine. `globalItemDropFactor` is the process-global at `0x00B45BC0` in this binary; its configuration loader logs it as `Item Drop Ratio`.

On success, the routine allocates an item data object, installs the record's reference item ID, count/stack data, drop-type byte, up to twelve magic-option ID/value pairs, and optional rent-item data, then links it into the caller's output list. The confirmed record-relative fields are:

| Offset | Width | Confirmed use |
|---|---:|---|
| `+0x00` | `u32` | reference item ID |
| `+0x05` | `u8` | copied to item data as its drop-type/category |
| `+0x06`, `+0x07` | `u8`, `u8` | inclusive amount range |
| `+0x08` | `float` | chance, scaled by global item-drop factor |
| `+0x0C` onward | vectors | magic-option IDs and paired values |
| `+0x2C` | `u32` | optional rent-item reference |

The output-list capacity check runs before each attempt. A full list stops further work rather than emitting a partial/invalid item. Rent-item creation failure is logged with the literal source name `CRefDropData::AddAssignedItemDrop`.

## 3. Random-group rolls - `0x00724E30`

The random-assignment array begins at `dropData + 0x29C`, again with `0x30` byte records. For each record the binary:

1. verifies output capacity;
2. tests the assignment's chance against the same million-point random roll and global item-drop factor;
3. obtains the referenced drop-item group; a missing group logs `Invalid DropItemGroupID is used!`;
4. chooses an inclusive number of selections from the record's byte range;
5. repeatedly chooses a group candidate and accepts it only when a second million-point roll is below that candidate's weight times the global factor;
6. instantiates the chosen reference item and applies its magic-option group.

The group-resolution boundary is concrete. `0x00724E30` calls
`0x006BEE50` with the assignment's `DropItemGroupID`; that resolver probes
the hydrated reference manager's keyed collection at `+0x864` and copies the
matched group node's candidate vector at node `+0x10` into the roller's
temporary pointer vector. For each candidate pointer, the roller reads the
float acceptance weight at `+0x24`, resolves the item reference from
`+0x20`, and uses the `u32` at `+0x28` as the magic-option-group lookup key.
This proves the group-candidate data path is in-memory
`DropItemGroupID -> keyed group node -> candidate vector -> item/magic
instantiation`; `0x006BEE50` itself contains no database call.

The `Overlap` byte is binary-confirmed behavior: when its value is `1`, an accepted candidate is removed from the local candidate-index list. It cannot be selected again during that one assignment's multi-selection loop. Other values do not remove the candidate in this routine. This is the precise meaning that must be preserved; it is not a statement about stacking or cross-monster uniqueness.

Magic-option lookup failure is non-fatal to the loop: the binary logs `Unregistered MagicOptionID[%d] used!` with the `RefItemDrop.cpp(813)` source marker and continues. A failed item-reference instantiation similarly continues with another candidate where possible.

### Profile-driven generation extension

Before it calls either ordinary item roller, `0x007245C0` checks the
profile/control predicate `0x0072DEE0` for its packed drop-class value. On a
positive result it calls `0x00725FD0`. That helper does not instantiate a
world object: while the caller-owned output list remains below its cap, it
tries at most sixteen selector positions (`0..15`) through `0x00725990`.
Each attempt is gated again by `0x0072DEE0`, `0x0072DF90`, and
`0x0072DF10`; absent profile material fails that attempt without stopping the
later ordinary rolls.

`0x00725990` has its own bounded item-reference resolution, chance tests,
and generated-description mutations. It works against the same
caller-owned output-list context and follows the coordinator's list-cap
rule. Neither `0x00725FD0` nor its immediate loop calls the runtime factory,
packet writer, SQL queue, or database API. This is therefore a confirmed
profile-driven **generation** extension, separate from the direct and
random-group table loops; exact profile record field names and source SQL
remain unrecovered.

### Hard-coded exception generator

If the profile predicate is not present and the low nibble of the packed
drop-class is `3` or `8`, the coordinator calls `0x00726020` instead. This
is a separate in-memory exception generator, not a fallback database query.
Its literal comparisons name `MOB_RM_ROC`, `MOB_TQ_WHITESNAKE`, and
`MOB_TK_BONELORD`; it also constructs the literals `MATTR_NOT_REPARABLE` and
the four `ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_*_B` identifiers. The function
uses `0x00725600` and `0x00725820` to resolve/generate entries and appends
successful descriptions to the caller's same output list.

The branch has its own count randomization and special-name conditions. It
contains no direct call to the runtime factory, object-manager admission,
packet writer, SQL queue, or database API. These named exceptions are
therefore version-specific generation behavior that precedes the common
`0x004B7680` materialization path, rather than separate world-object or SQL
lifecycles.

The four count phases, including defaults consumed before an override, are:

| phase | default | `MOB_RM_ROC` | `MOB_TQ_WHITESNAKE` | `MOB_TK_BONELORD` | monster TypeID4 `8` |
|---|---:|---:|---:|---:|---:|
| enhanced generated equipment | `rand()%4+5` | `rand()%31+50` | default | default | `rand()%2+1` |
| ordinary generated equipment | `rand()%3` | `rand()%11+10` | `rand()%3+1` | `rand()%2` | `0` |
| one-count random reinforcement recipe | `rand()%3` | `rand()%51+50` | `rand()%3+1` | default | `rand()%2+1` |
| `ITEM_ETC_SCROLL_RETURN_02` | `rand()%11+10` | `rand()%51+100` | default | default | `0` |

The enhanced pass conditionally attaches `MATTR_NOT_REPARABLE` while below
the item's `MaxMagicOptCount`, then increases durability by the option's
percentage. Accessory candidates that fail generation do not consume the
enhanced attempt (`--i` before the loop increment). The recipe phase chooses
uniformly among weapon, shield, armor, and accessory `_B` recipes. Agent's
`NativeDropExceptionPlan` now preserves the exact count and random-draw
contract. The kill path supplies monster code/TypeID4 from `RefChars`, resolves
the recipes and return scroll from loaded `RefItems`, reuses the generic
equipment builder for both equipment phases, applies the retained
`MATTR_NOT_REPARABLE` option/durability mutation subject to the item's authored
magic-option cap, and sends every description through the common ground-item
materializer.

The specialized predicate is a sixteen-vector scan on the monster reference,
not a global switch: `0x0072DF90` addresses vector `monster+0x2A8+16*index`,
whose records have stride `0x20`, and matches record byte zero against the
packed drop-class low nibble. `0x0072DF10` returns that matching record.
`0x00725FD0` visits all sixteen vector indices while capacity remains. In
`0x00725990`, selector indices `0`, `6`, `10`, and `14` respectively drive
generated equipment, fixed reinforcement recipes, a class-selected catalog,
and return scrolls; indices `1`, `11`, `12`, and `13` are explicit no-ops in
this binary. The remaining record fields at `+0x0C/+0x10` are inclusive count
bounds. The executable registers the source as `_SupportGoldConfig`. A
read-only audit of the retained `SRO_VT_SHARD`, `SRO_VT_ACCOUNT`, and their
stored-procedure definitions found no such table or procedure. This retained
vSRO deployment therefore has no authoritative specialized rows:
`0x0072DEE0` is false and drop classes `3/8` use the named exception generator.
Agent does not fabricate specialized rows, but now implements that proven
exception path.

## 4. Gold

### Generation, range, and gold-item selection

Gold generation is a confirmed first branch of the same monster-drop
coordinator, not an inferred side feature. `0x004C12B0` calls
`0x007245C0` at `0x004C143D`; after its direct and random-group rolls, that
coordinator allocates a generated-item description and runs branch zero
through `0x00726900` before the ordinary item-selection branches. A
successful branch-zero description is appended to exactly the same output
list later consumed by `0x004B7680`.

`0x00726900` calls `0x00724330`. The latter looks up a three-`u32` value
record in the collection at `dropManager + 0x90`, where `dropManager` is the
`0x00D6AA14 + 0x268` argument supplied by `0x004C12B0`; its lookup key is
the monster-reference byte at `+0x198`. A missing record returns failure.
For a found record `r`, the exact executable behavior is:

```text
chance succeeds when rand() % 101 <= r[0]
baseAmount = round(r[1] + (r[2] - r[1]) * (rand() / 32767.0))
```

`r[2] < r[1]` is an assertion path. A zero base amount fails the branch.
The record is therefore a level-keyed chance/minimum/maximum triple. The
binary's typed catalog descriptor identifies the corresponding reference type
as `CRefDropGold`, and the construction/catalog path contains `_REFDROPGOLD` plus a
`CRefDropGold` record size of `0x30`. The repository's reference snapshot
labels the matching three values `DropProb`, `GoldMin`, and `GoldMax`, and
labels the key `MonLevel`. Those names corroborate the binary layout; the
offset/key mechanics and formula above come from the executable.

The catalog metadata itself is also recovered. `0x00831250` allocates a
`0x30`-byte `CRefDropGold` catalog object and initializes it through
`0x00734670`, which installs vtable `0x00B0C340`. Its virtual `+0x00`
returns descriptor `0x00ADDD40`; that descriptor begins with the literal
`CRefDropGold`, carries the same `0x30` record size, and names
`0x00831250` as its creator. `0x008312C0` registers the `_REFDROPGOLD`
catalog name. This proves the binary's typed catalog/record contract. It
does not recover an original database query or establish a per-drop SQL
read; the range lookup above is entirely in-memory.

`0x00726900` then applies these further confirmed transformations to the
base amount. The high nibble of the coordinator's drop-class byte is either
zero (round the base amount) or one (multiply it by the executable constant
`9.0`, then round); other high-nibble values enter an assertion path. Its
low-nibble and profile branches may multiply the result by `4`, `30`, or by
a selected profile record's `+0x0C` integer. In particular, low values `3`
and `8` multiply by `30`, low value `4` multiplies by `4`, and low value `7`
multiplies the pre-profile amount by `4`. These are branch-specific
generation rules, not the configured server-wide gold factor.

Finally, the routine writes the computed amount to generated description
`+0x38` and assigns `+0x28` as follows:

| Computed amount | `+0x28` value |
|---:|---:|
| `< 1000` | `1` |
| `1000..9999` | `2` |
| `>= 10000` | `3` |

These are not arbitrary bucket labels: the checked reference catalog maps
reference IDs `1`, `2`, and `3` to `ITEM_ETC_GOLD_01`, `ITEM_ETC_GOLD_02`,
and `ITEM_ETC_GOLD_03`, respectively. The coordinator's ordinary item
branches also write an item reference ID at the same generated-description
offset before `0x004B7680` materializes it. Together with the factory result
for the `3/3` type family, this confirms that the range branch produces a
concrete `CGItemExpendable` gold ground-item candidate, with the selected
amount available later as item-data `+0x38`.

The coordinator is bounded. Its low-nibble class chooses a list-cap and an
outer retry count; it attempts branch zero followed by seven item-selection
branches, stops when the output list reaches that cap, and appends only
successful generated descriptions. Thus failure of a gold probability/range
attempt does not suppress the direct/group rolls or the later ordinary-item
attempts in that coordinator invocation.

### Configured gold factor boundary

The executable's configuration loader independently logs `Gold Drop Amount
Factor` and stores its parsed value at `0x00C82524`. The recovered
`0x00724330 -> 0x00726900` gold generation chain does not read that address;
its only directly observed constant multiplier is the mode-specific `9.0`
described above. The point at which, or whether, the configured factor is
consumed by another gold path remains unrecovered. It must not be substituted
for any part of the confirmed formula.

### Pickup credit for generated gold

The generated gold candidates above are connected to the concrete
amount-credit pickup route. Every recovered item vtable maps virtual `+0x7C`
to `0x00483120`. That predicate rejects packed-reference types except the
following bit pattern:

```text
typeID1 == 3
typeID2 == 3
typeID3 == 5
typeID4 != 1
```

The three IDs selected by `0x00726900` have the checked reference types
`3/3/5/0`, so they satisfy this predicate. In `0x0048FC70`, a successful
`+0x7C` result takes the amount at `item + 0x34 + 0x38` and calls
`0x004EACA0` on the collector. The helper then invokes collector virtual
`+0x16C` with that amount and its recovered action constants. This establishes
the generated-gold path as an amount-credit operation rather than a stack
merge, subject to the common transfer gates immediately before the call
(including the selector/context checks and the collector predicate). The
predicate is shared by all recovered item vtables, so it alone must not be
used to classify an arbitrary item as gold; the classification here follows
the already-confirmed `0x00726900` choice of IDs `1..3`.

The concrete `CGItemExpendable` vtable also maps `+0x4E8` to
`0x0049A150`, a two-instruction getter for `item + 0x34 + 0x38`. Thus both
the gold credit branch and the generic transferable-count interface observe
the same generated amount field; this is a direct range-generation to
pickup-value continuity check, not a presumed name match.

The pickup transfer's amount-credit branch passes the item-data amount
(`item + 0x34 + 0x38`) to `0x004EACA0`. That helper invokes the collector's
virtual `+0x16C` with the amount and related action constants; for amounts
above `1000` it additionally invokes collector virtual `+0x274`. For a
`CGObjPC` collector, `+0x16C` is `0x004E4B60`: it adds the incoming 64-bit
amount to character data at `+0x34 + 0x78/+0x7C`, marks that data with dirty
bit `0x8`, and has a separate `0x304E` state-update path at `0x004E3FB0`.
For the recovered pickup call shape (`0x004EACA0` passes action `0x19`, a
zero high word, and the notification flag `1`), `0x004E4B60` calls that
writer with selector `1` and trailing byte `0`. The emitted body is exactly:

```text
0x304E { u8 selector = 1, u64 characterDataPlus0x78, u8 trailing = 0 }
```

`0x004E3FB0` writes the selector, serializes eight bytes from
`character + 0x34 + 0x78`, then writes the trailing call byte. The semantic
field name and the durable consumer of dirty bit `0x8` remain unrecovered,
so this still does not prove the gold storage-update query.

There is, however, a conditional audit-log edge for this player amount-credit
route. `0x004EACA0` supplies action value `0x19` and a zero high word to
`0x004E4B60`. For that call shape, `0x004E4B60` reaches `0x00468A80` when
the credited amount is greater than `100000` (the binary adds `100000` and
compares the result with `200000`; a nonzero positive high word also reaches
the log path). `0x00468A80` constructs the `_ADDLOGITEM` record at
`0x0043A510` and submits it via `0x00469100`. Thus large player
amount-credit pickups have a binary-confirmed asynchronous audit-log job
edge. This is not evidence that every credit, ordinary item pickup, COS
pickup, or drop creation calls `_ADDLOGITEM`, nor that the job has completed
its SQL execution before the pickup response.

## 5. Pickup, visibility, and packets

### Confirmed packet facts

- Client opcode `0x7034` is the binary-confirmed inbound item-operation
  family for both player and COS pickup. The network registration at
  `0x00CD3EA8` selects thunk `0x0051CB30`, which is exactly
  `mov eax,[ecx]; jmp dword ptr [eax+0x608]`. RTTI resolves that virtual
  slot to `0x0050F5C0` for `CGObjPC` (primary vtable `0x00AF59FC`) and to
  `0x004D4F60` for `CGObjCOS_Pet`, `CGObjCOS_GoldPet`, and
  `CGObjCOS_SilkPet` (their vtables are `0x00AF1A4C`, `0x00AF20AC`, and
  `0x00AF270C`). Thus `0x004D4F60` is specifically the COS implementation,
  not a generic player decoder.
- The confirmed pickup request within that family is exactly
  `0x7034 { u8 actionKind = 6, u32 targetObjectUID }`, in that wire order.
  `CGObjPC::+0x608` (`0x0050F5C0`) parses the payload into an internal
  action record through `0x004D8BF0`, then dispatches it through the
  `{ adjustment, handler }` table at `0x00CD3B58`. Its kind-6 table entry
  calls `0x00526090`. The parser first reads the action kind as one byte;
  kind `6` selects branch `0x004D8CCA`, which takes the non-`0x11` and
  non-`0x1C` path at `0x004D8CE3` and calls `0x00404E50` for record `+0x0C`.
  That reader consumes exactly four bytes: its two implementations,
  `0x00403F60` and `0x005216C0`, copy a `u32` and advance/retreat the packet
  cursor by four. `0x00526090` then loads record `+0x0C` at `0x005260CD` and
  passes it to `0x004A9050`, whose first operation resolves it through
  `0x00485D90` as the target runtime object. This proves both the body width
  and its target-object-ID meaning; the handler subsequently performs
  eligibility and distance checks before calling `0x004B29A0`.
- The COS pickup variant is also binary-decoded, but has a distinct action
  body: `0x7034 { u8 actionKind = 0x11, u32 fieldA, u32 targetObjectUID }`.
  `CGObjCOS_*::+0x608` (`0x004D4F60`) uses the same `0x004D8BF0` parser and
  first requires record action `0x11` at `0x004D4FD0`. The action-`0x11`
  parser branch at `0x004D8CCA` reads a four-byte field into record `+0x30`,
  then a second four-byte field into record `+0x0C`. The COS handler loads
  record `+0x0C` at `0x004D4FF3` and resolves it through `0x00485D90` as the
  target runtime object before the same `0x004B29A0` completion gate. The
  first parsed `fieldA` is not consumed by the recovered COS pickup path, so
  its semantic name must remain unrecovered.
- `0x3036` is constructed in the pickup/action completion path at `0x004D51DE`. Its complete emitted payload is confirmed as five bytes, in this order:

  ```text
  u32 collectorUniqueID  // read from the actor object's +0x08 field
  u8  heading            // calculated from the collector/target positions
  ```

  The packet is then dispatched through `0x00484D90`. The same five-byte writer and broadcast sequence appears independently at `0x0052643C` through `0x005264AA`, confirming a reusable pickup-animation layout rather than a one-off COS response artifact. The `0x004D51DE` function later produces `0xB0C5` command responses, so that particular invocation includes COS handling; the binary evidence does not justify calling every invocation a player-only pickup.
- `0x3015` and `0x304D` occur in the same executable and are the repository's current entity single-spawn and ownership-removal labels. The labels are protocol-corpus names, not proofs of the drop object layout.
- The final direct-entry update tail is exact. After item virtual `+0x498`, GameServer writer `0x00533F70` emits one byte from its entry-state argument and emits the associated `u32` only when that byte is `5` or `6`. Client wrapper `0x008A6CA0` selects mode `1`, so `0x008A6BD0` calls both `CIItem` initial parser `0x009E1D00` and update parser `0x009E1A30`; the latter consumes that same one-byte/conditional-`u32` grammar. Ordinary ground entry is state `0` and therefore has no following `u32`. Two captured assertion dumps (`2026-07-24 04:16:18` and `04:27:48`) faulted at client assertion `0x00830DDA` with current opcode `0x3015` because the former Agent writer appended an unconditional zero `u32`, leaving four bytes unread. Agent now emits the exact one-byte ordinary tail and retains direct `0x3015` visibility for a single ground item.
- The generic visibility writer constructs `0x3015` at `0x00533FA6` and passes the target object to virtual `+0x498` in serialization mode `2`. For vtable `0x00AEB2B4`, that slot is `0x0048FB60`. In its mode-`2`/no-context branch, it calls the item-data object's virtual `+0x28` with the output stream and mode `2`, then calls item virtual `+0x494` with the same stream/mode; in its context-bound branch it delegates to the item-data serializer without that item-level follow-up. This proves that the item portion of `0x3015` is data-object polymorphic, with a conditional item-state extension, rather than a fixed packet literal in `0x00533F70`. The recovered mob factory writes a non-null `CGObjMob` source context to item `+0x14C`, so an item serialized before a later `+0x504(0)` clear takes this context-bound branch. The direct factory-to-transition-to-observer edge is now recovered for the seven ground-item families; the exact serializer mode used by a particular branch still follows the context predicate. The `CGItemExpendable +0x494` implementation is `0x0048FC10`: after the data serializer it writes `u32 itemUID` from item `+0x08`, then a helper-produced 14-byte base record derived from the object's placement fields. In this mode-`2`/no-context use it additionally writes `u8 hasOwnershipMarker = (item + 0x150 != 0)`, conditionally writes the raw `u32` marker at `+0x150`, and writes one reference/state byte returned by `0x004909C0`. These fields follow variable item-data content, so this does not establish their absolute offsets in every `0x3015` body or a complete spawn wire layout. Its concrete observer writer owner is the `CMsgBlock` RTTI family: `0x00AF7F90` (`CMsgBlock`), `0x00AF7FB8` (`CMsgBlockObject`), and `0x00AF7FD8` (`CMsgBlockTerrain`) all expose `+0x08 = 0x00534340` and `+0x0C = 0x005344B0`. That method iterates its recipient/object tracking vectors, calls `0x00533E70` in mode `1`, then calls the `0x00533F70` `0x3015` writer for each selected recipient before recording the tracked object. The separate `CWorldLayerObjContainer` RTTI fragment is not the concrete writer owner in this image. This proves the observer-layer entry, polymorphic single-entity serialization, and downstream socket handoff; the selected service identity and complete body grammar remain unresolved.
- The stock-client object factory and CIItem class link provide a second, independent boundary around the `0x3015` body. `0x008A6BD0` reads the initial `u32` discriminator; ordinary values go through `0x009C1FA0`, whose `0x0093F670` type-word lookup selects `DAT_00EF1C28` when `(typeWord & 0x1c) == 0x0c`. `0x009C1CE0` allocates through the global object manager/class-link allocator and initializes the object with `0x009C1840`, which stores the item reference ID at `+0x21c` and resolves its reference data at `+0x220`/`+0x224`. The CIItem constructor `0x009E1AB0` installs primary vtable `0x00DE317C` and secondary vtable `0x00DE318C`. The `0x3015` dispatcher calls slot `+0x64` on the pointer returned by that class-link path and, for an existing-object update, slot `+0x68`. The vtable layout supplies an important multiple-inheritance alias: secondary vtable `0x00DE318C + 0x64 = 0x00DE31F0`, and `0x00DE31F0` points to `0x009E1D00`; secondary `+0x68 = 0x00DE31F4` points to `0x009E1A30`; primary `0x00DE317C + 0x74` points to the same `0x009E1D00`. The RTTI complete-object locators confirm that the primary table has subobject offset `0`, while the secondary table has subobject offset `0x54` (`0x00E2D0C4` versus `0x00E2D124`), and the class-link template is explicitly `CClassLink<CIGIDObject>`. Thus the `0x3015` `+0x64/+0x68` calls are binary-confirmed as the CIGIDObject-subobject routes into CIItem's initial and update parsers when the class-link result is used in its declared interface form; only the allocator's raw pointer-adjustment instruction is not recovered in the decompilation. `0x009E1A30` reads an additional state byte at item `+0xf4`, and values `5`/`6` read a `u32` at `+0x28c` and call `0x008AF620` modes `6`/`7`. The initial parser reads conditional item-type data, an optional quantity at item `+0x97`, a marker-present-like byte at `+0x95`, a conditional `u32` at `+0x96`, a state byte at `+0x98`, then copies placement fields and invokes world/layer update helpers. It also formats the recovered gold quantity or item name and marker text. The exact conditional predicates and the semantics of `+0x95`, `+0x96`, `+0x98` remain unrecovered; this still does not establish a complete `0x3015` wire grammar. Evidence: `analysis/decompiled/client-3015-handler.asm`, `analysis/decompiled/client-object-factory/009C1FA0_FUN_009c1fa0.c`, `analysis/decompiled/client-item-classlink-impl/009E1D00_FUN_009e1d00.c`, `analysis/decompiled/client-item-parser/009E1A30_FUN_009e1a30.c`, `analysis/decompiled/client-item-classlink-impl/009E1AB0_FUN_009e1ab0.c`, and `analysis/decompiled/client-item-parser/CIItem_vtable_slots.txt`.
- The `CGItemExpendable` vtable mapping is independently recoverable at `0x00AEB2B4`: `+0x3AC -> 0x00482520` stores the current layer at item `+0x128`; `+0x3B0 -> 0x00485BB0` replaces the dynamic attachment at `+0x12C`; `+0x364 -> 0x0048FB30` is the successful-transfer leave/attachment callback; `+0x494 -> 0x0048FC10` writes the conditional item-state tail; `+0x498 -> 0x0048FB60` is the polymorphic `0x3015` serializer; `+0x4D8 -> 0x0048FC70` is the pickup transfer operation; `+0x4DC -> 0x0048FDF0` is its context path; `+0x4F0 -> 0x0049A9B0` mutates stack count; and `+0x504 -> 0x0048F990` binds or clears source context. The `0x00485BB0` attachment setter first calls the old attachment's virtual `+0x0C(item, newAttachment)` when non-null (the argument is zero on the pickup/removal call), then calls the new attachment's virtual `+0x08(item, oldAttachment, flags)` when non-null, and finally stores the new pointer through item virtual `+0x4AC` (`0x00484040`, item `+0x12C`). The generic transition producer is now identified: destination `CRgnObject +0x18 = 0x00538A00` returns a selected entry whose constructor writes `CMsgBlockObject` vtable `0x00AF7FB8`; therefore the transition setter's new `+0x08` is the concrete CMsgBlockObject enter writer. On the successful pickup transfer, `+0x364 = 0x0048FB30` calls `0x00485150`, which conditionally invokes the old CMsgBlockObject `+0x0C = 0x005344B0 -> 0x00534050 -> 0x3016` and clears the attachment. Reference-threshold expiry reaches `+0x368 = 0x00484CF0`, writes state `3` through `+0x1F0`, and is later removed by the central `0x00417090` path; it does not directly call the `+0x364` leave edge. See `analysis/decompiled/item-vtable/00AEB2B4_CGItemExpendable_slots.txt`, `analysis/decompiled/item-vtable/00485BB0_attachment_setter.asm`, `analysis/decompiled/msgblock-attachment/CMsgBlockObject_region_attachment.txt`, `analysis/decompiled/removal-callback-refs/pickup-expiry-3016-edge.txt`, and `analysis/decompiled/pickup-transfer/expiry-vs-3016-boundary.txt`.
- The placement helper's width is now exact even though its field semantics are not: `0x0048FC10 -> 0x00484FB0 -> 0x00487CE0 -> 0x00487D90` appends exactly `0x0E` bytes as `u32`, `u32`, `u32`, `u16`. The producer reads the item's `+0x24`, `+0x2C`, `+0x84`, `+0x88`, `+0x8C`, and `+0x90` fields, passes the three-word placement source through `0x00488050`, and writes the final 16-bit component through the stream's `0x0048BEE0` helper. This fixes the 14-byte subrecord's internal width/order while leaving coordinate units, quantization, and semantic names unrecovered.
- The `0x0048FB60` item serializer has three additional guarded data-only forms: mode `1` requires a non-null context and calls data virtual `+0x28` with mode `1`; mode `2` calls data `+0x28` with mode `2` and, only when context is null, appends the item-level `+0x494` extension; mode `4` requires a null context and calls data `+0x28` with mode `4`. Other modes enter the assertion path. These branches are serializer capability boundaries, not proof of which mode a particular initial spawn used.
- The cross-binary `0x3015` grammar boundary is now written explicitly in `analysis/decompiled/client-3015-parser/server-3015-grammar-boundary.txt`. The client CIItem parser proves the conditional reference/type byte, dynamic message-id field, virtual reference-data body, optional quantity, marker-present byte plus optional marker `u32`, and state byte; the server proves the corresponding polymorphic `+0x28` data serializer and, in mode-2/no-context, the concrete `+0x494` tail (`u32 itemUID`, exact 14-byte placement, marker-present/marker, and one state byte). Because the server's context-bound branch omits `+0x494` and the runtime data `+0x28` implementations are not all recovered, this aligns field families and order boundaries without claiming a single fixed offset grammar.
- The factory-side argument trace sharpens, but does not close, the variable `0x3015` body. Generic creation `0x00489730` selects the concrete object factory through `0x00489DE0`, generates a local UID with `0x00489560`, and calls the new object's virtual `+0x350`. Its failure path explicitly clears object `+0x34` at `0x00489811`. For the primary `CGItemExpendable` vtable (`item[0] = 0x00AEB2B4`, secondary subobject `item[+4] = 0x00AEB7E4`), `+0x350` is `0x0048F930`; that wrapper calls `0x00484B80`, whose machine code stores its third explicit stack argument (`[EBP+0x10]`) at item `+0x34` (`0x00484CCC`). The generic caller supplies its `CGObjFactory` context in that argument position, then the wrapper copies context `+0x20/+0x24` to item `+0x160/+0x164` and optionally calls `0x00492480`. Item teardown also treats the field as an `OwnerTable`-compatible pointer (`0x00484A80` reads `field + 0x10` and may call `0x00972210(field)`). This closes the pointer provenance and metadata-like behavior, but not a source-level runtime class/table name: the SQL builder at `0x0045D9E0` receives the object held at item `+0x34`, and the object later reached by `0x0048FB60`'s `data->virtual +0x28(stream, mode)` remains runtime-polymorphic with serializer writes still unrecovered. Evidence: `analysis/decompiled/itemdata-init/00489730_factory_create.asm`, `0048F930_CGItemExpendable_init.c`, `00484B80_item_data_assignment.asm`, `analysis/ghidra_scripts/tmp-out/factory-deep/00484A80_FUN_00484a80.c`, and `analysis/decompiled/pickup-transfer/0045D9E0-itemdata-sql-this.txt`.
- The RTTI class-hierarchy descriptors sharpen the ownership boundary: `CMsgBlockObject` (`COL 0x00B57CC0`) and `CMsgBlockTerrain` (`COL 0x00B57D0C`) each derive from `CMsgBlock` (`COL 0x00B57C78`), while `CWorldLayerObjContainer` (`COL 0x00B57C30`) is a separate one-base type. The shared `0x00534340`/`0x005344B0` writer pair is therefore confirmed on the message-block family, not attributed to the unrelated container RTTI fragment.
- The CIItem RTTI base array separately includes `CIGIDObject` (`0x00E1DA78`). Standalone `CIGIDObject::vftable` `0x00DE1D1C` has the default `+0x64 -> 0x009C1950`; CIItem's secondary table at the RTTI-confirmed `+0x54` subobject overrides that same interface slot with `0x009E1D00` (and `+0x68` with `0x009E1A30`). This is the concrete vtable-overriding evidence behind the `CClassLink<CIGIDObject>` interpretation of the `0x3015` dispatch; it still does not recover every variable serializer field.
- The shared message-block pre/post helper `0x00533E70` is itself a packet-producing boundary, not merely a recipient predicate. Its mode-1/mode-2 branches allocate and dispatch `0x3017`, `0x3019`, and `0x3018` around each `0x3015`/`0x3016` operation. The `0x3017` branch writes the helper's mode argument verbatim as `u8` (`1` for enter, `2` for leave), followed by the exact `u16` count returned by `0x00533B90`; `0x00533B90` builds the temporary grouped-record list by invoking each selected object's virtual `+0x498` in mode `2`, stores the resulting bytes through the temporary buffers rooted at `0x00C66A00`/`0x00CD7DFC`, and returns that record count. The `0x3019` branch passes that grouped buffer, recipient context, and opcode through `0x00430A20`; and the `0x3018` branch obtains a packet through recipient virtual `+0x278` and completes it through `+0x27C`. These are generic message-block control packets, not evidence that a ground drop itself has a second item body.
- The recipient virtual identities are also resolved. Base functions `0x0057F110` and `0x0057F140` are named by their embedded diagnostic strings as `IGObj::AllocMsgForPeer` and `IGObj::SendMsgToPeer`; the former logs the illegal-base diagnostic and returns zero, while the latter logs the corresponding diagnostic and returns without a concrete send. The recovered primary vtables for `CGObjPC` (`0x00AF59FC`), `CGObjChar` (`0x00AEC0A4`), `CGObjMob` (`0x00AEDDAC`), and `CGObjCOS` (`0x00AF0C1C`) all map `+0x278 -> 0x0057F110` and `+0x27C -> 0x0057F140`. An exhaustive data-reference scan finds 31 vtable `+0x278`/`+0x27C` pairs in this image; every pair resolves to the same two base stubs, so no data-referenced gameplay-class override supplies the peer selector. The generic region transition and mob mode-5 path now reach the CMsgBlockObject single-object writer.
- The connection-table lookup itself is now structurally bounded. `0x0096B1B0` treats the object at process `+0x360` as a vector body with start/end pointers at `+0xBC/+0xC0`, checks the caller-supplied `EBX` index, invokes the vector guard virtual `+0x04(-1)`, and returns the indexed entry. `0x0096BE80` then reads that entry's `+0x28` `CSockStream`, rejecting a null/closed stream with `0x8008` before `0x00980050`/the `+0x230` callback. The selector never reads a role/name field, so this closes the vector and validation offsets without upgrading the selected client/Agent/ShardManager identity. See `analysis/decompiled/msgblock-transport/0096b1b0-connection-vector-layout.txt`.
- The concrete `0x3015`/`0x3016` writers have a second, process-level dispatch boundary. `0x00533F70` (single-enter) and `0x00534050` (single-leave) allocate through `0x00430A00`, serialize their bodies, finalize with `0x005341E0`, and then call `DAT_00C8258C` virtual `+0x4C`; `0x005341E0` hands its queued target records to `0x005342A0`, which ultimately reaches `0x004054D0` and the process `+0x50` send slot before the message is released. The relevant decompilations are `analysis/decompiled/c8258c-functions/00533F70_FUN_00533f70.c` and `00534050_FUN_00534050.c`. `0x00935760` assigns that global to the process subobject at `this + 0x140`, whose vtable `0x00B4239C` has `+0x48 = 0x0096BF10` (message-object allocation/refcount increment), `+0x4C = 0x0096C030` (refcount decrement/pool release), and `+0x50 = 0x0096BE80` (dispatch/send). The `+0x50` path indexes `manager + 0x360` via `0x0096B1B0`, validates the selected entry's `+0x28` CSockStream through `0x00980050`, and jumps through stream `+0x230` (`0x00966320` or `0x009663F0`). The direct callback branch reaches `0x00966560`, which calls `WSASend(stream + 0x04, message + 0x24, message + 0x20, ..., message)`, with send-reference accounting at stream `+0x14C`. This is the confirmed GameServer socket handoff. The selected service identity (client versus Agent/ShardManager) and the complete packet grammar remain unrecovered; the separate `0x00430990` ShardManager-session diagnostics must not be promoted to the loot route. See `analysis/decompiled/msgblock-transport/C8258C_transport_pool.txt`.
- A bounded service-role clue exists in the fanout helper: `0x00405430` contains the diagnostic `CAgentMsg::AddtargetClient ClientContext is NULL`, and `0x004054D0` emits internal `0x220A`, `0x220E`, and `0x220F` messages while attaching target-client records. This supports an Agent-message/fanout interpretation of that intermediate layer, but the static table does not prove whether the selected `CSockStream` is the Agent link or another server-side peer; retain the service identity as unresolved.
- The same binary explicitly distinguishes connection roles in `0x00416E90`/`0x00416F80`: `SR_GameServer`, `AgentServer`, `FarmManager`, and `SR_ShardManager` each have separate open/close branches; the ShardManager branch stores `DAT_00C825D0`, while AgentServer teardown invokes `0x00417B30`. This proves the role vocabulary and lifecycle distinctions, but the loot `manager +0x360` selected entry is not statically joined to its module-name field. See `analysis/decompiled/service-role/connection-role-labels.txt`; selected loot service identity remains a bounded inference rather than a binary-confirmed role.
- The direct constructor scan is also bounded: `CMsgBlock` construction at `0x00533520` is reached directly only by the derived constructors at `0x00537506` and `0x0053774B`; no direct call from `0x004C12B0`, `0x004B7680`, `0x00417240`, or `0x0048FA30` reaches that constructor or the `0x00534340`/`0x005344B0` writer pair. Indirect allocation or an external world-service callback remains possible, so this is a negative direct-call result rather than proof that no observer bridge exists.
- The constructor layout also explains the nearby RTTI fragment: `0x00533520` initializes a `CWorldLayerObjContainer` subobject at `CMsgBlock + 0x28` through `0x00533260`, while the concrete `CMsgBlock`/`CMsgBlockObject`/`CMsgBlockTerrain` vtables remain the owners of `+0x08`/`+0x0C`. This is composition inside the message-block object, not evidence that the unrelated `CWorldLayerObjContainer` RTTI entry is itself the packet writer or that a drop factory constructs one.
- The sibling `CMsgBlock`-family `+0x0C` method is `0x005344B0` (shared by `CMsgBlock`, `CMsgBlockObject`, and `CMsgBlockTerrain`). A separate generic writer it calls constructs `0x3016` at `0x0053407F` and emits exactly one `u32` from the target object's `+0x08` unique ID. `0x005344B0` removes tracked objects from its local vectors, uses `0x00533E70` in mode `2`, and calls the `0x3016` writer for each removal. This confirms `0x3016 { objectUniqueID }` as a client-facing generic visibility-removal notification. The successful pickup transfer has a direct preceding leave edge through item `+0x364 = 0x0048FB30 -> 0x00485150`; that helper invokes the old attachment's `+0x0C` only when item `+0x12C` is non-null and manager state permits it. Therefore pickup-to-`0x3016` is conditional on the recovered region attachment, while the later `0x00417090` pooled-removal path itself does not call the tracker. See `analysis/decompiled/removal-callback-refs/pickup-expiry-3016-edge.txt`.
- A direct-reference scan still bounds the remaining gap: `0x004858E0` has one direct caller in this image, `0x00485740` at `0x004857F5`, the generic layer-transition path. The timeout/expiry chain (`0x004139B0 -> item +0x34C = 0x0048F9B0 -> +0x368 = 0x00484CF0 -> later 0x00417090`) has no recovered direct `+0x364 -> 0x0048FB30 -> 0x00485150` edge, so expiry-to-`0x3016` remains unrecovered/conditional rather than universal.
- `0x375C` is **not an opcode occurrence** in this binary. Disassembly fixes both scanner hits as offsets: `0x0086A4A7` executes `add eax, 0x375c`, and `0x0092ABAB` executes `add ebx, 0x375c`, while traversing global containers. The older literal scanner catalog therefore has two false positives. The historical eSRO label `SERVER_ITEM_DROP` must not be associated with this TestIn executable on that evidence.
- `0x3138` (`SERVER_PLAYER_PICKUP` in eSRO) has no hit in the pinned binary's opcode candidate catalog. It is an external-version lead only, not binary-confirmed for this build.

### Bounded pickup behavior

The common pickup completion gate is `0x004B29A0`. Its only recovered direct callers are the two paths that emit `0x3036`: `0x004D5143` and `0x00526335`. It first runs a target virtual operation at slot `+0x4D8`; on success it marks the result structure successful and calls `0x00485E50` on the target. That wrapper delegates the target to the global object manager (`0x00417090` -> `0x00489FC0`), removing it from the active object-management path before the caller broadcasts the five-byte animation packet. This is the confirmed pickup ordering:

```text
validate / target virtual pickup operation
    -> global object-manager removal
    -> broadcast 0x3036 { collectorUniqueID, heading }
    -> caller-specific immediate result path
    -> player: AQ_SlotUpdater admission, SQL worker success, then 0xB034
    -> COS: separate AQ owner completion hook (0x004D4AB0)
```

`0x004D4F60` is not a recovered player-network decoder. RTTI resolves the three vtables that contain it as `CGObjCOS_Pet` (`0x00AF1A4C`), `CGObjCOS_GoldPet` (`0x00AF20AC`), and `CGObjCOS_SilkPet` (`0x00AF270C`), all at virtual slot `+0x608`. It accepts the action-`0x11` request layout described above, resolves an actor and target, rejects absent or ineligible state, delegates to the same completion gate, then sends the animation only on success. Its failure/result branches produce `0xB0C5` command responses. Error/result codes include `0x189C`, `0x18B4`, and `0x18B5` on distinct failure branches. The exact mapping from these codes to client text is not recovered. The routine also distinguishes a full/blocked inventory-style condition from target resolution failure.

The player success response is a separate `0xB034` family. `0x004E9480`
writes a one-byte status `1`, then `0x004F76E0(operationRecord, 1)` writes
the operation-specific success fragment. The stock client helper
`0x0087EB50`, called by `CNetProcessIn::OnB034`, independently parses the
corresponding success form. The length-confirmed pickup forms are:

```text
0x06: u8 operation, u8 record+0x04 (selector)
0x11: u8 operation, u32 record+0x30 (normalized value), u8 record+0x04
0x1C: u8 operation, u32 record+0x34, u8 record+0x04
```

Thus the confirmed leading `0xB034` fragment is `{ u8 status=1, ... }`;
the target UID at record `+0x0C` is not written by these `param_2 == 1`
pickup branches. This is a bounded response fragment, not a complete
`0xB034` grammar. See
`analysis/decompiled/pickup-transfer/004F76E0-b034-payload.txt`.

The stock vSRO 1.188 client parser for `0xB0C5` (`0x00872590`) confirms a
two-byte prefix (`mode`, `subcommand`), an optional `u16 modeValue` when
`mode == 2`, and a mandatory `u32 objectOrContextID`; subcommand `8` then
adds a `u32 amountOrCount`. Its mode-2/subcommand-2 and subcommand-8 branches
dispatch to client object/state and amount helpers, while the remaining
mode-2 form reaches a generic status/UI helper. This makes the response a
caller-specific COS/result message, not an inventory persistence
acknowledgement. The server-side `FUN_00404090` calls in `0x004D4F60` do not
retain enough scalar-type information to name every written field; see
`analysis/decompiled/client-cos-response/B0C5_parser-layout.txt`.
The successful action-`0x11` branch instead emits `0x3036`, then calls
`0x0050FA50`, whose record-`+0x38 == 1` branch dispatches `AQ_SlotUpdater`
through `0x0045D970 -> 0x0045D9E0` and queues a metadata-driven slot-update
SQL builder. If `0x0050FA50` returns `1`, the COS handler jumps directly to
cleanup; no `0xB0C5` writer is executed on that success branch. The recovered
`0xB0C5` writers belong to the COS handler's failure/result branches; they are
not successful-pickup persistence acknowledgements. The COS response body and
updater job are distinct and must not be conflated. See
`analysis/decompiled/pickup-transfer/004D4F60-cos-response-order.txt`.

The second completion caller is inside the internal action-kind handler at `0x00526090` (`0x00526335`). Its address is installed in the action-handler table at `0x00CD3B88` by `0x0050CE80`; the shared dispatcher at `0x0050F86A` indexes that table as `{ adjustment, handler }` pairs using an internal action byte, putting this handler at index `6`. The upstream player path is now fully anchored: registered client `0x7034`, action byte `6`, and the following four-byte target object UID produce this record and reach this common gate. The matching COS implementation is also reached through the same registered opcode family, although its separate handler has its own validation and response branches.
After a successful table-handler return, `CGObjPC::+0x608 = 0x0050F5C0`
calls `0x0050F1F0` to prepare the operation record, loads that record in
`EDI`, and calls `0x0050FA50`. Action `6` then dispatches through
`0x0045D970`/`0x0045D9E0` (because successful transfer set record `+0x38 = 1`)
and admits the updater through `0x004E7350(1)`;
the register-level boundary is preserved in
`analysis/decompiled/pickup-transfer/0050F1F0_operation-prep-boundary.txt`.

### Pickup operation-record layout

The parser and action-handler writes now establish the pickup record fields
without assigning unsupported source-level names:

| Record offset | Confirmed action-6 value | Confirmed action-`0x11` value | Evidence/role |
|---|---|---|---|
| `+0x00` | `0x06` | `0x11` | internal operation discriminator |
| `+0x04` | selector byte (`0xFE` on the common successful pickup gate) | selector byte from the common gate | `0x004B29A0`; copied to updater `+0x2C` and consumed by the slot updater's `+0x578` lookup |
| `+0x0C` | target object UID (`u32`) | target object UID (`u32`) | `0x004D8BF0`; both handlers consume this field through the lookup path |
| `+0x28` | selected collector/owner context pointer | `*(u32 *)(iVar2 + 0x1CD8)` selected by `0x004D5510` | `0x00525DC0` (player) / `0x004D5510` (COS); exact class/name unrecovered |
| `+0x2C` | same selected context pointer | `iVar2` selected by `0x004D5510` | `0x00525DC0` (player) / `0x004D5510` (COS); exact class/name unrecovered |
| `+0x30` | selected context `+0x08` value | `*(u32 *)(iVar2 + 0x08)` after `0x004D5510` normalization | parser first reads COS wire `fieldA` here, then the COS normalizer overwrites it |
| `+0x34` | not needed by recovered action-6 path | COS handler `param_1 + 0x08` value | written by `0x004D5510`; semantic name unrecovered |
| `+0x38` | `0` on transfer failure, `1` after successful `+0x4D8` transfer | same common-gate flag | `0x004B29A0`; `0x0050FA50` selects `AQ_SlotUpdater` when this is `1` |
| `+0x3C` | item virtual `+0x04` result | not written by recovered COS handler | `0x00526090` success path |
| `+0x40` | `u16` from `0x00485C90` | not written by recovered COS handler | `0x00526090` success path |
| `+0x44` | item/data `+0x38` value | not written by recovered COS handler | `0x00526090` success path |

The wire order is not the same as the record's physical proximity. For
action `0x06`, `0x004D8BF0` reads one `u32` into `+0x0C`. For action `0x11`, it
reads the first tail `u32` into `+0x30`, then the second into `+0x0C`; the
client action-`0x11` serializer writes those same fields in that order. The
COS normalizer `0x004D5510` then overwrites `+0x30` and fills the final
context fields used downstream. The copy helper `0x0042CBF0` transfers
`+0x28..+0x60` into updater state before `0x0045D9E0` builds the slot-update
SQL. Full
evidence is preserved in
`analysis/decompiled/pickup-transfer/0050FA50-slot-branch.txt`,
`analysis/decompiled/pickup-transfer/004D8BF0-operation-record-layout.txt`,
`analysis/decompiled/pickup-transfer/00525DC0-operation-record-population.txt`, and
`analysis/decompiled/pickup-transfer/004D5510-cos-operation-normalizer.txt`.

The removal part of this path is more concrete than a map unlink. `0x00417090` first invokes the target's virtual `+0x2D4`, then calls `0x00489FC0`. All seven recovered ground-item vtables map `+0x2D4` to `0x00485B60`, a simple `lea eax, [this + 0x40]; ret` accessor; it is therefore not the missing visibility-tracker call. `0x00489FC0` saves the object's unique ID (`+0x08`) and invokes virtual `+0x354` before returning that ID to `0x00417090`, which erases it from the global object index at `0x00D6A8E8 + 0x22B4`. For the confirmed `typeID1/typeID2 == 3/3` item vtable `0x00AEB2B4`, `+0x354` is `0x0049A880`: it clears active flag `+0x140`, invokes virtual cleanup `+0x358`, returns the object to the pool through `0x004A0CE0`, and clears the owner pool slot. Together with the four other pooled-family releases classified above, successful pickup is a binary-confirmed world-object deactivation and pool return for five concrete item families. `CGItem` and `CGItemContainer` remain the two unrecovered release implementations.

Immediately before that outer object-manager removal, the successful item
transfer itself performs a separate virtual `+0x364` leave/attachment step.
All seven recovered item-family vtables map `+0x364` to `0x0048FB30`; it
removes the item from the active update list through `0x00417DB0`, then
tail-calls `0x00485150`. On its normal manager-present path, this helper
invokes item virtual `+0x3B0` whenever item `+0x12C` is non-null, then clears
item `+0x128` and `+0x12C`. Every same vtable maps `+0x3B0 = 0x00485BB0`: it calls
`(item + 0x12C)->virtual +0x0C(item, 0)` and then item virtual `+0x4AC`
with the null attachment argument. The shared `+0x4AC` implementation
`0x00484040` stores that argument at `+0x12C`.

`0x00485150` also exposes a generic event-source boundary at item `+0x10`.
When event slot `4` is not already dispatching, it sends event id `4` in
phase `0` with `{ item* }`. If the object manager is present, the item has a
non-null attachment pointer at `+0x12C`, and the manager is not in state `1`,
it performs the attachment removal, clears item fields `+0x128`/`+0x12C`, resets a temporary `CEventArg`, and
sends event id `4` again in phase `2`. The recovered event dispatcher
(`0x0063D190`) is generic; no callback in the recovered slices constructs an
`AQ_StorageUpdater`, appends SQL, or queues a database job. Event id `4` is
therefore a real deferred-extension point, not proof of ordinary pickup
persistence. See `analysis/decompiled/pickup-transfer/00485150_event-boundary.txt`.

This is a binary-confirmed pickup-to-dynamic-attachment-removal boundary.
The non-null writer is now concrete for the generic region path: successful
`0x004858E0` transition calls destination `CRgnObject +0x18 = 0x00538A00`,
whose selected vector entry is constructed with `CMsgBlockObject` vtable
`0x00AF7FB8`; item `+0x3B0 = 0x00485BB0` passes that entry to its `+0x08`
enter writer. The same setter calls an existing attachment's `+0x0C`, which
for this CMsgBlockObject entry is `0x005344B0 -> 0x00534050 -> 0x3016`.
Pickup/expiry still does **not** prove a `0x3016` emission in every case:
the recovered removal path only establishes the call when item `+0x12C` is
non-null and has this CMsgBlockObject region-entry identity.
For successful pickup this conditional call is reached through the direct
`+0x364 = 0x0048FB30 -> 0x00485150` edge above. Reference-threshold expiry
instead reaches `+0x368 = 0x00484CF0`, writes state `3`, and later enters
generic `0x00417090` removal; no direct `+0x364` leave edge is recovered on
that chain, so expiry-to-`0x3016` stays unrecovered/conditional.

For this concrete family, the `+0x358` cleanup is `0x0049A9A0 ->
0x0048F8F0`. It releases the attached item-data state, resets local item
buffers, and clears local flags; it contains no call to either
`CMsgBlock`-family visibility method. This rules out the pooled-item
cleanup itself as the missing direct bridge to `0x3016`. The established
layer association at item `+0x128` is still required by transfer and is
managed outside the recovered factory/release slice.

The generic layer-transition helper `0x004858E0` resolves a destination
region entry from the object's position, compares it with object `+0x128`,
and calls that entry's virtual `+0x0C` at `0x004859FF` with the object and its
prior layer pointer. For the RTTI-confirmed `CRgnObject` vtable this is the
region-admission method, not the CMsgBlock removal writer. On the
transition-success branch, destination `CRgnObject +0x18 = 0x00538A00`
returns a selected `CMsgBlockObject` entry at `0x00485B21`; item
`+0x3B0 = 0x00485BB0` then invokes the new attachment's `+0x08`, which is
`0x00534340 -> 0x00533F70 -> 0x3015`. If a prior attachment is present,
`0x00485BB0` first invokes its `+0x0C`; for a prior CMsgBlockObject region
entry that is `0x005344B0 -> 0x00534050 -> 0x3016`. It then calls item
`+0x3AC = 0x00482520` to store the destination region at `+0x128` and
`+0x4AC = 0x00484040` to store the attachment at `+0x12C`.
The recovered drop factory (`0x004B7680 -> 0x00489730`) reaches
`0x004858E0` through item `+0x2B0 -> 0x0048FA30 -> CGObj::EnterWorld ->
item +0x39C = 0x00485740` on the successful mode-5 ground-drop path. See
`analysis/decompiled/msgblock-attachment/CMsgBlockObject_region_attachment.txt`
and `analysis/decompiled/world-enter-events/00485740_item_transition_bridge.asm`.

### Concrete item-transfer virtual

The formerly unresolved `+0x4D8` target is now recovered for every known
item-family vtable. Vtables beginning at `0x00AE9384`, `0x00AE9934`,
`0x00AE9E5C`, `0x00AEA394`, `0x00AEA9EC`, `0x00AEB2B4`, and `0x00AEB824`
all resolve that slot to `0x0048FC70`. This is therefore the concrete
transfer implementation reached by the shared completion gate for those
ground-item runtime classes; it is not merely an opaque virtual boundary.

`0x0048FC70` first requires its virtual `+0xF8` discriminator to equal `2`.
For a non-`2` result it returns success immediately; the semantic name of
that discriminator is unrecovered. In the `2` case it has two confirmed
transfer routes:

- When the item's virtual `+0x7C` predicate succeeds, it takes the amount
  from the item's data at `+0x34 + 0x38` and calls `0x004EACA0` on the
  collector. This is an amount-credit route. The generated gold IDs `1..3`
  recovered in section 4 satisfy the predicate and therefore use this route;
  the predicate alone is not a gold classification for arbitrary item
  references.
- Otherwise it requires the item's `+0x4C` predicate, obtains a destination
  item/object candidate through the collector's virtual `+0x578` with the action's byte
  argument, and calls `0x00490230`. That helper verifies compatible runtime
  kinds, target/container state, stack/merge restrictions, and available
  capacity. It returns the transferable amount, bounded by both the item's
  virtual `+0x4E8` count and the destination's remaining capacity.
  `0x0048FC70` then invokes virtual `+0x4F0` on that returned destination
  object with the bounded amount. This is the binary-confirmed inventory
  mutation boundary; the concrete destination class/vtable reached by a
  normal item pickup remains unrecovered.

On a successful transfer route, `0x0048FC70` clears the item context with
virtual `+0x504(0)`, sends a state update through virtual `+0x1F0`, removes
the item from the active-update path through `0x00417DB0`, and invokes
virtual `+0x364` (`0x0048FB30` for all seven recovered item families). The
outer common gate verifies that clear explicitly: `0x004B2A21` requires
`item + 0x14C == 0` before it calls `0x00485E50` for global removal. Thus
the originating-mob context is a required released association on the
successful pickup route, not a value left attached to the pooled object. A
failed ordinary-item route binds the collector through `+0x504(collector)`
and takes the alternate state-update parameters instead. The gate removes
the world object only when this virtual returns `1`; it then broadcasts
`0x3036` as shown above. The recovered direct post-transfer slice contains
no mutation of the originating `CGStorage + 0x04` vector, so the source
storage's post-pickup membership handling remains unrecovered. No direct SQL
call occurs on the common gate/item-transfer stack. Both outer handlers then
have confirmed storage-update edges. COS/action-`0x11` goes from
`0x004D4F60` through `0x0050FA50`, which consumes the parsed operation record,
dispatches the record-`+0x38 == 1` branch to `0x0045D970 -> 0x0045D9E0`,
builds a metadata-driven slot-update SQL statement, and admits the updater
through `0x004E7350(1)`; see
`analysis/decompiled/pickup-transfer/004D4F60-storage-updater-edge.txt`.
The ordinary player action-`6` route goes through `CGObjPC +0x608 =
0x0050F5C0`, table handler `0x00526090`, `0x0050F1F0`, and then
`0x0050FA50` with operation byte `0x06`; see
`analysis/decompiled/pickup-transfer/0050F5C0-storage-updater-edge.txt`.
The common gate (`0x004B29A0`) and item virtual (`0x0048FC70`) remain
non-database transfer boundaries; `0x0050FA50` returns `1` for successful
queue admission and `0x1811` for queue rejection. The updater is
asynchronous: ordinary/player success emits `0x3036` before admission and
reaches `0xB034` only through the later `AQ_SlotUpdater` worker-success
callback (`+0x20 = 8 -> 0x004E73D0` case 8 -> `0x004E9480`).

The two shared virtuals immediately after transfer are also concrete
non-database operations in this image. All seven item vtables map `+0x1F0`
to `0x00484A20`, which only updates the item state byte and, for action `2`,
the lifetime accumulator. Their `+0x504` context-binding implementations are
`0x0048F990` (or the `0x00492B00` tail-jump to it), which only writes
`+0x14C` and copies context `+0x08` to `+0x154`. Neither implementation
constructs an updater, appends SQL, or admits a queue entry. This keeps the
persistence boundary downstream of the post-transfer virtuals, at the
confirmed action-handler updater calls documented above.

This resolves local transfer and capacity behavior, but not all semantic
names: the concrete collector/container classes, several operation-context
pointer names, and the COS completion-hook response semantics remain
unrecovered. The downstream updater/queue edge is confirmed for both action
handlers; the ordinary/player worker-success callback is joined to the
`0xB034` pickup-response builder, while COS uses its separate `0x004D4AB0`
owner callback.

### Recovered character storage and stack mutation

The ordinary route is now more concrete for a character collector. The primary `CGObjChar` vtable is `0x00AEC0A4` (the MSVC RTTI type descriptor is `CGObjChar`), and its `+0x578` slot is `0x004A68E0`. That method passes its argument into `0x004B8000` with the character's embedded storage at `character + 0x1C10`. The constructor at `0x004A6AD0` constructs that embedded object with the `CGStorage` vtable `0x00AED54C`; a second `CGStorage` is constructed at `character + 0x1C34`.

`0x004B8000` is a storage-operation resolver, rather than an inventory SQL writer. It reads the storage's `+0x18` kind, maps it through `0x004A6A50` to an `IStorageOP` implementation, then calls that implementation's virtual `+0x14` with the storage and the supplied selector. The recovered `CGPCInventoryOP` vtable is `0x00AEC7A4`; its `+0x14` implementation (`0x004B9BA0`) bounds-checks the selector against the storage capacity at `+0x20` and returns the corresponding pointer from the storage's vector (`+0x08`/`+0x0C`). The source-mob kind `1` maps instead to `CGNPCInventoryOP` vtable `0x00AEC85C`, which uses that same `+0x14` implementation. This proves the `+0x578` route is an in-memory character storage/item resolution boundary and distinguishes the player collector's policy from the source mob's policy.

The default selector is now exact at the machine-code level. `0x004B29A0` initializes its one-byte local selector to `0xFE` at `0x004B29CA`, then supplies it to the target's `+0x4D8` pickup operation at `0x004B2A16`--`0x004B2A1A`. In `0x0048FC70`, `movzx eax, byte ptr [ebp+8]` at `0x0048FCFA` converts that byte to unsigned `254` before invoking the collector's `+0x578`; the character implementation therefore calls `0x004B8000(character+0x1C10, 254)`. `0x004B9BA0` treats its selector as signed only for the negative rejection, so this is not the signed value `-2` or a negative sentinel. It is an exact in-memory selector value in the common pickup attempt. The binary does not disclose what logical destination or policy selector `254` denotes, nor does it prove that it is a normal user-visible main-inventory slot; retain that semantic boundary.

For the RTTI-confirmed `CGItemExpendable` vtable `0x00AEB2B4`, virtual `+0x4F0` resolves to `0x0049A9B0`. This routine adds the requested amount to the item-data quantity at `item->data(+0x34) + 0x38`, clamps the resulting value to zero and to the reference maximum at `item->data->ref(+0x18) + 0x198`, and, when the value actually changes, marks the data dirty with `item->data(+0x08) |= 0x4`. The write/dirty branches require global byte `0x00D2043C & 1`; the alternate branch enters the binary assertion path rather than performing the mutation. It is therefore a concrete, guarded stack-merge implementation, including its lower and maximum-stack bounds. This dirtied state is consistent with later storage persistence, but no call edge from this method or `0x0048FC70` to `AQ_StorageUpdater` has been recovered; it is not proof that SQL text was accumulated or executed by itself.
The exact guarded dirty write and its negative persistence result are preserved in `analysis/decompiled/itemdata-trace/0049A9B0_stack-dirty-boundary.txt`.

The pickup request is binary-confirmed as client `0x7034 { u8 actionKind=6,
u32 targetObjectUID }`; it contains no actor UID in this recovered body, as
the actor is the session-bound virtual receiver. `0x3138` remains absent from
the pinned binary and must not be substituted for this build's request
opcode. A packet capture would still be useful to confirm framing, cipher
state, and client-version behavior outside this decoded payload.

The client binary also contains a reusable `0x7034` action builder at
`0x007950B0 -> 0x007923A0`. Its action-6 serializer writes one four-byte
field after the action byte, matching the server-decoded request width, but
the recovered action-6 callers are inventory/storage drag-drop and equipment
UI paths (`006B4627`, `00825F50`, and `006AD180`). None is rooted in a
world-ground click/object interaction. This confirms the generic builder and
width, not the ordinary ground-pickup sender; see
`analysis/decompiled/client-pickup-refs/7034-action6-negative-evidence.txt`.

## 6. SQL boundary

The binary SQL string catalog proves these potentially relevant contracts:

```sql
{CALL _ADDLOGITEM (%d,%d,%I64d,%d, %u,%u,%u,%u, '%s','%s', %I64d)}
UPDATE _Char SET RemainGold = %I64d WHERE CharID = %d
```

`0x0043A590` is the `_ADDLOGITEM` formatter: it reads the log record's fields at `+0x08` through `+0x24`, formats the eleven stored-procedure arguments, and returns the SQL text to its caller. Direct format-string references put the `RemainGold` update at `0x0043FD00` and `0x00454D7F`. The surrounding diagnostics classify the first as an item-exchange database workflow (`item exchange stored procedure returns error ...`) and the second as `AQ_OpenMarket::_DoWork_Update_Gold`; they are not ground-pickup persistence anchors. None of these functions is called by the recovered `0x004D4F60` pickup/action completion path.

There is also a concrete execution surface for this log-record family. The
nearby binary registration literal names `AQ_LogWriter`; its worker
`0x0043B220` obtains a payload at `this + 0x1C0`, invokes the payload's
first virtual to produce SQL text, and passes that text to database API
`0x009724C0`. For the `_ADDLOGITEM` record constructed by `0x0043A510`, the
first virtual is precisely `0x0043A590`, so this is the formatter-to-database
execution path when an `AQ_LogWriter` is scheduled with that record. The
worker treats return values with all bits except the low bit clear as its
success condition. The recovered large-credit producer reaches submission
code, but the scheduling edge from that submission to a particular
`AQ_LogWriter` invocation is not recovered; this does not convert the
asynchronous audit edge into a synchronous pickup guarantee.

The binary further identifies `_ADDLOGITEM` as a distinct asynchronous log-job family. Constructor `0x0043A510` installs the log-record vtable at `0x00AE12D0`; its first virtual formatter is `0x0043A590`. Recovered producers at `0x00468AD5`, `0x00468C3E`, and `0x00468DB8` construct that record and submit it through `0x00469100`. The first producer is conditionally connected to pickup: the `CGObjPC` amount-credit chain is `0x0048FC70 -> 0x004EACA0 -> 0x004E4B60 -> 0x00468A80`, with the constructor at `0x00468AD5`, under the greater-than-`100000` condition described above. No corresponding edge has been recovered for ordinary item transfer, COS collection, or direct drop creation. Thus `_ADDLOGITEM` is a binary-proven asynchronous audit side effect for that bounded player-credit case, not a universal ground-pickup SQL contract.

Successful pickup takes a distinct slot-update writer. `0x0050FA50` tests
record `+0x38`; after `0x004B29A0` succeeds it is `1`, so the call goes through
`0x0045D970 -> 0x0045D9E0` (`AQ_SlotUpdater::Init`). The slot updater copies
the operation record, resolves the selected storage item through the
collector/COS `+0x578` lookup using the selector byte at record `+0x04`
(copied to updater `+0x2C`), validates the target and
reference capacity (`0x00459D60`/`0x00459D80`), and invokes
`0x009742D0(2, ...)` to construct a metadata-driven SQL `UPDATE`. The query is
appended through `0x00408870` and admitted to the asynchronous queue by
`0x004E7350(1)`. The exact table/column names are generated from runtime
metadata and are not present as a pickup-specific literal at this boundary;
the direct `UPDATE` builder is nevertheless binary-confirmed.

The field ordering and value formatting of that `UPDATE` are also bounded.
`0x009742D0` delegates to the generic metadata builder (`0x009732F0`), which
emits `UPDATE <registered-table> SET <descriptor-column>=<formatted-value>,...`
in registered descriptor order and appends the registered identity/key fields
as a `WHERE` predicate. Descriptor tags `0`, `1/2`, `3`, `4`, `6`, and `7`
select `%u`, `%d`, `%I64d`, `%f`, quoted `%s`, and the six-component
`convert(datetime, ...)` form respectively. The same builder owns generic
`SELECT`, `INSERT`, and `DELETE` branches. This fixes the SQL grammar and
ordering contract without inventing runtime table names, column names, or the
exact key/value set used by a given pickup operation. See
`analysis/decompiled/pickup-transfer/009742D0-slot-update-shape.txt`.

The receiver is also exact at the register boundary: `0x0045D9E0` moves the
selected destination item pointer through `[item + 0x34]`, places that object
in `ECX`, and calls `0x009742D0(2, &localValueBuffer)`. Thus the pickup SQL
update is generated from metadata reached through the destination item's
`+0x34` receiver; the common transfer virtual and queue object do not supply a
hard-coded table name. Creation and teardown identify that receiver as a
factory-context/OwnerTable-compatible pointer, while its exact runtime class
and table role remain unresolved. See
`analysis/decompiled/pickup-transfer/0045D9E0-itemdata-sql-this.txt`.

The executable also contains a separate generic inventory persistence writer,
not the successful pickup slot-update branch. `0x0045A9A0`
(`AQ_StorageUpdater` initialization/build path) formats the following add-item
procedure families, choosing the form by the item's runtime
predicates and magic-option state:

```sql
{CALL _STRG_ADD_EXPENDABLE (?, ?, %d,%d, %d,%d,%d)}
{CALL _STRG_ADD_ALCHEMYSTONE (?, ?, %d,%d, %d,%d,%d,%d)}
{CALL _STRG_ADD_SPECIALTY (?, ?, %d,%d,%d,%d,%d, '%s')}
{CALL _STRG_ADD_EQUIP_NORMAL (?, ?, %d,%d,%d, %d,%d,%I64d,%d)}
{CALL _STRG_ADD_ITEM_MAGIC (?, ?, %d, %d,%d, %d,%d,%I64d,%d, %d ... )}
```

The magic form is extended by up to twelve `%I64d` option ID/value pairs;
the routine validates `MagParamNum <= 12`. The generated SQL text is
appended to the updater's internal query list by `0x00408870`, not submitted
there: the call sites in `0x0045A9A0` pass the list rooted at
`AQ_StorageUpdater + 0x160`. `0x0045CE90` is the generic updater worker,
not the successful slot-update worker: the `0x00AE5688` vtable installed by
`0x004595D0` has RTTI type descriptor `.?AVAQ_StorageUpdater@@`, and its
`+0x18` slot is `0x0045CE90`. Its own
`AQ_StorageUpdater::DoWork()` diagnostics agree with that binding. It
iterates the accumulated list (sentinel at `this + 0x164`, count at
`this + 0x168`), dispatches each query through the database API at
`0x009724C0`, and treats database errors or zero affected rows as failure.
The successful pickup branch instead uses `AQ_SlotUpdater` vtable
`0x00AE56C8`; its `+0x14` worker is `0x0045DC80`, which reports
`AQ_SlotUpdater::DB Query Error` and executes the generated slot-update list
through `0x009724C0`.
The same writer family also has
restore-sold-item formats (`0x0045BB60`) and gold/move formats
(`0x0045AF70`), including:

```sql
{?=CALL _UPDATE_USER_N_CHAR_GOLD (%d,%I64d, %d,%I64d)}
{?=CALL _UPDATE_GUILD_N_CHAR_GOLD (%d,%I64d, %d,%I64d)}
{?=CALL _STRG_MOVE_ITEM (%d,%d,%d,%d,%d,%d,%d,0)}
```

The surrounding operation dispatcher has a switch at `0x0045A0E1` with
direct calls to the normal add-item builder at `0x0045A1B1` and
`0x0045A1D9`, and to the gold-update builder at `0x0045A25D`. One of its
query-producing branches appends constructed SQL text to the same updater
list through `0x00408870` at `0x0045A4DD`. This proves common
operation-driven storage-query accumulation; by itself this generic switch
does not identify which action-handler caller supplied each operation record
and is not the successful pickup branch while record `+0x38 == 1`.

The subsequent queue boundary is also recoverable. `0x004E7350` receives
an updater in `EAX`, chooses one of two queue objects rooted at
`0x00D6A8DC + 0x423E4` (the boolean argument selects `+0x423E4` or
`+0x423E8`), invokes the updater's virtual `+0x24` pre-enqueue hook, then
calls the chosen queue's virtual `+0x4C` with that updater. For the generic
`AQ_StorageUpdater` vtable above, the hook is `0x00459EE0`. This is
binary-confirmed asynchronous queue admission. The successful
`AQ_SlotUpdater` vtable's corresponding `+0x24` slot is `0x0066B100`.
At the caller boundary, `0x0050FA50` maps a nonzero `0x004E7350` return to
status `1` and queue rejection to `0x1811`; this is an enqueue result, not a
database-worker completion signal.
For ordinary player action `6`, `0x0050F5C0` consumes a non-`1` result by
calling `0x004E7250(status)` (normally `0x1811`), which obtains a player
message through virtual `+0x278`, writes two one-byte fields, and dispatches
through `+0x27C`; it then logs the failed asynchronous operation and reaches
the disconnect path. This immediate queue-rejection response is distinct from
the later worker-success `0xB034` and worker-failure AQ-type-8 logout paths.
See `analysis/decompiled/pickup-transfer/0050F5C0-queue-rejection-response.txt`.
Admission is distinct from both
SQL-string accumulation and later `0x0045CE90` database execution. The
successful COS/action-`0x11` and ordinary action-`6` handlers reach
`0x0045D9E0` through `0x0050FA50` because their common-gate record has
`+0x38 == 1`. The generic `0x00459F70` callers remain a separate operation
family. No direct call within the common transfer `0x0048FC70` or common gate
`0x004B29A0` reaches a database API; both action handlers do so downstream.

This proves that both the COS/action-`0x11` and ordinary player action-`6`
pickup routes are represented by accumulated slot-update queries and
specifies their metadata-driven SQL `UPDATE` boundary. It still does **not** prove
synchronous persistence: `0x3036` is sent before queue admission. For the
ordinary/player owner, the later `0xB034` builder is reached only after the
successful SQL worker callback; the COS owner callback's higher-level response
semantics remain separate. None of this makes the common transfer virtual a
database boundary.

The updater's vtable and worker path further bound the asynchronous edge.
For successful pickup, `AQ_SlotUpdater` uses vtable `0x00AE56C8`, with SQL
worker `+0x14 = 0x0045DC80`, owner/context refresh `+0x18 = 0x0045DD50`,
and failure refresh `+0x1C = 0x0045DD90`.
The generic `AQ_StorageUpdater` uses vtable `0x00AE5688`, where `+0x18` is
`0x0045CE90` (`DoWork`), `+0x1C` is `0x0045D4A0` (failure/cleanup), `+0x24`
is `0x00459EE0` (pre-enqueue owner hook), and `+0x2C` is `0x0045D940` (stores
completion/context state). Both workers execute their accumulated queries
through `0x009724C0` and check database error/affected-row results.
`0x0045DD50`/`0x0045DD90` resolve the owner/context IDs at updater
`+0x160/+0x164`, store the resolved pointers at `+0x50/+0x54`, and invoke the
owner's virtual `+0x5D4` with completion flag `1` or `0`. For `CGObjPC`, that
slot is `0x004E9440`, which dispatches success to `0x004E73D0` and failure to
`0x004E9110`; COS-pet vtables use `0x004D4AB0`. The `AQ_SlotUpdater`
constructor at `0x004597F0` writes updater `+0x20 = 8`, while
`0x0045D9E0` copies the operation record (including its action byte) to
updater `+0x28`. Consequently the successful player callback is now joined:
`0x004E9440 -> 0x004E73D0 -> case 8 -> 0x004E9480(owner, updater + 0x28)`.
`0x004E9480` constructs `0xB034`, writes status `1`, and dispatches the
copied action `0x06`/`0x11` family. The action handler emits `0x3036` before
queue admission; this `0xB034` builder is reached on the later successful
SQL-worker callback, not on the immediate `0x0050FA50` enqueue return. The
separate COS callback `0x004D4AB0` performs its COS completion hook and does
not itself contain a `0xB0C5` writer. See
`analysis/decompiled/pickup-transfer/004597F0-slot-updater-response-join.txt`
and `0045DD50-owner-completion-callback.txt`. On SQL-worker failure, the
player callback dispatches to `0x004E9110`; because `AQ_SlotUpdater +0x20 = 8`
has no recovered failure case there, it logs `AQ Failed! Log out!! [AQType: 8]`
and calls `0x004DF050(4)`. COS status-zero completion similarly logs out via
`0x004D4AB0`. See
`analysis/decompiled/pickup-transfer/004E9110-slot-failure.txt`.

The catalog also proves inventory/item hydration queries joining `_Inventory` and `_ITEMS` (and analogous COS, avatar, chest, and storage tables). It does **not** establish that a monster drop writes `_ITEMS`, that ordinary/COS pickup calls `_ADDLOGITEM`, or that any recovered audit/storage statement runs synchronously. The bounded player amount-credit edge above is the sole recovered pickup-to-`_ADDLOGITEM` attachment. The safe lifecycle statement is therefore:

- ground-drop selection and item-description construction are in-memory in the recovered routines;
- successful item pickup reaches concrete capacity/transfer logic before removal and `0x3036`;
- both COS/action-`0x11` and ordinary player action-`6` reach `AQ_SlotUpdater` and its asynchronous queue. The
  ordinary/player worker-success callback is joined to `0x004E73D0` case `8` and the `0xB034` response builder;
  `0x3036` precedes queue admission, while `0xB034` follows the later successful SQL-worker callback. SQL-worker
  failure takes the `0x004E9110` default logout path for AQ type `8`; the separate COS `+0x5D4 = 0x004D4AB0`
  completion hook likewise logs out on status zero and does not itself contain a `0xB0C5` writer. A conditional
  large player amount-credit pickup separately enqueues the `_ADDLOGITEM` audit job, but that is not proof of
  synchronous SQL completion.

## 7. Evidence matrix and remaining boundaries

### Maintained Agent pickup handoff

The maintained Agent now implements the recovered player request boundary:

```text
0x7034 { u8 actionKind = 6, u32 targetObjectUID }
    -> resolve authenticated player and ground item
    -> verify runtime owner, region, 160-unit range, and normal slot
    -> transactionally lock CHAR, ITEM, and INVENTORY
    -> insert INVENTORY(FK_CHAR, SLOT, FK_ITEM)
    -> reload and commit the inventory snapshot
    -> deactivate/remove the world object
    -> 0x3036 { collectorUniqueID, u8 heading }
    -> existing server item-update serializer
```

Generated gold follows the binary's separate amount-credit predicate instead
of the inventory-link branch:

```text
0x7034 { 0x06, targetUID }
    -> verify type 3/3/5/(type4 != 1)
    -> lock CHAR + ITEM, credit CHAR.GOLD by ITEM.DATA, consume ITEM
    -> deactivate/remove the world object
    -> 0x3036 { collectorUniqueID, heading }
    -> 0x304E { selector=1, characterGold, trailing=0 }
```

The SQL credit/consume transaction is maintained-server composition around
the recovered amount-credit virtual path; the binary's asynchronous worker
and exact durable statement remain metadata-driven/unrecovered.

The SQL transaction is an AegisCore safety boundary around the recovered
`ITEM`/`INVENTORY` relationship; the pinned executable's storage-updater call
is downstream of the action handler, after `0x3036`. The player success
callback is binary-confirmed to reach the `0xB034` builder through
`0x004597F0`/`+0x20 = 8` and `0x004E73D0` case `8`. COS pickup remains on its
separate `0x004D4AB0` completion-hook path; this document does not substitute
the maintained Agent serializer for that route.

| Lifecycle boundary | Status | Binary anchor | What remains unknown |
|---|---|---|---|
| Source-side generation hook | confirmed virtual dispatch | `CGObjChar +0x350 = 0x004A6F40 -> CGObjMob +0x574 = 0x004C12B0` | source event semantic name and argument meaning |
| Reference-catalog SQL loading | confirmed metadata-driven table load | `0x00401B70` DB bootstrap -> `0x00731630` service-mode branch -> `0x00829080` -> `0x009725C0(0)`; schema registration `0x00745A50`, `0x00746250`, `0x00746050`, `0x007402A0`; typed build `0x006BE970`; attachment `0x006E13E0`/`0x006E1580` | queue/object identity and stored-procedure body; runtime table/column values remain metadata-driven |
| Gold/profile/direct/random/exception generation | confirmed | `0x00724330`, `0x00726900`, `0x00725FD0`, `0x00725990`, `0x00726020`, `0x00724A00`, `0x00724E30` | exact loader SQL and profile-record declarations |
| Runtime factory/world registration/context binding | confirmed | `0x004C12B0 -> 0x004B7680 -> 0x00485E70`, `0x00489730`, `0x004117E0`, `+0x504(CGObjMob)`, `0x004C1DE3 -> 0x004C3F60 -> 0x004C41F3 -> 0x004901B0 -> +0x4DC(0x0048FDF0, param9=5) -> +0x2B0(0x0048FA30) -> +0x39C(0x00485740, mode 5) -> 0x004858E0` | marker source-parameter semantics and service identity selected by the connection table |
| Observer visibility serialization | confirmed for recovered ground-item world-entry path | `CGObj::EnterWorld 0x00485360 -> item +0x39C = 0x00485740 -> 0x004858E0`; `CRgnObject` RTTI/vtable `0x00AF805C +0x18 = 0x00538A00` returns a vector entry constructed as `CMsgBlockObject` vtable `0x00AF7FB8`; item `+0x3B0 = 0x00485BB0 -> attachment +0x08 = 0x00534340 -> 0x00533F70 -> 0x3015`; old attachment `+0x0C = 0x005344B0 -> 0x00534050 -> 0x3016`; helper `0x00533E70` emits `0x3017 { mode, count }`, grouped `0x3019`, and `0x3018`; single-object writers finalize through the C8258C message pool (`+0x48 = 0x0096BF10`, `+0x4C = 0x0096C030`), whose `+0x50 = 0x0096BE80` selects a `CSockStream` and reaches `WSASend(stream +0x04, message +0x24, message +0x20, ..., message)`; pickup leave also dispatches generic event id `4` phase `2` when an attachment pointer is non-null | socket handoff is confirmed; selected client/Agent/Shard service identity and complete `0x3015` grammar remain unresolved; grouped peer allocation/send remains base-stub-only on recovered gameplay vtables; event id `4` subscribers are unresolved and no recovered subscriber reaches storage SQL; event ID 3 registrations do not identify an additional loot subscriber; pickup `0x3016` is conditional on the +0x364/+0x3B0 attachment edge and expiry `0x3016` remains unrecovered/conditional |
| Timed ownership release / expiry | confirmed | `0x004139B0 -> virtual +0x34C -> 0x0048F9B0 -> 0x004901C0`, `0x304D`; `0x00484D10 -> +0x368 -> state 3 -> 0x00417090` | delta/reference-threshold units and client removal packet |
| Item transfer | confirmed boundary | `0x004B29A0`, `0x0048FC70`, `0x00490230`, `0x004A68E0`, `0x0049A9B0` | runtime operation/selector choice; downstream updater and player owner callback are confirmed; COS completion uses a separate owner virtual |
| Ground-object removal | confirmed for five pooled item families and source-storage teardown | successful transfer `+0x364 = 0x0048FB30 -> 0x00485150 -> +0x3B0`; then pickup/expiry `0x00485E50 -> 0x00417090`; removal dispatches a `CEventArg` through object virtual `+0x2D4 = 0x00485B60` / `0x0063D190` before `0x00489FC0 -> +0x354`; character cleanup `0x004DEC10 -> 0x004A72E0 -> 0x004B75D0 -> 0x00485E50` | `CGItem`/`CGItemContainer` release bodies, immediate source-vector mutation, and expiry-to-`0x3016` delivery edge |
| Inbound pickup request | confirmed layouts | player `0x7034 { 0x06, u32 targetUID }`; COS `0x7034 { 0x11, u32 fieldA, u32 targetUID }`; `0x0051CB30`, `0x004D8BF0` | packet framing/cipher behavior and COS `fieldA` semantics |
| Pickup animation | confirmed layout | `0x004D51DE`, `0x0052643C`, `0x3036` | response-code text mapping |
| Client peer receive lifecycle | confirmed inbound handlers | `0x008A6CA0` (`0x3015`), `0x008A6CC0` (`0x3016`), `0x008A78B0` (`0x3036`), `0x008A75C0` (`0x304D`); CIItem secondary `+0x64/+0x68` alias `0x009E1D00`/`0x009E1A30` | server delivery ordering, complete `0x3015` wire grammar, raw class-link pointer adjustment details, and ordinary client ground-pickup sender |
| Client SQL boundary | confirmed negative | no database-driver import or SQL/database query literal in pinned `sro_client.exe` | server/external durable persistence implementation |
| Inventory persistence | confirmed downstream for COS/action-`0x11` and player action-`0x06` | both successful paths set record `+0x38 = 1`, then `0x0050FA50 -> 0x0045D970 -> 0x0045D9E0 -> 0x009742D0(2, ...) -> 0x00408870 -> 0x004E7350(1)`; metadata builder emits descriptor-ordered `UPDATE ... SET ... WHERE ...`; slot worker `0x0045DC80`; updater vtable `0x00AE56C8`; owner callback `+0x18/+0x1C -> +0x5D4`; `AQ_SlotUpdater +0x20 = 8` joins player success to `0x004E73D0 -> 0x004E9480 -> 0xB034`; worker failure dispatches to `0x004E9110`/logout for AQ type `8` | runtime-generated table/column names, source-level context names, and COS completion-hook response semantics |
| Audit log | confirmed conditional player amount-credit submission and generic execution surface | `0x0048FC70 -> 0x004EACA0 -> 0x004E4B60 -> 0x00468A80`, `0x0043A510 -> 0x0043A590`, `AQ_LogWriter 0x0043B220 -> 0x009724C0` | ordinary/COS pickup coverage and submission-to-worker scheduling |

## Implementation guardrails

- Preserve per-attempt chance rolls, the inclusive amount ranges, and the two-`rand()` million-point construction; replacing it with one uniform floating random changes deterministic behavior.
- Apply the configured item-drop factor to both direct and group candidate acceptance exactly as the recovered routines do.
- Treat `Overlap=1` as per-assignment candidate removal only.
- The `typeID1/typeID2 == 3/3` pickup path deactivates and returns the item to its pool; do not generalize that concrete release function to every item family without the matching vtable evidence.
- Do not identify the client-visible remove packet for a ground item until the `0x00417090` removal path is connected to a packet writer by capture- or binary-level evidence.
- Keep the world-object lifecycle, visibility bridge, SQL caller, and owner-callback anchors explicit; the GameServer socket handoff is now closed through the C8258C `+0x50`/CSockStream/`WSASend` path, but the selected service identity, complete wire grammar, source-level names for operation-context pointers, and COS completion-hook response semantics remain unresolved.
