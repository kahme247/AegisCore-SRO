# SR_GameServer combat-skill execution lifecycle

This note is the binary-first reference for executing an already learned skill
in the pinned TestIn `SR_GameServer.exe`. Learning, mastery growth, and skill
withdrawal are documented separately in `sr-gameserver-mastery-skills.md`.
AegisCore and emulator behavior are comparison material, not evidence for the
claims marked **confirmed** here.

## Evidence identity and confidence

- Binary: `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`
- Size: `8,925,184` bytes
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Architecture: 32-bit Windows x86
- Saved program: `analysis/ghidra-projects/vsro-testin-gameserver:/SR_GameServer.exe`
- Focused exports: `analysis/decompiled/gameserver-combat/`,
  `gameserver-combat-core/`, `gameserver-combat-strings/`, and
  `gameserver-skill-systems/`

**Confirmed** means the instruction, field access, branch, serializer call,
diagnostic string, or SQL literal is present in this binary. **Derived** means
a direct interpretation of confirmed control flow. Anonymous function and
structure names below are descriptive.

## Opcode and dispatcher map

The entity dispatcher `0x004ECBC0` reads the message ID through the packet
object at `+0x1050`. It routes `0x7074` to `0x004ACC40`.

| Direction | Opcode | Confirmed role |
|---|---:|---|
| client to GameServer | `0x7074` | generic entity action: begin, stop/cancel, attack, learned skill, or buff removal |
| GameServer to client | `0xB074` | acceptance/action-state response emitted by the outer action controller |
| GameServer to client | `0xB070` | skill/attack begin, result block, or immediate cast failure |
| GameServer to client | `0xB071` | skill action completion/interruption |
| GameServer to client | `0xB072` | active buff/effect removal |

`0x7074` is not a skill-only opcode. The first byte selects the outer action
phase. On a start request the next byte selects an action kind. Confirmed kinds
handled by `0x004ACC40` are `1` through `5`; kinds `4` and `5` resolve a
learned-skill/reference-skill object, while kind `2` is the stop/cancel path.
The exact user-facing name of every numeric kind remains unresolved.

The stock wire forms corroborated by the packet reader and pinned client are:

```text
stop/cancel:       phase:u8
basic target:      1 | 4 | targetUID:u32 | 0
entity skill:      1 | 4 | skillID:u32 | 1 | targetUID:u32
position skill:    1 | 4 | skillID:u32 | 2 | region:i16 | x:i32 | z:i32 | y:i32
remove own buff:   1 | 5 | skillID:u32 | 0
```

The action-kind names in these layouts are descriptive. The bytes and field
widths are the protocol contract. The position tuple is serialized as X,
height/Z, map-axis/Y.

## Recovered runtime structures

### Connection/action controller

`0x004ACC40` uses these connection-relative fields:

| Offset | Shape | Confirmed use |
|---:|---|---|
| `+0x103C`, `+0x103E` | `u16` | packet/action sequence values copied on rejection |
| `+0x1048` | pointer/state | chooses one of two packet readers |

The dispatcher-owned action manager, reached through the handler's working
object, contains an active-action pointer at `+0x0C`, an owning entity pointer
at `+0x10`, and the last-start tick at `+0x18`.

### Action request object

The object allocated by `0x004AC910` is populated and advanced by
`0x004ACC40`, `0x004AD030`, and `0x004AD8E0`:

| Offset | Shape | Confirmed use |
|---:|---|---|
| `+0x05` | `u8` | action kind (`1..5`) |
| `+0x08` | pointer | learned skill or resolved reference object for kinds `4/5` |
| `+0x14`, `+0x18` | `float` | computed minimum/maximum reach envelope |
| `+0x1C` | `u32` | action phase/state, dispatched as states `0..3` |
| `+0x30` | pointer | target/skill actor-data record |

State `0` resolves and validates the action, state `1` pursues or closes range,
state `2` queues the engage message, and state `3` maintains a repeating
attack. This is a derived state naming from the confirmed switch in
`0x004AD8E0`.

### Skill actor-data record

The actor-data object allocated by `0x005AA0B0` is shared by targeting,
pre-engage validation, begin serialization, and damage/effect processing:

| Offset | Shape | Confirmed use |
|---:|---|---|
| `+0x08` | `u32` | reference skill ID |
| `+0x0C` | bit flags | target/position/multi-target and execution flags |
| `+0x10` | `u32` | selected target unique ID |
| `+0x14` | `u32` | auxiliary action value |
| `+0x24..+0x38` | six scalar fields | copied target position/bounds data |
| `+0x40`, `+0x44` | begin/end pointers | vector of eight-byte target entries |

Flag `0x01` indicates target-list data, `0x10` is temporarily set for one actor
class and cleared during keep-up processing, `0x20` is used for a synthetic
self/non-target invocation, and `0x40` adds an extra float to `0xB070`.

### Skill execution/change object

The execution object allocated in the `0x0059B480`/`0x00586700` path links:

| Offset | Shape | Confirmed use |
|---:|---|---|
| `+0x06` | `u16` | result family (`0x3000`, later `0x3002`) |
| `+0x08` | tick | `GetTickCount()` start time |
| `+0x0C` | state/flags | active/continuation state |
| `+0x10` | scalar | continuation counter/state |
| `+0x14` | actor-data pointer | target and request data |
| `+0x18` | skill-actor pointer | reference skill and computed costs/results |

The owning character skill manager is reached as `character + 0xA30`; the
pre-engage path repeatedly addresses its extended state as manager `+0xA30`
plus index `0x28C`.

## End-to-end execution lifecycle

1. `0x004ECBC0` routes `0x7074` to `0x004ACC40`.
2. The handler reads the phase and action kind, rejects kinds above `5`,
   allocates an action object, stores `GetTickCount()`, and reads target or
   skill identifiers into a new actor-data record.
3. Skill kinds resolve the learned skill (`0x0059D600`) or reference skill
   (`0x0046C1F0`). An unresolved reference aborts before engagement.
4. `0x0058D8F0` performs pre-engage validation. The outer controller also
   prevents duplicate concurrent actions against the same learned skill or
   target state.
5. `0x004AD8E0` resolves the target object, calculates effective range from
   both actors plus the authored skill range and modifiers, and either engages
   immediately or enters pursuit.
6. `0x0059B480` creates the skill actor/execution graph, resolves target-list
   and positional data, applies special reference-skill branches, and registers
   the action with the character.
7. `0x00586700` rechecks pre-engage conditions, calculates HP/MP costs, applies
   cost modifiers, resolves linked/parasite data, stamps the execution tick,
   and emits `0xB070` through `0x0059E8F0`.
8. Immediate skills enter effect/damage application. Timed skills retain the
   execution object in the manager and advance on later ticks. Multi-hit and
   keep-up skills reuse the actor graph rather than accepting a new client cast
   for every hit.
9. Completion or interruption calls `0x0059AE10`, which constructs `0xB071`.
   Buff removal uses the separate `0xB072` path.

An action is therefore not atomic at packet receipt. Acceptance, pursuit,
cast-begin, impact(s), and completion are distinct server states.

## Validation and failure contract

`0x0058D8F0` is the common pre-engage validator. Confirmed checks include:

- reference skill existence;
- actor alive/action-compatible state;
- sleep and stone-skill restrictions (with explicit
  `CheckSkillPreEngageCondition() -- Skill Cancel` diagnostics);
- target existence and target-type eligibility;
- target range and position;
- weapon/equipment requirements prepared from the reference catalog;
- HP/MP availability and ratio-derived resource cost;
- recovery/cooldown state;
- linked/parasite skill prerequisites;
- special fortress, seal-stone, monster, and scripted-skill exceptions.

Confirmed result values observed in this path include:

| Result | Confirmed trigger/role |
|---:|---|
| `0x3000` | initialized successful execution result |
| `0x3002` | engage accepted / active execution state |
| `0x3003` | unresolved or invalid reference skill |
| `0x3006` | linked/secondary validation failure family |
| `0x3009` | action forbidden by abnormal state such as sleep/stone restriction |
| `0x300E` | target/application rejection in the target-processing path |
| `0x3049` | special reference skill rejected because its required runtime owner/state is absent |
| `0x4004` | malformed, conflicting, or rejected outer action request |

`0x0059AD90` emits immediate `0xB070` failures for player-controlled actors;
non-player actors receive an internal cancellation callback instead. Several
codes are families with context-dependent UI text, so this note does not assign
a guessed one-line meaning to every `0x30xx` value.

### Hardcoded and parameter-driven pre-engage exceptions

The common validator does not contain a broad boss/fortress bypass. It
hardcodes exactly three reference-code comparisons in this function:

- `MSKILL_SD_HAROERIS_ATTACK04`;
- `MSKILL_SD_SETH_ATTACK04`;
- `MSKILL_SD_SETH_ATTACK10`.

Those three Sand Dungeon monster attacks set a local exception flag used by
the sleep/abnormal-state rejection branch. Independently, an actor whose code
matches `MOB_RM_SEALSTONE` is allowed through two otherwise failing abnormal
state masks. The separate manager stone-skill slot still produces `0x3009`;
neither exception is blanket immunity to every pre-engage check.

The first mask is explicitly bit `0x40` and the adjacent diagnostic labels it
sleep. The broader test is `flags & 0x4041`; its remaining `0x0001` and
`0x4000` producers are guarded by separate runtime bytes at `+0xD9C` and
`+0x13BC` and map to internal state values 10 and 9. Those state names are not
embedded and AegisCore has no equivalent owner fields. The actor lookup itself
is available through imported `RefChar.CodeName`, so AegisCore implements the
confirmed sleep exception for exact code `MOB_RM_SEALSTONE`. It deliberately
does not extend the exception to `MOB_RM_SEALSTONE_2` through `_8`, nor guess
that unrelated Go motion/status fields represent bits `0x0001/0x4000`.

Other exceptional admission is descriptor-driven. For `msch` at reference
`+0x4A4`, mode `1` resolves the required runtime owner from the request's
owner/key field, rejects an absent owner with `0x3006`, compares the actor
level against owner `+0x198` (`0x3008` on failure), and rejects conflicting
actor/body states with `0x3009`. Mode `2` rejects its conflicting equipment or
runtime singleton state with `0x3039`. The common path also uses distinct
descriptors for body/equipment restrictions, target region/subregion checks,
active-effect overlap, and linked target-list geometry; these remain authored
rules rather than hardcoded skill-name families.

Consequently, an implementation should preserve the three explicit monster
exceptions and the seal-stone actor exception narrowly, while dispatching the
rest from imported reference parameters. Prefix-based `MSKILL_`, fortress,
job, or boss exemptions would be broader than the binary.

## Resource consumption and cooldowns

The pinned reference-skill object exposes these execution fields:

| Ref-skill offset | Shape | Confirmed use |
|---:|---|---|
| `+0xCA` | `u16` | fixed HP consumption basis |
| `+0xCC` | `u16` | fixed MP consumption basis |
| `+0xCE` | `i16` | percentage HP component using actor maximum HP getter |
| `+0xD0` | `i16` | percentage MP component using actor maximum MP getter |
| `+0xD2` | `u8` | copied into the computed recovery/cost record |

`0x00586700` copies the fixed values, adds percentage components using virtual
getters `+0x108/+0x10C`, and then applies actor and skill modifiers before
engagement. It uses integer division by `100` for percentage components.

### Cast timing and cooldown registration

The timing fields immediately after the basic reference header are:

| Ref-skill offset | Loaded column | Runtime meaning |
|---:|---|---|
| `+0x70` | `Action_PreparingTime` | preparation duration |
| `+0x74` | `Action_CastingTime` | post-load cumulative prepare-plus-cast duration |
| `+0x78` | `Action_ActionDuration` | action/effect animation duration |
| `+0x7C` | `Action_ReuseDelay` | authored reuse interval |
| `+0x80` | `Action_CoolTime` | separate global/category cool-time interval |

During reference post-load, `0x005893E0` performs:

```text
runtimeCasting = Action_CastingTime + Action_PreparingTime
```

The timed engage path in `0x00586700` stamps `GetTickCount()`, emits `0xB070`,
and waits until elapsed time exceeds runtime `+0x74` before impact and `0xB071`.
The repeating-action controller `0x004AE590` uses `+0x74 + +0x78` as the
prepare/cast/action lifecycle interval, with an additional accumulated delay
for the flagged repeating variant.

The adjacent catalog columns `ACTION_FLYINGSPEED` and
`ACTION_INTERRUPTIBLE` are present in the SQL schema and are now retained in
AegisCore's `RefSkill` snapshot. The traced common execution/controller paths
do not read them as cast timing or cancellation inputs. The observed
neighboring byte checks belong to the auto-attack/target required fields. They
therefore remain data-only fields; no projectile-delay or interruptibility rule
is inferred here.

`0x0064C700` registers cooldown state when engagement begins. For ordinary
activity it uses `Action_ReuseDelay`. For activity byte `2` with a nonzero
action duration, it derives:

```text
lifecycle = runtimeCasting + Action_ActionDuration
tail       = max(0, Action_ReuseDelay - lifecycle)
scaled     = round(actionTimePercent * lifecycle / 100)
reuse      = scaled + tail
```

The divisor `100.0` is confirmed at `0x00B45AF0`. Thus the actor's timing ratio
scales the active lifecycle portion but does not alter any authored reuse tail
beyond it. The function separately records `Action_CoolTime` as a tick/value pair and
records the scaled activity-2 lifecycle as another tick/value pair.

Per-skill reuse records below `300000` ms remain in the local runtime map.
Intervals of at least `300000` ms additionally use wall-clock `_time32` and
send service message family `6`, subtype `200`, through `0x00655BE0`; this is a
long-cooldown persistence/coordination boundary. No SQL literal is formatted in
that GameServer path.

### Abnormal-status recovery registry (`real`)

The separately allocated `sSkillRecoveryStatus` type is not the ordinary cast
cooldown timer. The pool allocator `0x005ABDE0` allocates exactly `0x14` bytes,
`0x005A9FF0` marks the record live at byte `+0x04`, and `0x005AA020` is its
guarded destructor.

The reference parser `0x00587630` recognizes parameter tag `real`
(`0x7265616C`) and stores its three payload words behind reference-skill
`+0x300`:

| Descriptor offset | Confirmed use |
|---:|---|
| `+0x00` | abnormal/recovery category bit mask |
| `+0x04` | recovery value, copied into the status record |
| `+0x08` | recovery grade/rank key used for insertion/removal |

`0x0059DF20` fans the mask into 17 abnormal-status buckets. The confirmed bits
are `0x40`, `0x80`, `0x100`, `0x200`, `0x400`, `0x800`, `0x2000` through
`0x400000`, and `0x1000000`; their bucket bases run from manager `+0x14` to
`+0x194` in `0x18`-byte increments. One passive/effect can therefore register
recovery state for multiple abnormal families.

`0x00599840` adds or removes a recovery status in one bucket. On insertion it
stores the owning actor/skill key and `real` value in the `0x14`-byte record,
then indexes it using the `real` grade. Register flag `ESI=0` is used by the
effect-application path `0x00594AC0`; `ESI=1` is used by removal/teardown paths
`0x005829D0`, `0x00587210`, and `0x0059F0E0`.

The old `_RefSkill` database confirms 76 `real` rows. They are passive or
effect skills such as warrior abnormal resistance, cleric recovery passives,
rogue poison guard, and monster trap skills. Representative payloads are
`0x017FAFC0, 50, rank`, `0x01618600, 50, rank`, and
`0x001E2800, 50, rank`. Many have both authored reuse and cool time equal to
zero. This rules out interpreting `real` or `sSkillRecoveryStatus` as the
ordinary skill-cast cooldown mechanism.

AegisCore now retains the exact `real` descriptor tuple in `RefSkill` as
`RecoveryStatusMask`, `RecoveryStatusValue`, and `RecoveryStatusGrade`. The
enabled passive skills now register those descriptors on the owning player as
an actor-local recovery registry. Runtime evaluation still requires
actor-specific recovery semantics before the entries can safely alter abnormal
application or cure outcomes.
Active buffs carrying `real` descriptors use a token-keyed registry with the
same replacement, linked-child, expiration, and use-limit teardown paths as
their owning effect actors.
The actor registry exposes highest-grade mask-overlap lookup with deterministic
value/skill tie-breakers, matching the native grade-indexed registration
boundary; snapshots are sorted by grade/value/skill for replay stability,
without asserting how the selected value modifies a status.

This abnormal-recovery registry is independent of the cast timing and reuse
records above.

### Conditional/toggle effect reevaluation

`0x0059F0E0` walks the manager's active skill/effect list and reevaluates up to
five authored conditions from reference-skill `+0x3A0`. Confirmed condition
selectors include `1`, `2`, `3`, `4`, `6`, `9`, `10`, `11`, and `14`; they call
actor virtuals for equipped weapon/armor and other body-state predicates.
Reference field `+0x3B4` changes the combination logic.

For reference activity byte `+0x65 == 0`, a newly satisfied condition applies
the effect through `0x00594AC0` and sets active-object `+0x10 = 1`. When the
condition becomes false, the server removes every recovery-category entry,
detaches the effect from the owner container at owner `+0x1EC`, and clears
`+0x10`. Other activity values take a special cancellation path, including an
explicit `SKILL_CH_FIRE_SHIELD_` exception. This is equipment/state-driven
toggle behavior; no recurring HP/MP debit was observed in this reevaluator.

### Authored duration and keep-up MP debit (`dura`, `onff`)

The reference parser recognizes two independent lifetime parameters:

| Tag | Ref-skill offset | Payload | Confirmed runtime use |
|---|---:|---|---|
| `dura` (`0x64757261`) | `+0x280` | one millisecond value | hard execution/effect lifetime |
| `onff` (`0x6F6E6666`) | `+0x284` | interval milliseconds, MP cost | recurring keep-up debit |

When `onff` is present, `0x00593540` allocates an `sBuffOnOffData` object and
stores it at active skill actor `+0x60`. The pool allocator is `0x005AD460`,
the construction wrapper is `0x005AA390`, and the guarded destructor is
`0x005AA3C0`. The object field at `+0x08` is initialized/updated with
`GetTickCount()` and is the last-debit tick.

The tick dispatcher `0x005830B0` gives `dura` precedence. With a nonzero
`dura`, it tears down the execution once:

```text
elapsedFromExecutionStart > dura + activeSkillActor.durationExtensionMS
```

The execution start tick is at execution object `+0x08`; the signed extension
is active skill actor `+0x24`. A second action-controller path in `0x0048CEA0`
uses the same `dura` value as a direct lifetime from that controller's start
tick. Dead or invalid targets can still terminate the action earlier.

The recurring `onff` branch is reached only when `dura` is absent, the active
actor is in keep-up state (`+0x20 == 1`), and the `sBuffOnOffData` object is
present. When elapsed time exceeds the authored interval, it reads the second
payload as the MP cost, checks it against current MP through virtual `+0x10C`,
optionally transforms the cost through the modifier descriptor at reference
skill `+0x554`, and debits MP through actor virtual `+0x308(0, cost, 4)`. It
then refreshes `sBuffOnOffData +0x08` with `GetTickCount()`.

Insufficient MP enters the common `0x005829D0` teardown path instead of
allowing a partial debit. The old `_RefSkill` catalog confirms five-second
interval rows for Chinese ice/fire walls and Bard dances/guards; for example,
`SKILL_CH_COLD_BINGBYEOK_A_01` carries `onff = 5000, 41`. No recurring HP
debit is present in this branch: its availability getter and mutation call are
the MP-specific pair, and the first mutation argument is zero.

### Active-effect overlap and replacement

`0x0059D870` is the admission/replacement gate for an effect entering an
owner's active execution list at manager `+0x26C`. It is not a simple
same-reference-ID test. The parser fields used directly by this gate are:

| Source | Runtime offset | Confirmed role in `0x0059D870` |
|---|---:|---|
| authored `Action_Overlap` | `+0x8C` | primary overlap lookup key passed to `0x0059DB10` |
| `ovl2` | `+0x37C` | optional secondary overlap key; an existing match rejects admission |
| `dttp` | `+0x424` | two-word family/rank descriptor |
| `lks2` | `+0x374` | presence flag affecting same-group linked replacement |
| `refr`, discriminator `2` | `+0x290` | special reference/link-owner replacement branch |
| `msch` | `+0x4A4` | mode descriptor; mode `1` selects ordinary overlap admission |

For the ordinary branch, a live `ovl2` match rejects the new effect first;
otherwise a live `Action_Overlap` match rejects it. The specialized runtime
class-`3` branch walks existing active effects. When both skills have `dttp`
and their first payloads match, the second payload is the precedence value:
a lower incoming value is rejected; an equal or higher incoming value clears
the old active object's live byte at `+0x10` and admits replacement. The old
catalog supports this interpretation: `dttp` families carry a stable first
value and a second value that rises through the skill ranks.

For same-`GroupID` effects outside that `dttp` match, the gate compares
`Basic_Level`: a lower incoming level is rejected. `lks2`, active actor state
`+0x20`, and the `refr` discriminator-`2` owner/link path can suppress or
redirect that ordinary same-group replacement. When the `refr` link resolves
to another live execution, both the incoming and linked execution live bytes
are cleared together. A `dttp` match belonging to the same execution owner is
accepted without replacing itself.

The exact branch establishes two distinctions important to implementation.
An incoming `lks2` presence bypasses the ordinary same-group replacement test;
it is not itself a request to remove the old effect. An incoming `refr`
discriminator-2 descriptor first attempts the linked-object lookup, but when
that lookup does not resolve it falls through to ordinary group/level
replacement. The old catalog shows why `lks2` needs token-addressable active
effects: its dominant users are the Warlock burn, poison, bleeding, and
disease DOT families (groups 677-684), plus Bard MP steal. AegisCore now keeps
those abnormal actors independently by token, so their expiration and DOT
ticks do not overwrite or cancel a sibling actor of the same abnormal kind.
Ordinary non-`lks2` effects retain single-kind replacement behavior.

For discriminator-2 propagation families, the secondary execution contains a
runtime link record at subordinate `+0x68` carrying the source actor, child
owner UID, and execution identifiers. This is ownership state, not a fresh
radius query. The old catalog identifies Bard guard, dance, mana recovery,
speed/hit-rate, and Cleric group-recovery families as the principal player
users. AegisCore now records each propagated child token under its parent
token. Parent replacement, manual cancellation, duration expiry, or failed
recurring upkeep removes every registered child and rolls back its modifiers,
including recipients that have moved outside the original propagation radius.

Forced cancellation uses `0x0059EFF0`: it clears active-object `+0x10`; when
called with flag zero, a protected reference field at `+0x35C` refuses the
request unless actor state `+0x20 == 1`. `0x0059EF90` performs the corresponding
lookup by reference skill and optional actor key, accepting execution states
`1` and `2`; `0x0059F0C0` combines the lookup and forced cancellation. Final
detachment, linked-object removal, abnormal-recovery unregistration, and
manager-slot cleanup are centralized in `0x005829D0`.

## Targeting, damage, and effects

Target resolution uses unique IDs through `0x00485D90`. The actor-data target
vector stores eight-byte entries and can contain one or many targets. The
action controller includes both actors' collision/radius getters when deriving
reach. A target that is valid but out of range enters pursuit instead of being
immediately failed.

`0x005830B0`, `0x005857B0`, `0x00586700`, and `0x0058E5F0` form the principal
application chain. Confirmed behaviors include:

- physical and magical result construction;
- per-target hit records and multi-target iteration;
- critical, block, and status/result flags;
- linked and parasite skill execution;
- abnormal-status creation and rejection;
- direct and timed HP/MP recovery;
- buff/stat modifier installation;
- damage reflection/return and follow-up actor data;
- special handling for dead targets, resurrection-capable skills, monsters,
  fortress objects, and scripted boss skills.

`0x0059E8F0` serializes `0xB070` from the execution result. Its common prefix
is a success byte (`1`), result/action value, reference skill ID, source actor
value, execution/action ID, target value, and the result-flag byte at skill
actor `+0x4C`. The flags then select these confirmed blocks:

| Flag test | Required pointer | Serialized block |
|---:|---:|---|
| `flags & 0x01` | actor `+0x50` | target/hit result list through `0x00585730` |
| `flags & 0x0A` | actor `+0x54` | compact two-field result at subordinate `+0x06/+0x08` |
| invocation byte `+0x0C & 0x40` | none | one additional floating-point value |

The common target/hit block is no longer inferred from a capture. Root
serializer `0x00585730` writes its count/control fields and walks target
records; `0x005856D0` writes each target UID from target record `+0x08` and
walks its hit list at `+0x5C`. Per-hit serializer `0x005855F0` first writes the
attack-state/result-kind byte at hit `+0x05`. For kinds `0`, `4`, and `5`, it
then writes a 32-bit value formed as:

```text
(damage24 << 8) | damageStateByte
```

followed by hit `+0x08` (the abnormal/effect field). Kinds `4` and `5` append
fields at `+0x0C` and `+0x0E`. Kind `7` uses the same packed damage value but
appends fields `+0x1A` and `+0x1C` instead. Other kinds carry only the initial
state byte in this serializer. The high bit of that first byte remains part of
the wire value while the variant dispatch masks it off with `& 0x7F`.

This confirms AegisCore's ordinary hit ordering and 24-bit damage packing.
Producer `0x0058E5F0` resolves the three extended kinds:

- kind `4` is the authored `ko` (`+0x23C`) knock-over result. It calculates a
  server-authoritative displacement endpoint using the fixed knock-over
  distance path, writes its region and three coordinates into hit `+0x0C`
  onward, updates the target movement/action state, and invokes target virtual
  `+0x570`;
- kind `5` is authored `kb` (`+0x254`) knockback. It has the same endpoint wire
  shape, but scales the displacement vector by the authored `kb` distance and
  selects the target action-state value `0x10`;
- kind `7` is damage intercepted by an active `pw` barrier actor held at target
  runtime `+0xC0C`. Its packed damage value is the amount applied to the
  barrier. Field `+0x1A` is the barrier HP remaining after accumulated damage,
  clamped to zero for a negative result and emitted as `u16`; field `+0x1C` is
  the second `pw` payload--the authored initial/maximum barrier HP--also emitted
  as `u16`. Exhausting the barrier sets the high/dead bit in the result-kind
  byte and later teardown subtracts the absorbed amount from the barrier's
  runtime HP at subordinate `+0x5C/+0x08`. The old catalog confirms this shape:
  `SKILL_CH_COLD_BINGBYEOK_A_01` has `pw = 7, 824, 0, 11`, while later ranks
  increase the second payload and absorption percentage; Fire Wall uses the
  corresponding mask family beginning with `11`.

These kinds must not be flattened into an ordinary hit record merely because
their outer list framing is shared: kinds `4/5` carry movement endpoints and
kind `7` reports barrier consumption rather than direct target HP damage.

The remaining state-only forms carry no trailing hit payload:

- kind `2` is the block/no-damage record. The producer selects it from the
  target's block registry/state path (`0x006159E0` searches the applicable
  runtime record list), increments the blocked-result count, and skips ordinary
  damage fields. This matches the stock wire contract already used by
  AegisCore;
- kind `8` is a suppressed/no-direct-application marker. It is used when an
  active `pw` barrier's component mask intercepts the applicable direct hit and
  in common producer branches where neither the normal target nor its barrier
  should receive a damage payload;
- kinds `1` and `3` are assigned before per-hit calculation by special runtime
  state predicates. Kind `1` is selected when the source's active singleton at
  `+0xC20` resolves to an `msch` mode-`1` reference. Kind `3` is selected when
  either target virtual `+0x3D0` or `+0x3E0` returns one. Their packet behavior
  (state byte only) is confirmed, but this executable provides no stable UI
  name for those virtual predicates, so semantic labels remain intentionally
  unresolved.

The helper pair `0x006159E0/0x00615A20` respectively tests the target runtime
record list for a matching key and retrieves its associated `u16` value; these
are runtime combat lookups, not SQL calls.

### Linked actors and parasite propagation

The binary implements these as two different execution mechanisms.

Linked effects allocate a subordinate record at active skill actor `+0x68`.
Creation in `0x005830B0` records the source unique ID, linked target unique ID,
parent action/result identifier, and a use counter. The owning execution keeps
the linked actor in a manager list; an absent subordinate is diagnosed as
`Empty LinkSkillactor` and the live byte at linked object `+0x10` is cleared.

During damage resolution, `0x005A0B80` reads the linked skill's `lkdr`
descriptor at reference `+0x478`. The parser normalizes its first payload into
physical/magical component-mask bits. A second payload of `100` consumes the
whole current damage value; other values derive separate amounts from the two
damage components selected by the mask. The function:

1. subtracts the selected amounts from damage record `+0x24` and `+0x28`;
2. caps their sum to the record's total at `+0x20` and subtracts it there;
3. accumulates intercepted damage in a separately allocated result block at
   result `+0x74`; and
4. increments linked subordinate counter `+0x3C` when the optional third
   `lkdr` payload is nonzero, clearing linked-object `+0x10` when the authored
   use limit is reached.

The result block preserves the original action identifier, linked skill ID,
source ID, and accumulated amount, so this interception remains visible to
the later polymorphic result serializer rather than silently changing only
the target's HP.

Parasite propagation uses the `refr` descriptor whose first payload is `2`
(reference `+0x290`). `0x005830B0` walks a runtime entity set, excludes the
caster and invalid/dead entity classes, requires the candidate to be in the
same region and within the authored radius, and calls `0x0059D870` before
installing the secondary effect. Each accepted candidate receives a new
execution/effect graph with active actor state `+0x20 = 2`; the propagation
radius/value is copied to actor `+0x6C`, and the actor is registered through
`0x0059E220` plus the target's active-effect container. Its secondary overlap
key is registered through `0x0059DC00(..., 0)`.

A separate parasite-attack branch uses preallocated attack/result data at the
parent execution's `+0x1C/+0x20`, validates the referenced attack parameters
through `0x0058CB70`, applies the secondary attack through `0x0058E5F0`, and
then runs the ordinary result/status publication helpers. Missing `refr`
payload or missing parasite attack data is logged and does not fabricate a
fallback skill. Teardown unregisters the secondary overlap key with
`0x0059DC00(..., 1)` and removes the subordinate graphs through the common
`0x005829D0` path.

### `refr` discriminator-1 target geometry

The discriminator-1 payload is six words: discriminator, geometry type,
range/value, simultaneous target count, falloff, and target-class mask. The
dispatch table at `0x00C63F60` selects seven producers (`0x0058A020`,
`0x0058A7C0`, `0x0058B160`, `0x0058B870`, `0x0058BEF0`, `0x0058C170`, and
`0x0058CA60`). Types 1 and 2 are caster- and selected-target-centered searches;
types 3 and 4 construct directional geometry; type 6 performs progressive
acquisition. Types 5 and 7 enumerate owner/runtime collections rather than an
ordinary visibility search.

The per-target byte accumulator is initialized to `100-falloff`, representing
the primary hit, and each admitted target multiplies it again by
`100-falloff`, dividing by 100 with native rounding. Thus falloff 35 produces
42 percent for the first secondary and 27 percent for the second. It does not
produce 65 and 42 percent.

The geometry mask at descriptor offset `+0x14` is consulted before target
admission. Its low bit is directly confirmed as the caster/self admission
flag: the native producers skip the caster when bit `0x01` is clear. AegisCore
now applies that rule to propagated player healing targets; the remaining
relationship/owner bits are retained as runtime-owner boundaries rather than
being guessed from visible-object classes.

The old `_RefSkill` catalog also constrains the owner-specific boundary. All
production type-7 rows in this database are monster/script skills such as
`MSKILL_GOD_FLAME_WITCH_A1_ATTACK02`, Togui, Jupiter, Sand Dungeon, and clone
families, with mask 1. No type-5 discriminator-1 row exists in this catalog.
Those producers therefore cannot be modeled as an arbitrary visible-mob
fallback in the player handler. Conversely, type-6 masks 4/5/7 are used by
real Bard mana-transfer and Chinese/European chain-heal families; AegisCore
now applies those descriptors to eligible living party members with
progressive range acquisition.

The common handler exposes `ConfigureSkillOwnerTargetProvider` for a fortress,
boss, or scripted runtime that owns the required type-5/7 collection. The
provider output is sanitized for live mob targets, duplicate UIDs, and the
primary target; without that explicit owner integration, the transfer remains
rejected rather than being widened to the observer list.

The NPC MobAI manager exposes the analogous
`ConfigureSkillOwnerTargetProvider(func(*NPC, *Player, RefSkill, int) []*Player)`
boundary for owner-managed player targets. It applies the same alive,
primary, duplicate, and count checks and permits zero-range descriptors; no
default provider is installed.

### Exceptional effect-application boundary

Across the traced core application functions (`0x005830B0`, `0x0058E5F0`,
`0x00590680`, `0x00593F50`, and `0x00594AC0`), the only direct skill-code
special case is the `SKILL_CH_FIRE_SHIELD_` prefix in the conditional
reevaluation/teardown branch. When its authored conditions are no longer
satisfied, that prefix selects the explicit teardown flag used by the common
`0x005829D0` path.

The periodic evaluator at `0x0059F0E0` walks active effects and evaluates as
many as five condition entries from reference `+0x3A0`, including weapon and
equipment virtual predicates. Ordinary conditional effects can toggle their
installed modifiers off and back on. For an effect whose reference-side mode
byte is nonzero, a failed condition normally remains installed; Fire Shield's
prefix is the narrow exception that calls forced teardown instead. The old
catalog makes the relevant player condition concrete: every
`SKILL_CH_FIRE_SHIELD_*` rank contains `reqi = 4, 1`, the shield equipment
class. AegisCore now removes a self-owned active Fire Shield, rolls back its
modifiers, and publishes buff removal when a committed inventory transition
removes that shield.

Detection actors must remain distinct from parasite propagation. European
Rogue and item detect rows use `dtt(type, level)` together with `refr`
discriminator 2; this descriptor owns a detection area and does not clone the
caster's detect buff onto nearby players. AegisCore imports the `dtt` metadata
and excludes those rows from generic discriminator-2 propagation. Actual
The `hide(type, level, chance)` actor is separately binary-confirmed at
reference offset `+0x428`. Application in `0x00594AC0` maps type `1` to body
state `6` (Stealth) and type `2` to body state `7` (Invisible); common removal
in `0x005829D0` restores body state `0`. AegisCore now imports that descriptor,
projects active hide buffs into the corresponding body state, restores normal
state on replacement/cancellation/expiry, and performs directional visibility
reconciliation. A hidden player continues observing ordinary players and NPCs
while the hidden player is removed from their observer lists.

The visibility predicate at `0x0059E130` supplies the exact reveal gate. It
loads the target hide actor from owner slot `+0x1EC`, then checks either an
attached detector actor at `+0x220` using reference `dttp` (`+0x424`) or the
observer's self-owned detector at `+0xC4C` using `dtt` (`+0x420`). Both paths
use the same arithmetic: `(hideMask & detectorMask) != 0` and
`hideLevel <= detectorLevel`. This explains the catalog values without name
heuristics: mask `5` reveals hide mask `1` but not `2`, while masks `6` and `7`
cover both. AegisCore now applies this mask/level rule to active detection
buffs. `0x0059E130` performs no detector-radius comparison; ordinary world
visibility remains the spatial boundary. The pinned old DB confirms `dttp`
family `6` is confined to Detect/Descry skill rows.

Fortress objects, job actors, monsters, and scripted owners are otherwise
distinguished by actor virtual predicates, runtime manager slots, target-type
masks, and reference descriptors. They share the same result construction,
damage/effect dispatch, and teardown machinery wherever their virtual
contracts permit it. No generic `MSKILL_`, fortress, job, or boss prefix table
is consulted by these core effect functions.

This establishes the implementation boundary for normal skills: parameter and
actor-type dispatch are required, while exceptional world systems should
provide the relevant actor interfaces and manager state. Adding name-based
effect behavior beyond the confirmed Fire Shield branch would be guessed
behavior, not parity with this binary.

## Cancellation and buff removal

A phase-only `0x7074` is a legitimate stop/cancel request. It must not be
treated as a truncated start packet. `0x004ACC40` also rejects or replaces a
conflicting active action and can cancel pursuit before impact.

`0x0059AE10` emits `0xB071` with two confirmed layouts selected by its first
byte. Mode `1` writes `mode:u8`, `param3:u32`, and a zero `u32`. Other modes
write `mode:u8`, `param2:u32`, and `param3:u32`. For non-player actors it invokes the
internal actor-state callback before deciding whether a network packet is
needed.

Right-click removal is action kind `5`. It resolves the requested reference
skill and removes the owned active effect; `0xB072` is the client-visible
effect-removal notification. This is distinct from interrupting an in-progress
cast with `0xB071`.

## SQL and persistence boundary

No direct SQL formatter or stored-procedure literal is reached in the traced
ordinary cast, target, cooldown, damage, abnormal-status, or cancellation
path. In particular, `_skill_manage` belongs to learn/mastery/withdraw
persistence and is not called merely because a learned skill is cast.

The execution graph mutates authoritative in-memory actor state and marks or
queues character changes through virtual callbacks. Durable character HP/MP,
death, inventory/ammunition, and other state are handled by their owning
character/save systems outside this action handler. Short cooldowns are local
runtime objects. Cooldowns of at least five minutes additionally send service
message family `6`, subtype `200`, with the skill/reference key, wall-clock
time, and duration data. The GameServer constructs no SQL text in this path;
the downstream service/database implementation is outside this executable's
synchronous `0x7074` call chain.

AegisCore's resource repository adds compare-and-debit `CHAR.HP` and
`CHAR.MP` updates for supported casts. These are implementation-side
persistence operations, not claims about a literal GameServer SQL statement
in the binary; the native evidence still places durable flushing behind the
owning character/save service.

The imported `SkillData_*.txt` catalog was also scanned at the four
`CONSUME_*` columns: the shipped vSRO rows contain fixed MP costs but no
nonzero fixed HP, HP-ratio, or MP-ratio costs, and no recurring HP upkeep row.
The implementation retains those fields and gates for compatibility with a
different reference catalog without treating absent values as authored costs.

Special systems can cross separate persistence boundaries--for example siege
bless skills and item-backed services--but those are not evidence of a generic
combat-skill SQL call. The exact generic character-save SQL reached after dirty
HP/MP state is flushed remains unresolved and must not be labeled as part of
the synchronous `0x7074` transaction.

## AegisCore comparison

AegisCore currently matches several important contracts:

- one `0x7074` handler distinguishes cancel, basic attack, entity-targeted,
  position-targeted, self/area skill, and buff-removal forms;
- cast ownership, target, weapon, MP, range, and cooldown checks are
  server-authoritative;
- `0xB074`, `0xB070`, `0xB071`, `0xB072`, and buff/vitals packets are separate;
- MP and resurrection mutations use repository-backed atomic operations;
- multi-target damage, buffs, healing, cure, resurrection, movement skills,
  barriers, abnormal statuses, and cancellation have explicit paths.
- delayed utility, NPC-target, player-target, status, and movement casts now carry an
  actor-owned cancellation token; movement or explicit cancel invalidates the
  pending impact before damage/effect application and emits the family-specific
  payload-free `0xB071` completion/interruption frame.
- healing casts now retain resource debit and cooldown as pre-engage state but
  defer the target-vital mutation, persistence, transfer healing, and vitals
  packets until the authored preparation/casting impact; cancellation therefore
  cannot apply a late heal.
- buff casts now use the same impact boundary: active-effect admission,
  replacement, modifier/recovery registration, propagation, and buff packets
  occur after the authored cast interval, while cancellation emits only the
  completion/interruption frame and never installs the buff.
- resurrection keeps its repository-backed caster-MP/target-vitals transaction
  atomic but defers that transaction and the life/vitals publication until
  impact; a canceled cast cannot revive or debit the target path.
- barrier casts defer barrier replacement, root/movement control, barrier
  packets, and recurring drain registration to impact; pre-engage resource and
  cooldown state remain authoritative and cancellation leaves no barrier.
- movement casts now perform weapon/resource eligibility and destination/path
  validation before the resource/cooldown transaction; invalid navmesh paths
  therefore cannot consume a cast or leave cooldown state behind.
- targeted player buffs now enforce the authored `ACTION_RANGE` before
  pre-engage resource/cooldown state or impact scheduling; out-of-range ally
  targets are rejected without mutating the active-effect list.
- healing and resurrection now use the same direct world-unit `ACTION_RANGE`
  comparison as the other targeted skill families; no client-unit `/10`
  conversion is applied.
- `real` abnormal-recovery descriptors are parsed and retained as their
  binary-confirmed mask/value/grade tuple; enabled passive skills and active
  effect actors register them with teardown-safe ownership, while outcome
  evaluation remains an explicit runtime boundary.
- the ordinary hit serializer uses the binary-confirmed target/hit ordering,
  attack-state byte, and packed damage-state/24-bit damage value;
- `dura` and generic `onff` lifetimes are represented independently, including
  scheduled MP consumption and cancellation on failed debit for barriers,
  Chinese keep-up effects, and Bard dances/guards;
- normal attack, buff, status, healing, and movement casts now resolve the imported
  `REF_SKILL` resource tuple: fixed and percentage HP/MP costs are calculated
  from derived maximum pools, HP is compare-and-debited without permitting
  self-kill, mixed-pool failures roll back to the pre-cast snapshot, and
  resource mutations are persisted before the cast proceeds;
- prepare/cast/action/reuse timing preserves the authored reuse tail and tracks
  actor-wide cool time separately; an explicit action-time provider can now
  supply actor-specific scaling while the default remains the binary's 100%
  path;
- long reuse intervals at or above 300 seconds retain local cooldown state and
  can notify an explicit external coordination sink, matching the native
  service-message boundary without inventing SQL.
- ordinary overlap, `dttp` rank, same-group level, linked-damage use limits,
  discriminator-2 propagation, and result kinds `4`, `5`, `7`, and `8` have
  explicit runtime representations.
- `TargetGroup_Enemy_P` normal attacks now resolve live player targets through
  the same cast/cooldown/MP and hit-record lifecycle as NPC attacks, using the
  target player's derived defense, parry, block, barrier, vitals, and abnormal
  status state. Their supported discriminator-1 transfer types also walk live
  player targets (including progressive type 6 acquisition) with the same
  target-count and falloff lifecycle. Authored casting delays use the native
  delayed B070 begin / B071 end sequence rather than applying immediately.
  Player-target status-only rows use the same delayed status-cast framing and
  publish the resulting abnormal-state mask. No-target area status rows with
  `TargetGroup_Enemy_P` now enumerate nearby live player targets with the
  authored transfer-count cap. `hste` damage-to-HP/MP recovery is also applied
  and persisted for the caster after primary and propagated player hits.
  Burn, poison, and bleed statuses on player targets now schedule the native
  five-second damage ticks with token replacement checks, vitals persistence,
  and player-target result/life-state packets. Those periodic hits also pass
  through linked-damage interception and can emit a redirected linked target
  in the same result block.
  Knock-over/knockback PVP hits now use the same displacement endpoint and
  result-kind framing as NPC targets.
  Player-target mitigation now applies the native `tisc` damage-taken-up
  final factor after defense/attack-percent scaling, matching the existing
  NPC damage path; periodic player abnormal damage uses the same factor.
  The attacker-side `AttackDown` abnormal factor is likewise applied after
  PvP attack variance and before target mitigation for primary and propagated
  player hits. Source-side `urd`/combat-damage-percent modifiers are applied
  after PvP target mitigation for the same hit families, matching the NPC
  producer ordering.
  Equipped Chinese/European accessory absorption is now decoded from
  `AccessoryVariance` and `RefItem` ranges into derived physical/magical
  absorption stats; PvP mitigation consumes those values through the native
  division-before-defense formula. NPC `RefChar` absorption rates now use the
  same formula. Balance fields remain separate and are not populated by the
  current catalog.
  Linked-damage interception is applied to both the primary and propagated
  player-target hit producers; redirected damage is source-UID/known-object
  bounded, consumes the authored use limit, persists linked vitals, and is
  serialized as an additional target in the same result block.
  Player freeze, stun, sleep, bind, and petrify statuses now gate authoritative
  destination/forward movement requests until expiry; explicit stop and turn
  requests remain allowed so the client can recover and orient while controlled.
  Applying one of those control effects also immediately finalizes any active
  player movement through the world authority, matching the NPC status path.
  Active buff and linked-buff commits use the same authoritative combat
  timestamp as cast scheduling, so `dura`, hidden/detection projections, and
  propagated child expiry cannot drift from the native execution clock.
  Abnormal-status admission for both players and NPCs now uses that same
  execution timestamp for native equal-class replacement/rejection (`tisc`).
  Linked owner teardown walks nested child actors depth-first with cycle
  protection, removing descendant buffs before the owning child record.
  Targeted and propagated `TargetGroup_Enemy_P` skills now pass through an
  explicit PvP admission provider; absent a provider, the existing permissive
  behavior is retained.

Known parity limits:

- AegisCore uses simplified Go execution categories rather than the binary's
  polymorphic skill-actor subclasses and full parameter dispatch;
- actor-specific cast-speed sources still require their runtime status/equipment
  provider; the common handler now consumes that provider and defaults to 100%
  when no source is available;
- `lks2` independent abnormal actors, discriminator-2 fallback, and propagated
  parent/child teardown are implemented; non-player scripted owner graphs
  remain outside the player runtime;
- hide types 1/2 now drive Stealth/Invisible body state and directional world
  visibility; `dtt` and attached-actor `dttp` detection use the native
  mask-overlap and level reveal gate;
- discriminator-1 types 1-4 and 6 have explicit damage geometry, native
  falloff arithmetic, and multi-target healing; types 5/7 remain bounded to
  their unavailable native owner/runtime collections;
- state-only result kind `1` is implemented from the active `msch(1)` source
  singleton. Kind `3` is traced through target virtuals `+0x3D0/+0x3E0` to
  packed permanent-TypeID predicates `0x00482970/0x00482AB0`. Using the stock
  packing `cash + TypeID1*4 + TypeID2*32 + TypeID3*128 + TypeID4*2048`, those
  predicates decode to `(1,2,4,2)` (siege-object subtype 2) and `(1,2,5,*)`
  (every siege structure); both now produce the payload-free target-state
  result without applying damage;
- many fortress, job, boss, and scripted skill families are not implemented
  one-for-one;
- the current NPC mob-AI impact path emits the confirmed timed attack/damage
  lifecycle (including barriers and linked interception) and now applies
  direct target abnormal actors from imported `RefSkill` descriptors, including
  token-safe expiry, the MobAI execution clock, and five-second
  burn/poison/bleed ticks. Ordinary discriminator-1 transfer types `1–4/6`
  now aggregate live player targets into one damage-result block with the
  authored count/falloff/chain geometry. NPC owner-list transfers, buffs, and
  scripted actors still require the native actor collections and packet/result
  contract to be reconstructed; an explicit MobAI owner-target provider now
  supplies types `5/7` when that external manager is available, with no
  observer-list fallback;
- AegisCore persists pre-engage resource debits immediately and persists
  impact-time healing/resurrection mutations at their producers, whereas the
  binary trace shows in-memory execution plus deferred owning-system flushes;
- exact `0xB070` polymorphic result blocks are implemented only for supported
  execution families.

Implementation readiness from this pass is therefore:

| Area | Evidence state | AegisCore action |
|---|---|---|
| common cast/target/cost/cancel lifecycle | binary-confirmed | safe to align now |
| prepare/cast/action/reuse/cool-time arithmetic | binary-confirmed; active lifecycle scales by action-time percentage while authored reuse tail remains unscaled | implemented for normal player skills with an explicit runtime action-time provider and 100% default |
| long cooldown coordination | native intervals >=300000 ms additionally notify service coordination | local cooldown remains authoritative; optional long-cooldown sink is invoked with the player, skill, and expiry |
| fixed/ratio HP and MP consumption | imported `REF_SKILL` fields plus native pre-engage resource gate | implemented for attack, buff, status, healing, and movement casts with compare-and-debit persistence; resurrection and barrier retain their dedicated transactional/specialized cost paths |
| `dura` hard lifetime | binary-confirmed | implemented generically |
| `onff` recurring MP | binary-confirmed | implemented generically; no recurring-HP path added |
| overlap/rank replacement | binary-confirmed gate and controlling fields | ordinary/rank/group paths implemented; linked-owner branch remains |
| linked damage interception | binary-confirmed mutation and use limit | implemented for mob-originated and player-originated hits; redirected player damage is source-UID/known-object bounded, use-limited, persisted, and emitted in the shared multi-target result graph |
| parasite propagation | binary-confirmed admission and secondary actor lifecycle | six-word descriptors imported; discriminator-2 propagation implemented |
| discriminator-1 geometry | seven-way dispatch, fields, falloff, and owner-list boundaries binary-confirmed | types 1-4/6 and chain healing implemented; types 5/7 accept only an explicit configured owner-target provider and otherwise reject safely |
| ordinary `B070` hit list and `B071` layouts | binary-confirmed | safe for supported result kinds |
| `TargetGroup_Enemy_P` normal attacks | reference target group and common result producer confirmed | implemented for live player targets; explicit PvP admission provider covers peace/battle/job/cape integration, while fortress/job-specific owner policy remains external |
| player movement-control statuses | native control effects stop locomotion while active | freeze/stun/sleep/bind/petrify immediately stop active movement and gate destination/forward requests; stop/turn remain allowed |
| buff expiry clock | effect lifetime is created during the owning execution transaction | timestamped player and propagated-buff commits share the combat handler clock |
| abnormal replacement clock | equal-class status admission compares active expiry against current execution time | timestamped player/NPC status admission with wall-clock compatibility wrappers |
| linked owner teardown | child effects detach with their owning actor graph | recursive depth-first removal with cycle protection and descendant packet/stat cleanup |
| delayed-cast cancellation | interrupted execution must not reach the impact producer | actor-owned delayed-cast tokens invalidate utility, attack, PvP, status, and movement callbacks |
| `real` recovery registration | mask/value/grade actor records attach and detach with effects | parsed tuples register on enabled passives and active buffs; outcome evaluation remains actor-specific |
| result kinds `4/5` (`ko`/`kb`) | framing and producer semantics confirmed | implemented with authoritative endpoint variants |
| result kind `7` (`pw` barrier interception) | framing and producer semantics confirmed | implemented with absorbed and remaining barrier HP |
| state-only kinds `2/8` | block and suppressed application confirmed | implemented as payload-free variants |
| state-only kind `1` | `msch(1)` source singleton and framing confirmed | implemented as a payload-free source-state variant |
| state-only kind `3` | target permanent-TypeID predicates, packing, concrete siege families, and framing confirmed | implemented for siege-object subtype 2 and all siege structures |
| pre-engage named exceptions | three Sand Dungeon skills plus exact seal-stone actor confirmed | three skill exceptions and exact seal-stone sleep bypass implemented; unnamed `0x0001/0x4000` actor states remain unmapped |
| normal effect application | common parameter/actor dispatch confirmed | implement without name-based subclass guesses |
| NPC-originated `RefSkill` effect execution | common graph is binary-confirmed, but NPC owner/actor collections and result producers are not reconstructed in the current mob-AI path | timing, direct damage, barriers, linked interception, direct abnormal actors, ordinary transfer types 1–4/6, expiry, and periodic damage ticks are implemented; types 5/7 require the explicit MobAI owner-target provider, while buff/script effects remain an explicit runtime boundary |
| fortress/job/boss/script owners | external actor/manager contracts are system-specific | integrate through explicit runtime interfaces |

## Reproduction anchors and unresolved edges

- entity dispatcher: `0x004ECBC0`
- `0x7074` outer handler: `0x004ACC40`
- action-state engine: `0x004AD030`, `0x004AD8E0`
- pre-engage validator: `0x0058D8F0`
- execution graph creation: `0x0059B480`, `0x0059B8D0`
- core engage/apply: `0x00586700`, `0x0058E5F0`
- broad effect/damage dispatcher: `0x005830B0`
- `0xB070` failure/begin serializers: `0x0059AD90`, `0x0059E8F0`
- `0xB070` target/hit serializers: `0x00585730`, `0x005856D0`, `0x005855F0`
- target/hit result producer: `0x0058E5F0`
- runtime keyed-record lookup/value helpers: `0x005A1A20`, `0x006159E0`, `0x00615A20`
- `0xB071` completion serializer: `0x0059AE10`
- reference-skill parser/parameter dispatcher: `0x00587630`
- reference-skill post-load/weapon preparation: `0x005893E0`
- `real` abnormal-recovery dispatcher/status mutation: `0x0059DF20`, `0x00599840`
- abnormal-recovery allocation/destruction: `0x005ABDE0`, `0x005A9FF0`, `0x005AA020`
- conditional/toggle reevaluator: `0x0059F0E0`
- active-effect admission/replacement: `0x0059D870`
- active-effect lookup/forced cancellation: `0x0059EF90`, `0x0059EFF0`, `0x0059F0C0`
- common effect teardown: `0x005829D0`
- linked damage interception/result construction: `0x005A0B80`
- `onff` runtime data allocation/destruction: `0x005AD460`, `0x005AA390`, `0x005AA3C0`
- cast/reuse/cool-time registration: `0x0064C700`
- long-cooldown service boundary: `0x00655BE0`

Do not yet claim as exact:

- semantic names for every `0x7074` action-kind byte;
- cooldown overrides used by every exceptional skill subclass;
- stable user-facing meanings of state-only kinds `1/3`, base kind `6`, and
  other exceptional result subclasses;
- one-to-one meanings for all `0x30xx` result values;
- synchronous SQL persistence for ordinary casts;
- applicability of these addresses and layouts to the separate 1.274 binary.
