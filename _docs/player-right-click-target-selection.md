# Player right-click target selection: binary lifecycle and implementation

Assessment date: 2026-07-23

## Evidence identity

| Role | Binary | SHA-256 |
| --- | --- | --- |
| Client | `VSRO_Client/sro_client.exe` | `3DF2282DAB84EE5439944B3E6CA8E8CFC69CB05963A45F0F7F4B8ADCA579C206` |
| Server | `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe` | `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35` |

**Confirmed** below means a packet read/write, immediate, branch, memory access or call is present in the pinned binary. **Derived** means a stable interpretation follows from those operations and the runtime-class map. **Unresolved** means the bytes are confirmed but their authored semantic name is not.

This path is the ordinary `0x7045/0xB045` object selection family. The `0x7445/0xB445` names found in the preserved eSRO source are a different protocol profile and are not used by the pinned vSRO 1.188 binaries.

## Lifecycle

```text
right-click / object interaction
    -> client resolves an in-world object UID
    -> 0x0078F5F0 builds 0x7045 { targetUID:u32 }
    -> SR_GameServer dispatcher slot 0x00CD3F30
    -> 0x0050FE50 reads one UID and resolves the world object
    -> selected object's virtual +0x34 handles the request
    -> target-specific path builds 0xB045
    -> client CNetProcessIn::OnB045 at 0x0088B200
    -> resolve target object, parse its runtime-class tail, update selection/UI
```

### Client request

At `0x0078F5F0` the client:

1. avoids resending when the requested UID is already selected;
2. verifies that opcode `0x7045` is supported;
3. creates a `CMsgStreamBuffer` for `0x7045`;
4. writes exactly four bytes from the UID argument; and
5. sends it through `0x008418D0`.

The same sender is reached by several object click/interaction branches. Right-click is therefore an input producer, while `0x0078F5F0` is the shared wire boundary; the packet itself does not encode which mouse button initiated selection.

### GameServer dispatch

The incoming dispatcher initializer at `0x0050D0C0` installs `0x0050FE50` in slot `0x00CD3F30`, derived as opcode `0x7045`.

`0x0050FE50` selects the packet reader from player offset `+0x1048`, consumes the UID, and resolves it through `0x00485D90`. A missing object copies the packet cursor/end marker from `+0x103E` to `+0x103C` and emits the generic failure path with reason `3`. A resolved object receives a virtual call at target vtable slot `+0x34` with the selecting player and target object.

The larger target-selection implementation at `0x0052B040` confirms that the selector and selected object are admitted through object/event checks before `0xB045` is created. Its successful player-capable branch writes the accepted byte and target UID, appends target-specific metadata, and publishes the packet through player virtual slot `+0x27C`. Rejected lookup/type/state branches emit a failure response or discard the partially built packet through the global message manager.

## Opcodes and layouts

### Request `0x7045`

```text
targetUID:u32le
```

No action kind, mouse-button value, name, database ID or coordinates follow the UID.

### Response `0xB045`

Common prefix:

```text
result:u8
if result == 1:
    targetUID:u32le
    target-specific body...
else if result == 2:
    reason:u16le
```

The stock client at `0x0088B200` reads result first and resolves `targetUID` before selecting a runtime-class branch.

Confirmed/triangulated player body:

```text
result       u8 = 1
targetUID    u32
metadataA    u8 = 1
metadataB    u8 = 5
jobType      u8
if jobType represents an active job:
    jobLevel u8
```

For a non-jobbing player, the pinned server path emits `jobType = 4` and no following job-level byte. GO-SRO does not yet model player job state, so its implementation deliberately emits this exact no-job form.

Other existing target-specific bodies remain separate:

- mob: accepted, UID, health-present flag, current HP, then `01 05` selection metadata;
- service NPC: accepted, UID, object-specific counted task data;
- teleport building: accepted, UID, task count/tasks and conditional fortress tax.
- Dimension Hole `INS_QUEST_TELEPORT`: accepted, UID, then an empty counted
  action list (`00`). A live stock-client dump proved that omitting this byte
  asserts in `CMsgStreamBuffer` at `0x004F7220`, from `OnB045` at
  `0x0088B5FC`, after the five-byte packet is exhausted.

They must not be decoded or serialized as one universal selection structure.

## Structures

| Object/offset | Width | Confirmed role |
| --- | ---: | --- |
| incoming message/player context `+0x1048` | 4 | selects one of two packet-reader paths in `0x0050FE50` |
| incoming message `+0x103C/+0x103E` | 2/2 | cursor/end fields synchronized on failed lookup |
| selected world object vtable `+0x34` | function | object-specific selection handler |
| selecting player vtable `+0x278` | function | creates outbound packet for an opcode |
| selecting player vtable `+0x27C` | function | publishes/finalizes outbound packet |
| client `CNetProcessIn +0x78` | 4 | selected target UID/state destination used by `OnB045` |
| client resolved object `+0xF8` (`index 0x3E`) | 4 | UID passed to shared `0x7045` sender at click call sites |

The client `OnB045` has many runtime-class branches. The offsets above are observations for these exact builds, not stable source-level ABI declarations.

## SQL boundary

No SQL call is part of the target-selection transaction.

The client has no SQL/database boundary at all, as recorded in `vsro188-client-binary-reference.md`. On the server, the recovered `0x7045` handler, object lookup, event/admission path and `0xB045` serializers contain no ODBC wrapper, asynchronous query allocation, stored-procedure string or table reference. Selection uses already-live world/player state. Nearby party, exchange, quest, inventory and character-save procedures are separate actions and must not be attributed to right-click selection.

Therefore GO-SRO correctly performs no repository/database write when a player is selected.

## GO-SRO implementation

- `protocol/vsro188/inworld_codec.go` now owns the byte-exact player `0xB045` encoder, including the conditional job-level tail.
- `agent-server/handler/character/select_entity.go` admits only another live `*model.Player` whose exact object identity is in the selector's known-object list and whose global position is inside the established selection range.
- The same handler admits only a valid, exact `*model.DimensionHolePortal`
  identity already present in the selector's known-object list, applies the
  established extended structure range, and emits exactly
  `01 <UID:u32le> 00`.
- Self, unknown, stale, mismatched, malformed and out-of-range targets remain rejected without inventing persistence or portal-entry state.
- Existing NPC, mob and teleport-building response forms are unchanged.

## Remaining uncertainty

- The exact Windows input function that distinguishes right from left mouse button has not been assigned a stable semantic name; the shared UID sender and complete network lifecycle are confirmed.
- The human-readable names of the two constant player metadata bytes `1,5` remain unresolved. Their order and values are fixed by the recovered player response family and stock-client parser behavior.
- Active job state is not implemented until GO-SRO has a binary-backed runtime job model; only the confirmed non-jobging branch is emitted.
- Dimension Hole selection does not itself teleport the player. The exact
  post-selection entry opcode, owner-party admission path and destination
  authority remain unresolved and must be implemented separately.
