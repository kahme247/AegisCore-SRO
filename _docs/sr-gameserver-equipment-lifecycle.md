# SR_GameServer equipment lifecycle

This is a binary-first account of ordinary character equipment in the pinned
TestIn `SR_GameServer.exe`. It concerns an item moving between normal
inventory and body slots; avatar equipment, COS equipment, storage, alchemy,
and item creation are separate operation families.

## Evidence and confidence

- Binary: TestIn `SR_GameServer.exe`, SHA-256
  `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`.
- Saved program: `analysis/ghidra-projects/vsro-testin-gameserver:/SR_GameServer.exe`.
- Focused exports: `analysis/decompiled/equipment-lifecycle/` and
  `analysis/decompiled/equipment-lifecycle-calls/`.

**Confirmed** means that the stated bytes, branch, call, literal, or SQL text
is present in that binary. **Derived** means a deterministic interpretation of
those facts. **Unresolved** must not be treated as an implementation contract.
In particular, the current Go server's packet definitions are not evidence for
the stock server's packet layout.

## Lifecycle

```text
0x7034 item-operation family
        |
        | operation descriptor: source slot -> target slot
        v
CGObjPC storage-operation dispatch (0x00523860)
   | normal (0x0d..0x6f) -> body (< 0x0d): AttachEquipItem  (0x00524cd0)
   | body (< 0x0d) -> normal (0x0d..0x6f): DetachEquipItem  (0x005255e0)
   | normal -> normal: general inventory relocation
        |
        v
in-memory slot mutation / replacement handling
        |
        +--> SQL writer capability exists, but no direct edge from either
             attach/detach routine to a SQL submission was recovered
```

`0x00523860` is installed in the storage-operation callback table by
`0x0050CE80` (at `0x00CD3B58` and again at `0x00CD3BD8`). Its branch on the
two slot bytes is the direct evidence that ordinary equip and unequip are
subcases of a relocation operation, rather than distinct equip request
opcodes.

## Confirmed opcodes

| Direction | Opcode | Confidence | Meaning established here |
| --- | ---: | --- | --- |
| client -> server | `0x7034` | confirmed family | Item-operation traffic. A received-packet branch checks `0x7034` in `0x004ECBC0`, and the executable also constructs `0x7034` in `0x0055AC20`. The exact ordinary-item request decoder is not recovered. |
| server -> client | `0xB034` | confirmed family | Item-operation response family. `0xB034` is an immediate packet ID in the GameServer and is registered to `CNetProcessIn::OnB034` by the matching vSRO client. The success/failure body for ordinary equip has not been recovered from the GameServer binary. |

Do not claim that `0xB034` has a particular `success, operation, source,
target, quantity, reserved` layout from this evidence alone. That layout may
be useful emulator compatibility work, but it needs a decoded capture or a
recovered serializer to become binary-confirmed.

## Recovered operation descriptor and slot domains

The operation descriptor consumed by `0x00523860`, `0x00524CD0`, and
`0x005255E0` has these confirmed byte offsets:

| Offset | Meaning | Evidence |
| ---: | --- | --- |
| `+0x00` | operation discriminator; ordinary relocation is `0` | `0x00523860` only enters its relocation dispatch when this byte is zero. |
| `+0x01` | result/mutation state byte | attach and detach write `0`, `3`, and (attach only) `4` before the lower storage helpers. Semantic names for those values are unresolved. |
| `+0x03` | source slot | used for source lookup and mutation throughout all three routines. |
| `+0x04` | target slot | used for destination lookup and mutation throughout all three routines. |
| `+0x28`, `+0x2c` | two opaque context values copied into temporary relocation records | copied by both attach and detach; their object identity is not recovered. |

The binary enforces these domains for the ordinary branch:

- Normal inventory is the inclusive wire-slot range `0x0d..0x6f`.
- A move from normal inventory to a slot below `0x0d` selects
  `CGObjPC::AttachEquipItem` (`0x00524CD0`), except body slot `8`, which
  takes the separate `0x00524950` path.
- A move from a slot below `0x0d` to normal inventory selects
  `CGObjPC::DetachEquipItem` (`0x005255E0`), except source slot `8`, which
  takes `0x00524B20`.
- Normal-to-normal remains general relocation through `0x004B2450`.

The character's current inventory upper bound is read at `PC + 0x1C30`
(`param_1[0x70c]` in the decompilation). It is checked in addition to the
`0x0d..0x6f` wire encoding. Thus `0x6f` is the protocol ceiling, not a
guarantee that every character has that slot.

## Attach: normal inventory to equipment

`CGObjPC::AttachEquipItem` is identified by the embedded assertion source
path `GObjPC_StorageManipulator.cpp` and literal method name at `0x00524CD0`.
It rejects a same-slot move, out-of-range slots, unavailable source item, and
failed item/body compatibility predicate (a virtual call at item `+0x4f4`).

After validation, it handles replacement and paired equipment. The code
explicitly examines body slots `6` and `7`, finds a normal-inventory
destination with helpers seeded at `0x0d`, and can build a temporary move
record to evacuate a displaced item. Its item virtual predicates at `+0x8c`,
`+0x90`, `+0x9c`, `+0xa0`, and `+0xa4` influence this branch; their public
semantic names are not recovered. The code therefore confirms replacement and
paired-item constraints, but does not justify assigning names such as
"right-hand" or "shield" to slots 6 and 7 solely from this pass.

On the straightforward path it sets descriptor `+1` to `0` and calls
`0x004B7F80(source, target)`. Failure/replacement branches use state `3` and
`0x004B80F0`; one paired-equipment branch uses state `4` and `0x004B81E0`.
Those helper names and state labels remain unresolved, so the calls are the
reliable contract.

## Detach: equipment to normal inventory

`CGObjPC::DetachEquipItem` at `0x005255E0` is identified by the same source
file and its `CGObjPC::DetachEquipItem` assertion literal. It requires a
distinct source/target, valid source item, and an available normal destination
from the inventory search beginning at `0x0d`.

For source slots `6` and `7`, the routine has extra paired-equipment
handling: it may allocate one normal destination for the selected item and
another for a related displaced item, then constructs a temporary relocation
record. Other body slots take the simple path: descriptor `+1 = 0`, target is
a free normal slot, then `0x004B7F80(source, target)`. If capacity or the
relevant predicates fail, it leaves rather than forcing an in-memory move.

## Database boundary and SQL calls

No recovered direct call edge leads from `AttachEquipItem`, `DetachEquipItem`,
or their dispatcher to SQL formatting, job submission, or database execution.
The safe conclusion is that these routines prove the *in-memory equipment
transition*, not synchronous durable persistence or its transaction boundary.

The binary does independently prove these related storage-writer capabilities:

| Function | Confirmed SQL contract | What it proves |
| --- | --- | --- |
| `0x0045AF70` (within `AQ_StorageUpdater`) | `{?=CALL _STRG_MOVE_ITEM (%d,%d,%d,%d,%d,%d,%d,0)}` | The server has a queued storage-update procedure for an item move. Its seven arguments and exact operation-to-equip attachment are unrecovered. |
| `0x0045A9A0` | `{CALL _STRG_ADD_EQUIP_NORMAL (?, ?, %d,%d,%d, %d,%d,%I64d,%d)}` | Normal equipment creation/addition can be formatted as a storage-update job; this is not evidence that equipping an existing item invokes it. |
| `0x0045CE90` (`AQ_StorageUpdater::DoWork()` diagnostic) | executes accumulated query text through `0x009724C0` and treats DB errors or zero affected rows as failure | The writer is asynchronous job-style persistence, not a direct SQL call in the attach/detach routines. |
| `0x0042B630` | `SELECT ITEM.* FROM _Inventory AS INV JOIN _ITEMS AS ITEM WITH (NOLOCK) ON INV.ItemID = ITEM.ID64 WHERE INV.CharID=%d AND INV.Slot>=%d AND INV.Slot<%d AND INV.ItemID<>0 ORDER BY INV.Slot ASC` | Inventory/equipment hydration joins `_Inventory` to `_ITEMS` by `ItemID = ID64`; it does not prove an equip write. |

`_ADDLOGITEM` is also present as a separate asynchronous record family, but
there is no recovered edge from either equipment routine to it. Do not infer an
item audit-log write from an equipment transition without a caller trace.

## Implementation limits

- Treat `0x0d..0x6f` as normal-inventory wire encoding and check the runtime
  inventory bound as well.
- Preserve the special body-slot-8 branch and paired handling for slots 6/7;
  collapsing all body slots into one generic swap loses confirmed behavior.
- Do not emit, persist, or roll back a particular SQL procedure from an
  attach/detach failure based only on these functions.
- Recover the `0x7034` decoder and the ordinary `0xB034` serializer (or obtain
  a sanitized capture) before making their byte-level layouts normative.

Related evidence: [binary reference](sr-gameserver-binary-reference.md),
[inventory persistence discussion](sr-gameserver-drops-loot.md), and
[alchemy slot-domain evidence](sr-gameserver-alchemy.md).
