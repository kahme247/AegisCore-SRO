# SR_GameServer movement recovery versus AegisCore

This note records the movement lifecycle recovered from the pinned TestIn
`SR_GameServer.exe` and compares it with the maintained AegisCore Agent. It
separates facts proved by executable control flow from client-only evidence and
protocol names.

## Evidence identity and confidence

- Binary: `SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Client request dispatcher: `0x0050D0C0`, `0x0050EEE0`
- Periodic movement integrator/correction path: `0x004B0490`
- Movement admission and `0xB021` producer: `0x004B0EA0`
- Authoritative position/correction producer: `0x004A9430`
- Script/AI movement request producers: `0x00540EE0`, `0x00540F70`, `0x00541110`
- Related action relay: `0x004D2200`
- Pinned-client request construction and response parsing: client functions
  `0x009EBBF0`, `0x009EB6E0`, `0x009EB470`, `0x008A5750`,
  `0x008A4EC0`, and `0x008A4F80`

In this document **binary-confirmed** means that the value participates in a
decoded instruction, branch, packet construction/parser, or call path in the
GameServer. **Client-confirmed** means exact construction/parsing in the pinned
vSRO 1.188 client but not yet an independently recovered GameServer handler.
Names taken only from opcode lists are not promoted to binary-confirmed.

## Recovered lifecycle

### 1. Admission

`0x004B0EA0` is the central recovered movement-admission/initialization path.
Before producing movement it rejects or returns for several actor states:

- missing owning/world object at actor offset `+0x12C`;
- restricted player/COS state reached through virtual slots `+0x1C` and
  `+0x540`;
- state bytes returned by virtual slot `+0xFC` equal to `8`, `16`, or `18`;
- a non-zero result from virtual slot `+0x5A0`;
- actor flags at `+0xD34` matching `0xC1` or `0x4000`;
- failure of the position/path validation operation at virtual slot `+0x4B4`.

When admitted, the routine cancels one existing state for player actors,
updates the motion-state object at actor offset `+0xA30`, constructs `0xB021`,
serializes the actor unique ID from `+0x08`, serializes the movement structure,
sends/broadcasts it, and invokes virtual slot `+0x55C` to transition the
actor's movement state. Player actors also notify a subordinate controller
with operation `6`.

The exact semantic names of every rejected state are not embedded in the
binary. The offsets and branch values are confirmed; labels such as stunned,
teleporting, dead, or rooted would currently be inference.

### 2. Authoritative simulation

`0x004B0490` is a periodic movement integrator. Its surrounding movement
controller contains an actor pointer at `+0x04`, a world/path object at
`+0x08`, two 16-bit distance/speed bounds at `+0x0C/+0x0E`, and the previous
tick at `+0x14`.

The routine:

1. requires an active movement/world object;
2. throttles processing with `GetTickCount()` to at most one pass per 100 ms;
3. rejects a terminal actor state returned by virtual slot `+0xF8`;
4. reads the authoritative Cartesian position at actor offsets
   `+0x88/+0x8C/+0x90` and the six-field nav position at `+0x7C..+0x90`;
5. resolves movement speed through actor/world virtual slot `+0x560`;
6. advances along a normalized direction vector and projects the result back
   through the world/navmesh helpers;
7. stops when the remaining distance reaches the configured bound, when path
   projection fails, or when actor/path state disallows the step;
8. emits an internal `0x7021` movement message (or `0x70C5` for one special
   controlled-actor case) after a corrected step;
9. emits a small `0x704F` state notification in one terminal branch.

The same function handles both ordinary progress and correction/arrival. It
does not trust the client to stream authoritative coordinates each tick.

### 3. Correction and stop

`0x004A9430` is an authoritative position update path. When its actor type is
eligible, it applies the position, optionally constructs `0xB023`, writes the
actor UID, serializes the current position, and sends the packet. It then
copies the six current nav-position fields from actor offsets `+0x7C..+0x90`
to a saved snapshot at `+0xA10..+0xA24`, clears movement for two recovered
actor-state values, and notifies the player controller with operation `8`.

This establishes `0xB023` as a correction/authoritative-position packet, not
the normal per-tick movement broadcast.

### 4. Forward and turn control

The GameServer binary contains executable uses of request opcodes `0x7023`
and `0x7024`. `0x00540EE0` constructs `0x7023` with a 16-bit angle derived from
a normalized angular value. `0x004D2200` also relays `0x7023` and `0x7024` for
two distinct action kinds. `0x00541110` constructs the adjacent `0x7022` when
a region/position mode bit changes.

The pinned stock client independently confirms both request bodies. Constructor
`0x009EB6E0` writes one `uint16` heading to `0x7023`; constructor `0x009EB470`
writes one `uint16` heading to `0x7024`. AegisCore treats those two-byte forms
as canonical and retains its older one/three-byte forms as compatibility only.

## Opcode and structure inventory

| Opcode | Direction/role | Evidence | Confirmed body |
| --- | --- | --- | --- |
| `0x7021` | client/internal movement request | GameServer producers; pinned-client constructor | Client-confirmed normal forms: `u8(0), u16 heading` or `u8(1), i16 region, i16 x, i16 y, i16 z` |
| `0x7022` | internal region/position-mode transition | GameServer `0x00541110` | construction confirmed; complete semantics unresolved |
| `0x7023` | forward/control request | GameServer `0x00540EE0`, `0x004D2200`; client `0x009EB6E0` | `u16 heading` |
| `0x7024` | turn/control request | GameServer `0x004D2200`; client `0x009EB470` | `u16 heading` |
| `0x70C5` | special controlled-actor movement | GameServer `0x004B0490` | movement position plus controlled-object ID in recovered branches |
| `0xB021` | movement broadcast | GameServer `0x004B0EA0`; pinned-client parser | starts `u32 entityUID`; client confirms destination/directional union and trailing control byte |
| `0xB023` | authoritative position/correction | GameServer `0x004A9430`; pinned-client parser | `u32 UID, u16 region, f32 x, f32 y, f32 z, u16 heading` |
| `0xB024` | authoritative angle update | GameServer `0x004B1360`; client `0x008A4EC0` | `u32 entityUID, u16 heading` |

The six-field actor nav position repeatedly copied by the binary is:

```text
+0x7C  field0:u32
+0x80  field1:u32
+0x84  region/mode:u16 plus adjacent state
+0x88  x:f32
+0x8C  y:f32
+0x90  z:f32
```

The field names before `x/y/z` remain unresolved. Packet serializers compress
or transform this internal form depending on the region-mode high bit.

## SQL and persistence boundary

No SQL formatter, ODBC execution routine, stored-procedure string, or direct
character-position update is reachable in the recovered `0x7021` admission,
100 ms integration, `0xB021`, or `0xB023` paths. Ordinary movement is an
in-memory world operation.

The binary-wide SQL catalog likewise contains no movement-specific query that
can safely be attributed to these functions. This does **not** prove that
positions are never persisted. It proves only that persistence is outside the
hot movement path, normally at a character save/logout/world-transfer
boundary. An exact legacy save query must be documented only after tracing
that separate boundary to its SQL command builder.

AegisCore intentionally differs: its position queue eventually executes the
parameterized statement:

```sql
UPDATE `SRO_SHARD`.`CHAR`
SET POS_X=?, POS_Y=?, POS_Z=?, FK_REGION=?
WHERE ID=?
```

It coalesces dirty snapshots, checkpoints them periodically, flushes on
important region/disconnect boundaries, and retries failures with bounded
backoff. That is a modern durability policy, not binary-confirmed TestIn
behavior.

## AegisCore comparison

| Concern | SR_GameServer binary | Current AegisCore |
| --- | --- | --- |
| Request ownership | shared movement/action dispatch and actor virtual state | one Agent handler serializes `0x7021/23/24` globally |
| Destination authority | server world/nav projection and actor-state gates | normalizes global coordinates, resolves navmesh, and validates each step |
| Tick cadence | explicit 100 ms lower bound | world simulation uses Go time deltas and configured update scheduling |
| Movement speed | actor/world virtual speed, including controlled-object contribution | authoritative player run speed used by motion resolver |
| Collision/path failure | terminates/corrects through world helpers | swept navmesh resolution, collision stop, generation-checked commit |
| Region transition | internal position mode and special `0x7022/0x70C5` paths | normalized cross-region coordinates, visibility repartition, immediate persistence flush |
| Normal broadcast | `0xB021` | destination to self and known; directional to observers only because stock client rejects self echo |
| Correction | `0xB023` and saved-position snapshot | `0xB023` on stop/collision/correction |
| Forward/turn | distinct `u16 heading` requests and `0xB024` turn broadcast | exact stock forms first; compatibility variants retained; combat canceled and turn synchronized with `0xB024` |
| Persistence | no SQL in hot movement path; exact save boundary unresolved | bounded coalescing queue plus explicit parameterized `CHAR` update |

## Parity conclusions and remaining gaps

AegisCore matches the recovered high-level authority model: the request starts
intent, the server advances and validates movement, observers receive
`0xB021`, and `0xB023` repairs authoritative position. Its navmesh validation,
generation-checked updates, visibility changes, and non-blocking persistence
are stronger and more explicit than the recovered legacy code.

The following must not yet be described as exact SR_GameServer parity:

- semantic names for all admission flags and virtual predicates;
- dungeon/extended-region world ownership and simulation (the high-bit region
  plus three `u32` coordinates is decoded, but authority rejects unowned planes);
- the precise legacy character-save trigger and SQL statement;
- whether every actor class uses the same 100 ms integrator;
- the source-client echo policy for every `0xB021` branch.

The supporting decompilations for this pass are retained under
`analysis/decompiled/gameserver-movement/`; they are reverse-engineering
artifacts, not maintained application packages.
