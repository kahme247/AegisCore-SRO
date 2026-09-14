# Binary-confirmed items, inventory, alchemy and item-mall surface

This is the cross-feature index for the pinned TestIn `SR_GameServer.exe` and
the matching vSRO client executable. It records executable evidence first and
separates it from AegisCore's current MySQL implementation.

## Evidence identity and confidence

| Artifact | SHA-256 | Role |
| --- | --- | --- |
| TestIn `SR_GameServer.exe` | `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35` | Server authority; 32-bit x86, 8,925,184 bytes |
| `sro_client.exe` | `3DF2282DAB84EE5439944B3E6CA8E8CFC69CB05963A45F0F7F4B8ADCA579C206` | Client construction and response parsing |

**Confirmed** means an instruction, branch, memory access, serializer,
dispatcher entry, RTTI name, or SQL literal is present. **Derived** means a
deterministic interpretation of confirmed facts. **Unresolved** means the
usual emulator name, full packet layout, stored-procedure body, transaction
boundary, or field semantics were not recovered and must not be treated as a
compatibility contract.

Primary focused reports:

- [equipment lifecycle](sr-gameserver-equipment-lifecycle.md)
- [alchemy](sr-gameserver-alchemy.md)
- [drops and loot](sr-gameserver-drops-loot.md)
- [COS/pets](sr-gameserver-pet-systems.md)
- [unified GameServer reference](sr-gameserver-binary-reference.md)

## Lifecycle map

```text
reference item data / server-generated item record
                     |
                     v
        CGItem-family runtime object or storage item
                     |
   +-----------------+-------------------+------------------+
   |                 |                   |                  |
   v                 v                   v                  v
inventory       equipment           storage/COS       ground item
0x7034          0x7034               0x7034            0x7034 action 6
   |                 |                   |                  |
   +-----------------+-------------------+------------------+
                     |
                     v
        asynchronous storage query / item mutation
                     |
                     v
             client inventory update

alchemy is a staged branch over inventory slots:
0x7150/0x7151/0x7155/0x7157/0x716A -> validation -> delayed/immediate result
                                                   -> item change accumulator
                                                   -> B150/B151/B155/B157/B16A

item mall is a cash-item storage branch:
catalog/package -> cash-item stored procedure -> inventory/storage item
```

The stock server's ordinary equip/unequip functions prove the in-memory
transition and slot rules, but no direct call edge from those routines to a
database submission was recovered. Persistence is performed by separate
`AQ_StorageUpdater` paths. This distinction is important for failure and
rollback analysis.

## Opcode registry for the item surface

| Direction | Opcode | Binary-confirmed role |
| --- | ---: | --- |
| C -> S | `0x7034` | Item-operation family; includes binary-confirmed item-mall purchase senders |
| S -> C | `0xB034` | Item-operation response family; discriminator-driven body |
| C -> S | `0x704C` | TypeID-driven item-use request family; variable action forms |
| S -> C | `0xB04C` | Ordinary PC item-use result; client reads `u8 result`, then either `u16 errorCode` or success fields `u8,u16,u16` |
| S -> C | `0xB0C5` | COS item-use result family; separate success/failure grammar from PC `0xB04C` |
| S -> C | `0x34B1` | Item-use state/flag side effect; focused branch emits `u8 0xFF,u8 0x0C` |
| C -> S | `0x7202` | Dedicated potion/skill-restoration request; client writes `u32,u32,u8` |
| C -> S | `0x7203` | Sibling dedicated potion/skill-restoration request; client writes `u32,u32,u8` |
| S -> C | `0xB202` | Dedicated `0x7202` status response; server writes `u8 2,u16 resultCode` on non-`0x7800` status |
| S -> C | `0xB203` | Dedicated `0x7203` status response; server writes `u8 2,u16 resultCode` on non-`0x7800` status |
| S -> C | `0x3040` | Masked inventory/item state update; server serializer and stock client decoder are binary-confirmed |
| S -> C | `0x3047` / `0x3048` / `0x3049` | Inventory/storage multipart transfer; begin and end widths, data framing, and client item parser are binary-confirmed |
| S -> C | `0xB03E` | Maintained repair response: success `u8 1`, failure `u8 2 + u16 code`; code semantics remain open |
| S -> C | `0x3052` | Maintained server contract: `u8 slot, u32 value`; stock client handler body still open |
| S -> C | `0x3092` | Capacity update: `u8 target` (`1` inventory, `2` storage), `u8 resultingCapacity` |
| S -> C | `0x305C` | Item-used/entity notification; server writes two `u32` values and the stock client reads two `u32` values |
| S -> C | `0x3153` | Three `u32` balance-state values; server state writer can emit them, client stores them at `+0x4AC/+0x4B0/+0x4B4` |
| S -> C | `0x3154` | `u8 reason, u8 cashType, u32 value`; validated server-side for reason `0..5` and cash type `0..2` |
| C -> S | `0x7150` | General reinforcement/elixir staged request |
| S -> C | `0xB150` | Reinforcement/elixir result, cancel, or error |
| C -> S | `0x7151` | Magic/attribute stone staged request |
| S -> C | `0xB151` | Magic/attribute stone result, cancel, or error |
| C -> S | `0x7155` | Material/resolve/compound family |
| S -> C | `0xB155` | Material result family |
| C -> S | `0x7157` | Dismantle/equipment-resolution family |
| S -> C | `0xB157` | Dismantle result family |
| C -> S | `0x716A` | Socket multiplexer |
| S -> C | `0xB16A` | Socket result/cancel/error |
| C -> S | `0x34A7` / `0x34A9` | Avatar magic-option and magic-option grant requests |
| S -> C | `0x34A8` / `0x34AA` | Avatar option result and magic-option update |
| S -> C | `0x3036` | Pickup animation constructor in the recovered loot path |
| S -> C | `0x304D` | Timed ground-item ownership removal notification |

The alchemy rows are exact GameServer dispatch entries. The inventory rows
marked as protocol-catalog entries are useful compatibility anchors, but the
current binary pass does not promote every catalog name to a function-level
wire proof. Direction and phase must remain part of the opcode key.

### `0x3092` two-byte update

The client registration and server disassembly independently establish a
server-to-client `0x3092` body containing exactly two consecutive bytes. The
first is a target (`1` player inventory, `2` storage) and the second is the
resulting capacity. GameServer builder `0x004E8B61` writes them in that order;
its inventory branch loads target `1` and the committed capacity. The stock
client `On3092` path dispatches target `1` to player inventory capacity and
target `2` to storage capacity.

## Inventory and equipment structure facts

### Slot domains

The stock equipment path confirms the normal inventory wire range
`0x0D..0x6F` inclusive. Body slots are below `0x0D`. The operation descriptor
used by `0x00523860`, `0x00524CD0`, and `0x005255E0` has these confirmed fields:

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x00` | `u8` | Operation discriminator; ordinary relocation enters when zero |
| `+0x01` | `u8` | Mutation/result state; observed values `0`, `3`, and attach-only `4` |
| `+0x03` | `u8` | Source slot |
| `+0x04` | `u8` | Target slot |
| `+0x28`, `+0x2C` | 32-bit values | Opaque context copied into temporary relocation records |

Normal-to-body selects the attach path, body-to-normal selects detach, and
normal-to-normal uses general relocation. Body slot `8` and paired slots `6/7`
have separate branches. The upper slot bound is also read from the player,
so `0x6F` is a protocol ceiling, not proof that every character has all slots.

### Storage ownership model

The GameServer SQL strings prove separate storage link tables rather than one
universal inventory table:

| Storage | Link table | Item row | Additional joins |
| --- | --- | --- | --- |
| character inventory | `_Inventory` | `_ITEMS` by `ItemID = ID64` | `_BindingOptionWithItem`, `_RentItemInfo` |
| avatar inventory | `_InventoryForAvatar` | `_ITEMS` | binding/rent rows |
| COS inventory | `_InvCOS` | `_ITEMS` | binding/rent rows |
| user chest | `_Chest` | `_ITEMS` | binding/rent rows |
| guild chest | `_GuildChest` | `_ITEMS` | binding/rent rows |
| linked storage | `_InventoryForLinkedStorage` | `_ITEMS` | linked-item ID |
| open market | `_OpenMarket` | `_ITEMS` | binding rows |
| magic-cube storage vocabulary | `_InventoryForMagicCube` | `_ITEMS` | table-name registry is confirmed; complete item load/use edge remains unresolved |

The stock loader orders occupied slots and filters out zero or null `ItemID`.
`_ITEMS.REFITEMID` is separately updateable; it is not safe to assume the
reference ID is immutable for every stock operation.

### Storage bootstrap and item-row load boundary

The stock character loader reaches `0x0043D2C0` before materializing storage
items. That helper first calls the expired-rental procedure with a storage
type, storage owner ID and a formatted time string:

```sql
{?=CALL _DelExpiredRentItemInStorage (%d, %d, '%s')}
```

The loader treats nonnegative procedure output as eligible to continue. The
diagnostic branches preserve three negative result values: `-3` item-delete
failure, `-2` unknown storage type, and `-1` storage update failure. These are
server diagnostics, not client-facing error opcodes.

After a nonnegative procedure result, it enumerates occupied slots with a
storage-specific query:

| Storage type | Slot query predicate |
| ---: | --- |
| `0` | `SELECT SLOT FROM _INVENTORY WHERE CharID=%d AND ItemID<>0 ORDER BY Slot ASC` |
| `1` | `SELECT SLOT FROM _CHEST WHERE UserJID=%d AND ItemID<>0 AND ItemID IS NOT NULL ORDER BY Slot ASC` |
| `2` | `SELECT SLOT FROM _INVCOS WHERE COSID=%d AND ItemID<>0 AND ItemID IS NOT NULL ORDER BY Slot ASC` |
| `4` | `SELECT SLOT FROM _GUILDCHEST WHERE GuildID=%d AND ItemID<>0 AND ItemID IS NOT NULL ORDER BY Slot ASC` |
| `5` | `SELECT SLOT FROM _INVENTORYFORAVATAR WHERE CharID=%d AND ItemID>0 ORDER BY Slot ASC` |

The subsequent storage-specific loader then issues independent whole-row
queries. The COS path at `0x00440CB0` is explicit:

```sql
SELECT IT.* FROM _InvCOS INV JOIN _ITEMS IT ON INV.ItemID=IT.ID64
  WHERE INV.COSID=%d AND INV.ItemID IS NOT NULL AND INV.ItemID<>0
  ORDER BY INV.Slot ASC
SELECT BIND.* FROM _InvCOS INV JOIN _ITEMS IT ON INV.ItemID=IT.ID64
  JOIN _BindingOptionWithItem BIND ON IT.ID64=BIND.nItemDBID
  WHERE INV.COSID=%d AND INV.ItemID IS NOT NULL AND INV.ItemID<>0
  ORDER BY INV.Slot ASC
SELECT R.* FROM _InvCOS INV JOIN _ITEMS IT ON INV.ItemID=IT.ID64
  JOIN _RentItemInfo R ON IT.ID64=R.nItemDBID
  WHERE INV.COSID=%d AND INV.ItemID IS NOT NULL AND INV.ItemID<>0
  ORDER BY INV.Slot ASC
```

The character and linked-storage variants use the corresponding `_Inventory`
or `_INVENTORYFORLINKEDSTORAGE` join. The COS loader explicitly performs the
three-way item/binding/rent split above; the linked-storage edge recovered in
this pass directly shows its `IT.*` query, while other storage loaders contain
the additional binding/rent queries. The binary assigns the resulting item
objects to storage records and retains the slot order, but the generic
result-set layer receives `SELECT IT.*`; this
pass does not recover a stable named `_ITEMS` column-to-offset table from the
indirect loader. Do not derive that order from the maintained AegisCore schema
or from the textual query alone.

#### What the binary does and does not expose about the row parser

The lower-level query worker at `0x00951970` confirms the transport boundary:
it executes the SQL, calls `SQLNumResultCols`, then loops ordinal `1..N`,
calling `SQLDescribeCol` and `SQLGetData` for each result column into a
caller-owned contiguous row buffer. That function is generic; it does not
name `_ITEMS` columns or select individual fields by name. The storage query
helpers then pass result containers through indirect callback/virtual paths.

The later object factory at `0x00489730` confirms the next boundary: it chooses
a concrete object from the reference type bits, invokes the selected object's
`+0x350` initializer, and returns the runtime item. Its recovered code
consumes an already-formed factory/reference context; it contains no direct
`SQLGetData` calls and no `_ITEMS` column identifiers. Therefore the following
claims are binary-confirmed: ordinal SQL-row buffering, storage-specific
`SELECT IT.*`, slot-order retention, reference-type dispatch, and runtime item
construction. The following claim is not recovered: `IT.*` ordinal *n* maps to
named `_ITEMS` column *x* or item-object offset *y*. The missing adapter is an
indirect callback path, so this report leaves the column map open rather than
guessing from schema order or object offsets.

Evidence for this boundary: [`00951970_FUN_00951970.c`](../../analysis/decompiled/gameserver-sql/00951970_FUN_00951970.c),
[`00489730_FUN_00489730.c`](../../analysis/decompiled/itemdata-trace/00489730_FUN_00489730.c),
and [`00485E70_FUN_00485e70.c`](../../analysis/decompiled/itemdata-factory-objects/00485E70_FUN_00485e70.c).

Raw loader evidence: [`0043D2C0_FUN_0043d2c0.c`](../../analysis/decompiled/gameserver-sql/0043D2C0_FUN_0043d2c0.c),
[`0043D980_FUN_0043d980.c`](../../analysis/decompiled/gameserver-sql/0043D980_FUN_0043d980.c),
[`00440CB0_FUN_00440cb0.c`](../../analysis/decompiled/gameserver-sql/00440CB0_FUN_00440cb0.c),
and [`0043EEC0_FUN_0043eec0.c`](../../analysis/decompiled/gameserver-sql/0043EEC0_FUN_0043eec0.c).

`_InventoryForMagicCube` is confirmed in the executable's database table-name
registry, but this pass did not recover a complete GameServer item load or
operation edge for that storage. It is therefore catalog evidence, not proof
of a live client opcode or a complete magic-cube packet lifecycle. The table
name is visible in [`00730120_FUN_00730120.c`](../../analysis/decompiled/gameserver-sql/00730120_FUN_00730120.c).

## Inventory/storage multipart transfer: `0x3047`, `0x3049`, `0x3048`

The storage packet family is now recovered beyond the opcode catalogue. The
server's world-entry path at `0x004E1B70` first requires the player state
checks `player[0x762] != 0` and `player[0x763] != -1`, allocates `0x3047`,
sets the stream-copy length to `8`, and copies the player's two-word amount
pair at byte offsets `+0x1CA8` and `+0x1CAC` into the packet. The packet is
then submitted through the normal recipient `+0x27C` completion call. The
binary does not attach a public currency name to these offsets. A separate
character-load query logs a two-word `RemainGold` value in the same load
subsystem, but the direct field-identity edge is not fully pinned; the safe
wire description is therefore an 8-byte character amount pair.

The server then resets a reusable transfer buffer, calls the storage manager
serialization edge `0x004B8AD0(..., 1)`, chunks the resulting bytes through
`0x00430A20(..., 0x3049)`, and finally sends an empty `0x3048`. The chunker
uses the buffer's produced length and caps each packet at the available
message capacity minus `0x20`; it does not reinterpret item bytes while
chunking. The buffer is a process-owned allocation initialized at
`0x00412C20` with a 10,000,000-byte capacity.

The mode-1 storage serializer at `0x004BA720` is exact at its framing layer:

```text
0x3047:
    u64 character amount pair     // two little-endian u32 writes

0x3049 data stream, before chunking:
    u8 storageCapacity
    u8 occupiedItemCount
    repeat occupiedItemCount times, in ascending slot scan order:
        u8 slot
        rent-info block
        u32 itemReferenceID
        type-dependent item body

0x3048:
    empty body
```

`0x004BA720` obtains the storage capacity from storage state `+0x20`, asks
the storage virtual `+0x58` for the occupied count, writes both as bytes, then
scans slots `0..capacity-1`. For each non-null item it writes the slot byte and
calls the item virtual `+0x498(stream, 1)`. This proves the slot ordering and
the two leading data bytes; it does not justify flattening all item classes to
one fixed body.

The matching stock client registration is in
[`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c):
`0x3047 -> 0x008A74F0`, `0x3049 -> 0x008A8C10`, and
`0x3048 -> 0x008A9100`. The handlers prove the reassembly lifecycle:

- `0x008A74F0` marks the client transfer state active, clears its accumulated
  byte range, reads exactly `8` bytes, and stores the two words through
  `0x006CC470`.
- `0x008A8C10` copies all remaining bytes in 4-KiB-or-smaller pieces into the
  transfer stream; it performs no item interpretation.
- `0x008A9100` clears the active state and accumulated range, then calls
  `0x008A8C20`, which parses the reassembled storage stream.

The client parser at `0x008A8C20` reads the capacity byte, then the occupied
count, then one slot byte per item. `0x008BB130` confirms the rent block
grammar before the item parser reads its reference ID:

```text
u32 rentFlags
if rentFlags & 1:
    u16 rentFieldA
    u32 periodBegin
    u32 periodEnd
if rentFlags & 2:
    u16 rentFieldB
    u32 meterRateOrPackingValue
u32 itemReferenceID
```

The byte widths, flag tests, and ordering are binary-confirmed. The labels
`canDelete`, `canRecharge`, and the exact distinction between meter-rate and
packing values are conventional interpretations of these fields, not names
recovered from this parser.

The remaining body is selected from the packed TypeID. The equipment branch
at `0x008BE4F0` reads the confirmed prefix `u8 optLevel`, `u64 variance`, and
`u32 data`, followed by a counted variable magic-option/binding area. Other
TypeID branches read different widths, including the expendable quantity and
COS/container forms. Thus the server's `+0x498` virtual dispatch and the
client's `0x008BE4F0` TypeID fan-out are the binary-confirmed structure
boundary; the complete body must remain class/type dependent.

This transfer is a load/notification path, not a direct SQL call. The SQL
calls that supply its in-memory items are the storage-specific loaders and
`_ITEMS`/binding/rent queries documented above. `0x004E1B70`,
`0x004BA720`, `0x00430A20`, and the client handlers contain no stored-procedure
literal or database submission. Mutations after load use the separate
`AQ_StorageUpdater` procedures documented in the operation sections below.

Raw server evidence: [`004E1B70_FUN_004e1b70.c`](../../analysis/decompiled/drop-refs/004E1B70_FUN_004e1b70.c),
[`004BA720_FUN_004ba720.c`](../../analysis/decompiled/storage-serialization/004BA720_FUN_004ba720.c),
[`00430A20_FUN_00430a20.c`](../../analysis/decompiled/storage-server-extra/00430A20_FUN_00430a20.c),
[`0048FB60_FUN_0048fb60.c`](../../analysis/decompiled/itemdata-extra/0048FB60_FUN_0048fb60.c),
and [`00409AE0_FUN_00409ae0.c`](../../analysis/decompiled/storage-serialization/00409AE0_FUN_00409ae0.c).
Raw client evidence: [`008A74F0_FUN_008a74f0.c`](../../analysis/decompiled/client-storage-handlers/008A74F0_FUN_008a74f0.c),
[`008A8C10_FUN_008a8c10.c`](../../analysis/decompiled/client-storage-handlers/008A8C10_FUN_008a8c10.c),
[`008A9100_FUN_008a9100.c`](../../analysis/decompiled/client-storage-handlers/008A9100_FUN_008a9100.c),
[`008A8C20_FUN_008a8c20.c`](../../analysis/decompiled/client-storage-extra/008A8C20_FUN_008a8c20.c),
[`008BB130_FUN_008bb130.c`](../../analysis/decompiled/client-storage-extra/008BB130_FUN_008bb130.c),
and [`008BE4F0_FUN_008be4f0.c`](../../analysis/decompiled/client-storage-extra/008BE4F0_FUN_008be4f0.c).

### Per-class `+0x498` item-body serializers

The storage writer does not call one monolithic item serializer. The primary
MSVC RTTI vtables show this `+0x498` dispatch map:

| Runtime class | Primary vtable | `+0x498` target | Confirmed storage-mode behavior |
| --- | ---: | ---: | --- |
| `CGItem` | `0x00AE9384` | `0x0048FB60` | shared item-data delegate |
| `CGItemContainer` | `0x00AE9934` | `__purecall` | abstract/base table; concrete derived classes supply the body |
| `CGItemCOSSummoner` | `0x00AE9E5C` | `0x00492D40` | shared body plus COS state/name/lifetime/status fields |
| `CGItemMonsterCapsule` | `0x00AEA394` | thunk `0x00493B60` | delegates to the shared item-data writer |
| `CGItemEquip` | `0x00AEA9EC` | `0x0048FB60` | shared item-data delegate; TypeID controls client body |
| `CGItemExpendable` | `0x00AEB2B4` | `0x0048FB60` | shared item-data delegate; TypeID controls client body |
| `CGItemStorage` | `0x00AEB824` | `0x004A2F50` | shared body plus one `u32` storage-derived value |

The shared `0x0048FB60(item, stream, mode)` path accepts modes `1`, `2`, and
`4`; mode `1` delegates to the item-data object's virtual `+0x28(stream,1)`.
The mode-1 `CGItemCOSSummoner` continuation at `0x00492D40` then writes, in
the confirmed order, a state byte, a COS/object reference `u32`, an optional
length-prefixed name or lifetime form, a status-count byte, and a variable
status list. Each status begins with a byte, a `u32`, and a `u32`; status kind
`5` adds another `u32` and a byte. The executable proves these widths and
branches, but not public names for every state/status field.

The `CGItemStorage` continuation at `0x004A2F50` calls virtual `+0x518` and
writes its returned `u32` after the shared body. Its concrete implementation
`0x004A30B0` reaches `0x004B86F0`, whose diagnostic names the operation
`CGStorage::CountItemsHasSameRefObj`; the value is therefore a count derived
from the selected storage context, not an item reference ID. This is the
binary edge behind the client container branch that consumes one additional
32-bit storage-item value. The exact catalog name of every runtime
`CGItemStorage` instance remains separate from this structural proof.

The complete client-side TypeID2/TypeID3 width table, including equipment,
stone/assimilation, gacha, quantity, container/COS, and rent branches, is
cross-indexed in [`vsro188-client-decoder-layouts.md`](vsro188-client-decoder-layouts.md#alchemy-result-prefixes-0xb150-0xb151-and-0xb16a).

The maintained serializer maps the confirmed gacha tail to the existing
`Item.MagicOptions` collection: it writes a bounded count (`0..12`) followed
by each option's two `u32` fields. This supplies the confirmed wire shape
without inventing gacha-specific SQL columns or meanings; an item with no
hydrated options correctly emits count zero.

These exceptions explain why the client parser cannot be represented as one
fixed `Item` struct: the shared item-data delegate, COS continuation, storage
continuation, and TypeID-dependent client readers are separate layers.

Raw vtable map: [`item-serializer-vtable-slots.txt`](../../analysis/decompiled/item-vtable/item-serializer-vtable-slots.txt).
Raw serializer evidence: [`00492D40_FUN_00492d40.c`](../../analysis/decompiled/item-storage-serialization/00492D40_FUN_00492d40.c),
[`004A2F50_FUN_004a2f50.c`](../../analysis/decompiled/item-storage-serialization/004A2F50_FUN_004a2f50.c),
[`004A30B0_FUN_004a30b0.c`](../../analysis/decompiled/item-storage-serialization/004A30B0_FUN_004a30b0.c),
[`004B86F0_FUN_004b86f0.c`](../../analysis/decompiled/item-storage-serialization/004B86F0_FUN_004b86f0.c),
and [`0048FB60_FUN_0048fb60.c`](../../analysis/decompiled/itemdata-extra/0048FB60_FUN_0048fb60.c).

## Server `0xB034` result emitter and ordinary-operation branches

The exact server result builder at `0x004E9480` starts the item-operation
response with `u8 success = 1`, then calls the common operation serializer
`FUN_004F76E0(operation, 1)`. The operation object is polymorphic; the emitter
does not serialize one universal inventory record. The recovered operation
discriminator branches are:

| Discriminator | Binary-confirmed continuation |
| ---: | --- |
| `0x02` | resolve the reference keyed by operation byte `+0x04`; after the item validity virtual call, dispatch player action `0x3B` |
| `0x03` | same reference/validity gate, then dispatch player action `0x20` |
| `0x06`, `0x1C` | resolve operation byte `+0x04`; valid items dispatch player action `9`, then the shared item/entity transition path runs |
| `0x0E` | resolve and validate the item, then dispatch player action `9` |
| `0x11` | resolve the item; an additional player-state virtual call precedes the shared transition path |
| `0x15` | use operation `+0x3C` as a reference/context key, derive a runtime object, and dispatch player action `0x15` |
| `0x07`, `0x0A`, `0x12` | resolve the object keyed by operation `+0x40`; compare the related object at `+0x3C` and take the object-state transition path |
| `0x08`, `0x13`, `0x18`, `0x2B` | enter the shared operation/finalization path; `0x08` and `0x2B` first take the extra object/action branch |
| `0x09` | resolve context `+0x3C` and dispatch player action `0x15` when its derived object passes the observed bound check |
| `0x2A` | no operation-specific post-action is recovered; the emitter reaches its generic output-write path |

The emitter also has an early special-object path that sends `0xB0C5` with an
operation context when operation byte `+0x04` is `0xFE` and player state
permits it. Several shared branches can send `0xB0C5` to a related
player/object before completing `0xB034`. These are exact response-side
branches, not a recovered `0x7034` request parser. The ordinary request
dispatcher and semantic names of the operation bytes remain open.

Raw evidence: [`004E9480_FUN_004e9480.c`](../../analysis/decompiled/gameserver-quest-async/004E9480_FUN_004e9480.c).

## `0x7034` request parser and operation dispatch

The common server path is now bounded more precisely. `0x0050F5C0` reads an
operation record, calls `0x004D8BF0` to parse its variable request body, then
indexes the initialized function table at `0x00CD3B58` with `operation * 8`.
The table is populated by `0x0050CE80`; a zero table entry is an unsupported
or intentionally unhandled operation, not proof of a missing handler.

The parser record is not one fixed item struct. Confirmed shared fields used
by the parser and handlers are:

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x00` | `u8` | operation discriminator |
| `+0x03`, `+0x04` | `u8` | slot/reference selectors in relocation, drop, linked-storage, and object branches |
| `+0x06` | `i16`/`u16` by branch | requested quantity or handler-derived count |
| `+0x0C` | 32-bit value | object/context key passed to state lookup |
| `+0x14` | `u8` | item-mall catalog index for operation `0x18` |
| `+0x2C`, `+0x30` | pointer/32-bit value | resolved runtime object and its identifier in several item branches |
| `+0x38` | 32-bit state | branch output/temporary state; cleared by one successful handler |
| `+0x88` | `u32` | parsed-field presence mask; bit `0` marks the bounded amount pair and bit `0x10` marks the mall block |
| `+0x90`, `+0x94` | `u64` split low/high | bounded money/amount pair; handlers enforce non-negative limits |

For mall operation `0x18`, the parser reads the exact fields already listed in
the client serializer section: a `u16` at `+0x12`, three one-byte values at
`+0x02..+0x03` and `+0x14`, a length-prefixed blob at `+0xF4`, a `u16` at
`+0x06`, and a `u64` at `+0xB0`. The server handler at `0x00526610` consumes
`+0x12` and `+0x14`, resolves the catalog through `0x00523740`, and enters
the package-item insertion chain.

### Client outbound storage-operation cross-check

The matching client has a common storage-operation sender at `0x007950B0`.
Its recovered send sequence is:

```text
IsOpcodeSupported(0x7034, 0)
select opcode 0x7034
serialize operation record with 0x007923A0(record, 0)
submit through 0x008418D0(record)
```

The normal inventory UI call sites pass inventory indices to this helper; the
helper encodes the two ordinary inventory slots as `index + 0x0D` before
serializing operation `0x00`. The mode-0 serializer at `0x007923A0` then emits
the exact fixed prefix:

```text
u8  record +0x03
u8  record +0x04
u16 record +0x06
```

For operation `0x00`, a count-prefixed child vector may follow. Each child is
serialized as `u8 +0x03, u8 +0x04, u16 +0x06`; the vector loop is bounded by
the client record's own begin/end pointers. These are wire widths and record
locations, not proof that every caller means move, swap, merge or split. The
server-side descriptor and SQL mode selection are independently confirmed by
`0x0045B5A0` above. The same client sender family contains the separately
documented operation-`0x18` mall serializer.

Raw client evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
and [`007923A0_disasm.txt`](../../analysis/decompiled/item-mall-client/007923A0_disasm.txt).

### Operation `0x00`: ordinary two-slot inventory relocation

The stock client has a direct operation-`0x00` call path in
`0x007950B0`'s `param_1 == 0x46, param_3 == 0x46` branch. For the ordinary
inventory form it validates both indices, encodes each as `index + 0x0D`,
initializes the quantity/context field, checks `0x7034` support, and submits
through `0x008418D0`. The mode-0 request body is:

```text
u8 operation = 0x00
u8 record +0x03 = source inventory slot
u8 record +0x04 = destination inventory slot
u16 record +0x06 = operation quantity/context field
```

The server entry `0x00523860` accepts the two-slot form only in the observed
inventory range (`0x0D..0x6F`) and dispatches to the ordinary item relocation
helpers. `0x00523F70` resolves the source runtime item, rejects protected
state, moves it directly when the destination is empty, or selects a free
slot through `0x004B8260` before attaching the item with
`0x004B7F10`/`0x004B7E90`. The operation record is then normalized by the
post-dispatch path and `0x0050FA50` selects the generic storage updater for
operation `0x00`.

The generic builder `0x0045B5A0` uses descriptor byte `+0x01` to select the
stored procedure:

```text
descriptor +0x01 = 0 or 4 -> _STRG_MOVE_ITEM
descriptor +0x01 = 1       -> _STRG_MERGE_ITEM
descriptor +0x01 = 2       -> _STRG_SPLIT_ITEM
descriptor +0x01 = 3       -> _STRG_SWAP_ITEM
```

For the normal two-slot relocation, the successful base-updater completion
reaches `0x004E9480` and emits `0xB034` with status `1`. Its operation-`0x00`
success body is:

```text
u8 operation = 0x00
u8 record +0x03
u8 record +0x04
u16 record +0x06
u8 child-count
repeat child-count: u8 child +0x03, u8 child +0x04, u16 child +0x06
```

The child vector is emitted from the updater's result state; its relation to
stack splitting/merging is structurally confirmed, but the public quantity
and result semantics remain separate from the wire widths.

Raw evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
[`00523860_FUN_00523860.c`](../../analysis/decompiled/gameserver-item-7034/00523860_FUN_00523860.c),
[`00523F70_FUN_00523f70.c`](../../analysis/decompiled/gameserver-item-7034/00523F70_FUN_00523f70.c),
[`0045B5A0_FUN_0045b5a0.c`](../../analysis/decompiled/gameserver-sql/0045B5A0_FUN_0045b5a0.c),
and [`004F76E0_FUN_004f76e0.c`](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c).

### Operation `0x07`: client drop request, ground-object construction, and deletion SQL

One ordinary inventory call site is binary-confirmed in the stock client:
`0x007950B0`'s `param_1 == 0x46, param_3 == 0` branch prepares operation
`0x07`, adds `0x0D` to the supplied inventory index, checks support for
`0x7034`, serializes with `0x007923A0(record, 0)`, and submits through
`0x008418D0`. The operation-`0x07` serializer writes exactly one request-body
field after the operation discriminator:

```text
C -> S 0x7034
u8 operation = 0x07
u8 record +0x03 = inventory index + 0x0D
```

The caller's UI parameters are not promoted to a public action name solely
from the `0x46/0` constants. The `+0x0D` slot convention and the server's
operation-`0x07` handler do, however, join this sender to the normal
inventory-item path.

On the server, `0x005264E0` applies the player virtual `+0x244` gate, requires
the relevant message state to be zero, rejects a slot below `0x0D` with
`0x186D`, and calls `0x004B2AB0`. That helper bounds the slot against the
player inventory count at `+0x70C`, resolves the runtime item through
`0x004B8000(slot)`, requires the item data state at `+0x20/+0x24`, applies the
item virtual `+0x50`/`+0xD0` validity gates, and copies the resolved item
pointer/identifier/data value into operation record `+0x3C/+0x40/+0x44`.
It then executes the item-to-ground transition helpers
`0x004B7F10`, `0x004901B0`, the player factory virtual `+0x14`, item virtual
`+0x4DC`, and cleanup `0x00485E50` on failed construction. These calls prove
the transfer/ground-object construction edge, but not a fixed ground-object
wire body or a public name for every state field.

After the handler succeeds, the generic storage boundary selects updater
operation `0x07` and `0x0045A840`, which formats the exact deletion call:

```sql
{?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}
```

The formatter supplies the updater storage context, a 64-bit resolved value,
a count/value field, and the final storage selector/context in the observed
order. Their SQL column names and procedure transaction semantics are not
recoverable from this format string.

The successful asynchronous response edge is also binary-confirmed for this
family. The base storage-updater constructor initializes completion type
`+0x20 = 1`; its owner callback reaches `0x004E73D0`, whose type-`1` path for
operation `0x07` falls through to `0x004E9480`. That function emits `0xB034`
with leading `u8 status = 1` and calls `0x004F76E0(record, 1)`. The success
body for the three related operation values is:

```text
0x07: u8 operation, u8 record +0x03
0x12: u8 operation, u32 record +0x30, u8 record +0x03
0x17: u8 operation, u32 record +0x118, u8 record +0x03
```

After emitting the response, `0x004E9480` resolves the ground-object context
at record `+0x40`, checks it against record `+0x3C`, and calls
`0x004901C0` or `0x004901B0` according to record `+0x118`; this is the
post-completion object/attachment cleanup branch. A non-successful database
completion is routed through `0x004E9110`; this pass does not promote a
dedicated operation-`0x07` failure packet or assign the response fields a
public semantic name.

The same server handler accepts operations `0x12` and `0x17`; operation `0x17`
can be normalized to `0x07` after its equipped-weapon rejection check. The
client serializer has distinct capability branches. Their mode-0 bodies are:

```text
0x12: u8 operation, u32 record +0x30, u8 record +0x03
0x17: u8 operation, u32 record +0xF0, u8 record +0x03
```

The available `0x007950B0` call site for the `0x12` form is the
`param_1 == 0x7A, param_3 == 0` branch; its UI/object meaning remains
unresolved. The serializer contains a `0x17` branch, but the examined
`0x007950B0` sender does not provide a direct `0x17` call site, so that edge
remains capability-only.

Raw evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
[`005264E0_FUN_005264e0.c`](../../analysis/decompiled/gameserver-item-7034/005264E0_FUN_005264e0.c),
[`004B2AB0_FUN_004b2ab0.c`](../../analysis/decompiled/item-use-extra-refs/004B2AB0_FUN_004b2ab0.c),
and [`0045A840_FUN_0045a840.c`](../../analysis/decompiled/gameserver-sql/0045A840_FUN_0045a840.c).

### Operation `0x08`: package-item insertion through the ordinary `0x7034` path

#### Compatibility boundary: overloaded `0x08`

This discriminator must not be treated as one universal business operation.
The recovered package-item sender writes four bytes after the operation:

```text
0x7034 payload after operation: u8 selectorA, u8 selectorB, u16 quantity
```

AegisCore's maintained NPC-economy adapter currently accepts a separate
eight-byte body for its existing transaction:

```text
0x7034 payload after operation: u8 tab, u8 slot, u16 amount, u32 npcUID
```

The two forms are safely demultiplexed by exact remaining body length. The
four-byte gold/package form now resolves against the authenticated selected
NPC and maintained shop/tab/package chain, applies the recovered package
quantity and slot rules in one transaction, and returns the primary generated
slot vector. Token operation `0x2B` resolves the selected expanded price-policy
entry, atomically debits the matching token stacks in ascending inventory-slot
order, and emits the confirmed continuation vector for every deleted or
partially reduced stack.

The stock client has a direct sender for operation `0x08` in
`0x007950B0`'s `param_1 == 0x0F, param_3 == 0x46` branch. It prepares the
two-byte selector from `param_2` using the observed `param_2 / 0xF0` and
low-byte/nibble calculation, stores `param_5` as the `u16` amount field, and
tests five local inventory/reference categories (`6`, `7`, `8`, `10`, `9`).
If any category test succeeds, the operation byte changes from `0x08` to
`0x2B`; otherwise it remains `0x08`. It then checks `0x7034` support,
serializes with `0x007923A0(record, 0)`, and submits through `0x008418D0`.
The mode-0 request bodies are exact:

```text
0x08/0x2B: u8 operation, u8 record +0x02, u8 record +0x03, u16 record +0x06
```

The client call site also fills the shared catalog/context value used by the
server's `+0x14` lookup; the public meaning of the selector and amount is not
assigned from the UI constants alone.

Server handler `0x005266A0` applies player virtual `+0x244(4)`, requires the
observed global state value `5`, rejects a non-positive `u16` at record `+0x06`
with `0x1829`, resolves record `+0x0C` through `0x004E72A0`, and looks up the
catalog/reference through `0x005236F0(record +0x14)`. For operations `0x08`
and `0x2B` it passes context selector `0x0D` into `0x00618FE0`; operation
`0x13` instead resolves an additional runtime object through
`0x004FD7F0` and stores it at record `+0x2C/+0x30` before entering the same
construction chain.

`0x00618FE0` validates available capacity and quantity, obtains the source
reference list through `0x00616780` using the selector fields, selects
destination slots through `0x004B8260`, constructs runtime items through the
player virtual `+0x14` and item virtual `+0x360`, and applies the per-item
insertion/update continuation. These calls prove a generated/runtime-item insertion lifecycle;
they do not prove a public NPC/shop name for operation `0x08`.

The post-dispatch selector at `0x0050FA50` is an important boundary: for
operation `0x08`, `0x13`, `0x18`, or `0x2B` it selects
`AQ_StorageUpdaterForPacakageItem` (`0x0045E040` -> `0x0045E080`), rather than
the generic `0x00459E20`/`0x00459F70` updater. The package builder selects
these exact SQL families:

```sql
{CALL _STRG_ADD_PACKAGEITEM (...)}
{CALL _STRG_ADD_PACKAGEITEM_WITH_STOCKQUANTITY (...)}
```

The variant is selected by the package/reference stock state at the builder's
record context. The result path is package-updater type `0x12/0x16`; after
child-record status validation (`child +0x19 == 1`), it can reach
`0x004E9480` and emit `0xB034`. For success mode `1`, the operation `0x08`
body is:

```text
u8 operation = 0x08
u8 record +0x02
u8 record +0x03
u8 generated-child-count
u8 child-byte[generated-child-count]
u16 record +0x06
```

Operation `0x13` adds `u32 record +0x30` immediately after the operation
byte. The child bytes are observed result-vector entries; their item/stock
semantics and the failure result mapping remain unresolved. Operation `0x2B`
then appends the exact additional vector:

```text
u8 continuationCount
repeat continuationCount:
    u8 child record +0x03
    u16 child record +0x06
```

The stock client `CNetProcessIn::OnB034` (`0x0087F370`) delegates to decoder
`0x0087EB50`, which reads each pair and constructs a child operation record
through `0x00878AB0`; the handler subsequently applies every constructed
record through the same `0x00871940`/`0x0087BA20` update path used for the
primary result. This proves widths, ordering, and child-continuation behavior,
but not a universal public name for the two fields. AegisCore's codec now
preserves this distinct `0x2B` envelope rather than collapsing it into the
`0x08` grammar.

Raw evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
[`005266A0_FUN_005266a0.c`](../../analysis/decompiled/gameserver-item-7034/005266A0_FUN_005266a0.c),
[`00618FE0_FUN_00618fe0.c`](../../analysis/decompiled/gameserver-item-7034/00618FE0_FUN_00618fe0.c),
[`0045E080_FUN_0045e080.c`](../../analysis/decompiled/gameserver-sql/0045E080_FUN_0045e080.c),
and [`004F76E0_FUN_004f76e0.c`](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c).

### Operation `0x09`: quantity mutation with delete-versus-slot-update persistence

The stock client has a direct operation-`0x09` sender in
`0x007950B0`'s `param_1 == 0x46, param_3 == 0x0F` branch. It writes the
inventory slot as `input + 0x0D`, stores `param_5` as the `u16` quantity, fills
the shared context value, checks `0x7034` support, and submits the serialized
record. The exact mode-0 request body is:

```text
u8 operation = 0x09
u8 record +0x03 = inventory slot + 0x0D
u16 record +0x06 = requested quantity
u32 record +0x0C = client context value
```

The sibling `param_1 == 0x7A, param_3 == 0x0F` branch prepares operation
`0x14`. Its serializer adds `u32 record +0x30` before the same slot, quantity,
and context fields; the public object/category meaning of that extra value is
not assigned here.

Server handler `0x005267F0` gates player state with virtual `+0x244(0x10)`,
requires a positive quantity, requires a normal inventory slot (`+0x03 >=
0x0D`) for operation `0x09`, or resolves the operation-`0x14` object through
`0x004FD7F0`, and then enters `0x006194E0` after the shared context and
reference checks. The handler itself contains no item-specific SQL literal.
`0x006194E0` applies the recovered quantity/state checks and updates the
operation record according to the resulting stack state:

```text
requested quantity == current stack:
    record +0x38 = 0
    item-removal continuation

requested quantity < current stack:
    record +0x38 = 1
    item virtual +0x4F0 decrements the in-memory stack
```

The post-dispatch selector preserves this distinction. `0x0050FA50` routes
`record +0x38 == 1` to `AQ_SlotUpdater` (`0x0045D970` -> `0x0045D9E0`), whose
metadata-driven SQL `UPDATE` is executed by `0x0045DC80`. A zero `+0x38`
continues through the generic storage updater; operation `0x09` or `0x14`
selects `0x0045A840` and the exact deletion procedure:

```sql
{?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}
```

Both successful asynchronous branches reach the player owner callback and
`0x004E9480`, which emits `0xB034` with status `1`. The success serializer
body is:

```text
0x09: u8 operation, u8 +0x03, u16 +0x06, u32 +0x0C, u32 +0x10
0x14: u8 operation, u32 +0x30, u8 +0x03, u16 +0x06, u32 +0x0C, u32 +0x10
```

The `+0x38 == 1` path uses `AQ_SlotUpdater` completion type `8`; the full
removal path uses the generic updater completion type `1`. Their common
`0xB034` success edge is confirmed, while the client-facing semantic names of
the quantity/context/result fields and the operation-specific failure body
remain unresolved.

Raw evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
[`005267F0_FUN_005267f0.c`](../../analysis/decompiled/gameserver-item-7034/005267F0_FUN_005267f0.c),
[`006194E0_FUN_006194e0.c`](../../analysis/decompiled/gameserver-quest-async/006194E0_FUN_006194e0.c),
[`0045DD50-owner-completion-callback.txt`](../../analysis/decompiled/pickup-transfer/0045DD50-owner-completion-callback.txt),
[`0045DC80-slot-updater-worker.txt`](../../analysis/decompiled/pickup-transfer/0045DC80-slot-updater-worker.txt),
and [`004F76E0_FUN_004f76e0.c`](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c).

### Operations `0x1A`/`0x1B`: participant item transfer and exchange SQL

The client sender `0x007950B0` contains direct call sites for both operation
values. The `0x1A` branch resolves a client object whose observed type byte is
`2`, uses the supplied inventory slot plus `0x0D` as the other selector, and
submits `0x7034`. The `0x1B` branch resolves a bounded object-list index and
uses the same common serializer. In mode 0, `0x007923A0` writes the exact
request body:

```text
u8 operation = 0x1A or 0x1B
u32 record +0x0C = resolved client context/object value
u8 record +0x03 = source selector
u8 record +0x04 = destination selector
```

The server handler `0x00523AA0` accepts `0x1A`, `0x1B`, and the linked helper's
forms `0x31`/`0x32`. It resolves the context at record `+0x0C`, validates the
two participant storage contexts through `0x00523960`, and creates a
`trade_item` record. The recovered record fields are:

```text
trade_item +0x00 = trade_item vtable
trade_item +0x02 = record +0x03 source selector (u8)
trade_item +0x04 = u64 result of a virtual +0x380 call
trade_item +0x09 = record +0x04 destination selector (u8)
```

The handler attaches the record through `0x00471700`, applies the observed
storage-selector helpers `0x004B7F10`/`0x004B7E90`, creates the participant
exchange object through `0x0043F370`, initializes it through `0x0043F3F0`,
and admits it to the asynchronous queue with `0x004E7350(1)`. The initializer
requires nonempty exchange state, nonzero participant database IDs, live
participant objects, and item database values; its diagnostics explicitly
include P2P trade/exchange and flea-market validation paths. These are direct
binary edges for participant item transfer, not names inferred from an opcode
number.

The corresponding completion worker at `0x0043FB20` walks both participant
storage lists and formats the exact nine-argument procedure call:

```sql
{?=CALL _STRG_MOVEITEM_NEW (%d, %d, %d, %d, %d, %d, %d, %d, %d)}
```

For participant balance state that is present, the same worker formats:

```sql
UPDATE _Char SET RemainGold = %I64d WHERE CharID = %d
```

The executable logs a failed procedure result as `item exchange stored
procedure returns error`. The worker contains no direct `0xB034` writer; the
client-visible completion/error packet for this exchange path therefore
remains unresolved, as do the stored-procedure argument and column names. The
local decompilation does not preserve the callee identity of the `+0x380`
virtual used for `trade_item +0x04`; only its 64-bit width and placement are
promoted.

Raw evidence: [`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
[`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c),
[`00523AA0_FUN_00523aa0.c`](../../analysis/decompiled/gameserver-item-7034/00523AA0_FUN_00523aa0.c),
[`0043F3F0_FUN_0043f3f0.c`](../../analysis/decompiled/gameserver-item-7034/0043F3F0_FUN_0043f3f0.c),
and [`0043FB20_FUN_0043fb20.c`](../../analysis/decompiled/gameserver-sql/0043FB20_FUN_0043fb20.c).

### Initialized dispatch entries

The following table records the non-zero entries in the binary-initialized
table. It intentionally uses address-level descriptions where the executable
does not establish a safe emulator name.

| Operation | Handler | Binary-confirmed behavior |
| ---: | ---: | --- |
| `0x00`, `0x10` | `0x00523860` | resolves item/reference context and enters the normal item relocation/object path; slot and TypeID-specific continuations are delegated |
| `0x01` | `0x00523EC0` | checks object state via `0x004E72A0` and player state at `+0x1D74`; successful branch clears record `+0x38` |
| `0x02` | `0x00524160` | validates a normal-inventory slot, resolves the reference item, derives a count through item virtual `+0x4E8`, bounds the amount, and invokes `0x00523F70` plus player delta virtual `+0x16C` |
| `0x03` | `0x00524090` | linked player/object-state gate followed by `0x00523F70` when the state differs |
| `0x04` | `0x00526910` | item service/type and option-state gates, then manager calls `0x00480A20` and optional `0x00442370`/`0x004806D0` notification path |
| `0x05` | `0x00526A50` | manager call `0x00480C70` followed by the same player-state notification family |
| `0x06`, `0x11` | `0x00526090` | player virtual `+0x244` gates the operation; failure returns `0x185F` or an error envelope |
| `0x07`, `0x12`, `0x17` | `0x005264E0` | validates a resolved item and calls `0x004B2AB0`; the `0x17` form can be normalized to `0x07`, with an explicit equipped-weapon drop rejection |
| `0x08`, `0x13`, `0x2B` | `0x005266A0` | validates quantity/context and enters `0x00618FE0` item construction/insertion; the handler supplies context `0x0D` for the `0x08`/`0x13` forms |
| `0x09`, `0x14` | `0x005267F0` | validates quantity and reference/slot state, then enters `0x006194E0` |
| `0x0B` | `0x00526B00` | consumes a bounded `u64` amount, invokes player delta virtual `+0x16C` with action `0x26`, and updates a player state flag |
| `0x0C`, `0x0D` | `0x00526CE0` | adjusts the player `+0x72A/+0x72B` amount pair and emits player delta virtual `+0x16C`; the two forms use opposite signs/contexts |
| `0x0E` | `0x00526E20` | converts the parsed amount through `0x00480F30` and sends the resulting value through `0x004806D0` |
| `0x18` | `0x00526610` | item-mall purchase: catalog resolve, capacity/price validation, package-item updater, and `_STRG_ADD_PACKAGEITEM*` SQL boundary |
| `0x1A`, `0x1B` (also accepted by the linked-storage helper as `0x31`, `0x32`) | `0x00523AA0` | two-party trade/exchange validation over the `+0x704` contexts, creates a `trade_item` record, queues the asynchronous operation, and retains the `MoveAlliedStorageOperation Failed` diagnostic on queue failure |
| `0x20`, `0x21` | `0x005243D0` | paired object/storage state branch; exact semantic name is unresolved |
| `0x1E` | `0x00524510` | validates a resolved item, derives and bounds a count, enters `0x00523F70`, and emits player delta action `0x1D` |
| `0x1F` | `0x005247D0` | linked state gate followed by `0x00523F70` |
| `0x23`, `0x24` | `0x00526ED0` | dispatches the `#`/`$` subforms to `0x00525950`/`0x00525AC0` |
| `0x27`, `0x28` | `0x00526F20`/`0x00527090` | object/reference and slot validation with open-object/item insertion continuations; exact semantic names remain unresolved |
| `0x2A` | `0x00527180` | initialized table entry is a code label continuation; its full function boundary is not separately recovered |

The table has additional non-zero entries at `0x20..0x24` and `0x2A..0x2B`
whose handlers are state/object helpers rather than direct SQL builders. The
important negative result is also binary-confirmed: `0x0050CE80` leaves many
operation slots null, so a protocol catalogue that labels every possible
`0x7034` byte as a live TestIn request would overstate the executable.

### Operation `0x02`: item validation, amount calculation, and gold persistence

The `0x7034` dispatch entry for operation `0x02` is `0x00524160`. Its
binary-confirmed path is a useful concrete example of an item operation whose
public English name is not safe to infer from the mechanics:

```text
C -> S 0x7034, operation 0x02
  -> 0x004D8BF0 request parser
  -> 0x00524160 validation/amount path
  -> 0x00523F70 two-slot storage mutation
  -> player virtual +0x16C(..., action 0x21, 1, 0)
  -> common 0x0050F1F0 / 0x0050FA50 updater submission
  -> 0x00459F70 -> 0x0045AF70
  -> _STRG_MOVE_ITEM
  -> _UPDATE_USER_N_CHAR_GOLD when updater operation +0x28 == 2
```

The handler requires the source slot at request `+0x03` to be at least
`0x0D`, resolves the request context at `+0x0C` through `0x004E72A0`, and
resolves the source item through `0x004B8000(request +0x03)`. The item must
pass virtual `+0x484`. Its current stack quantity from virtual `+0x4E8` is
copied into request `+0x06`; the unit-value helper `0x00523650` is then
multiplied by that quantity. The resulting signed 64-bit amount is stored in
request `+0x90`/`+0x94` (low/high words).

The amount gate preserves these exact boundaries: a non-positive/zero result
is normalized into the observed minimum path, positive values above the
handler's `10,000,000` ceiling are clamped to that ceiling, and a value that
exceeds the player's current 64-bit state at `player +0x78/+0x7C` returns
`0x180F`. Other exact rejection values are `0x1809` for an unresolved item,
`0x1843` for the failed item virtual gate, and the status returned by the
context resolver. These are executable values; no user-facing labels are
promoted here.

On successful validation, `0x00523F70` checks both slot bounds, resolves the
source and destination items, rejects the observed protected-item condition
with `0x18B2`, and performs the in-memory slot removal/placement or merge
operation. The caller then invokes player virtual `+0x16C` with the negated
64-bit amount, action/control value `0x21`, and trailing values `1, 0`. This
is the binary boundary for the player-state mutation; it does not by itself
prove the display meaning of action `0x21`.

The common post-handler creates/submits the storage updater. For this
operation, `0x00459F70` selects `0x0045AF70`, whose operation-`2` branch
formats:

```sql
{?=CALL _STRG_MOVE_ITEM (%d,%d,%d,%d,%d,%d,%d,0)}
```

The formatter visibly loads the item virtual `+0x08` and `+0x380` values,
source/destination slot bytes from the updater record `+0x2B/+0x2C`, and a
derived value from `0x004EA580`; the decompiler does not preserve a stable
name/order for every variadic formatter argument, so those values remain
offset-level evidence rather than guessed stored-procedure column names.

When the updater record's operation byte at `+0x28` is `2`, the same branch
also formats the gold-state procedure after the move query:

```sql
{?=CALL _UPDATE_USER_N_CHAR_GOLD (%d,%I64d, %d,%I64d)}
```

The call site visibly supplies the two item-side values loaded above, the
player's mutable balance pair at `+0x72A/+0x72B`, and the player's current
balance pair at `player +0x78/+0x7C`; exact procedure parameter names and
transaction semantics remain outside the executable evidence. Both queries
are submitted through the asynchronous storage path and admitted through
`0x004E7350(1)`. The synchronous `0x0050F5C0` branch contains no direct packet
writer for operation `0x02`; the later client notification is supplied by the
binary-confirmed asynchronous completion edge documented next, not by SQL
success alone.

The asynchronous completion path is now binary-confirmed for this operation.
`0x00459E20` allocates the base `AQ_StorageUpdater`; its constructor at
`0x004595D0` installs the base vtable and initializes completion family
`+0x20 = 1`. The base vtable selects `0x0045CE90` for query work and
`0x0045D4A0` for the successful post-query callback. The latter reaches the
owner's `+0x5D4` adapter `0x004E9440(updater, 1)`, which calls updater vfunc
`+0x2C = 0x0045D940` and then `0x004E73D0(player, updater)`.

For updater operation `+0x28 = 2`, `0x004E73D0`'s completion-family-`1`
branch enters `0x004E9480`. That emitter sends server-to-client `0xB034`,
calls `0x004F76E0(operation, 1)`, and therefore writes the operation's
`+0x03` and `+0x04` slot bytes after the leading success byte. Its operation-
`0x02` continuation resolves the returned `+0x04` item, checks virtual `+0xB8`,
and invokes the player virtual at `+0x144` with action `0x3B`. This is a
response-side action gate; it does not rename action `0x3B` or prove a second
database mutation.

The matching client handler `0x0087F370` reads the leading response byte and
parses operation `0x02` through `0x0087EB50`. For the successful response
emitted by `0x004E9480`, the confirmed body after that leading byte is
`u8 +0x03, u8 +0x04`; the parser also contains an alternate mode that reads a
32-bit `+0x0C` field, but that field is not emitted by the operation-`0x02`
success serializer and is not promoted as part of this response grammar. The
generic post-parse path then invokes the client item-operation updater
`0x0087BA20` with the parsed operation and two derived state values. The
client-side semantic meaning of those derived values remains unresolved, but
the server-to-client opcode, operation discriminator, response widths, and
completion edge are no longer hypothetical.

The matching client call site is now established in `0x007950B0`. On the
`param_1 == 0x46` / `param_3 == 0x13` branch it initializes the operation
record with `+0x00 = 0x02`, `+0x03 = (u8)param_2`, `+0x04 =
0x006CCE10(param_4)`, and `+0x0C = *FUN_00778B80()`, then selects `0x7034`,
serializes with `0x007923A0(record, 0)`, and submits through `0x008418D0`.
The serializer's operation-`0x02` branch writes exactly:

```text
u8  record +0x03
u8  record +0x04
u32 record +0x0C
```

The server consumes `+0x03` as the source slot, `+0x04` as the destination
slot passed to `0x00523F70`, and `+0x0C` as the context key passed to
`0x004E72A0`. The helper's higher-level UI parameter meaning remains
unresolved.

Raw evidence: [`00524160_FUN_00524160.c`](../../analysis/decompiled/gameserver-item-7034/00524160_FUN_00524160.c),
[`00523F70_FUN_00523f70.c`](../../analysis/decompiled/gameserver-item-7034/00523F70_FUN_00523f70.c),
[`0050F5C0_FUN_0050f5c0.c`](../../analysis/decompiled/gameserver-item-7034/0050F5C0_FUN_0050f5c0.c),
[`0050FA50_FUN_0050fa50.c`](../../analysis/decompiled/gameserver-quest-async/0050FA50_FUN_0050fa50.c),
[`00459F70_FUN_00459f70.c`](../../analysis/decompiled/gameserver-sql/00459F70_FUN_00459f70.c),
[`0045AF70_FUN_0045af70.c`](../../analysis/decompiled/gameserver-sql/0045AF70_FUN_0045af70.c),
[`004595D0_FUN_004595d0.c`](../../analysis/decompiled/cash-completion-pc/004595D0_FUN_004595d0.c),
[`0045CE90_FUN_0045ce90.c`](../../analysis/decompiled/gameserver-sql/0045CE90_FUN_0045ce90.c),
[`0045D4A0_FUN_0045d4a0.c`](../../analysis/decompiled/cash-completion-pc/0045D4A0_FUN_0045d4a0.c),
[`004E9440_FUN_004e9440.c`](../../analysis/decompiled/cash-completion-pc/004E9440_FUN_004e9440.c),
[`004E73D0_FUN_004e73d0.c`](../../analysis/decompiled/gameserver-quest-async/004E73D0_FUN_004e73d0.c),
[`004F76E0_FUN_004f76e0.c`](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c),
[`0087F370_CNetProcessIn__OnB034.c`](../../analysis/decompiled/client-quest-30d5/0087F370_CNetProcessIn__OnB034.c),
[`0087EB50_FUN_0087eb50.c`](../../analysis/decompiled/shop-client-exact/0087EB50_FUN_0087eb50.c),
[`0087BA20_FUN_0087ba20.c`](../../analysis/decompiled/client-quest-30d5/0087BA20_FUN_0087ba20.c),
[`007950B0_FUN_007950b0.c`](../../analysis/decompiled/client-pickup-refs/007950B0_FUN_007950b0.c),
and [`007923A0_FUN_007923a0.c`](../../analysis/decompiled/client-pickup-refs/007923A0_FUN_007923a0.c).

This pass promotes parser shape, table population, the linked-storage
`trade_item` helper, item construction handlers, and the mall handler. It does
not rename the remaining currency/service branches as sell, buyback, repair,
avatar, COS, or guild without a direct call edge or typed capture; the
participant-transfer path is separately documented as the exchange/trade
family because the client sender, `trade_item` constructor, and exchange SQL
worker are now joined.

Raw bounded listings: [`004D8BF0_FUN_004d8bf0.c`](../../analysis/decompiled/gameserver-item-7034/004D8BF0_FUN_004d8bf0.c),
[`0050CE80_FUN_0050ce80.c`](../../analysis/decompiled/gameserver-item-7034/0050CE80_FUN_0050ce80.c),
[`0050F5C0_FUN_0050f5c0.c`](../../analysis/decompiled/gameserver-item-7034/0050F5C0_FUN_0050f5c0.c),
[`00523860_FUN_00523860.c`](../../analysis/decompiled/gameserver-item-7034/00523860_FUN_00523860.c),
[`00523AA0_FUN_00523aa0.c`](../../analysis/decompiled/gameserver-item-7034/00523AA0_FUN_00523aa0.c),
[`00523960_FUN_00523960.c`](../../analysis/decompiled/gameserver-item-7034/00523960_FUN_00523960.c),
[`00523F70_FUN_00523f70.c`](../../analysis/decompiled/gameserver-item-7034/00523F70_FUN_00523f70.c),
[`0043F3F0_FUN_0043f3f0.c`](../../analysis/decompiled/gameserver-item-7034/0043F3F0_FUN_0043f3f0.c),
[`00526610_FUN_00526610.c`](../../analysis/decompiled/gameserver-item-7034/00526610_FUN_00526610.c),
[`00526CE0_FUN_00526ce0.c`](../../analysis/decompiled/gameserver-item-7034/00526ce0_FUN_00526ce0.c),
and [`00618FE0_FUN_00618fe0.c`](../../analysis/decompiled/gameserver-item-7034/00618FE0_FUN_00618fe0.c).

## Alchemy, stones and material lifecycle

The GameServer dispatcher at `0x0050D0C0` and alchemy registration table prove
the following request/response families:

| Type byte | Constructed operation | Response |
| ---: | --- | --- |
| `3` | `CEnchant_Reinforce` (`0x44` bytes) | `0xB150` |
| `4` | `CEnchant_EnchantMagicStone` (`0x44` bytes) | `0xB151` |
| `5` | `CEnchant_EnchantAttrStone` (`0x44` bytes) | `0xB151` |
| `7` | `CEnchant_EnchantAvatarItem` (`0x60` bytes) | `0x34A8` |
| `8` | `CEnchant_UpperReinforce` (`0x48` bytes) | `0xB150` |
| `9` | Socket insert delayed operation | `0xB16A` |

For `0x7150`/`0x7151`, the recovered request prefix is:

```text
u8 action       // action 2 starts the staged operation
u8 type
u8 slotCount
u8 slots[slotCount]
```

Every slot is checked against `0x0D..0x6F`, resolved through the character
inventory lookup, and rejected if it is absent or invalid. The first slot is
validated by the type-specific operation; later slots enter the shared
material path. Results use a delayed state machine and a generic item-change
accumulator rather than embedding every changed item in the result packet.

For `0x716A`, the confirmed action-specific bodies are:

```text
action 2, subtype 9: u8 targetSlot, u8 stoneSlot, u8 socketIndex
action 3, subtype 8: u8 targetSlot, u8 socketIndex
action 4, subtype 10: u8 targetSlot, u8 socketIndex
```

The matching client response handlers `CNetProcessIn::OnB150` and
`CNetProcessIn::OnB151` parse status/action branches, inventory slot bytes,
failure codes, and item-record data. The client pass confirms response
handling, but not one universal response struct for all success and failure
variants.

The client-side `0x7150` constructor at `0x00825C50` writes this partial shape:

```text
02 | vector_length | (vector_item + 0x0D)*
```

The vector's exact semantic meaning and the enclosing fields for the other
constructor sites remain open. The client RTTI/memory evidence identifies
`CAlchemyMgrReinforce` as the UI-side route.

Material/compound and dismantle are separate staged families. The complete
details, including result-size evidence and validation codes, are in the
[alchemy report](sr-gameserver-alchemy.md).

## Scrolls and ordinary item use

### Dedicated potion/skill-restoration requests: `0x7202` and `0x7203`

These are separate live request-table entries, not aliases for `0x704C`.
The GameServer dispatcher stores `0x005169D0` at table slot
`0x00CD4D18` (`0x7202`) and `0x00516BB0` at `0x00CD4D20` (`0x7203`). The
matching client senders at `0x00820E30` and `0x00820F10` both select their
opcode and write the exact request body:

```text
u32 itemRefID
u32 targetSkillOrMasteryID
u8  targetLevel
```

The server handlers resolve an item/reference from the first request value
through `0x004260B0`, then compare its name against the following exact
strings:

```text
ITEM_QSP_ALL_POTION_1_01
ITEM_MALL_SKILL_RESTORATION_POTION
ITEM_QNO_RM_OLDWOMAN_2_02
```

For `0x7202`, the first name selects internal mode `3`; either of the latter
two selects mode `6`. The handler calls `0x0059F8B0` with the resolved name,
the two decoded 32-bit request values, the decoded byte, and the selected
mode. For `0x7203`, the same name allow-list is used and the operation calls
`0x0059FD60` with its corresponding request values. These are exact item-name
gates and call edges; the executable does not provide universal English names
for the three request fields.

Both server handlers treat `0x7800` as the successful/no-error return from
their internal operation and do not emit the error response in that case. A
non-`0x7800` result emits server-to-client `0xB202` or `0xB203` with the
confirmed body `u8 2, u16 resultCode`. The client result handlers are
registered at `0x00880DB0` and `0x00880E90`; the `0x7203` success branch also
invokes the player virtual at `+0x50C` with `0x20`. Their UI interpretation is
now bounded further: `0xB202` success reads `u32`, while `0xB203` success
reads `u32,u8`; both failure forms read `u8 result=2,u16 errorCode`. The
client does not assign stable English names to those success fields, so the
server-side status contract remains semantic-neutral.

The success handlers do expose the local client state mutations. `0xB202`
reads the resulting `u32`, resolves the corresponding skill through
`0x0093F5D0`, obtains related client state through `0x009C0F40`, compares the
returned value, and applies the resulting state through `0x009C1750` before
the interface refresh calls. `0xB203` reads a `u32` mastery key and a `u8`
level, verifies that the mastery is learned with `0x009C0B50`, resolves its
client record through `0x009C1180`, writes the returned level at that record's
`+0x04`, and refreshes the interface. These are client-state roles confirmed
by the handlers; they do not establish database column names or replace the
server's generic `_skill_manage` persistence path.

Raw evidence: [`0050D0C0_FUN_0050d0c0.c`](../../analysis/decompiled/gameserver-mastery-skills/0050D0C0_FUN_0050d0c0.c),
[`005169D0_FUN_005169d0.c`](../../analysis/decompiled/gameserver-mastery-skills/005169D0_FUN_005169d0.c),
[`00516BB0_FUN_00516bb0.c`](../../analysis/decompiled/gameserver-mastery-skills/00516BB0_FUN_00516bb0.c),
[`00820E30_FUN_00820e30.c`](../../analysis/decompiled/client-pickup-refs/00820E30_FUN_00820e30.c),
[`00820F10_FUN_00820f10.c`](../../analysis/decompiled/client-pickup-refs/00820F10_FUN_00820f10.c),
[`00880DB0_FUN_00880db0.c`](../../analysis/decompiled/client-skill-restoration/00880DB0_FUN_00880db0.c),
and [`00880E90_FUN_00880e90.c`](../../analysis/decompiled/client-skill-restoration/00880E90_FUN_00880e90.c).

The internal routines behind these handlers are also bounded. `0x0059F8B0`
first calls `0x0059F410` and then performs the item/state mutation path;
`0x0059FD60` analogously calls `0x0059F610` for the sibling operation. Both
paths can invoke the generic item-count mutation helper `0x0059F7B0`, the
player item virtual at `+0x230`, and async queue admission through
`0x004E7350(1)`. Neither routine formats an item-specific SQL procedure.
Their observed internal status vocabulary is:

| Status | Confirmed branch meaning |
| ---: | --- |
| `0x7800` | operation accepted/completed; the server handler suppresses `B202/B203` |
| `0x7801` | missing/invalid item or target object |
| `0x7802` | target level/value is not below the current state bound |
| `0x7803` | generic invalid, apply, resource, or async-admission failure |
| `0x7804` | insufficient restoration currency/resource or requirement failure |
| `0x7805` | item-count mutation request exceeds the available count |
| `0x7806` | another active learned-skill dependency/conflict blocks the change |

The status labels above describe observed control-flow predicates, not public
UI text. Persistence remains through the generic mutation/change path rather
than a dedicated skill-restoration SQL call.

The focused mastery/skill pass resolves the otherwise-neutral request fields:
the nine-byte body is `u32 itemRefID | u32 targetSkillOrMasteryID | u8
targetLevel`. The first value is an `ITEMDATA` reference ID, not an inventory
slot or item instance ID. `ITEM_QSP_ALL_POTION_1_01` selects mode flags `3`
for `0x7202`; the mall and quest restoration items select mode `6`.

The persistence call is the shared skill job, not a dedicated restoration
procedure:

```sql
exec _skill_manage %d, %d, %d, %d
```

The formatter at `0x004587A0` records character ID, operation, reference ID,
and level at job offsets `+0xC28`, `+0xC2C`, `+0xC30`, and `+0xC34`.
Withdrawal uses operation `2` for skill removal and operation `3` for mastery
removal. The executable confirms this operation selection and generic item
mutation boundary; the remaining SQL argument/result-column meaning is not
promoted beyond the evidence in the focused mastery report.

The focused binary mapping identifies `0x7202` as skill withdrawal and
`0x7203` as mastery withdrawal. Their deferred success bodies are respectively
`1,u32 resultingSkillID` and `1,u32 masteryID,u8 level`; the direct failure
forms are `2,u16 errorCode`. These response names are qualified by the
matching client handlers and the dedicated [mastery/skill report](sr-gameserver-mastery-skills.md).

Cross-reference: [mastery and skill restoration report](sr-gameserver-mastery-skills.md).

Raw internal-path evidence: [`0059F8B0_FUN_0059f8b0.c`](../../analysis/decompiled/gameserver-mastery-skills/0059F8B0_FUN_0059f8b0.c),
[`0059FD60_FUN_0059fd60.c`](../../analysis/decompiled/gameserver-mastery-skills/0059FD60_FUN_0059fd60.c),
[`0059F410_FUN_0059f410.c`](../../analysis/decompiled/gameserver-mastery-skills/0059F410_FUN_0059f410.c),
[`0059F610_FUN_0059f610.c`](../../analysis/decompiled/gameserver-mastery-skills/0059F610_FUN_0059f610.c),
and [`0059F7B0_FUN_0059f7b0.c`](../../analysis/decompiled/gameserver-mastery-skills/0059F7B0_FUN_0059f7b0.c).

### Client `0x704C` TypeID classifier and request shapes

The client-side use path at `0x00796660` performs a real TypeID classifier;
this is distinct from the server's mastery gate. The packed 16-bit TypeID is
read from the client item object and the binary uses these exact bit fields:

| Bits | Width | Confirmed use |
| ---: | ---: | --- |
| `1` | 1 | must be clear for the normal ETC-use admission test |
| `2..4` | 3 | compared with `3` (`0x1C == 0x0C`) |
| `5..6` | 2 | compared with `3` (`0x60 == 0x60`) |
| `7..10` | 4 | compared with `1`, `2`, or `3` by the local branches |
| `11..15` | 5 | TypeID4 switch selector (`0x1..0x10` and other values) |

For the ordinary ETC-use admission branch, TypeID1 and TypeID2 therefore
match `3`, while TypeID3 `1` or `2` takes the normal-use family. TypeID3 `3`
is separately accepted by the global-message-scroll branch when TypeID4 is
`5`. The client then dispatches local checks by TypeID4. Confirmed branches
include:

| TypeID4 | Binary-confirmed client behavior |
| ---: | --- |
| `6` | local `0xB04C` rejection when the client feature flag at `+0x65C` bit `0` is set |
| `7` | inventory/equipment capacity checks; failure builds local `0xB04C` |
| `8` | target/action checks for caller values `0x46` and `0x47` |
| `9` | target validation, then the ordinary `0x704C` request branch |
| `0x0B`, `0x0C`, `0x0F`, `0x10` | local item/action or UI checks; the binary does not assign universal scroll names |
| `1..5`, `7`, `9` in the alternate branch | local state/target checks before the shared `0x704C` writer |

The common request prefix in the recovered client writer is:

```text
u8  inventorySlot + 0x0D
u16 packed item TypeID       // 0x0053D460 / 0x00508FE0 width-2 write
```

The TypeID4=`9` target form then appends `u32 param_3` and `u8 param_4`.
Other accepted branches append caller- and state-dependent values: an
optional `u32`, an optional serialized `0x100`-byte local object/state block,
or position-like fields written as `u32,u16,u32,u32,u32`. Those branches are
confirmed variable forms, not one universal `{slot, itemID, target}` struct.

The global-message-scroll sender at `0x007901C0` is narrower and exact: it
requires packed TypeID1..4 `(3,3,3,5)`, selects `0x704C`, and writes
`u8 slot+0x0D`, `u16 TypeID`, then a length-prefixed encoded message. This
branch is also corroborated by the DevKit symbol map/source for
`CGInterface::WriteGlobalMessage`; the wire widths come from the executable
writer calls.

Raw client evidence: [`00796660_FUN_00796660.c`](../../analysis/decompiled/cos-rental/00796660_FUN_00796660.c),
[`007901C0_FUN_007901c0.c`](../../analysis/decompiled/item-use-client-exact/007901C0_FUN_007901c0.c),
and the width helper listing [`mall-serializer-width-helpers.txt`](../../analysis/decompiled/item-mall-client/mall-serializer-width-helpers.txt).

### Reference-data cross-check: item names to packed TypeID

The executable classifies the packed TypeID, while the item name and numeric
reference ID come from the external `itemdata_*.txt` catalog. The following
cross-check is therefore data evidence, not a claim that the names are
embedded in `0x00510980` or `0x00796660`. For the vSRO catalog used with this
pass, the non-cash packed value is
`(TypeID1<<2)|(TypeID2<<5)|(TypeID3<<7)|(TypeID4<<11)`:

| CodeName | Reference ID | TypeID1..4 | Packed `u16` | Data-confirmed family/use label |
| --- | ---: | --- | ---: | --- |
| `ITEM_ETC_HP_SPOTION_01` | `9` | `3,3,1,1` | `0x08EC` | HP potion; client TypeID3=`1`, TypeID4=`1` branch |
| `ITEM_ETC_MP_SPOTION_01` | `16` | `3,3,1,2` | `0x10EC` | MP potion; TypeID4=`2` branch |
| `ITEM_ETC_ALL_SPOTION_01` | `23` | `3,3,1,3` | `0x18EC` | all/vigor potion; TypeID4=`3` branch |
| `ITEM_COS_P_REVIVAL` | `7552` | `3,3,1,6` | `0x30EC` | COS revival family; TypeID4=`6` target branch |
| `ITEM_COS_P_REVIVAL_SILK` | `9286` | `3,3,1,6` | `0x30EC` | same TypeID family, separate catalog item |
| `ITEM_ETC_SCROLL_RETURN_01` | `61` | `3,3,3,1` | `0x09EC` | ordinary return scroll |
| `ITEM_ETC_SCROLL_RETURN_02` | `2198` | `3,3,3,1` | `0x09EC` | special return scroll; same TypeID as ordinary return |
| `ITEM_EVENT_RETURN_SCROLL_HIGH_SPEED` | `24448` | `3,3,3,1` | `0x09EC` | event/high-speed return; same TypeID family |
| `ITEM_EVENT_REVERSE_RETURN_SCROLL` | `24449` | `3,3,3,3` | `0x19EC` | reverse-return event family |
| `ITEM_EVENT_COS_C_SCARABAEUS_SCROLL` | `38817` | `3,3,3,2` | `0x11EC` | COS-C summon/event family |
| `ITEM_EVENT_COS_C_WILDPIG_SCROLL` | `38818` | `3,3,3,2` | `0x11EC` | COS-C summon/event family |
| `ITEM_EVENT_COS_C_OSTRICH_SCROLL` | `38819` | `3,3,3,2` | `0x11EC` | COS-C summon/event family |
| `ITEM_EVENT_COS_C_PEGASUS_SCROLL` | `38820` | `3,3,3,2` | `0x11EC` | COS-C summon/event family |
| `ITEM_EVENT_COS_C_RUDOLPH` | `38821` | `3,3,3,2` | `0x11EC` | COS-C summon/event family |
| `ITEM_EVENT_REPAIR_HAMMER` | `24458` | `3,3,13,7` | `0x3EEC` | repair/service family; not a potion TypeID |

This table explains why a complete item-name-to-action switch cannot be
recovered from the `0x704C` TypeID predicates alone: multiple named items
share `0x09EC`, and the executable reaches item/service hooks after the
authoritative TypeID check. The reference row, item virtual, and any event
handler must be correlated before assigning the effect or SQL call. Source
catalogs: [`itemdata_5000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_5000.txt),
[`itemdata_10000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_10000.txt),
[`itemdata_25000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_25000.txt),
and [`itemdata_40000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_40000.txt).

The two return-scroll rows also carry different catalog parameters (`30000`
versus `15000`) while retaining the same packed TypeID. Those values are
reference-row inputs; their unit and exact server effect are not established
by the item-use handler. The client predicate helpers used by the potion and
service branches are [`00666DE0_FUN_00666de0.c`](../../analysis/decompiled/client-item-use-classifiers/00666DE0_FUN_00666de0.c),
[`00666C80_FUN_00666c80.c`](../../analysis/decompiled/client-item-use-classifiers/00666C80_FUN_00666c80.c),
and the linked-list/cooldown lookup [`009B9520_FUN_009b9520.c`](../../analysis/decompiled/client-item-use-classifiers/009B9520_FUN_009b9520.c).

### Server `0x704C` request prefix and TypeID integrity

The server-side `0x00510980` parser resolves the item-use request with the
same widths. Depending on the packet-reader mode at `param_2 + 0x1048`, it
reads:

```text
u8  inventorySlot       // 0x00521750 or reverse/encoded 0x005217A0
u16 requestedTypeID     // 0x0048BA70 or reverse/encoded 0x00409910
```

The slot is accepted only in `0x0D..0x6F`. The server then obtains the
authoritative item TypeID through `0x00485C90` from the runtime object’s data
(`item + 0x34 -> data + 0x18 -> +0x80`) and compares it with the requested
`u16`; a mismatch enters the item-use error path rather than trusting the
client-supplied type. This pins the client/server relationship for the common
`0x704C` prefix and makes the TypeID a validation field, not merely a UI hint.

### Server-side packed-TypeID action predicates

The server also applies exact packed-TypeID predicates inside the
`0x00510980` post-validation path. These are binary classifiers, not display
names:

| Helper | Exact packed-TypeID condition | Use in `0x00510980` |
| --- | --- | --- |
| `0x0047ED60` | bit `1` clear, TypeID1 `3`, bit `0` set | first branch of the later status/action family check |
| `0x0051DC70` | bit `1` clear, TypeID1 `3`, TypeID2 `3`, TypeID3 `12`, TypeID4 `1..3` | alternate status/action family check |
| `0x0051DC30` | bit `1` clear, TypeID1 `3`, TypeID2 `3`, TypeID3 `1`, TypeID4 `8` | alternate status/action family check |
| `0x004890B0` | bit `1` clear, TypeID1 `3`, TypeID2 `3`, TypeID3 `1`, TypeID4 `1` | suppresses the generic event/log fallback |
| `0x00489070` | bit `1` clear, TypeID1 `3`, TypeID2 `3`, TypeID3 `1`, TypeID4 `2` | suppresses the generic event/log fallback |
| `0x004929C0` | bit `1` clear, TypeID1 `3`, TypeID2 `3`, TypeID3 `13` | controls an earlier special-use state gate; its TypeID4 is checked separately against `6` |

When the status reaches the later success branch, the first three predicates
are tested in sequence; if none matches, execution jumps to the shared
completion label. If one matches, the server evaluates the
`0x004890B0`/`0x00489070` pair. If neither of those matches, it invokes
the global event manager returned by `0x00401D70`, then constructs the
asynchronous item audit/log record through `0x00468A80` with action value
`0x10`.
The pair therefore controls whether that fallback is entered for TypeID3 `1`,
TypeID4 `1` or `2`. A separate `0x004929C0` match requires the authoritative
TypeID4 mask `0xF800 == 0x3000` (TypeID4 `6`) before its earlier state path.
The executable does not expose a universal semantic name for these families.

The same earlier state path can call `0x006480E0` when the player-state flag
branch is active; that function emits opcode `0x34B1` with the observed
two-byte body `u8 0xFF, u8 0x0C`. This is a flag-event side effect, not proof of a specific
scroll effect or a replacement for the common item-consumption result.

Two additional server hooks sit between the TypeID checks and the generic
mutation boundary. `0x005F88F0` resolves an item/service object through
`0x005F6B80` and invokes its virtual `+0x7C`; the return value controls the
`0x18F3` rejection in `0x00510980`. When that hook does not consume the path,
the handler either dispatches event `0x16` through `0x00580380` (a linked
handler list whose entries receive the item/player context) or, for the
flag-enabled branch, calls `0x006480E0`. These are real TypeID/item-use
extension points, but neither the executable nor the recovered call edges
assign them universal names such as teleport, return scroll, resurrection, or
COS.

Raw predicate evidence: [`0047ED60_FUN_0047ed60.c`](../../analysis/decompiled/gameserver-item-use-action/0047ED60_FUN_0047ed60.c),
[`0051DC70_FUN_0051dc70.c`](../../analysis/decompiled/gameserver-item-use-action/0051DC70_FUN_0051dc70.c),
[`0051DC30_FUN_0051dc30.c`](../../analysis/decompiled/gameserver-item-use-action/0051DC30_FUN_0051dc30.c),
[`004890B0_FUN_004890b0.c`](../../analysis/decompiled/gameserver-item-use-action/004890B0_FUN_004890b0.c),
[`00489070_FUN_00489070.c`](../../analysis/decompiled/gameserver-item-use-action/00489070_FUN_00489070.c),
[`004929C0_FUN_004929c0.c`](../../analysis/decompiled/gameserver-item-use-action/004929C0_FUN_004929c0.c),
[`00468A80_FUN_00468a80.c`](../../analysis/decompiled/gameserver-item-use-action/00468A80_FUN_00468a80.c),
and [`006480E0_FUN_006480e0.c`](../../analysis/decompiled/gameserver-item-use-action/006480E0_FUN_006480e0.c).

Raw server reader evidence: [`00409910_FUN_00409910.c`](../../analysis/decompiled/gameserver-combat-core/00409910_FUN_00409910.c),
[`0048BA70_FUN_0048ba70.c`](../../analysis/decompiled/gameserver-combat-core/0048BA70_FUN_0048ba70.c),
[`00521750_FUN_00521750.c`](../../analysis/decompiled/gameserver-combat-core/00521750_FUN_00521750.c),
[`005217A0_FUN_005217a0.c`](../../analysis/decompiled/gameserver-combat-core/005217A0_FUN_005217a0.c),
and [`00485C90_FUN_00485c90.c`](../../analysis/decompiled/gameserver-combat-core/00485C90_FUN_00485c90.c).

### PC item-use flow (`0x704C`)

The recovered `PC::UseItem`-like handler at `0x00510980` performs this exact
boundary sequence:

```text
validate encoded request/session state
read the requested inventory slot through the player inventory accessors
require slot in 0x0D..0x6F
resolve the item object and its reference data
invoke item-use dispatch around 0x004919E0
consume/mutate through the player item virtual at +0x230
send 0xB04C result
on success, send 0x305C item-used notification
```

Observed failure constants include `0x1806`, `0x1809`, `0x1811`, `0x1814`,
`0x183E`, `0x185B`, `0x1889`, and `0x18F3`; their display-string meanings are
not all present in the executable export. The handler proves the consumption
boundary, not a complete classifier for every teleport, return, resurrection,
pet, premium, and consumable scroll.

### Function-level pre-use gate and consume call

The call around `0x004919E0` is a mastery/eligibility gate, not a universal
scroll-code classifier. It examines four reference requirement pairs at the
item reference object (`+0xB4 + index*8`). The confirmed requirement families
include ordinary mastery IDs `1..4`, mastery IDs `0x201..0x206`, and the
special requirement `0x301`; the required level is read from the paired byte.
It compares those requirements with the player's mastery/level state and
returns `0x186C` for an unsatisfied ordinary requirement or `0x18F7` for the
special requirement path. An item with no active requirements passes this
gate. Invalid mastery encodings take the executable's diagnostic/assertion
path rather than acquiring a guessed semantic name.

After the remaining item-state checks pass, `0x00510980` invokes the player
item-mutation virtual at `+0x230` with the observed call shape:

```text
vfunc +0x230(0, inventorySlot, 0xFFFFFFFF, &status, 2, 0)
```

Only after that call reports success does the handler build `0xB04C` success
and emit `0x305C`; failure uses the `0xB04C` error path. The recovered
`+0x230` edge is a virtual mutation/change boundary, not a direct SQL call.
The stock pass therefore does not justify assigning one stored procedure to
every scroll family; persistence must be followed through the resulting
generic change accumulator or the relevant runtime subclass.

The server-side control-flow summary is therefore:

```text
0x704C
  -> 0x00510980
  -> slot 0x0D..0x6F and authoritative TypeID equality checks
  -> item/service hook 0x005F88F0 (+0x7C), or event-0x16 / 0x34B1 extension
  -> mastery/eligibility gate 0x004919E0
  -> CGObjPC +0x230 mutation boundary
  -> 0xB04C result, then 0x305C on success
```

Raw control-flow evidence: [`00510980_FUN_00510980.c`](../../analysis/decompiled/item-use-client-exact/00510980_FUN_00510980.c),
[`00580380_FUN_00580380.c`](../../analysis/decompiled/gameserver-item-use-action/00580380_FUN_00580380.c),
[`005F88F0_FUN_005f88f0.c`](../../analysis/decompiled/gameserver-item-use-action/005F88F0_FUN_005f88f0.c),
[`005F6B80_FUN_005f6b80.c`](../../analysis/decompiled/item-use-extra-refs/005F6B80_FUN_005f6b80.c),
and [`006480E0_FUN_006480e0.c`](../../analysis/decompiled/gameserver-item-use-action/006480E0_FUN_006480e0.c).

### Stock-client `0xB04C` result decoder

The matching client dispatcher registers `0xB04C` to
`CNetProcessIn::OnB04C` at `0x0087A7B0` and `0x305C` to
`CNetProcessIn::On305C` at `0x008735D0`. The `0xB04C` decoder reads this
binary-confirmed shape:

```text
u8 result
if result != 1:
    u16 errorCode
else:
    u8  encodedSlot       // client derives inventory index as encodedSlot - 0x0D
    u16 packedTypeID      // reused by the TypeID4/action branches
    u16 actionOrState     // consumed by the final UI/action split; name unresolved
```

The decoder's failure path passes category `1`, the `u16` code, resource key
`0xFFDBC99B`, and the fixed notification arguments to `0x00778190`. The
success path uses the packed TypeID bits to select item-family UI/action
branches, while the final `u16` only controls the observed zero/nonzero split;
the executable does not establish a universal semantic name for it. This is
the client-side response grammar for the ordinary PC item-use result, not the
separate COS `0xB0C5` grammar.

The server handler's construction edge remains:

```text
0x00510980 -> player network vfunc +0x278(0xB04C)
           -> success serializer / failure serializer
           -> +0x27C send
success    -> 0x305C with two u32 values
```

Because the recovered `0x00404090` writer is an overloaded stream boundary
and the server decompiler does not preserve each template width, the client
decoder is the authoritative width evidence for the result body. Do not
replace these roles with a guessed `{status, slot, itemID, quantity}` struct.

Raw client evidence: [`0087A7B0_CNetProcessIn__OnB04C.c`](../../analysis/decompiled/client-quest-30d5/0087A7B0_CNetProcessIn__OnB04C.c),
[`0087A7B0_listing.txt`](../../analysis/decompiled/item-use-client-exact/0087A7B0_listing.txt),
[`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c),
and [`008735D0_CNetProcessIn__On305C.c`](../../analysis/decompiled/client-item-use/008735D0_CNetProcessIn__On305C.c).
The same neutral field contract is indexed in the [client decoder layouts](vsro188-client-decoder-layouts.md#0xb04c-at-0x0087a7b0).

### Binary-confirmed return-scroll dispatch

The return-scroll family is now bounded below the generic item-use handler.
The local reference catalog identifies these rows as follows:

| Reference row | TypeID tuple | Packed TypeID | Server branch |
| --- | --- | ---: | --- |
| `ITEM_ETC_SCROLL_RETURN_01` (ID `61`) | `3,3,3,1` | `0x09EC` | `0x004A0380` |
| `ITEM_ETC_SCROLL_RETURN_02` (ID `2198`) | `3,3,3,1` | `0x09EC` | `0x004A0380` |
| `ITEM_EVENT_RETURN_SCROLL_HIGH_SPEED` (ID `24448`) | `3,3,3,1` | `0x09EC` | `0x004A0380` |
| `ITEM_EVENT_REVERSE_RETURN_SCROLL` (ID `24449`) | `3,3,3,3` | `0x19EC` | `0x004A00C0` |

The names and IDs are reference-catalog evidence. The executable branch is
established by the packed TypeID and the item virtual chain:

```text
0x704C
  -> 0x00510980
  -> CGItemExpendable vtable 0x00AEB2B4 + 0x4C8 = 0x0049ACA0
  -> TypeID3 == 3: item vfunc + 0x4CC = 0x0049B9F0
  -> TypeID4 == 1: 0x004A0380
  -> TypeID4 == 3: 0x004A00C0
```

`0x0049ACA0` first obtains the current expendable quantity and initializes
the consumed-count value to one. Its TypeID3 switch routes packed TypeID
`0x09EC`/`0x19EC` to the `+0x4CC` virtual. `0x0049B9F0` then switches on
`packedTypeID >> 11`, which is TypeID4 for these rows. TypeID4 `2` is a
separate COS-C scroll branch; it must not be merged with return scrolls.

The ordinary/high-speed branch at `0x004A0380` performs the binary-observed
player-state gates (`+0x244(0x20000)`, `+0x120`, context `+0x0C`, and the
`0x0062EB50`/`0x005F6C90` checks), then:

```text
FUN_00466F90(0x0B, mode, 0)
player vfunc +0x264()
player vfunc +0x4B0(item-derived float)
FUN_004E0B50(mode, 3, item-derived float, action-parameter)
```

`0x004E0B50` creates or updates internal action type `0x0B`; the queued
record stores subtype `3`, a positive float delay, an action parameter and an
opaque pointer. The item-derived float is read through the item `+0x14`
accessor, but its reference-data field name is unresolved. This is the safe
binary boundary for the ordinary and high-speed rows: their packed TypeID
and callback are identical, so a faster timing claim must be tied to the
reference-data value, not to a TypeID-only predicate.

The reverse branch at `0x004A00C0` performs its own state/location checks,
obtains a location through the `0x004E0130` or `0x004E01C0` resolver, updates
the same player timing/state virtual, then schedules:

```text
FUN_004E0B50(selection-dependent mode, 3, item-derived float, action-parameter)
FUN_00466F90(0x0B, 4, action-parameter)
```

The queued completion is now binary-confirmed beyond this scheduling call.
`0x004E0B50` creates an `AJStateChanger` record with type `0x0B`, subtype `3`,
a positive delay, action parameter and opaque pointer. `CGObjChar` updates the
queue from `+0x1D8` through `0x004A6990 -> 0x004065D0`; its `AJStateChanger`
update virtual `0x00405F70` subtracts the elapsed time and, once ready,
resolves the action parameter. Resolver parameters `2` and `3` select
`0x004E0130` and `0x004E01C0`, respectively. A valid result is passed to the
owner's virtual `+0x378` with mode `2`, followed by
`0x004E0B50(0, 0xFF, 0, 0)` to reset the action. The update returns zero and
`0x00405F30` removes/cleans the completed record. Thus the binary-confirmed
item lifecycle is `0x704C -> item callback -> delayed AJStateChanger -> player
transition virtual -> action cleanup -> item consumption`; it still does not
prove a destination SQL procedure.

The matching client UI strings mention last-return and last-death choices,
but the stock client/server pass has not recovered a distinct `0x704C`
option grammar or a binary mapping from those UI choices to the resolver's
opaque values. Do not promote AegisCore's option `2/3/7` contract to a stock
wire layout.

Both branches return status `1` to `0x0049ACA0`. The generic caller then
consumes one item through the already recovered `CGObjPC +0x230` path and
publishes `0xB04C`, followed by `0x305C`, on success. Thus the stock binary
does prove the item-consumption SQL edge for these scrolls through
`_STRG_DEL_ITEM`; it does not expose a travel-specific SQL literal or prove
that the destination/state action itself is persisted by a separate travel
procedure.

Raw evidence: [`0049ACA0_FUN_0049aca0.c`](../../analysis/decompiled/item-use-extra-refs/0049ACA0_FUN_0049aca0.c),
[`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/item-use-extra-refs/0049B9F0_FUN_0049b9f0.c),
[`004A0380_FUN_004a0380.c`](../../analysis/decompiled/gameserver-quest-async/004A0380_FUN_004a0380.c),
[`004A00C0_FUN_004a00c0.c`](../../analysis/decompiled/item-use-extra-refs/004A00C0_FUN_004a00c0.c),
[`004E0B50_FUN_004e0b50.c`](../../analysis/decompiled/gameserver-cos-state/004E0B50_FUN_004e0b50.c),
and [`00AEB2B4_CGItemExpendable_slots.txt`](../../analysis/decompiled/item-vtable/00AEB2B4_CGItemExpendable_slots.txt).
The queue-completion evidence is in [`00405F70_FUN_00405f70.c`](../../analysis-tmp/action-record/00405F70_FUN_00405f70.c),
[`00405F30_FUN_00405f30.c`](../../analysis-tmp/action-record/00405F30_FUN_00405f30.c),
and [`004065D0_FUN_004065d0.c`](../../analysis-tmp/action-queue-callers/004065D0_FUN_004065d0.c).

### Expendable TypeID fan-out beyond the generic `0x704C` handler

The generic expendable callback at `0x0049ACA0` is itself a second classifier.
It reads TypeID3 from bits `7..10` and selects item virtual slots before the
common `+0x230` consumption boundary. The confirmed dispatch matrix is:

| TypeID3 | TypeID4 condition | Confirmed callback/effect boundary |
| ---: | --- | --- |
| `1` | `4,5,6,7,9` | item virtual `+0x51C` |
| `1` | `10` | item virtual `+0x528` |
| `1` | other | item virtual `+0x4D0` |
| `2` | `7` | item virtual `+0x51C` |
| `2` | other | item virtual `+0x4D0` |
| `3` | any | item virtual `+0x4CC` = `0x0049B9F0` |
| `6` | `2` | item virtual `+0x524` |
| `6` | other | success only when the item-use mode flag is enabled; otherwise `0x186B` |
| `9`, `0x0F` | any | direct success result; no item-specific callback in this function |
| `0x0C` | `1,2,3` | `0x0049AFE0` |
| `0x0C` | `5` | `0x004A0780` |
| `0x0C` | `7` | `0x004A08E0` |
| `0x0D` | `12` | item virtual `+0x520` |
| `0x0D` | other | item virtual `+0x4D4` |

#### TypeID3 `0x0C`, TypeID4 `5`: recovered callback and consumption edge

The `TypeID3=0x0C`, `TypeID4=5` row is dispatched by `0x0049ACA0` to
`0x004A0780`. This is a packed-TypeID branch only; the recovered code does
not establish a public item name or constrain TypeID1/TypeID2 beyond the
caller's `TypeID3`/`TypeID4` tests.

The callback's binary-confirmed result flow is:

```text
0x0049ACA0
  -> TypeID3 == 0x0C, TypeID4 == 5
  -> 0x004A0780
  -> status 0x183E, 0x1818, 2, or 1
  -> common 0x0049ACA0 success test
  -> 0x00510980 quantity/deletion path
  -> 0xB04C, then 0x305C on success
```

Inside `0x004A0780`, a null callback object or a failed virtual `+0x01C`
validity check writes status `0x183E`. The callback then obtains an
owner/context value through the surrounding object's virtual `+0x0E8` and
passes it to the item virtual `+0x348`; a return value of `1` writes
`0x1818`. The separate `0x004F2A80` gate must be nonzero for the success
branch. If it is zero, the callback writes status `2`.

When the gate succeeds, the callback writes status `1`, runs the observed
`0x00402440`/`0x00404CA0` helper sequence, and calls `0x004A1D30` to obtain a
record. If that lookup is null, the function still returns with status `1`.
When a record is present, it calls `0x0041CE40` and emits through
`0x00466F90` with operation `0xF3` and the record's two `u32` fields at
`+0x94` and `+0x58`, respectively. `0xF3` is retained here as the observed
argument to the shared server emitter; this evidence does not independently
prove a client-facing opcode or names for those two fields.

The common caller initializes the consumption count to one and treats only
status `1` as success. Therefore this branch reaches the same one-item
decrement and, when the stack reaches zero, `_STRG_DEL_ITEM` persistence edge
already documented for `0x00510980`. No callback-specific SQL formatter or
stored procedure literal occurs in `0x004A0780`; the state/action side effect
and inventory deletion must remain separate claims.

The decompiler renders `0x004A0780` as `__thiscall(this, resultState*)` while
the call site shows only the explicit result-state argument. The item/context
object is therefore the implicit `ECX` receiver; the apparent one-argument
call is a calling-convention artifact, not evidence that the result state is
the callback object.

Raw evidence: [`0049ACA0_FUN_0049aca0.c`](../../analysis/decompiled/item-use-extra-refs/0049ACA0_FUN_0049aca0.c),
[`004A0780_FUN_004a0780.c`](../../analysis/decompiled/gameserver-quest-async/004A0780_FUN_004a0780.c),
and [`00510980_FUN_00510980.c`](../../analysis/decompiled/gameserver-quest-async/00510980_FUN_00510980.c).

The `TypeID3 == 3` callback at `0x0049B9F0` provides the scroll/event
sub-matrix:

| TypeID4 | Recovered callback/effect boundary |
| ---: | --- |
| `1` | `0x004A0380`, ordinary/high-speed return-scroll timing/action path |
| `2` | COS-C/creature summon validation and spawn path; failure includes the literal `Invalid Summon Scroll Item!! RefObjCodeName[%s]` |
| `3` | `0x004A00C0`, reverse-return location-selection/timing path |
| `4` | `0x004A3F10`, updates an internal state/context field and reaches generic `0x004E7350(1)`; public semantic name unresolved |
| `5` | `0x004F0A00`, then emits an internal `0x7025` state message and performs player-side state calls; public item name unresolved |
| `6`, `7` | `0x0049DC80`, target/distance validation and SiegeGuarder summon path |
| `8` | `0x0049E3F0`, target/distance validation and object transition; public item name unresolved |
| `9` | `0x0049E0B0`, target/distance validation and object transition; public item name unresolved |
| `10` | `0x004A0650`, internal action/state scheduling callback; success status depends on `0x0049A390`/`0x00655BE0` |
| `0x0B` | `0x0049E620`, SiegeSummonMonster lookup/spawn path |

These are callback boundaries, not English item classifications. The same
packed TypeID can be shared by multiple catalog rows, and several callbacks
identify their object family only through diagnostics or reference lookups.
The generic caller still decrements/consumes one item after a successful
callback and then emits `0xB04C`/`0x305C`; the callback-specific code does not
by itself establish a distinct SQL procedure. Raw evidence:
[`0049ACA0_FUN_0049aca0.c`](../../analysis/decompiled/item-use-extra-refs/0049ACA0_FUN_0049aca0.c),
[`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/item-use-extra-refs/0049B9F0_FUN_0049b9f0.c),
[`0049AFE0_FUN_0049afe0.c`](../../analysis/decompiled/item-use-extra-refs/0049AFE0_FUN_0049afe0.c),
[`004A0780_FUN_004a0780.c`](../../analysis/decompiled/gameserver-quest-async/004A0780_FUN_004a0780.c),
[`0049DC80_FUN_0049dc80.c`](../../analysis/decompiled/gameserver-quest-async/0049DC80_FUN_0049dc80.c),
[`0049E3F0_FUN_0049e3f0.c`](../../analysis/decompiled/gameserver-quest-async/0049e3f0_FUN_0049e3f0.c),
[`0049E0B0_FUN_0049e0b0.c`](../../analysis/decompiled/gameserver-quest-async/0049e0b0_FUN_0049e0b0.c),
[`0049E620_FUN_0049e620.c`](../../analysis/decompiled/gameserver-quest-async/0049E620_FUN_0049e620.c),
and [`004A3F10_FUN_004a3f10.c`](../../analysis/decompiled/gameserver-quest-async/004A3F10_FUN_004a3f10.c).

#### TypeID3=`3` catalog cross-check

The loaded reference catalog gives concrete examples for the callback
sub-matrix above. These rows share TypeID1..3=`3,3,3`; the fourth component
selects the callback. The names and IDs below are catalog evidence, while the
callback column is executable evidence:

| Representative catalog rows | Ref ID(s) | TypeID4 | Callback boundary |
| --- | ---: | ---: | --- |
| `ITEM_ETC_SCROLL_RETURN_01`, `ITEM_ETC_SCROLL_RETURN_02`, `ITEM_EVENT_RETURN_SCROLL_HIGH_SPEED` | `61`, `2198`, `24448` | `1` | `0x004A0380` return/timing path |
| `ITEM_COS_C_SCARABAEUS_SCROLL`, `ITEM_COS_C_WILDPIG_SCROLL`, `ITEM_COS_C_OSTRICH_SCROLL`, `ITEM_COS_C_PEGASUS_SCROLL`, `ITEM_EVENT_COS_C_RUDOLPH` | `23339`, `23338`, `22932`, `22933`, `38821` | `2` | `0x0049B9F0` COS-C summon branch |
| `ITEM_MALL_REVERSE_RETURN_SCROLL`, `ITEM_EVENT_REVERSE_RETURN_SCROLL` | `3795`, `24449` | `3` | `0x004A00C0` reverse-return path |
| `ITEM_MALL_BOOTH_MOB_MANGYANG`, `ITEM_MALL_BOOTH_MOB_BIGEYEGHOST` | `3847`, `3848` | `4` | `0x004A3F10` state/context path |
| `ITEM_MALL_GLOBAL_CHATTING`, `ITEM_EVENT_GLOBAL_CHATTING` | `3851`, `35592` | `5` | `0x004F0A00` state-message path |
| `ITEM_FORT_TABLET_HANGA_GOLD`, `ITEM_FORT_TABLET_TAESE_GOLD` | `10588`, `19666` | `6` | `0x0049DC80` target/distance and SiegeGuarder path |
| `ITEM_COS_GUARD_CH_SPEAR_01`, `ITEM_COS_GUARD_EU_BOW_01` | `10589`, `19678` | `7` | `0x0049DC80` target/distance and SiegeGuarder path |
| `ITEM_FORT_MANUAL_BATTLECAMP`, `ITEM_FORT_MANUAL_BARRICADE` | `10590`, `10591` | `8` | `0x0049E3F0` object-transition path |
| `ITEM_FORT_AGGRESSIVE_FLAG`, `ITEM_FORT_HEALING_FLAG` | `10592`, `19682` | `9` | `0x0049E0B0` object-transition path |
| `ITEM_ETC_INTERNAL_100EXP_SCROLL`, `ITEM_ETC_INTERNAL_150SP_SCROLL`, `ITEM_EVENT_20EXP` | `24169`, `24172`, `35739` | `10` | `0x004A0650` internal action/state scheduling path |
| `ITEM_FORT_SCROLL_TAESE`, `ITEM_FORT_SCROLL_MUJIGI` | `24995`, `24998` | `11` | `0x0049E620` SiegeSummonMonster path |
| `ITEM_EVENT_RENT_SKILLPOINT_SCROLL` | `36771` | `12` | no case in `0x0049B9F0`; shared result remains `0x183E` |
| `ITEM_EVENT_OPTLEVEL_UPGRADE_SCROLL_1`, `_2`, `_3` | `39364`, `39365`, `39366` | `14` | no case in `0x0049B9F0`; shared result remains `0x183E` |

This cross-check does not prove that every row is reachable under every
server state gate, nor that a shared TypeID4 has one display effect for all
rows. TypeID4=`12` (`ITEM_EVENT_RENT_SKILLPOINT_SCROLL`) and TypeID4=`14`
(the event opt-level upgrade rows) are present in the catalog but are outside
the recovered `0x0049B9F0` cases `1..11`; they must not be forced into this
sub-matrix without another call edge. Source evidence:
[`itemdata_5000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_5000.txt),
[`itemdata_15000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_15000.txt),
[`itemdata_20000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_20000.txt),
[`itemdata_25000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_25000.txt),
[`itemdata_35000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_35000.txt),
and [`itemdata_40000.txt`](../Media/Media/server_dep/silkroad/textdata/itemdata_40000.txt).

The catalog presence of TypeID4=`12` and `14` does not establish a usable
item callback in this executable. `0x0049ACA0` initializes the shared result
to `0x183E`, dispatches TypeID3=`3` to `0x0049B9F0`, and that callback's switch
contains no cases for TypeID4=`12` or `14`; it returns without replacing the
initialized result. The common caller therefore rejects those rows before
the success-only consumption path, so this binary pass proves no
TypeID4=`12/14` callback-specific SQL or client success response. This is a
build-specific negative result, not a claim about other vSRO revisions.

#### TypeID4 `2`: COS-C summon branch

TypeID4=`2` is handled directly inside `0x0049B9F0`; it is not the same
callback as the TypeID4=`6/7` `0x0049DC80` path. The recovered branch first
requires the item-use mode flag at the surrounding object (`+0x11`) and then
applies these observed gates:

| Gate | Result on failure |
| --- | ---: |
| player virtual `+0x244(0x10000)` | `0x185F` |
| player virtual `+0x120()` returns `1` | `0x1869` |
| player virtual `+0xFC()` returns `4`, or the related object has type byte `4` | `5` |
| surrounding state byte at `+0x13` is nonzero | the observed `0x18AD`/`0x189D`-family expression |
| reference/object resolution or summon context fails | `5` |

The success path resolves the item/reference and related runtime object through
`0x00426C40`, `0x00426760`, player virtual `+0x14`, and `0x005F6C90`. It then
passes through `0x0044B4B0`/`0x004EB2C0`. One accepted subpath resolves a
manager through `0x00440040`, initializes the summon operation with
`0x00440120`, and admits it through generic `0x004E7350(1)`; another builds an
action record and schedules it through `0x005F6EB0`. The branch writes status
`1` only after that work is accepted. A resolved incompatible TypeID4 `1`
returns `0x1876`; TypeID4 `3/4` takes the observed invalid-summon diagnostic
and returns `0x1811`, while a different summon-state branch returns `0x1878`.

No callback-specific SQL formatter or stored-procedure literal occurs in this
branch. `0x004E7350(1)` is generic asynchronous admission, not proof of a
database write. The common expendable caller still owns the one-item
consumption and `_STRG_DEL_ITEM` edge after status `1`.

Raw evidence: [`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/gameserver-quest-async/0049B9F0_FUN_0049b9f0.c),
[`005F6C90_FUN_005f6c90.c`](../../analysis/decompiled/item-use-extra-refs/005F6C90_FUN_005f6c90.c),
and [`005F6EB0_FUN_005f6eb0.c`](../../analysis/decompiled/item-use-extra-refs/005F6EB0_FUN_005f6eb0.c).

#### Remaining TypeID4 callback bodies: confirmed gates and side effects

The four remaining callbacks can be bounded more tightly than the dispatch
table alone. In all four cases, the callback writes a result status and
performs object/player work; none contains an SQL literal, an
`AQ_StorageUpdater` allocation, or a direct database submission. A returned
status of `1` is consumed by the common `0x0049ACA0` caller, which owns the
one-item decrement and the shared `_STRG_DEL_ITEM` edge described below.

* **TypeID4 `6` and `7` -> `0x0049DC80`.** The callback starts with status `1`,
  resolves the player target from the surrounding context, and applies a
  TypeID4-dependent world-state gate: TypeID4 `6` requires the observed
  `0x0062EB50()` condition to be nonzero, while TypeID4 `7` requires it to be
  zero. The corresponding failures are `0x18E2` and `0x18EE`. It then applies
  the generic `0x0049ED20` player/state gate, calculates target distance from
  the context coordinates at `+0x88/+0x8C/+0x90`, and compares it with the
  target's `u16` range field at `+0x224`; the range failure is `0x18D8`.
  The successful branch resolves the SiegeGuarder-related reference through
  `0x00426C40`, `0x00426760`, `0x004A2B00`, and `0x004A2CE0`, then schedules the
  object transition through `0x004EA580` and `0x00621ED0`. Diagnostics include
  an unknown SiegeGuarder codename (`0x18E7`/`0x1811` paths), but do not prove a
  public item name or a separate persistence procedure.

* **TypeID4 `8` -> `0x0049E3F0`.** The callback resolves a target through
  `0x00425CD0`, repeats the same coordinate/range validation (`0x18D8` on
  failure), and obtains a related object through `0x00426760`. Its `u16`
  object field at `+0x80` selects one of two transition helpers:
  `0x00489170` leads to `0x0049EA20`, while the alternate
  `0x004891B0` path leads through `0x0049EB10`; a failed alternate check emits
  `0x18D6`. The helpers validate the target/reference relationship and then
  schedule object work through `0x006226B0` or `0x006228F0`. The callback's
  other failures include missing target/context (`2`) and reference/state
  failures (`0x18E2`, `0x18D7`, `0x18E3`, `0x18E8`, `0x18EF`, `0x18F1`, and
  `0x18F4` in the helper paths). These literals are preserved as observed
  result values; they are not assigned emulator-facing names.

* **TypeID4 `9` -> `0x0049E0B0`.** This is a sibling target/distance path. It
  checks global state, resolves the player target with `0x00425CD0`, applies
  `0x0049ED20`, and rejects a target outside the `+0x224` range with `0x18D8`.
  After `0x00426760` and `0x00456E20` resolve the related object, the success
  branch builds the same observed object/action tuple used by the sibling
  transition code and submits it through `0x004EA580`/`0x00621ED0`. Missing
  player/target and callback-object cases return `2` or `5`; no
  TypeID4-specific SQL edge is present.

* **TypeID4 `0x0B` -> `0x0049E620`.** This callback first requires the
  surrounding `+0x53` object and its virtual `+0x1C` validity check. It then
  resolves the player and applies `0x0049ED20`, requires a summon-capable
  context (`+0x2B8`), resolves the SiegeSummonMonster reference through
  `0x004A25C0`, and obtains the runtime object through `0x004260B0`.
  Free-parameter fields at the reference context offsets `+0x2C0` and
  `+0x2E0` are checked against the observed values `1` and `2`; unsupported
  values log the corresponding `e_SISFPI_Place`/`e_SISFPI_Time` diagnostic.
  The final spawn/action path uses `0x005F6EB0`, followed by `0x0041CE40`
  and `0x00466F90` with the observed operation argument `0xED`. Failure
  statuses include `2`, `5`, `0x18E1`, `0x18EE`, and `0x18F3`. The executable
  confirms a SiegeSummonMonster lookup/spawn lifecycle, not the semantic
  meaning of each free parameter and not a separate SQL call.

The callback-specific side effect therefore has this safe lifecycle:

```text
0x704C / TypeID3=3
  -> 0x0049B9F0 selector
  -> callback-specific target/reference/state work
  -> status 1 only on callback success
  -> common 0x0049ACA0 consumption
  -> 0x004EEB70 / _STRG_DEL_ITEM when the stack reaches zero
  -> 0xB04C and 0x305C success notifications
```

The callback helpers do not replace the shared inventory mutation boundary,
and their object/action calls must not be reported as SQL persistence.

Raw callback evidence: [`0049DC80_FUN_0049dc80.c`](../../analysis/decompiled/gameserver-quest-async/0049DC80_FUN_0049dc80.c),
[`0049E3F0_FUN_0049e3f0.c`](../../analysis/decompiled/gameserver-quest-async/0049E3F0_FUN_0049e3f0.c),
[`0049EA20_FUN_0049ea20.c`](../../analysis/decompiled/gameserver-quest-async/0049EA20_FUN_0049ea20.c),
[`0049EB10_FUN_0049eb10.c`](../../analysis/decompiled/gameserver-quest-async/0049EB10_FUN_0049eb10.c),
[`0049E0B0_FUN_0049e0b0.c`](../../analysis/decompiled/gameserver-quest-async/0049E0B0_FUN_0049e0b0.c),
and [`0049E620_FUN_0049e620.c`](../../analysis/decompiled/gameserver-quest-async/0049E620_FUN_0049e620.c).

#### TypeID3=`3`, TypeID4=`10`: internal EXP/SP-scroll callback

The TypeID4=`10` case is no longer only a switch-table boundary. The callback
at `0x004A0650` starts with result status `2`, reads two reference/free
parameter values at the surrounding item-data offsets `+0x2E0` and `+0x2C0`,
and initializes a small time/state record through `0x009754A0`,
`0x009753F0`, and `0x00975540`. It then queries player virtual `+0x5D8`.
When that gate reports an existing/conflicting state, the callback scans the
associated registry through `0x0049A260`; a matching entry returns status
`0x1894`.

Otherwise it obtains an opaque value from player virtual `+0x4FC`, builds an
11-argument action record through `0x009754C0`, and calls:

```text
0x0049A390(10, player + 0x08, actionRecord, refParam + 0x2C0,
           refParam + 0x2E0, ...)
```

`0x0049A390` rechecks virtual `+0x5D8`, forwards the action arguments to that
virtual, and then enters `0x00655BE0`. The latter constructs/populates an
indirect action object by invoking its virtual setters at `+0x28..+0x5C`,
zeros two internal fields, and inserts it into the owning container. Its
recovered path contains no SQL literal, `AQ_StorageUpdater` call, or direct
database submission. `0x004A0650` changes the result to status `1` only when
`0x0049A390` returns `1`; otherwise the initial status `2` remains.

The shared expendable caller therefore still owns the persistence edge: only
status `1` reaches the common one-item consumption path and its confirmed
`_STRG_DEL_ITEM`/`0xB04C`/`0x305C` completion. The callback-specific action is
confirmed as an in-memory/player action scheduling path, but the executable
does not prove a separate EXP or SP SQL procedure or the semantic names of
the two free-parameter values. The catalog names `ITEM_ETC_INTERNAL_100EXP_SCROLL`,
`ITEM_ETC_INTERNAL_150SP_SCROLL`, and `ITEM_EVENT_20EXP` remain reference-data
labels, not proof that every callback field has been semantically decoded.

Raw evidence: [`004A0650_FUN_004a0650.c`](../../analysis/decompiled/item-use-extra-refs/004A0650_FUN_004a0650.c),
[`0049A390_FUN_0049a390.c`](../../analysis/decompiled/item-use-extra-refs/0049A390_FUN_0049a390.c),
[`00655BE0_FUN_00655be0.c`](../../analysis/decompiled/item-use-extra-refs/00655BE0_FUN_00655be0.c),
[`00656BF0_FUN_00656bf0.c`](../../analysis/decompiled/item-use-extra-refs/00656BF0_FUN_00656bf0.c),
and [`0049A260_FUN_0049a260.c`](../../analysis/decompiled/item-use-extra-refs/0049A260_FUN_0049a260.c).

#### TypeID3=`3`, TypeID4=`4`: state-field mutation and generic persistence

The TypeID4=`4` callback at `0x004A3F10` is a narrower state mutation than
the TypeID4=`10` action scheduler. It compares the incoming value against the
context record's field at `+0x1C`; when different, the binary stores the new
value and sets that record's dirty mask bit `0x4` (subject to the executable's
dirty-write guard). It then prepares a generic state/update record through
`0x009742D0(2, ...)`, obtains the generic manager/container through
`0x00441F90` and `0x00442070`, and admits the resulting work through
`0x004E7350(1)` when the surrounding object checks succeed.

The caller at `0x0049B9F0` converts a successful `0x004A3F10` return into item-
use status `1`; failure becomes status `2`. The common expendable path still
owns one-item consumption and the `_STRG_DEL_ITEM` edge. No item-specific SQL
literal appears in `0x004A3F10`; `0x009742D0` is a generic dirty-record/update
builder and its exact procedure/column contract is not assigned to this item
from the callback alone. The catalog rows for TypeID4=`4` therefore identify
the reference-data family, while the binary-confirmed effect is only the
context-field mutation plus generic persistence admission.

Raw evidence: [`004A3F10_FUN_004a3f10.c`](../../analysis/decompiled/gameserver-quest-async/004A3F10_FUN_004a3f10.c),
[`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/gameserver-quest-async/0049B9F0_FUN_0049b9f0.c),
[`009742D0_FUN_009742d0.c`](../../analysis/decompiled/gameserver-sql/009742D0_FUN_009742d0.c),
[`00441F90_FUN_00441f90.c`](../../analysis/decompiled/gameserver-quest-save/00441F90_FUN_00441f90.c),
and [`00442070_FUN_00442070.c`](../../analysis/decompiled/gameserver-quest-save/00442070_FUN_00442070.c).

#### TypeID3=`3`, TypeID4=`5`: global-message/state side effect

The TypeID4=`5` branch at `0x0049B9F0` first calls `0x004F0A00(0)`. A return
of `1` produces item-use status `2` and copies the surrounding context's
16-bit field at `+0x103E` to `+0x103C`. Otherwise the callback constructs a
temporary message/state record, obtains a player-side stream through the
`0x00C8258C` virtual `+0x48`, stores the observed literal `0x7808` in the
stream state word at `+0x1050`, and passes the observed literal `0x7025` plus
another two-byte value through the generic serializer `0x00404090`. It then
invokes player virtual `+0xEC`, two `0x00426040` helpers, and `0x00430990`,
finally returning item-use status `1`.

This proves a state/message side effect and its success/failure gate, but not
that `0x7025` is a client-facing packet opcode: the recovered callback does
not expose a complete network send boundary there. The common expendable
caller remains responsible for consuming one item and publishing the generic
`0xB04C`/`0x305C` result. No callback-specific SQL formatter or stored
procedure is present in this branch.

Raw evidence: [`0049B9F0_FUN_0049b9f0.c`](../../analysis/decompiled/gameserver-quest-async/0049B9F0_FUN_0049b9f0.c),
[`0049ACA0_FUN_0049aca0.c`](../../analysis/decompiled/item-use-extra-refs/0049ACA0_FUN_0049aca0.c),
and [`00510980_FUN_00510980.c`](../../analysis/decompiled/gameserver-quest-async/00510980_FUN_00510980.c).

### Ordinary consumable deletion: concrete `+0x230` and SQL edge

The concrete virtual edge is now resolved from the RTTI-confirmed `CGObjPC`
vtable at `0x00AF59FC`: vtable offset `+0x230` contains `0x004EEB70`.
The function's own diagnostic string identifies the operation as
`::DelItem_EXT()`. This closes the previously unresolved ordinary
consumable-deletion path, while still leaving the item-specific effect/action
classifier separate.

For the `0x00510980` call, the confirmed path is:

```text
0x00510980
  -> CGObjPC vfunc +0x230 = 0x004EEB70 (DelItem_EXT)
  -> item vfunc +0x4F0(-quantity)       // expendable quantity mutation
  -> 0x004B81E0                         // CGStorage::RemoveItem
  -> CGPCInventoryOP vfunc +0x20 = 0x004B9E40
  -> 0x00459E20                         // allocate AQ_StorageUpdater
  -> 0x00459F70                         // initialize operation
  -> operation byte 0x0F
  -> 0x0045A840                         // delete-item SQL builder
  -> {?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}
  -> 0x004E7350(1)                      // async-query admission
```

`0x004EEB70` resolves the slot through the character storage lookup and reads
the item's current stack quantity through virtual `+0x4E8`. A negative
requested quantity is clamped to that current quantity, then the item
quantity is decremented through `+0x4F0`. The zero-quantity branch records the
item's runtime database identity, removes the item from the in-memory storage
operation, and initializes the asynchronous storage updater. In
`0x00459F70`, operation byte `0x0F` selects `0x0045A840`, whose literal is the
`_STRG_DEL_ITEM` call above. The updater is queued only after its SQL text is
successfully built; a failed admission or updater initialization returns
`0x1811` to the caller.

If a caller supplies a partial quantity instead, the same virtual method can
retain the item and use the `0x3040` inventory-item update builder at
`0x004EFA00`. The server serializer and stock client decoder now pin the
following variable layout:

```text
u8 slot
u8 mask
if mask & 0x01: u32 item-data +0x28
if mask & 0x02: u8  item-data +0x2C
if mask & 0x04: u64 item-data +0x30
if mask & 0x08: u16 item-data +0x38
if mask & 0x10: u32 item-data +0x38
if mask & 0x20: u8  item-data +0x58, then count * u64 entries at +0x60
if mask & 0x40: u8  trailing state byte
if mask & 0x80: u32 trailing value
```

The server calls `0x00404090` with the widths above and sends through the
player network vfunc `+0x27C`; the client decoder at `0x0088BF30` reads the
same `slot`, `mask`, and conditional widths. The `0x00510980` call uses the
full-consumption form, so this partial update branch is a reusable storage
notification boundary rather than proof that every scroll emits `0x3040`.

The same successful `0x00510980` path also constructs `0x305C` after `0xB04C`
success and the mutation call. It writes two four-byte values—`iStack_CC`
from the player context and `uStack_D0` returned by the item virtual—then
broadcasts via `0x00484D90`. The stock client handler `0x008735D0` reads two
`u32` values in that order, resolves the first through its entity lookup, and
compares the second with the local player/entity value before applying the
TypeID-driven UI/status side effect. The executable does not make the two
wire values' display names safer than these roles.

This establishes SQL persistence for ordinary stack-consuming items. It does
not establish the stored-procedure body, transaction/rollback behavior, or a
universal mapping from TypeID4 to a named scroll effect. Those remain
separate from the deletion/quantity lifecycle.

### Runtime item-data subset used by mutation and `0x3040`

The concrete mutation/notification functions also pin this small, reusable
runtime layout subset. It is not a complete C++ declaration:

| Location | Width/use confirmed by executable |
| --- | --- |
| runtime item `+0x34` | pointer to mutable item-data block |
| item-data `+0x08` | dirty/state word consulted by item-data mutation paths |
| item-data `+0x18` | reference-data pointer used by TypeID/reference checks |
| item-data `+0x28`, `+0x2C`, `+0x30` | fields conditionally serialized by `0x004EFA00` mask bits `0x01`, `0x02`, `0x04` |
| item-data `+0x38` | quantity/stack field; written by `0x004EFA00` mask bit `0x10` and adjusted by expendable `+0x4F0` |
| item-data `+0x58` | `u8` magic-option count, bounded by the serializer loop |
| item-data `+0x60 + index*8` | repeated eight-byte magic-option entries, serialized when mask bit `0x20` is set |
| item virtual `+0x4E8` | returns current quantity/stack amount |
| item virtual `+0x4F0` | adds the signed quantity delta |
| item virtual `+0x4FC` | returns the runtime item database identity used before deletion persistence |

The `+0x28..+0x60` fields are wire-visible only under their corresponding
`0x3040` mask conditions; they must not be treated as a fixed packet layout
for every item family. The item-data reference object and its TypeID remain
separate from the mutable instance and from the storage link row.

Raw evidence: [`004EEB70_FUN_004eeb70.c`](../../analysis/decompiled/gameserver-item-use-mutation/004EEB70_FUN_004eeb70.c),
[`004B81E0_FUN_004b81e0.c`](../../analysis/decompiled/gameserver-item-use-mutation/004B81E0_FUN_004b81e0.c),
[`004B9E40_FUN_004b9e40.c`](../../analysis/decompiled/gameserver-item-use-mutation/004B9E40_FUN_004b9e40.c),
[`00459F70_FUN_00459f70.c`](../../analysis/decompiled/gameserver-sql/00459F70_FUN_00459f70.c),
[`0045A840_FUN_0045a840.c`](../../analysis/decompiled/gameserver-sql/0045A840_FUN_0045a840.c),
[`004EFA00_FUN_004efa00.c`](../../analysis/decompiled/gameserver-item-use-mutation/004EFA00_FUN_004efa00.c),
[`0088BF30_CNetProcessIn__On3040.c`](../../analysis/decompiled/client-item-use/0088BF30_CNetProcessIn__On3040.c),
[`008735D0_CNetProcessIn__On305C.c`](../../analysis/decompiled/client-item-use/008735D0_CNetProcessIn__On305C.c),
and the vtable ledger [`vtable-item-use-mutation.txt`](../../analysis/decompiled/gameserver-item-use-mutation/vtable-item-use-mutation.txt).

A separate helper at `0x00497070` supplies additional binary-confirmed
item-use-style preconditions, but its direct static caller set is not the
`0x00510980` path and it still is not a display-code-to-scroll classifier. It
rejects a null/invalid item object, restricts its explicit
`param_3` kind byte to `0x00..0x0C`, checks the runtime/item category and player
state, then reuses `0x004919E0`. On the accepted mastery path it compares two
item-derived values against player thresholds and checks the race/category byte;
the observed rejection values include `0x1806`, `0x183C`, `0x1816`, `0x1837`,
`0x1830`, `0x1831`, `0x182F`, `0x180D`, and `0x180E`. These are exact gate
outputs, while the meaning of the kind byte and the final item-specific action
remain runtime-vtable dependent.

The COS analogue at `0x004D57B0` follows the same error/result split, sends
`0xB04C` on failure, and sends `0xB0C5` plus `0x305C` on the successful COS
use path. It is a separate COS state machine rather than proof that ordinary
player inventory and COS inventory share one item record.

The matching stock client decoder at `0x00872590` confirms the successful
`0xB0C5` body as `u8 result=1, u8 action/command, u32 action payload`. Its
failure grammar is `u8 result=2, u16 errorCode, u8 action/command, u32
target-or-action payload`, followed by one extra `u32` for command `8`.
The result handler treats command `2` as an object/COS lookup and command
`8` as a COS state/update call. This is the recovered COS-specific response
grammar; it must not be reused as the ordinary PC `0xB04C` response layout.

The server-side COS request/mutation order is also bounded:

```text
C -> S 0x704C:
    u8  encodedSlot       // required in 0x0D..0x6F
    u16 packedTypeID

0x004D57B0:
    resolve COS item through owner virtual +0x578(encodedSlot)
    require the item and compare its packed TypeID with the request value
    run the shared mastery/eligibility gate 0x004919E0
    call the COS-item use virtual +0x4C8(..., resultState)
    if resultState[0] == 0:
        consume through COS-owner virtual +0x230
    else if result == 1:
        adjust through COS-item virtual +0x4EC(resultState[0])
    on result == 1:
        S -> C 0xB0C5, then S -> C 0x305C
    otherwise:
        S -> C 0xB04C with the error/result value
```

The COS callback's `resultState[1]` is the four-byte payload carried by the
successful `0xB0C5` response; its stable English meaning is unresolved. The
server export does not preserve every overloaded stream-write width, so the
client decoder remains authoritative for the response grammar. On the stock
client, command `2` with result `2` displays the error and clears the resolved
COS object state; command `8` consumes one additional `u32` and calls the COS
state helper with either success selector `1` or the decoded error value.
Other command values do not establish a universal payload meaning. Raw
server evidence: [`004D57B0_FUN_004d57b0.c`](../../analysis/decompiled/gameserver-cos/004D57B0_FUN_004d57b0.c);
raw client evidence: [`00872590_CNetProcessIn__OnB0C5.c`](../../analysis/decompiled/client-cos-response/00872590_CNetProcessIn__OnB0C5.c).

Raw evidence: [`00510980_FUN_00510980.c`](../../analysis/decompiled/gameserver-combat-core/00510980_FUN_00510980.c),
[`004D57B0_FUN_004d57b0.c`](../../analysis/decompiled/gameserver-cos/004D57B0_FUN_004d57b0.c).
The focused mastery-gate export is [`004919E0_FUN_004919e0.c`](../../analysis/decompiled/item-use-client-exact/004919E0_FUN_004919e0.c).
The additional item-target validator is [`00497070_FUN_00497070.c`](../../analysis/decompiled/item-use-client-exact/00497070_FUN_00497070.c).

The item-use boundary is `0x704C`. Return-scroll TypeID3/TypeID4 dispatch is
now recovered as above, but the stock client/server pass still has not
recovered a complete item-code-to-action table for every teleport,
resurrection, pet, premium, and consumable scroll. The safe model is:

1. decode the authenticated inventory slot and item reference;
2. validate the item against server reference data and current player state;
3. consume or mutate the item in the storage layer;
4. publish item-use, inventory-update, stat/buff, teleport, or entity-state
   notifications according to the selected item family.

This sequence is partly a compatibility model until each scroll family has a
function-level binary trace. Do not identify an item from its display name or
from a mall code alone. The current AegisCore item-use implementation and
tests are at `agent-server/handler/inventory/item_use_handler.go` and
`agent-server/service/item_use.go`.

## Ground items and pickup

The recovered loot path establishes a separate world-object lifecycle:

- drop reference records are hydrated and rolled into an in-memory item list;
- the item-family factory creates a runtime object and registers its unique ID;
- the player sends `0x7034` with action `6` and the target object UID;
- validation resolves the object from the same global UID index;
- successful pickup emits a `0x3036` animation constructor and transfers the
  item through the item mutation virtual path;
- ownership timeout emits `0x304D` after the exact recovered `30.0` accumulator
  threshold; reference-controlled expiry is a separate branch.

The recovered path does not prove that item-object creation itself sends the
initial visibility packet, nor that `0x304D` is a despawn packet. See the
[drops report](sr-gameserver-drops-loot.md).

## Ordinary storage-operation SQL dispatch

The GameServer has a second, generic persistence boundary after a storage
operation handler returns success. `0x0050F5C0` calls the operation table and
then reaches `0x0050FA50`; that selector chooses the updater family from the
operation descriptor:

| Operation byte(s) | Updater selected by `0x0050FA50` | Confirmed builder family |
| --- | --- | --- |
| `0x08`, `0x13`, `0x18`, `0x2B` | `AQ_StorageUpdaterForPacakageItem` | `_STRG_ADD_PACKAGEITEM*` |
| `+0x38 == 1` | `AQ_SlotUpdater` | slot-state updater; exact SQL branch not promoted here |
| other accepted operations | `AQ_StorageUpdater` | operation-specific branches below |

Within `0x00459F70` (`AQ_StorageUpdater::Init`), the confirmed operation
groups select these builders:

| Operation byte(s) | Builder | Exact SQL family |
| --- | --- | --- |
| `0x00`, `0x10` | `0x0045B5A0` | move/merge/split/swap, selected by descriptor state `+0x01` |
| `0x06`, `0x0E`, `0x11`, `0x15`, `0x1C` | `0x0045A9A0` | add expendable, alchemy stone, specialty, or normal equipment |
| `0x07`, `0x0F`, `0x12`, `0x16`, `0x09`, `0x14`, `0x29` | `0x0045A840` | `_STRG_DEL_ITEM` |
| `0x08`, `0x13` | `0x0045A9A0` | generated-item insertion branch in the base updater |
| `0x18` | base updater vfunc `+0x30` = `0x0045A5D0` | `_STRG_BUY_CASHITEM_EXP` / `_STRG_BUY_CASHITEM_EQUIP_NORMAL` |
| `0x22` | `0x0045BB60` | `_STRG_RESTORE_SOLDITEM_*` |
| `0x27`, `0x28` | `0x0045C9A0` or related linked-item branch | move/link-related storage update; exact subprocedure depends on the nested record |
| `0x2A` | direct formatter in `0x00459F70` | `_STRG_ADD_LINKED_STORAGE` |

For the `0x00`/`0x10` branch, `0x0045B5A0` uses descriptor byte `+0x01`:

```text
0/4 -> _STRG_MOVE_ITEM
1   -> _STRG_MERGE_ITEM
2   -> _STRG_SPLIT_ITEM
3   -> _STRG_SWAP_ITEM
```

The relocation builder exposes a more precise temporary descriptor contract:

| Descriptor offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x00` | `u8` | storage-operation discriminator forwarded to the SQL formatter |
| `+0x01` | `u8` | relocation mode: `0`/`4` move, `1` merge, `2` split, `3` swap |
| `+0x03` | `u8` | first resolved item slot passed to the builder |
| `+0x04` | `u8` | second resolved item slot passed to the builder |
| resolved item `+0x38` | `u32` | per-item value copied into the move/merge/split/swap call context |

For move and merge, the builder first resolves both descriptor slots and calls
`0x004EA580` to derive the remaining operation context before formatting the
procedure. Swap calls the same helper with the two item slots reversed. Split
uses the direct format below and stores the second resolved item in the parent
updater result state after successful query construction:

```sql
{?=CALL _STRG_MOVE_ITEM (%d,%d,%d,%d,%d,%d,%d,%d)}
{?=CALL _STRG_MERGE_ITEM (%d, %d,%d,%d, %d,%d, %d,%d)}
{CALL _STRG_SPLIT_ITEM (?, ?, %d, %d,%d, %d,%d, %d,%d)}
{?=CALL _STRG_SWAP_ITEM (%d, %d,%d,%d, %d,%d, %d,%d)}
```

The executable proves the order of the descriptor discriminator, updater
context arguments, slot bytes and resolved item values at each formatter
call, but the local decompilation does not preserve stable names for every
`param_4`/`param_5` context or the helper's derived value. Those values remain
neutral call-site fields rather than being labeled as item IDs, quantities or
character IDs. The procedure bodies and the asynchronous query result are a
separate evidence layer.

Raw relocation-builder evidence: [`0045B5A0_FUN_0045b5a0.c`](../../analysis/decompiled/gameserver-sql/0045B5A0_FUN_0045b5a0.c)
and [`0045AF70_FUN_0045af70.c`](../../analysis/decompiled/gameserver-sql/0045AF70_FUN_0045af70.c).

This proves operation-to-builder selection and the asynchronous persistence
boundary. It does not rename every operation byte, prove that every
operation group is reachable from the same client request, or establish
transaction/result semantics inside the stored procedures.

The stock binary also contains a separate item-exchange completion path at
`0x0043FB20`. It walks the two participant storage lists, formats and submits
the following nine-argument procedure call, checks the returned value, and
then conditionally formats `UPDATE _Char SET RemainGold = %I64d WHERE CharID =
%d` for the participant balance:

```sql
{?=CALL _STRG_MOVEITEM_NEW (%d, %d, %d, %d, %d, %d, %d, %d, %d)}
```

The executable labels a failed result `item exchange stored procedure returns
error`; the exact participant, slot, item, and operation argument names are
not promoted beyond the observed call order. The direct `0x1A`/`0x1B` handler
now joins this exchange object family to a client request, while the exact
virtual binding from its initializer to the `0x0043FB20` worker remains a
separate binary edge. This path is distinct from the generic `0x0045B5A0`
move/merge/split/swap selector.

Raw exchange SQL evidence: [`0043FB20_FUN_0043fb20.c`](../../analysis/decompiled/gameserver-sql/0043FB20_FUN_0043fb20.c).

## Item mall and cash-item SQL boundary

### Stock TestIn SQL

The GameServer contains two distinct cash-related procedure families. The
client-confirmed operation-`0x18` edge selects the package-item updater; the
same updater class is also reached by the generic package insertion dispatch
used by item-operation `0x2B` (and the overloaded four-byte `0x08` form):

```sql
{CALL _STRG_ADD_PACKAGEITEM (%d,%d,%d,%d,%d,%I64d,%d,%d,%d,%d,'...')}
{CALL _STRG_ADD_PACKAGEITEM_WITH_STOCKQUANTITY (%d,%d,%d,%d,%d,%d,%I64d,%d,%d,%d,%d,'...')}
```

The decompiled package initializer (`0x0045E080`) proves the two formatter
arities (11 versus 12 values, with the latter carrying a stock-quantity
field), but does not preserve stable names for all context/derived arguments.
The pinned shard procedure bodies close these names. `_STRG_ADD_PACKAGEITEM`
passes `Operation`, `CharID`, `PaymentDevice`, `RefPackageItemID`, `Count`, the
remaining currency balances, and `CreaterName` to
`_STRG_ADD_PACKAGEITEM_CORE`. That core explicitly names operation `43`
(`0x2B`) `SP_BUY_ITEM_WITH_TOKEN`; it consumes token devices `64`, `128`,
`256`, `512`, and `1024` and returns their inventory changes as the trailing
continuation records. Payment device `1` updates remaining gold and is used by
the ordinary four-byte operation-`0x08` package path. Both operations insert
into character inventory; operation `0x13` is the COS form.

`_GetNeededSlotCountByBuyPackageItem` also closes quantity expansion. A
single non-bionic `TypeID1/2 == 3/3` member with package `Data == 0` consumes
one slot and receives the request count as its stack quantity. Every other
package clamps the requested count to `[1,5]`, creates that many instances of
each scrap member, and therefore requires `memberCount * clampedCount` slots.
The package core passes the current job creator name to the specialty-item
constructor and debits `Cost * Count` through the selected payment device.

The exact cash-item procedures are also present through the base
`AQ_StorageUpdater` vtable slot `+0x30` (`0x00AE5688 + 0x30 -> 0x0045A5D0`):

```sql
{CALL _STRG_BUY_CASHITEM_EXP (?, ?, %d,%d,%d, %d,%d, ?,?,?)}
{CALL _STRG_BUY_CASHITEM_EQUIP_NORMAL (?, ?, %d,%d,%d, %d,%d,%I64d, %d, ?,?,?)}
```

The first is selected for expendable/cash-item storage and the second for a
normal equipment cash item. The surrounding `AQ_StorageUpdater` validates
the item type, target slot, reference-derived values, and item state before
submitting the query. The stored-procedure bodies, return codes, Silk debit
owner, and transaction isolation are not in the executable.

This does not make the cash procedures the same updater class as the client
mall edge: `0x0050FA50` selects the package-item updater for operation byte
`0x18`, whose confirmed builder is `0x0045E080`. The cash procedures are a
separate, binary-confirmed base-updater route. Its database completion branch
is pinned:

```text
AQ_StorageUpdater::DoWork 0x0045CE90
  -> operation byte 0x18
  -> AQ_StorageUpdater vfunc +0x34 = 0x0045CB70
  -> 0x009722D0 database/result execution
  -> updater result fields +0x198/+0x19C, +0x1A8/+0x1AC, +0x64, +0x68, +0x6C
```

The general storage dispatcher at `0x00459F70` has its own operation-`0x18`
case: it calls updater vfunc `+0x30`, which is `0x0045A5D0` and formats the
`_STRG_BUY_CASHITEM_*` procedures. This is a real operation-to-cash-SQL edge,
but it is not the client-mall selector proven by `0x0050FA50`; that client
path selects the package-item updater instead. The same operation byte is
therefore reused by two distinct updater selections in the stock server.

`0x0045CB70` executes the result query and copies returned values into the
updater state, but its recovered body emits no client opcode and does not call
the recovered Silk notification builder at `0x004F0750`. The generic player
relay is connected at the callback boundary: the base constructor
`0x004595D0` sets completion discriminator `+0x20 = 1`; base success post-work
when its positive-result gate passes, `0x0045D4A0` calls the owner `+0x5D4`
adapter `0x004E9440`; that adapter calls
base updater `+0x2C = 0x0045D940` and then dispatches type `1` through
`0x004E73D0`. However, the type-`1` switch explicitly groups updater operation
byte `+0x28 = 0x18` with the no-special-action cases `3` and `0x1D`; that case
does not call `0x004E9480`. A non-`1` adapter result instead routes to
`0x004E9110`. Therefore this base cash-updater edge does not by itself prove a
`0xB034`, `0x3153`, or `0x3154` response.

The `0x004E9480` -> `0x004E9AE0` -> `0x004F0940` balance continuation belongs
to the separate package-updater completion path. There, the package result
relay reaches the emitter for operation `+0xB8 = 0x18`; the emitter's own
operation switch calls `0x004E9AE0` with the player in `EAX` and the operation
record in `EDI` (confirmed by the function-entry register moves). When the
operation presence mask at `+0x88` contains bit `0x02`, `0x04`, or `0x10`,
`0x004E9AE0` validates the corresponding high/low value pairs and calls
`0x004F0940(1)`. The exact state mapping is:

```text
operation +0x98/+0x9C -> player +0x862 -> 0x3153 field 1
operation +0xA0/+0xA4 -> player +0x863 -> 0x3153 field 2
operation +0xB0/+0xB4 -> player +0x864 -> 0x3153 field 3
```

`0x004F0940(1)` clamps negative input values to zero, stores the three values,
and emits `0x3153` with three `u32` writes. The operation parser records bit
`0x10` for the mall block, so a package operation can reach this balance
notification when the validated result fields are present. This establishes
the branch and widths, but not the currency/database-column names. The shared
operation byte must not be used to merge the two updater lifecycles.

The other `0x004E9AE0` flags are also bounded without assigning currency names:

| Operation `+0x88` bit | Confirmed continuation |
| ---: | --- |
| `0x02`, `0x04`, or `0x10` | validate the three pairs at `+0x98/+0x9C`, `+0xA0/+0xA4`, and `+0xB0/+0xB4`; call `0x004F0940(1)` |
| `0x08` | validate `+0xA8/+0xAC`, negate the low value at `+0xA8`, pass it to player virtual `+0x08`, then call `0x005C7AA0(operation +0x110, returnedValue, -value)` |
| `0x20` | validate `+0xB8/+0xBC`, call player virtual `+0x380`, and, when the player-side registry exists and accepts the value, call `0x005DE6C0(..., 3, ...)` |

The first row is the only recovered path that directly emits `0x3153` from
this completion helper. The `0x08` and `0x20` rows are state/economy-side
calls with no recovered item-change or explicit `0x3154` writer in this
function. `0x004E9480` sends the enclosing `0xB034` after the helper returns;
the helper itself does not prove a package-specific inventory packet.

The operation-`0x18` branch in `0x0045CE90` is an early return: it invokes
base-updater vfunc `+0x34` and returns its Boolean result before reaching the
ordinary rent-info/item-state branches below it. Thus, for this base cash
updater, the static completion boundary is exactly the result-set decode in
`0x0045CB70`; no later packet writer is hidden in the same `DoWork` function.
The binary also initializes a queue explicitly named by RTTI as
`CQue<AQ_StorageUpdaterForCashPacakageItem*>` at `0x00462E90`, and the pool
allocator at `0x004604F0` constructs `0x1D8`-byte `AQ_StorageUpdater` objects.
This proves the cash updater is live infrastructure. It also separates the
named pool/free-list queue from the now-pinned generic completion relay.

#### Base cash-updater result record and relay

The base cash updater's observed result fields are now bounded by the
`0x0045CB70` result worker and the `0x0045D4A0` success callback:

| Offset | Width | Confirmed use |
| ---: | ---: | --- |
| `+0x20` | `u32` | completion family; the base constructor sets `1` |
| `+0x28` | `u8` | storage operation byte; the cash path is `0x18` |
| `+0x198`, `+0x19C` | `u32` pair | first returned result pair; the success callback uses it as a positive-result gate. On updater operation branches that enter `0x0045D5D0`, it is verified and written as the item DBID; the base cash operation-`0x18` branch does not enter that item-mutation switch |
| `+0x1A8`, `+0x1AC` | `u32` pair | later returned result pair copied by the result worker and passed through item virtual `+0x500`; destination semantics unresolved |
| `+0x64` | `u32` | later returned result value copied by the result worker |
| `+0x68`, `+0x6C` | `u32` pair | later returned result values copied by the result worker and retained in updater state |

These offsets are storage facts, not account-column names. The direct
completion chain is:

```text
_STRG_BUY_CASHITEM_* 
  -> 0x0045CE90, operation byte 0x18
  -> updater vfunc +0x34 = 0x0045CB70
  -> result pairs/values above
  -> 0x0045D4A0 success callback
  -> owner vfunc +0x5D4 = 0x004E9440(..., 1)
  -> 0x004E73D0 type-1 completion relay
  -> operation-`0x18` no-special-action case
  -> updater completion/finalization; no direct packet writer recovered
```

The result worker's extraction order is exact at the offset level. Before
executing the query, `0x0045CB70` binds the existing updater values at
`+0x1A8/+0x1AC`, `+0x64`, `+0x68`, and `+0x6C`. It then reads the returned
row/vector sequentially:

| Returned result position | Destination | Gate or continuation |
| ---: | --- | --- |
| 1 | `+0x198`, `+0x19C` | must be a positive 64-bit pair for the worker to continue; the success callback also returns early for a negative or zero pair |
| 2 | `+0x1A8`, `+0x1AC` and mirrored to `+0x80`, `+0x84` | retained as a second result pair; no column name is recovered |
| 3 | `+0x64` | copied as one `u32` |
| 4 | `+0x68` | copied as one `u32` |
| 5 | `+0x6C` | copied as one `u32` |

The DBID promotion is limited to updater branches that actually call
`0x0045D5D0`: that helper checks that `+0x198/+0x19C` is nonzero, verifies that
the target item is the same item selected during initialization, and calls
item virtual `+0x37C` with that 64-bit pair. It then passes `+0x1A8/+0x1AC`
through item virtual `+0x500`. The operation-`0x18` case in
`0x0045D4A0` takes the default/no-item-mutation path, so the executable does
not establish an item-DBID write for this base cash route. Neither the
stored-procedure output-column names nor the semantic meaning of the second
pair is present in the executable. Raw evidence:
[`0045D5D0_FUN_0045d5d0.c`](../../analysis/decompiled/cash-completion-pc/0045D5D0_FUN_0045d5d0.c)
and [`0045D4A0_FUN_0045d4a0.c`](../../analysis/decompiled/cash-completion-pc/0045D4A0_FUN_0045d4a0.c).

The generic adapter can reject or route failure through `0x004E9110`; the
remaining stored-procedure result values that select each callback outcome
remain unresolved. The target-item DBID role of `+0x198/+0x19C` is proven only
on the separate `0x0045D5D0` branches. The explicit `0x004F0750` writer for
`0x3154` is an independent Silk notification path and is not directly called
by the recovered base operation-`0x18` branch.

The only direct caller present in the recovered/exported reference set for
the explicit `0x004F0750` writer is `0x00414930`. That caller first checks
`FUN_00403970` and then invokes the
Silk notifier; the available caller evidence does not connect it to the
cash-updater operation-`0x18` completion chain. Raw caller evidence:
[`00414930_FUN_00414930.c`](../../analysis/decompiled/silk-update-callers/00414930_FUN_00414930.c).

Raw relay evidence: [`0045CB70_FUN_0045cb70.c`](../../analysis/decompiled/gameserver-item-mall-cash/0045CB70_FUN_0045cb70.c),
[`0045CE90_FUN_0045ce90.c`](../../analysis/decompiled/gameserver-item-mall-cash/0045CE90_FUN_0045ce90.c),
[`004E9440_FUN_004e9440.c`](../../analysis/decompiled/cash-completion-pc/004E9440_FUN_004e9440.c),
[`004E73D0_FUN_004e73d0.c`](../../analysis/decompiled/gameserver-sql/004E73D0_FUN_004e73d0.c),
[`004E9480_FUN_004e9480.c`](../../analysis-tmp/admission-callers/004E9480_FUN_004e9480.c),
[`004E9AE0_FUN_004e9ae0.c`](../../analysis/decompiled/silk-update-callers/004E9AE0_FUN_004e9ae0.c),
and [`004F0940_FUN_004f0940.c`](../../analysis/decompiled/silk-update-callers/004F0940_FUN_004f0940.c).

Base-constructor evidence: [`004595D0_FUN_004595d0.c`](../../analysis/decompiled/cash-completion-pc/004595D0_FUN_004595d0.c).
Success-post-work evidence: [`0045D4A0_FUN_0045d4a0.c`](../../analysis/decompiled/cash-completion-pc/0045D4A0_FUN_0045d4a0.c).
Balance-continuation evidence: [`004E9AE0_FUN_004e9ae0.c`](../../analysis/decompiled/silk-update-callers/004E9AE0_FUN_004e9ae0.c),
[`004F0940.asm`](../../analysis/decompiled/cash-silk-trace/004F0940.asm).
The shared adapter and type-`1` dispatcher are linked in the package
completion-relay evidence below.

The named cash queue is more precisely a pool/free-list lifecycle than a
proven client-completion queue. RTTI places
`CQue<AQ_StorageUpdaterForCashPacakageItem*>` at vtable `0x00AE57C8`, with
the type descriptor string at `0x00C688BC`. Its only observed queue-global
references are the initializer/cleanup family around `0x00462BB0`:
`0x00462BB0` initializes the embedded queue and sentinel at `0x00CC4B3C` /
`0x00CC4B5C`; `0x00462C70` invokes `0x00462E00`; and `0x00462E00` walks the
linked nodes, invokes the first virtual with argument `3`, and frees the
nodes. That path contains no `DoWork` call, result-field decode, packet
builder, or Silk writer. This narrows the static conclusion: the binary
proves a cash-updater allocator/cleanup family and the separate
`0x0045CB70` result worker, but does not prove that this named queue is the
runtime completion relay.

Raw queue evidence: [`00462BB0_FUN_00462bb0.c`](../../analysis/decompiled/cash-global-refs/00462BB0_FUN_00462bb0.c),
[`00462C70_FUN_00462c70.c`](../../analysis/decompiled/cash-global-refs/00462C70_FUN_00462c70.c),
[`00462E00_FUN_00462e00.c`](../../analysis/decompiled/cash-global-refs/00462E00_FUN_00462e00.c),
and [`00462E90_FUN_00462e90.c`](../../analysis/decompiled/cash-queue-extra/00462E90_FUN_00462e90.c).

The separate Silk notifier is independently binary-confirmed. `0x004F0750`
reads two `u32` values
and two `u8` values, validates cash type `0..2` and reason `0..5`, sends
server-to-client opcode `0x3154`, and then refreshes the three-value balance
state through `0x004F0940`, whose optional notification opcode is `0x3153`.
The matching client handlers resolve the wire order: `0x3154` is
`u8 reason, u8 cashType, u32 displayedValue`, while `0x3153` is three `u32`
values forwarded to the client balance/UI helper and stored at client-object
offsets `+0x4AC`, `+0x4B0`, and `+0x4B4`. Those are concrete client state
destinations, but not executable-confirmed account-column names. These facts
identify the available Silk notification family. The operation-
`0x18` continuation reaches the shared three-value state writer
`0x004F0940`, but does not directly call the explicit `0x3154` writer
`0x004F0750` in the recovered branch.

The client’s `0x3154` handler contains the exact localized reason labels
`UIIT_STT_SILK_BUY_RESULT`, `UIIT_STT_SILK_BUY_CANCLE`,
`UIIT_STT_SILK_BUY_RECEIVE`, `UIIT_STT_SILK_SEND_RESULT`,
`UIIT_STT_SILK_POINT_BUY_RECEIVE`, and `UIIT_STT_SILK_POINT_USE_RECEIVE`
for reasons `0..5`; `0xFF` selects `UIIT_MSG_STRGERR_TRADE_FAILURE`.
This is a binary-confirmed UI classification only. It does not establish the
meaning of each cash type, the account column behind any of the three balance
values, or a stored-procedure result mapping.

For reference, the client dispatch registration and handlers make the balance
wire grammar independently checkable:

| Server-to-client opcode | Client decode order | Confirmed client action |
| ---: | --- | --- |
| `0x3153` | `u32`, `u32`, `u32` | forwards all three values to `0x00778E50`; the handler's caller path stores them at client offsets `+0x4AC`, `+0x4B0`, `+0x4B4` |
| `0x3154` | `u8 reason`, `u8 cashType`, `u32 displayedValue` | selects the localized Silk/trade message and formats it with `displayedValue`; `0xFF` selects the trade-failure message |

This is a wire-structure statement only. It does not make `cashType`,
`displayedValue`, or the three `0x3153` values synonyms for particular SQL
columns. Raw client evidence:
[`00882DE0_FUN_00882de0.c`](../../analysis/decompiled/client-silk-handlers/00882DE0_FUN_00882de0.c),
[`00882E50_FUN_00882e50.c`](../../analysis/decompiled/client-silk-handlers/00882E50_FUN_00882e50.c),
[`00778E50_FUN_00778e50.c`](../../analysis/decompiled/client-silk-handlers/00778E50_FUN_00778e50.c),
[`0086FBB0_FUN_0086fbb0.c`](../../analysis/decompiled/client-silk-handlers/0086FBB0_FUN_0086fbb0.c),
[`004F0940_FUN_004f0940.c`](../../analysis/decompiled/silk-update-callers/004F0940_FUN_004f0940.c),
and [`004F0750_FUN_004f0750.c`](../../analysis/decompiled/silk-update-callers/004F0750_FUN_004f0750.c).

`0x0045A5D0` also proves two important preconditions: the target item's cash
item DBID must still be null, and the item must pass the runtime service/type
gate. It then formats the stored-procedure call and submits it through the
asynchronous query layer. The executable does not connect this base cash
operation-`0x18` completion to the conditional `0xB034` emitter or the
three-value balance-state writer; those edges are present on the separate
package-updater completion path. It does not prove the stored-procedure
result values that select each field, a separate inventory update, or an
atomic Silk debit.

Related stock storage calls recovered from the same SQL pass include:

```sql
{CALL _STRG_ADD_EXPENDABLE (?, ?, %d,%d, %d,%d,%d)}
{CALL _STRG_ADD_ALCHEMYSTONE (?, ?, %d,%d, %d,%d,%d,%d)}
{CALL _STRG_ADD_EQUIP_NORMAL (?, ?, %d,%d,%d, %d,%d,%I64d,%d)}
{CALL _STRG_ADD_SPECIALTY (?, ?, %d,%d,%d,%d,%d, '%s')}
{CALL _STRG_ADD_ITEM_MAGIC (?, ?, %d, %d,%d, %d,%d,%I64d,%d, %d, ...)}
{?=CALL _STRG_MOVE_ITEM (...)}
{?=CALL _STRG_MERGE_ITEM (...)}
{CALL _STRG_SPLIT_ITEM (...)}
{?=CALL _STRG_SWAP_ITEM (...)}
{?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}
```

The same generated-item updater has a distinct specialty branch selected by
the runtime item-state/type predicates; it is not evidence that specialty
items use the expendable or alchemy-stone procedure. The sold-item restore
family has the matching exact literal
`{CALL _STRG_RESTORE_SOLDITEM_SPECIALTY (?, ?, %d,%d,%d,%d,%d,%I64d, '%s')}`
at `0x0045BB60`. These strings prove formatter selection and the presence of
the output-parameter prefix, but not the specialty-name field's database
meaning, procedure result columns, or transaction semantics.

The deletion format is exact at `0x0045A840`: it supplies storage/operation
context, a 64-bit item identifier, a quantity/count value, and the final
storage selector/context value in the order shown. The magic-parameter insert
at `0x0045ADA0` starts with the fixed prefix shown above and appends one
`%I64d,%I64d` pair per copied magic parameter, with the executable limiting
the reference object's parameter count to `0..0x0C`. The pair names and
procedure transaction behavior are not present in the format string.

Raw storage-builder evidence: [`0045A9A0_FUN_0045a9a0.c`](../../analysis/decompiled/gameserver-sql/0045A9A0_FUN_0045a9a0.c),
[`0045ADA0_FUN_0045ada0.c`](../../analysis/decompiled/gameserver-sql/0045ADA0_FUN_0045ada0.c),
[`0045BB60_FUN_0045bb60.c`](../../analysis/decompiled/gameserver-sql/0045BB60_FUN_0045bb60.c),
and [`0045B970_FUN_0045b970.c`](../../analysis/decompiled/gameserver-sql/0045B970_FUN_0045b970.c).

The package-item procedures are not merely catalog strings: the operation-
`0x18` call edge is recovered below. The remaining strings are capabilities
until their individual `AQ_StorageUpdater::Init/DoWork` call edge is pinned.

### Operation-`0x18` SQL call edge

The complete static edge for the client-confirmed mall request is:

```text
C -> S  0x7034, operation 0x18
  -> 0x004D8BF0       parse operation record
  -> 0x0050F5C0       dispatch table slot 0x00CD3C18
  -> 0x00526610       resolve reference and build runtime item
  -> 0x00618FE0       item-record/inventory construction
  -> 0x00617A50       capacity, price/state, and item validation
  -> 0x0050FA50       select async storage updater
  -> 0x0045E080       AQ_StorageUpdaterForPacakageItem::Init
  -> _STRG_ADD_PACKAGEITEM[(_WITH_STOCKQUANTITY)]
  -> 0x0045E620       AQ_StorageUpdaterForPacakageItem::DoWork
  -> 0x00951880       execute accumulated query
```

`0x0050FA50` selects the package updater for operation bytes `0x08`, `0x13`,
`0x18`, and `0x2B`; the class vtable constructor identifies it as
`AQ_StorageUpdaterForPacakageItem` (the misspelling is present in the binary).
`0x0045E080` chooses the ordinary package procedure when the referenced
package has no stock-quantity mode, and the `_WITH_STOCKQUANTITY` procedure
when that mode is set. It also checks the owner/item objects, target slot,
reference-derived values, and non-negative quantity/currency fields before
formatting the query.

The query worker accepts the operation-`0x18` result and performs a separate
rent-info maintenance call when applicable. The SQL worker itself does not
write a client packet; `0x0050FA50` returns success when async work is accepted
and returns `0x1811` on updater/queue failure. Do not infer transaction
atomicity, Silk debit ownership, or the stored-procedure result-set contract
from the call string alone.

#### Package SQL formatter stack contract

The assembly at `0x0045E080` is more informative than the decompiler's
variadic call rendering. Before calling `0x00459E00`, it pushes eight words in
the order shown below. Those words remain on the stack and are intentionally
consumed later by `0x00425F70`; the ordinary branch cleans `0x34` bytes and the
stock-quantity branch cleans `0x38` bytes after its extra helper word has been
removed. Consequently, the visible four-argument decompiler output is
not the SQL parameter list.

| Helper-stack word | Push site | Confirmed source at the push | Later formatter position |
| ---: | ---: | --- | --- |
| `A1` | `0x0045E21B` | result of owner/item virtual `+0xF0` | normal `12`; stock `13` |
| `A2` | `0x0045E222` | record `+0x168` | normal `11`; stock `12` |
| `A3` | `0x0045E229` | stack-carried word loaded through `[ESP+0x20]` | normal `10`; stock `11` |
| `A4` | `0x0045E22E` | record `+0x158` | normal `9`; stock `10` |
| `A5` | `0x0045E233` | record `+0x150` | normal `8`; stock `9` |
| `A6` | `0x0045E23B` | stack-carried word loaded through `[ESP+0x20]` | normal `7`; stock `8` |
| `A7` | `0x0045E23C` | stack-carried word loaded through `[ESP+0x20]` | normal `6`; stock `7` |
| `A8` | `0x0045E23D` | sign-extended record `+0xBE` | normal `5`; stock `6` |

The formatter's leading arguments are then pushed around those retained words:

| SQL position | Ordinary procedure | Stock-quantity procedure |
| ---: | --- | --- |
| `1` | record `+0xB8` | record `+0xB8` |
| `2` | stack word loaded at `0x0045E2B1` | stack word loaded at `0x0045E24E` |
| `3` | dereferenced record `+0x140` | dereferenced record `+0x140` |
| `4` | result of `0x00459E00` | stock quantity returned by `0x00459DB0` |
| `5` | `A8` | result of `0x00459E00` |
| `6` | `A7` (low word of `%I64d`) | `A8` |
| `7` | `A6` (high word of `%I64d`) | `A7` (low word of `%I64d`) |
| `8` | `A5` | `A6` (high word of `%I64d`) |
| `9` | `A4` | `A5` |
| `10` | `A3` | `A4` |
| `11` | `A2` | `A3` |
| `12` | `A1` (`%s`) | `A2` |
| `13` | — | `A1` (`%s`) |

The last row in each format is the `%s` conversion. Its source is the owner/item
virtual `+0xF0` result (`A1`), but the executable does not establish whether
that returned value is a public name, a code, or another formatter string, so
it is deliberately not assigned a semantic name here. Likewise, `A3`, `A6`, `A7`, and
the leading stack words are recorded as stack-carried values rather than
invented item IDs, character IDs, or currency columns. The only safe SQL
claim from this call site is the exact procedure, conversion order, and the
record/virtual-call sources shown above. Raw formatter evidence:
[`0045E080_FUN_0045e080.c`](../../analysis/decompiled/gameserver-sql/0045E080_FUN_0045e080.c).

The rent-maintenance edge is independently visible in the package updater's
post-work helper. `0x0045EA20` selects this branch only when the updater
operation byte at `+0xB8` is `0x18`; `0x0045EA30` then formats the following
call before `0x0045EBD0` executes it through the database connection:

```sql
{?=CALL _Rent_Info_Manager (
  %d, %I64d, %d, %d, %d,
  '%d-%d-%d %d:%d:%d',
  '%d-%d-%d %d:%d:%d',
  '%d-%d-%d %d:%d:%d',
  %d, %d
)}
```

The binary confirms that the first argument is formatted as `0`, followed by
an item-derived 64-bit value, an item/reference-derived value, two 16-bit
values, three timestamp triples, and two final scalar values. The exact
database-column meanings of those values remain unresolved. `0x0045EBD0`
checks the database result list and logs either `DB Query Error(%d)` or
`Stored procedure Error(%d)` with the literal source label
`AQ_StorageUpdaterForPacakageItem::DoWork_AddRentInfo`; this is a separate
maintenance query, not proof that the package insertion and rent update share
one transaction.

Raw evidence: [`0045EA20_FUN_0045ea20.c`](../../analysis/decompiled/gameserver-item-7034/0045EA20_FUN_0045ea20.c),
[`0045EA30_FUN_0045ea30.c`](../../analysis/decompiled/gameserver-item-7034/0045EA30_FUN_0045ea30.c),
and [`0045EBD0_FUN_0045ebd0.c`](../../analysis/decompiled/gameserver-sql/0045EBD0_FUN_0045ebd0.c).

The acceptance step is separately visible in `0x004E7350(1)`: it checks the
server's async-trigger slot and calls `0x00403600`. That is queue admission,
not evidence of a client response or of a completed database transaction.

The bounded `0x00617A50` validation listing also exposes several exact
pre-database rejection branches. The request count byte at record `+0x02` is
compared with the resolved package/item vector length and can return
`0x1806`; a missing resolved package/reference returns `0x1809`. For
operation `0x18`, an extended package-state predicate over the record's
`+0x11C` block can return `0x18AB`. A signed 64-bit comparison between the resolved
item-side value and the request-side amount can return `0x1893`, while the
later availability/stock comparison can return `0x1807`. These values are
exact rejection codes and branch conditions; the executable does not attach
stable UI names to them, so they must not be relabeled as “insufficient Silk,”
“inventory full,” or “out of stock” without a capture or localization match.

### Package updater completion callback

The package updater's completion callback is now pinned independently of the
SQL worker. RTTI/vtable construction at `0x0045F910` identifies
`CClassTrigger<AQ_StorageUpdaterForPacakageItem>` and installs callback
`0x0045E490`. On its first invocation, `0x0045E490` allocates and clears a
27-byte result buffer through `0x0045E460`; the buffer is stored as a
trigger-owned byte sequence at `param_2 + 4`, with length/capacity initialized
to `0x1B`.

For operation bytes `0x08`, `0x13`, `0x18`, and `0x2B`, the callback examines
the result bytes at `+0x19` and `+0x1A` and, for the observed completion/error
cases, calls `0x0045EE10`. That helper does only vector-style result
aggregation: it appends one four-byte value from the current callback context
or grows the result vector through `0x0045FB40`. It does not format SQL, write
`0xB034`, update an inventory object, or debit/update Silk. A `+0x19 == 2`
branch instead copies the value at result `+1` into the parent trigger's
`+0x68` and clears `+0x6C`; unknown status combinations fall into the binary's
failure/assertion path.

The callback's neutral result grammar can be stated more precisely:

| Offset | Width | Confirmed role |
| ---: | ---: | --- |
| `+0x00` | `u8` | operation discriminator; the callback handles `0x08`, `0x13`, `0x18`, and `0x2B` |
| `+0x01` | `u32` | value copied to parent trigger `+0x68` when status `+0x19 == 2` |
| `+0x0D` | `u32` | flag word tested for bits `0x02`, `0x04`, `0x08`, `0x10`, and `0x20` when status is `1` |
| `+0x19` | `u8` | callback status discriminator |
| `+0x1A` | `u8` | secondary state tested when status is `0` |

For the handled operation family, the callback branches as follows:

```text
status 0:
    state 1 or 2 -> append the current callback value to the trigger vector
    other state  -> return without appending
status 1:
    any tested flag bit set -> append the current callback value
    no tested flag bit set  -> diagnostic/assertion path
status 2:
    parentTrigger +0x68 = u32(result + 1)
    parentTrigger +0x6C = 0
other status:
    return without appending
```

This is a callback/result-buffer contract only. It does not prove that the
status values mean database success, Silk debit, inventory insertion, or
client-visible success. Raw evidence:
[`0045E490_FUN_0045e490.c`](../../analysis/decompiled/gameserver-sql/0045E490_FUN_0045e490.c),
[`0045E460_FUN_0045e460.c`](../../analysis/decompiled/gameserver-sql/0045E460_FUN_0045e460.c),
and [`0045EE10_FUN_0045ee10.c`](../../analysis/decompiled/gameserver-sql/0045EE10_FUN_0045ee10.c).

The type-`0x12/0x16` downstream relay imposes another neutral gate before
the conditional response emitter. During child-record processing, every
record in the returned vector must have its own status byte at `+0x19 == 1`.
After that validation, the parent trigger values at `+0x68/+0x6C` have these
observed control outcomes for the ordinary package path:

| Parent pair | Confirmed continuation |
| --- | --- |
| `0 / 0` | candidate item-operation completion; after the remaining authority/item checks, may call `0x004E9480` and therefore emit `0xB034` |
| `0x18CF / 0` | special cleanup/state path, then generic response helper `0x004E7250` |
| `0x18D0 / 0` | generic response helper `0x004E7250` |
| any other nonzero pair | diagnostic/assertion path |

`0x18CF` and `0x18D0` are preserved as raw control values. The executable
does not establish that either means “out of stock”, “insufficient Silk”, or
any other user-facing error. `0x004E7250` is a generic network-response
helper; its complete payload naming is not recovered here. Raw relay
evidence: [`004E73D0_FUN_004e73d0.c`](../../analysis/decompiled/gameserver-quest-async/004E73D0_FUN_004e73d0.c)
and [`004E7250_FUN_004e7250.c`](../../analysis/decompiled/package-completion-trace/004E7250_FUN_004e7250.c).

The flagged-value helper used by this relay, `0x004B2BD0`, has an exact
neutral destination layout:

```text
u32 flags                         // destination word +0x00
u32 untouched by 0x004B2BD0       // destination word +0x04
u64 value[16]                     // value[i] at destination +0x08+i*8
```

For each index `i` in `0..15`, the helper sets `flags |= 1 << i` and stores
the supplied low/high `u32` pair at `+0x08+i*8`. The type-`0x12/0x16` relay
tests each returned child record's `u32` flag word at `+0x0D` and, when a bit
is set, validates the child pair at `+0x05/+0x09` before invoking this helper.
The helper's exact destination base is intentionally left unnamed because
the exported decompilation does not preserve all implicit register arguments
at that call site. Raw helper evidence:
[`004B2BD0_FUN_004b2bd0.c`](../../analysis/decompiled/package-completion-trace/004B2BD0_FUN_004b2bd0.c).

The package completion relay is now connected to the generic player adapter.
The package object constructor at `0x00459A00` initializes its asynchronous
completion discriminator at object `+0x20` to `0x12`; the requested package
operation remains at `+0xB8` (for the client mall edge, `0x18`). Its vtable at
`0x00AE572C` supplies `+0x2C = 0x009BF500`, whose body is a confirmed no-op.
The owner callback chain is:

```text
_STRG_ADD_PACKAGEITEM* -> 0x0045E620 query worker
                       -> 0x0045E490 trigger callback
                       -> 0x0045EE10 result-vector append
                       -> CGObjPC vfunc +0x5D4 = 0x004E9440
                       -> updater vfunc +0x2C = 0x009BF500
                       -> success: 0x004E73D0(player, updater)
                       -> type 0x12/0x16 completion path
```

Within the type-`0x12/0x16` path, the binary validates the returned item
records, applies item-state/option mutations, and conditionally calls
`0x004E9480`. That emitter constructs server-to-client `0xB034` and serializes
the polymorphic operation with `0x004F76E0`. This is a binary-confirmed
conditional success-path edge for the package updater. For operation `0x18`,
`0x004E9480` then calls `0x004E9AE0`, whose validated flag/value path can
update the player's three balance-state fields through `0x004F0940(1)`. The
binary still does not establish which stored-procedure result values select
each branch or whether an additional inventory notification is required.

The failure side is also pinned: `0x004E9440` routes a non-`1` completion flag
to `0x004E9110`, while the successful type-`0x12/0x16` path can reach the
`0x004E9480` emitter. This replaces the earlier negative conclusion that no
static mall-to-`0xB034` edge existed; the remaining gap is the precise result
buffer/status-to-branch mapping, balance-field semantics, and any separate
explicit Silk notification.

The ordinary failure relay is now bounded at the family-switch level. In
`0x004E9110`, updater object `+0x20` selects the completion family. For the
package updater's family `0x12`, the only special branch is operation byte
`+0x28 == '+'` with a nonzero `+0x50`, which calls `0x004F1DA0`. The client mall
operation uses `+0x28 = 0x18`, so it does not take that special branch. Its
ordinary non-success path falls through to the diagnostic
`AQ Failed! Log out!! [AQType: %d]` and `0x004DF050(4)`; no package-specific
`0xB034`, `0x3153`, or `0x3154` emission is present on that path.

`0x004E7250` is a generic response helper used by several unrelated async
families and by the package relay for the raw parent outcomes `0x18CF/0` and
`0x18D0/0`. It obtains a message stream through the player virtual at `+0x278`,
serializes a one-byte literal `2`, then performs a two-byte serialization, and
sends through the player virtual at `+0x27C`. The helper's caller-supplied
opcode and the second field's semantic value are not stable in the exported
decompilation; therefore this helper must not be relabeled as a package error
packet. Bounded assembly confirms the two write widths. Raw evidence:
[`004E7250_004E9110_disasm.txt`](../../analysis/decompiled/gameserver-quest-async-completion/004E7250_004E9110_disasm.txt),
[`004E7250_FUN_004e7250.c`](../../analysis/decompiled/package-completion-trace/004E7250_FUN_004e7250.c),
[`004E9110_FUN_004e9110.c`](../../analysis/decompiled/gameserver-quest-async-completion/004E9110_FUN_004e9110.c),
and [`004E9440_FUN_004e9440.c`](../../analysis/decompiled/gameserver-quest-async-completion/004E9440_FUN_004e9440.c).

The same reference sweep finds `0x004F0750` called by the independent explicit
Silk-notification path at `0x00414930`, while the package operation-`0x18`
branch reaches `0x004F0940` indirectly through `0x004E9AE0` with the
state-only flag. The recovered package branch does not directly call the
explicit `0x3153` or `0x3154` writers; its three-value balance update is a
server-side state mutation, not proof of a client balance packet.

#### Completion matrix for the two operation-`0x18` routes

The recovered completion edges can be summarized without assigning names to
the unresolved balances or stored-procedure result columns:

| Route | SQL/query completion | Confirmed success-side wire/state edge | Confirmed failure-side edge | Not proven by this pass |
| --- | --- | --- | --- | --- |
| client mall/package request | `_STRG_ADD_PACKAGEITEM*` through `0x0045E620` | package family `0x12/0x16` can reach `0x004E9480` -> server `0xB034`; the operation serializer is `0x004F76E0`; the client parses it in `0x0087EB50`, then performs the local registry/UI path through `0x0087F370` and `0x0087BA20`; validated balance fields reach the state-only `0x004F0940` continuation | non-`1` completion is routed by `0x004E9440` to `0x004E9110`; package operation `0x18` takes the ordinary AQ failure/log-out path | exact result-vector-to-branch/status mapping, balance names, stored-procedure output columns, atomic debit semantics, and any direct `0x3153`/`0x3154` notification |
| base cash-item updater | `_STRG_BUY_CASHITEM_EXP` or `_STRG_BUY_CASHITEM_EQUIP_NORMAL` through `0x0045CB70` | result values are decoded and relayed through `0x0045D4A0` -> `0x004E9440` -> type-`1` `0x004E73D0`; operation `0x18` is a no-special-action case in that relay | non-success reaches `0x004E9110` and its generic AQ failure/log-out path | direct `0xB034`, `0x3153`, `0x3154`, an inventory-change packet, and a proven client-facing success message |
| independent cash notification | not tied to either recovered operation-`0x18` SQL edge | `0x004F0750` validates reason/cash-type, sends `0x3154`, then calls `0x004F0940(1)` for `0x3153`; the recovered direct caller is `0x00414930` | invalid reason/type logs and returns without sending the notification | connection from this notifier to either operation-`0x18` updater completion |

This matrix is an evidence boundary, not an implementation recommendation.
In particular, a successful database result from the base cash updater must
not be converted into a presumed `0xB034` or Silk packet merely because the
same operation byte is used by the package route.

Raw callback evidence: [`0045F910`](../../analysis/decompiled/gameserver-sql/0045F910_FUN_0045f910.c),
[`0045E490`](../../analysis/decompiled/gameserver-sql/0045E490_FUN_0045e490.c),
[`0045E460`](../../analysis/decompiled/gameserver-sql/0045E460_FUN_0045e460.c),
and [`0045EE10`](../../analysis/decompiled/gameserver-sql/0045EE10_FUN_0045ee10.c).
Completion-relay evidence: [`004E9440.asm`](../../analysis/decompiled/gameserver-quest-async-completion/004E9440.asm),
[`009BF500_FUN_009bf500.c`](../../analysis/decompiled/gameserver-quest-async-completion/009BF500_FUN_009bf500.c),
[`00459A00_FUN_00459a00.c`](../../analysis/decompiled/package-constructor-trace/00459A00_FUN_00459a00.c),
[`004E73D0_FUN_004e73d0.c`](../../analysis/decompiled/gameserver-quest-async-completion/004E73D0_FUN_004e73d0.c),
and [`004E7250_FUN_004e7250.c`](../../analysis/decompiled/package-completion-trace/004E7250_FUN_004e7250.c).
[`00414930_FUN_00414930.c`](../../analysis/decompiled/mall-completion-refs/00414930_FUN_00414930.c),
[`004E9AE0_FUN_004e9ae0.c`](../../analysis/decompiled/mall-completion-refs/004E9AE0_FUN_004e9ae0.c),
and the generated caller/reference set in
[`mall-completion-refs`](../../analysis/decompiled/mall-completion-refs).

### AegisCore implementation boundary

AegisCore deliberately uses a separate MySQL model:

| Purpose | AegisCore tables |
| --- | --- |
| reference item | `ITEMDATA`, plus additive `REF_*_VSRO` catalogs |
| mutable item instance | `ITEM` (`VARIANCE`, `FK_REF_ITEM`, `STACK_COUNT`, `OPT_LEVEL`, `DATA`, rent fields) |
| character placement | `INVENTORY` (`FK_CHAR`, `SLOT`, `FK_ITEM`) |
| item options/protection | `ITEM_MAGIC_OPTION`, `ITEM_BINDING_OPTION`, `ITEM_ALCHEMY_PROTECTION` |
| mall catalog/audit | `ITEM_MALL_GOODS`, `ITEM_MALL_PURCHASE` |
| account balances | `SRO_ACCOUNT.USER.SILK`, `GIFT_SILK`, `SILK_POINT` |

The current mall transaction locks the account balance, loads the enabled
catalog row joined to `ITEMDATA`, checks inventory capacity, inserts one or
more `ITEM` rows plus `INVENTORY` links, debits the selected balance, inserts
`ITEM_MALL_PURCHASE`, commits, and reloads the inventory snapshot. Service
items instead update `CHAR_ITEM_SERVICE` and do not necessarily create a
normal inventory row. This is the maintained implementation contract, not a
claim that the stock TestIn server uses these table names.

### Implemented stock-protocol compatibility boundary

AegisCore keeps the binary-confirmed wire contracts in `protocol/vsro188/` and
leaves authorization, catalog resolution, persistence, and transaction
ownership in the Agent handlers and repositories. Current adapter coverage is:

| Stock surface | Protocol adapter | Application integration |
| --- | --- | --- |
| `0x7034` operation `0x18` request and `0xB034` package success/failure | `item_mall_codec.go` | `inventory_handler.go` validates and commits through the mall repository, then publishes the confirmed `0xB034`; the binary-confirmed state-only balance continuation does not directly emit `0x3153` |
| `0xB034` legacy item-mall operation `0x12` completion | `item_mall_codec.go` | legacy mall handler uses the centralized selector/destination/quantity envelope |
| `0x7034/0xB034` token package operation `0x2B` | `item_specialty_codec.go`, `package_item_repository.go` | exact request/success envelope, selected price-policy authority, atomic package insertion and slot-ordered token debit continuation |
| `0xB034` operation `0x06` pickup completion | `item_operation_codec.go` | ground-item pickup publishes `result=1, operation=0x06, destinationSlot` |
| `0xB034` operation `0x00` relocation completion | `item_operation_codec.go` | inventory relocation publishes source slot, destination slot, quantity, and an empty child vector |
| `0x7034/0xB034` operation `0x07` ground drop | `item_operation_codec.go` | handler locks and removes only the `INVENTORY` link, preserves the `ITEM` row, admits the item through `GroundItemService`, compensates with a restore link if admission fails, then publishes the exact slot-only completion |
| `0xB034` operations `0x08`/`0x09` ordinary economy completion | `item_economy_codec.go` | the maintained NPC buy path accepts the exact eight-byte `0x08` context form; the four-byte stock package-item form is length-demultiplexed into its selected-shop package transaction |
| `0xB034` operation `0x08` package-item success | `item_operation_codec.go` | exact selector/result-vector/quantity envelope is available for replay and future domain integration; result-vector entries remain opaque |
| `0x7168/0xB168` plus maintained `0xB034` buyback completion | `item_economy_codec.go` | dedicated NPC buyback handler commits through `EconomyRepository`, acknowledges on `0xB168`, then publishes the maintained destination/source/quantity envelope; stock updater operation `0x22` is not accepted as a `0x7034` request |
| `0xB034` avatar transfer completion | `item_operation_codec.go` | avatar equip/unequip publishes operation, source/destination, zero quantity, and reserved byte through the protocol codec |
| `0x3036/0x3038/0x3039` item entity notifications | `entity_item_codec.go` | pickup animation and equip/unequip world notifications are emitted through typed fixed-layout codecs |
| `0xB034` operation `0x17` COS transfer completion | `item_operation_codec.go` | COS pickup publishes `result=1, operation=0x17, contextU32, destinationSlot` |
| `0xB034` operation `0x0E` generated-item completion | `item_operation_codec.go` | combat-generated items and quest rewards publish `result=1, operation=0x0E, u8, u8`; the second byte remains opaque and is preserved as `0x3C` |
| `0xB034` maintained server item-removal completion | `item_operation_codec.go` | quest and alchemy material paths publish the existing `operation=0x0F, slot` contract |
| `0xB034` storage/gold success envelopes | `item_operation_codec.go` | storage handlers use neutral slot-pair, slot-pair-plus-quantity, and opaque-u64 builders after transaction commit |
| `0xB03E` repair response | `repair_codec.go` | repair handler emits the neutral success/failure envelope before durability and point updates |
| `0x304E` point updates | `point_update_codec.go` | inventory/economy paths emit kind-1 `u64` gold; skill/mastery/restoration paths emit kind-2 `u32` skill points |
| `0x3153` balance state | `item_mall_codec.go` | GameReady and the maintained legacy mall path use the neutral three-value codec; modern operation `0x18` deliberately omits a direct `0x3153` send because stock passes the state-only continuation flag |
| `0x704C` item-use request and `0xB04C` result envelope | `item_use_codec.go` | `item_use_handler.go` and present-point resurrection, including reverse-scroll selector/map validation |
| `0x7202/0xB202` skill withdrawal | `skill_restoration_codec.go` | `SkillRestorationHandler` validates the binary-confirmed item/reference/target request, commits through `SkillRestorationRepository`, consumes the restoration item, refunds skill points, and publishes the exact success/error plus state updates |
| `0x7203/0xB203` mastery withdrawal | `skill_restoration_codec.go` | `SkillRestorationHandler` applies mastery dependency checks, commits through the shared restoration transaction, consumes the restoration item, refunds skill points, and publishes the exact success/error plus state updates |
| `0x305C` item-used notification | `item_notification_codec.go` | item-use handler publishes the confirmed two-`u32` notification after the transactional use succeeds |
| `0x3040` quantity/state updates | `item_update_codec.go` | COS, alchemy material, and skill-restoration notifications |
| `0x3047/0x3049/0x3048` storage transfer | `storage_codec.go` | normal storage begin/data/end framing plus centralized capacity/slot/item-record envelopes; guild storage uses the same codec with its `0x3253/0x3255/0x3254` message IDs and centralized open/close responses |
| `0x7150/0x7151/0x7155/0x7157/0x716A` alchemy envelopes | `alchemy_codec.go` | staged request parsing plus centralized elixir, stone, socket, avatar, cancel/failure/material, and dismantle response envelopes; variable target bodies remain domain-serialized |

These adapters intentionally do not reproduce `AQ_StorageUpdater`, stock
stored procedures, or unresolved cash/result-column semantics. A codec test
proves byte order and widths; it does not promote an unresolved stock field
into an AegisCore business name. Remaining adapter work is differential
capture testing against the stock
client. Ground-item drop is implemented as a thin compatibility adapter over
the maintained domain model: the transaction locks the character and selected
inventory row, validates the maintained `RefItem.IsDropable` flag, deletes the
placement link while retaining the `ITEM` instance, and reloads the remaining
inventory before commit. World admission occurs only after that commit. If
admission rejects the item, the handler attempts a compensating `INVENTORY`
insert; a failed compensation is returned as an unknown cross-boundary outcome
so the session cannot continue with an untrusted snapshot. This ordering is
the AegisCore implementation contract and is not presented as a claim about
the stock `_STRG_DEL_ITEM` transaction boundary. Ground-item pickup is
deliberately emitted through the confirmed
`0x06` slot-only completion grammar; it does not append a rent block or a
variable item body because the stock `0x004F76E0` serializer does not emit
those bytes for that operation. COS transfer completion follows the same
rule: operation `0x17` emits only its context `u32` and slot. Alchemy response
codecs own the fixed envelopes and append an opaque target payload supplied by
the existing TypeID-driven item serializer; this keeps wire framing centralized
without inventing a second item-body model.

Storage framing is now centralized in the protocol layer as well. The codec
writes the eight-byte begin value, preserves the raw `0x3049`/guild-data byte
stream during 4-KiB chunking, and emits an empty end packet. It does not parse,
rename, or reconstruct the variable item records; those remain owned by the
existing TypeID-driven model serializers.

The machine-readable version of this boundary is
`protocol/vsro188/compatibility_ledger.go`. It records, for each recovered
item surface, whether the maintained domain is implemented, partial, framing-
only, or deliberately fail-closed. The ledger is an implementation contract,
not a claim that every stock callback has been reproduced. In particular,
modern mall operation `0x18` remains partial until result-vector/status and
cash-notification semantics are capture-correlated; specialty operation
`0x2B` is implemented as the recovered token package purchase; operation
`0x2C`, magic-cube lifecycle, and the unresolved TypeID3=`3` callbacks remain
fail-closed. This prevents a future
codec addition from silently becoming an unsafe item mutation.

The item-use notification is likewise isolated from the use transaction: the
protocol codec owns only `0x305C { u32, u32 }`, while the handler decides when
the committed item-use result should be published and what the two values are.

The generated-item and quest-reward paths use the separate operation `0x0E`
success grammar. The stock server serializer and vSRO client decoder both
consume exactly two scalar bytes after the operation discriminator. The
maintained handlers preserve the observed second value `0x3C`, but do not
label it as rent state, a reference ID, or an item type; appending any of
those variable blocks is incompatible with the confirmed client read path.

### Item-mall request status

The client binary now pins the click-to-network purchase family. The outbound
sender at `0x007978A0` (the decompiler-recognized source string is
`GInterfaceSend.cpp`) and the sibling sender at `0x00826460` both perform:

```text
IsOpcodeSupported(0x7034, 0)
construct/send opcode 0x7034
serialize local record with 0x007923A0(record, 0)
submit with 0x008418D0(record)
```

Both functions contain the exact diagnostic format string
`SendNetMsgItemMall : ShopID(%d), Tab(%d), Index(%d)`. Therefore `0x7034` is
not merely a pickup/use opcode in this client: it is also the item-mall
purchase request family, and the three logged selector fields are confirmed
caller inputs. The older sender additionally gates the path on the local mall
dealing counter and emits client-built `0xB034` error envelopes with observed
codes `0x1807` and `0x1880` on rejection branches; the binary does not assign
those constants reliable display meanings here.

The serializer is shared and discriminator-driven, but the mall-relevant
mode-0 `0x18` branch is now width-resolved:

```text
u16  record +0x12       // 0x0053D460 -> raw write width 2
u8   record +0x14       // 0x00509580
u8   record +0x02       // 0x00509580
u8   record +0x03       // 0x00509580
blob record +0xF4       // 0x0078F480: u16 length, then length bytes
u16  record +0x06       // 0x0053D440 -> raw write width 2
u64  record +0xB0       // 0x005DE520 -> raw write width 8
u32  record +0x0C       // 0x005095A0
```

The blob helper uses the client string/small-buffer representation at
`+0xF4`; its inline/heap branch is a storage representation choice, while
the serialized prefix is a 16-bit length followed by that many bytes. This
is a wire-width/order table, not a claim that the fields are semantically
named `shop`, `tab`, `index`, or `Silk` without a typed capture.

The server side now correlates this operation value. `0x004D8BF0` reads the
same one-byte operation discriminator and its `0x18` parse branch populates
the corresponding extended record. `0x0050F5C0` dispatches operation `0x18`
through table slot `0x00CD3C18` to `0x00526610`. That handler consumes a
`u16` at record `+0x12` and a `u8` at `+0x14`, resolves the catalog/reference
through `0x00523740`, then enters the inventory-add chain
`0x00618FE0 -> 0x00617A50`. The post-dispatch path through `0x0050FA50`,
`0x0045E080`, and `0x0045E620` now pins the package-item SQL boundary as
shown above. The completion path conditionally reaches `0xB034` and the
shared three-value balance-state writer through `0x004E9AE0`/`0x004F0940`;
the result-status mapping, explicit Silk-notification behavior, and stored-
procedure result contract remain unresolved. The package updater's
intermediate completion callback/relay is pinned in the section above.

The handler's diagnostic string provides the only stable server-side names
for these two selectors: record `+0x12` is logged as `shopGroup_id`, and
record `+0x14` as `shopIndex_InGroup`. The catalog resolver call visibly
receives the `+0x14` byte; the error log prints both values. This does not
prove that the client-side `ShopID`, `Tab`, and `Index` labels map one-to-one
onto these two fields, nor does it name the remaining operation-record
fields. Raw evidence: [`00526610_FUN_00526610.c`](../../analysis/decompiled/gameserver-item-7034/00526610_FUN_00526610.c).

For the successful operation-`0x18` response, the server serializer at
`0x004F76E0(operation, 1)` writes `u16 +0x12`, `u8 +0x14`, `u8 +0x02`,
`u8 +0x03`, a count byte derived from the result vector at `+0x68/+0x6C`,
that many contiguous result bytes, and trailing `u16 +0x06`. The matching
client `0xB034` item-record decoder reads the same `0x18` response grammar in
mode zero. This closes the package-response width/order correlation, while
leaving the result-byte semantics and stored-procedure column mapping
unresolved.

The client consumer's operation-`0x18` mode-zero reads are independently
visible in `0x0087EB50`:

```text
u16  response operation +0x12
u8   response operation +0x14
u8   response operation +0x02
u8   response operation +0x03
u8   resultCount
u8   resultBytes[resultCount]    // each passed to 0x00878A40
u16  response operation +0x06
```

`0x0087F370` then performs the operation-`0x18` registry lookup described
below and routes the resulting client-side item/state records through
`0x0087BA20`. The recovered `0x0087BA20` body performs inventory/equipment UI
and local item-state work; its recovered call graph contains no network-send
serializer or `0x3040` emission. Thus the binary-confirmed package success
notification is the `0xB034` operation response plus local client updates, not
a proven second item-change packet. This is a bounded negative result for the
recovered handler, not proof that no unrelated later subsystem can refresh the
inventory.

Raw client response evidence: [`0087EB50_FUN_0087eb50.c`](../../analysis/decompiled/shop-client-exact/0087EB50_FUN_0087eb50.c),
[`0087F370_CNetProcessIn__OnB034.c`](../../analysis/decompiled/client-quest-30d5/0087F370_CNetProcessIn__OnB034.c),
and [`0087BA20_FUN_0087ba20.c`](../../analysis/decompiled/client-quest-30d5/0087BA20_FUN_0087ba20.c).

The client-side completion lookup after that decode is now binary-confirmed.
In `0x0087F370`, the operation-`0x18` branch calls `0x007E28E0` with the
receiver at client object `+0x7CC`, a 32-bit key obtained from the result of
`0x00982750`, and the response operation-record bytes `+0x02` and `+0x03`:

```text
MOV EAX,[ESP + 0x37]       // operation record +0x03
MOV EDX,[ESP + 0x36]       // operation record +0x02
MOV ECX,[EBP + 0x7CC]      // lookup receiver
PUSH EAX
PUSH EDX
PUSH ESI                   // key = [0x00982750() + 0x04]
CALL 0x007E28E0
```

`0x007E28E0` walks the receiver's registry/tree linkage rooted at
`+0x3F8/+0x3FC`. For each candidate it calls `0x0097FA70` to obtain the
candidate's comparison records, then requires an exact match on one `u32`
key and two one-byte selectors. A match returns the candidate payload at the
observed node offset; an empty registry returns zero. The caller asserts when
the lookup returns zero and otherwise passes the returned value to the next
client completion step. This proves a client registry/completion lookup, not
the semantic identity of the key, selector bytes, or returned payload.

Raw bounded listings: [`0050F5C0_disasm.txt`](../../analysis/decompiled/gameserver-item-mall/0050F5C0_disasm.txt),
[`00617A30_00618FE0.txt`](../../analysis/decompiled/gameserver-item-mall/00617A30_00618FE0.txt),
the updater listings [`0050FA50_FUN_0050fa50.c`](../../analysis/decompiled/gameserver-quest-async/0050FA50_FUN_0050fa50.c),
[`0045E080_FUN_0045e080.c`](../../analysis/decompiled/gameserver-sql/0045E080_FUN_0045e080.c),
[`0045E620_FUN_0045e620.c`](../../analysis/decompiled/gameserver-sql/0045E620_FUN_0045e620.c),
the completion callback listings [`0045E490_FUN_0045e490.c`](../../analysis/decompiled/gameserver-sql/0045E490_FUN_0045e490.c)
and [`0045EE10_FUN_0045ee10.c`](../../analysis/decompiled/gameserver-sql/0045EE10_FUN_0045ee10.c),
[`004E7350_FUN_004e7350.c`](../../analysis/decompiled/gameserver-quest-async/004E7350_FUN_004e7350.c),
[`004F76E0_FUN_004f76e0.c`](../../analysis/decompiled/gameserver-quest-mission-wire/004F76E0_FUN_004f76e0.c),
and the client sender listing [`007978A0_disasm.txt`](../../analysis/decompiled/item-mall-client/007978A0_disasm.txt).
The response-lookup evidence is [`007E28E0_FUN_007e28e0.c`](../../analysis/decompiled/shop-client-exact/007E28E0_FUN_007e28e0.c),
[`007E28E0_op18_call.asm`](../../analysis/decompiled/shop-client-exact/007E28E0_op18_call.asm),
and [`0087F370_full_disasm.txt`](../../analysis/decompiled/shop-client-exact/0087F370_full_disasm.txt).

Cash-updater evidence: [`00459F70_FUN_00459f70.c`](../../analysis/decompiled/gameserver-sql/00459F70_FUN_00459f70.c),
[`0045A5D0_FUN_0045a5d0.c`](../../analysis/decompiled/gameserver-sql/0045A5D0_FUN_0045a5d0.c),
[`0045CE90_FUN_0045ce90.c`](../../analysis/decompiled/gameserver-item-mall-cash/0045CE90_FUN_0045ce90.c),
[`0045CB70_FUN_0045cb70.c`](../../analysis/decompiled/gameserver-item-mall-cash/0045CB70_FUN_0045cb70.c),
[`004F0750_FUN_004f0750.c`](../../analysis/decompiled/gameserver-sql/004F0750_FUN_004f0750.c),
[`004F0940_FUN_004f0940.c`](../../analysis/decompiled/silk-update-callers/004F0940_FUN_004f0940.c),
and the vtable ledger [`vtable-item-use-mutation.txt`](../../analysis/decompiled/gameserver-item-use-mutation/vtable-item-use-mutation.txt).

## SQL inventory map

| Lifecycle | Confirmed stock call/string | Evidence |
| --- | --- | --- |
| character bootstrap | `_Inventory` + `_ITEMS`, binding and rent joins | `0x0043D980`, `0x004548E0` |
| avatar/COS/chest/guild loads | `_InventoryForAvatar`, `_InvCOS`, `_Chest`, `_GuildChest` | `0x0043D2C0`, `0x00440CB0` and related loaders |
| linked storage | `_INVENTORYFORLINKEDSTORAGE`, `_STRG_ADD_LINKED_STORAGE` | `0x0043EEC0`, `0x00459F70` |
| expired rented item | `_DelExpiredRentItemInStorage` then storage-type slot lookup | `0x0043D2C0` |
| move/merge/split/swap | `_STRG_MOVE_ITEM`, `_STRG_MERGE_ITEM`, `_STRG_SPLIT_ITEM`, `_STRG_SWAP_ITEM` | `0x0045AF70`, `0x0045B5A0` |
| item exchange/participant transfer completion | `_STRG_MOVEITEM_NEW`; completion also updates `_Char.RemainGold` | `0x00523AA0` -> `0x0043F370`/`0x0043F3F0`, worker `0x0043FB20` |
| add generated item | `_STRG_ADD_EXPENDABLE`, `_STRG_ADD_ALCHEMYSTONE`, `_STRG_ADD_EQUIP_NORMAL`, `_STRG_ADD_SPECIALTY` | `0x0045A9A0` |
| add item with magic parameters | `_STRG_ADD_ITEM_MAGIC` with repeated `(optionID, optionValue)` pairs, bounded to `0..0x0C` parameters | `0x0045ADA0` |
| item-authorized skill/mastery restoration | `_skill_manage` operation `2` / `3`; generic item mutation for consumed restoration items | `0x005169D0`, `0x00516BB0`, `0x004587A0` |
| sold-item restoration (SQL-named; buyback is not independently proven) | `_STRG_RESTORE_SOLDITEM_EXPENDABLE`, `_STRG_RESTORE_SOLDITEM_ALCHEMYSTONE`, `_STRG_RESTORE_SOLDITEM_EQUIP_NORMAL`, `_STRG_RESTORE_SOLDITEM_SPECIALTY`, plus magic-option variant | `0x0045B970`/`0x0045BB60` |
| item mall package purchase, operation `0x18` | `_STRG_ADD_PACKAGEITEM`, `_STRG_ADD_PACKAGEITEM_WITH_STOCKQUANTITY` | `0x0045E080`, via `0x0050FA50` |
| separate cash-item insertion path | `_STRG_BUY_CASHITEM_EXP`, `_STRG_BUY_CASHITEM_EQUIP_NORMAL` | generic storage op `0x18` -> base updater `+0x30` (`0x0045A5D0`); live cash queue and result worker `+0x34` confirmed, direct client-mall `0x18` selector remains distinct |
| open market | `_OpenMarket_Insert`, `_OpenMarket_Update`, `_OpenMarket` item joins | `0x004535A0`, `0x00454270`, `0x00454490` |
| item audit/log | `_ADDLOGITEM` for log targets `9`, `10`, `0x0B`, `0x13` | `0x0048BB10` formatter, caller `0x0048BC80`; universal reachability unresolved |

### General `AQ_StorageUpdater` operation-to-builder selection

The switch in `0x00459F70` provides a second, lower-level operation map. It is
useful for SQL call selection, but it must not be confused with the
`0x0050F5C0` client-request table: the same operation byte can be routed into
different updater classes by different callers.

| Updater operation byte(s) | Binary-selected path |
| ---: | --- |
| `0x00`, `0x10` | `0x0045B5A0` move/merge/split/swap family; the helper selects the exact storage operation |
| `0x02`, `0x03`, `0x0A`, `0x0B`, `0x0C`, `0x1E`, `0x1F`, `0x20`, `0x21`, `0x23`, `0x24` | `0x0045AF70` storage-operation family |
| `0x06`, `0x0E`, `0x11`, `0x15`, `0x1C` | `0x0045A9A0` generated-item insertion (`_STRG_ADD_EXPENDABLE`, `_STRG_ADD_ALCHEMYSTONE`, `_STRG_ADD_EQUIP_NORMAL`, or `_STRG_ADD_SPECIALTY`) |
| `0x07`, `0x0F`, `0x12`, `0x16` | `0x0045A840` deletion (`{?=CALL _STRG_DEL_ITEM (...)}`) |
| `0x08`, `0x13` | `0x0045A9A0` insertion followed by the updater's rent/maintenance continuation |
| `0x09`, `0x14` | `0x0045A840` deletion followed by the updater's rent/maintenance continuation |
| `0x18` | base updater vfunc `+0x30` = `0x0045A5D0`, selecting `_STRG_BUY_CASHITEM_EXP` or `_STRG_BUY_CASHITEM_EQUIP_NORMAL` |
| `0x1D` | `0x0045B5A0` move/merge/split/swap family after the participant/state gate |
| `0x22` | `0x0045BB60` | sold-item restoration family; updater operation, not a proven `0x7034` request byte |
| `0x27`, `0x28` | `0x0045C9A0` linked/open-storage continuation |
| `0x29` | `0x0045A840` deletion helper for a resolved object |
| `0x2A` | direct `_STRG_ADD_LINKED_STORAGE` formatting path |

The SQL literal proves builder selection and formatting only. Queue admission,
stored-procedure result columns, transaction scope, and the client response
remain separate evidence questions.

The stock procedure bodies and database result-set contracts must be obtained
from the matching SQL database before documenting exact return-code or
rollback semantics. The executable alone proves formatting and call-site
selection, not atomicity.

### Updater operation `0x22`: sold-item restoration boundary

The binary separates updater operation `0x22` from the ordinary client
`0x7034` operation namespace. In the `0x7034` request parser,
`0x0050F5C0` rejects a parsed operation byte `0x22` with result `0x1811`
before indexing the dispatch table. The initializer at `0x0050CE80` also
leaves the table slot at `0x00CD3B58 + 0x22 * 8 = 0x00CD3C68` unset. Thus a
client sender for `C -> S 0x7034, operation 0x22` must not be documented from
the updater switch; the recovered evidence instead shows `0x22` being
constructed internally by a server-side item-restoration path.

`0x0042AAE0` takes a record-array context whose entries have an observed
stride of `0xE8`. It selects an entry with `0x00404F80`, checks the entry's
fields at `+0xD8/+0xDC` against player state at `+0x78/+0x7C`, reserves a
destination slot through `0x004B8260(0x0D)`, and constructs a runtime item via
the player/item factory chain. It then initializes an updater descriptor and
writes the following observed fields:

```text
descriptor +0x00 = 0x22                 updater operation byte
descriptor +0x04 = destination slot      (one-byte field in the local layout)
descriptor +0x10 = selected record index (one-byte field in the local layout)
descriptor +0x50 = record +0xE0          copied value
descriptor +0x54 = record +0xE4          copied value
descriptor +0x28/+0x2C = player          owner/context pointers
descriptor +0x58 = item virtual +0x4FC   copied 64-bit value
```

The local decompilation names the selected-index and context fields only by
stack locations; the table above records their widths/relationships without
assigning them item IDs, quantities, currency, or timestamps. The new item is
attached to the destination slot through `0x004B7E90`. The same path applies a
player virtual delta with action `0x23` using negative values derived from
record `+0xD8/+0xDC`; the executable does not establish the public currency or
field names for those values. It allocates/submits the storage updater through
`0x00459E20` -> `0x00459F70`. On an immediate setup/queue failure it changes the
descriptor operation to `0xFF` and destroys the newly constructed item. A
successful database result and any client response packet are not emitted by
this function, so their completion semantics remain open.

The updater builder at `0x0045BB60` validates the resolved item and selects
these exact stored-procedure format strings:

```sql
{CALL _STRG_RESTORE_SOLDITEM_EXPENDABLE (?, ?, %d,%d, %d,%d,%d, %I64d)}
{CALL _STRG_RESTORE_SOLDITEM_ALCHEMYSTONE (?, ?, %d,%d, %d,%d,%d,%d, %I64d)}
{CALL _STRG_RESTORE_SOLDITEM_SPECIALTY (?, ?, %d,%d,%d,%d,%d,%I64d, '%s')}
{CALL _STRG_RESTORE_SOLDITEM_EQUIP_NORMAL (%I64d, ?, ?, %d,%d,%d, %d,%d,%I64d,%d, %I64d)}
```

The normal-equipment branch is selected through the item's virtual type/state
checks. Items with magic parameters call `0x0045B970`, which bounds the item
data count at `1..0x0C`, copies up to twelve `(u64,u64)` pairs from item data
offset `+0x60`, and appends them to this exact prefix:

```sql
{CALL _STRG_RESTORE_SOLDITEM_ITEM_MAGIC (%I64d, ?, ?, %d, %d,%d, %d,%d,%I64d,%d, %d
```

The builder then appends one `%I64d` pair per parameter and closes with
`param_4,param_5`. The procedure names establish the stored-procedure
family's sold-item-restoration intent. They do not, by themselves, prove that
the record array is a UI buyback list, identify the SQL columns, establish
transaction/rollback behavior, or identify a wire response.

Raw evidence: [`0042AAE0_FUN_0042aae0.c`](../../analysis/decompiled/storage-updater-callers/0042AAE0_FUN_0042aae0.c),
[`0050F5C0_FUN_0050f5c0.c`](../../analysis/decompiled/gameserver-item-7034/0050F5C0_FUN_0050f5c0.c),
[`0050CE80_FUN_0050ce80.c`](../../analysis/decompiled/gameserver-item-7034/0050CE80_FUN_0050ce80.c),
[`0045BB60_FUN_0045bb60.c`](../../analysis/decompiled/gameserver-sql/0045BB60_FUN_0045bb60.c),
and [`0045B970_FUN_0045b970.c`](../../analysis/decompiled/gameserver-sql/0045B970_FUN_0045b970.c).

### Item audit/log SQL edge

The executable does contain a more concrete item-audit formatter than the
catalog-only wording suggests. `0x0048BB10` switches on the log record byte at
`+0x25`. Target values `9`, `10`, `0x0B`, and `0x13` select this exact format:

```sql
{CALL _ADDLOGITEM (%d,%d,%I64d,%d,
  %u,%u,%u,%u, '%s','%s', %I64d)}
```

The formatter visibly supplies a fixed first value `0`, record values at
`+0x68`, `+0x78/+0x7C`, `+0x04`, constants `0`, `99`, and `0xFF`, a byte at
`+0x80`, and a string/record-derived value from `0x0048B930`. Because the
variadic decompilation does not preserve every 64-bit/string argument boundary
reliably, the full stored-procedure parameter naming is intentionally not
derived from these offsets. Other target values select `_ADDLOGCHAR` or no
formatter in the same switch.

The recovered caller `0x0048BC80` performs the SMC-log/query setup and can
invoke `0x0048BB10`; its surrounding result/queue branch is not sufficient to
prove that every item mutation reaches this logger at runtime. Therefore the
safe claim is: `_ADDLOGITEM` formatting and a caller edge are binary-confirmed,
while universal per-operation reachability, query execution order, and audit
transaction semantics remain unresolved.

Raw evidence: [`0048BB10_FUN_0048bb10.c`](../../analysis/decompiled/alchemy-shardmanager/item-change/0048BB10_FUN_0048bb10.c),
[`0048BC80_FUN_0048bc80.c`](../../analysis/decompiled/alchemy-shardmanager/item-change/0048BC80_FUN_0048bc80.c),
and [`004883E0_FUN_004883e0.c`](../../analysis/decompiled/alchemy-shardmanager/item-change/004883E0_FUN_004883e0.c).

## Open evidence gaps

1. Correlate the recovered ordinary `0x7034` dispatch entries with typed
   client captures and the polymorphic `0xB034` emitter. In particular, the
   executable still does not justify the semantic labels sell, buyback,
   avatar, COS, or guild for every state/currency branch. The participant
   transfer operations `0x1A`/`0x1B` are the separately proven exchange/trade
   family. The mall
   operation `0x18` is correlated through the server dispatch table. Updater
   operation `0x22` is a separate case: the ordinary `0x7034` parser rejects
   that byte, while the server-side restoration constructor creates it
   internally; its initiating client request and response packet remain
   unresolved.
2. Correlate the now-complete `0x18` client/server width table with sanitized
   captures. The server names `+0x12` as `shopGroup_id` and `+0x14` as
   `shopIndex_InGroup`, but the remaining selector-field mapping and the
   package result-byte meanings are still unverified.
3. Trace the remaining `AQ_StorageUpdater` cash-item completion path from the
   pinned database-result worker to exact item-change, Silk-balance, and
   failure packets; do not assume a successful SQL return means a successful
   wire response without that edge.
4. Trace the remaining base cash-updater completion/queue path to exact
   item-change and failure packets, and match the three balance-state values
   to the downstream client state behind `0x00778E50`. The package updater now
   has static success and failure evidence: success can reach
   `0x004E9480`/`0xB034` -> `0x004E9AE0` -> `0x004F0940`, while ordinary failure
   reaches `0x004E9110` and the generic AQ failure/log-out path. The
   `0x3153` emission condition, field-to-player-offset mapping, wire widths,
   and client reason labels are confirmed; result-status mapping, cash-type
   semantics, and explicit operation-`0x18` `0x3154` notification remain
   unresolved.
5. Correlate the recovered `0x704C`/expendable TypeID fan-out with sanitized
   captures and catalog rows. The TypeID3/TypeID4 callback matrix, ordinary
   stack-consumption/deletion SQL edge, and return/reverse-return branches are
   now pinned, including the TypeID4=`10` action/state scheduling body;
   several service/summon callbacks still lack stable item names and distinct
   wire/SQL confirmation, and the TypeID4=`10` free-parameter semantics remain
   unresolved.
6. Match stock `_ITEMS`/storage result-set column order to a captured or
   recovered serializer; never derive it from the current MySQL schema order.
7. Add sanitized client/server captures for one successful and one failed
   path per family before promoting unresolved packet layouts.
8. Match the final skill/mastery state fields and any external capture for
   dedicated `0x7202`/`0x7203`. Their internal mutation routines,
   generic `+0x230`/`0x004E7350(1)` persistence boundary, status vocabulary,
   item-name allow-list, client state writes, and `B202`/`B203` response
   grammars are now confirmed; external capture correlation and public
   database/result-field naming remain open.
9. Trace the SMC-log caller branch around `0x0048BC80` far enough to prove
   which item/alchemy mutations execute `_ADDLOGITEM`, and correlate its
   stored-procedure result/queue behavior. The formatter and target-type
   switch are confirmed, but universal reachability and transaction semantics
   are not.
10. Complete the per-class server `+0x498` serializer/vtable map and correlate
    its variable item bodies with sanitized `0x3047/0x3049/0x3048` captures.
    The multipart framing, rent block, slot order, and client TypeID parser are
    confirmed; the remaining class-specific server serializer edges and
    capture-backed field semantics are still open.
11. Complete the atomic gold/package operation-`0x08` transaction using the
    now-maintained NPC/shop/tab/package authority chain and the recovered
    `_STRG_ADD_PACKAGEITEM_CORE` slot/count/creator rules. Operation `0x2B` is
    the separate token-payment variant and must debit token stacks rather than
    being labeled as specialty purchase.
