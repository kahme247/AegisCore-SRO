# SR_GameServer combat damage variance

This note records the damage-variance behavior recovered from the pinned
TestIn `SR_GameServer.exe`. It distinguishes binary-confirmed arithmetic from
the surrounding combat work that still requires recovery.

## Evidence identity

- Binary: `SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Primary physical variance routine: `0x0040DCE0`
- Parallel magical variance routine: `0x0040DFF0`
- attack/parry and level center routine: `0x0040FDD0`
- positive level-difference helper: `0x0040FD60`
- physical final-damage routine: `0x0040E450`
- related physical final-damage path: `0x0040EBE0`
- magical final-damage routine: `0x0040E830`

The implementation is in `agent-server/model/combat_variance.go`. Player
integration is in `agent-server/handler/character/combat.go`; monster
integration is in `agent-server/manager/mob_ai_manager.go`.

## Confirmed algorithm

Attack Rating and Parry Ratio do not form a hit/miss probability. Normal
attacks land unless an independent shield Block Rate roll blocks them. The two
ratings instead select a center within the attack's minimum/maximum range.

Given attack rating `AR`, target parry ratio `PR`, attacker level `AL`, target
level `TL`, and attack range `[min, max]`:

```text
levelBonus = clamp((AL - TL) * 0.03, 0.00, 0.30)
center     = clamp((AR / PR) * 0.50 + levelBonus, 0.10, 0.90)
```

When `PR` is zero, the center reaches the confirmed upper clamp of `0.90`.

SR_GameServer then consumes four C `rand()` results. For the first three it
converts each result from `[0, 32767]` to `[0, 1]` and retains the minimum:

```text
deviation = min(rand1 / 32767, rand2 / 32767, rand3 / 32767)
```

The parity of the fourth result chooses the direction:

```text
point = center - deviation  // even fourth roll
point = center + deviation  // odd fourth roll
point = clamp(point, 0, 1)
damage = min + point * (max - min)
```

Taking the minimum of three uniform values concentrates ordinary results near
the attack/parry center while retaining the full authored damage range.

For reference skills, the authored minimum/maximum additions join the physical
or magical base range before variance. `AttackPercent` is applied later, after
target mitigation. Absolute-damage skills bypass variance.

AegisCore now preserves that split: `playerVarianceDamage` receives the base
range plus authored minimum/maximum only, while the resolved hit applies
`AttackPercent` after defense. A non-positive authored percentage follows the
binary caller's ordinary-attack default of `100`.

## Confirmed post-variance mitigation

Further caller recovery establishes that the skill's minimum/maximum attack
additions participate in variance, but its authored attack percentage is
applied after target mitigation.

`0x0040E450` obtains target stat slot `5` (flat physical defense) and slot `7`
(physical absorption). `0x0040E830` uses the parallel magical slots `6` and
`8`. Both paths perform division before flat subtraction:

```text
physical = max(0, varianceAttack / (1 + physicalAbsorption / 100)
                  - physicalDefense)
magical  = max(0, varianceAttack / (1 + magicalAbsorption / 100)
                  - magicalDefense)

skillComponent = mitigated * AttackPercent / 100
```

This disproves both a plain `attack - defense` implementation and the
historical emulator formula `attack * (1 - absorption/100) - defense`.

The instruction listings establish this later common ordering:

1. apply the skill percentage and its paired attacker stat modifier;
2. add the special target-class component from attacker stat slot `0x103`;
3. apply the attacker/target skill relation multiplier (`0x00410BB0`) when
   that helper returns a non-zero value;
4. apply the flag-`4` multiplier based on attacker slot `0xBD + 2`;
5. on the physical path only, double that multiplier for flag `2` (critical);
6. when the attacker level exceeds the target level, multiply by
   `1 + clamp(levelDifference * 0.03, 0, 0.30)`;
7. apply the physical (`0x00410A00`) or magical (`0x00410AB0`) attacker
   scaling helper;
8. run the low-damage correction described below;
9. apply attacker then target final-damage stat factors (`0xB2`/`0xB4` for
   physical and `0xB3`/`0xB5` for magical) when non-zero;
10. clamp to `[0, 16,777,215]` and truncate toward zero.

The low-damage test is strictly `damage < varianceAttack * 0.05`. On entry,
the binary computes `upper = damage * 10 / 100`, forces `upper` to `1` when it
is non-positive, consumes one additional C `rand()` result, and replaces the
damage with:

```text
damage = 1 + (upper - 1) * (rand / 32767)
```

Thus it is a random low-damage correction, not a five-percent minimum. The
confirmed standalone slices are implemented in
`agent-server/model/combat_final_damage.go`. The confirmed `tisc` target
factor is integrated in both player-to-NPC and NPC-to-player damage paths;
unnamed intervening helpers and the separate outgoing `0xB2`/`0xB3`
producers remain outside that claim.

### `urd` damage-buff placement

The skill parser stores `urd` at skill-record offset `0x3E4`. The skill
application routine `0x00594AC0` writes its physical operand into stat slots
`0x80` and `0x81`, and its magical operand into slots `0x82` and `0x83`.
`0x0040E450`/`0x0040E830` consume the selected slot only after mitigation.

AegisCore therefore retains `BuffModifiers.DamagePercent` as a separate
combat-damage modifier. It is no longer folded into `PhyAttackPercent` and
`MagAttackPercent` before variance. Buff application, replacement, expiry and
passive-skill recalculation all update this separate accumulator.

### `gdmp` and `pdmp`

The parser stores `gdmp` at offset `0x340` and `pdmp` at `0x344`. In
`0x00594AC0`, only records whose fourth operand equals `2` participate. Both
payload magnitudes are negated and passed to the native modifier installer:
`gdmp` uses installer type `2`; `pdmp` uses type `1`. `0x00596A40` reverses
those installations. This proves installation and teardown, but not a
one-to-one mapping from the two payload words to the emulator's physical and
magical defense or outgoing-damage fields. They remain unsupported rather than
being assigned by descriptor-name inference. Target-side `0xB4`/`0xB5` use
the separate abnormal-status path documented below.

### Final-factor storage

RTTI/vtable recovery confirms that both `CGObjPC` and `CGObjNPC` dispatch
their stat getter at vtable slot `+0x104` to `0x004AA5C0`. That wrapper selects
the entity's stat container at object offset `0x1EC` and delegates to
`0x004B3740`. The latter indexes one of 512 modifier accumulators and returns
its aggregate through `0x004B33A0`. Updates go through `0x004B3510` and
`0x004B3000`, keyed by modifier type, owning runtime effect and float value.

Consequently `0xB2` through `0xB5` are four independent stored aggregates;
they are not aliases computed inside the damage function. `0xB2/0xB3` have
confirmed `gdmp`/`pdmp` producers. `tisc`, stored at skill offset `0x460`, is
the confirmed producer for both target factors.

`0x00590680` converts `tisc` into abnormal record type `0x14` and sets state
flag `0x00100000`. The record stores the effect level at `+0x18` and the
fourth `tisc` operand (the authored magnitude) at `+0x38`. The abnormal
manager's type-`0x14` callback is installed at object offset `0xE5C` by
`0x004A40C0` and resolves to `0x004A50B0`.

On apply (callback mode `0`), `0x004A50B0` calls `0x004B3510` twice with the
same magnitude from record `+0x38`:

- slot `0xB4`, modifier type `2`, owner key `5`;
- slot `0xB5`, modifier type `2`, owner key `5`.

Thus `tisc` does not assign separate physical and magical operands: its one
authored magnitude affects both final-damage factors. On removal (callback
mode `2`), the handler calls `0x004B3570` for `0xB4` and `0xB5`, again with
owner key `5`. The normal abnormal-manager update at `0x004A4390` invokes
mode `2` after duration expiry or after the active-state bit has been cleared,
so expiry and dispel use the same inverse update before the public abnormal
mask is rebuilt.

The record factory explicitly sets priority `+0x1A` to zero. Type `0x14`
belongs to abnormal class `2`; `0x004A4270` rejects a new record when an
active record has equal or greater priority/class. Multiple simultaneous
`tisc` effects therefore neither add nor replace one another: the active
record remains until its removal path runs. Reinitialization clears the stat
container before `0x004A41F0` resets the abnormal records, so no stale
`0xB4`/`0xB5` aggregate survives object/login reconstruction.

Death is intentionally different from destruction. HP mutation runs through
`0x004A86A0` and `0x004A87D0`; neither calls the abnormal removal dispatcher,
clears flag `0x00100000`, nor resets the abnormal manager. A live `tisc`
record therefore survives death and continues to own `0xB4`/`0xB5` until its
normal expiry or dispel path runs. On logout/object destruction,
`0x004A72E0` destroys the stat container through `0x004B3460` before
`0x004A4230` destroys the abnormal records, making a separate inverse update
unnecessary. The ordering is therefore: expiry/dispel performs the inverse
slot update first and then rebuilds the public abnormal mask; death preserves
the record; destruction clears the entire stat container before record
destruction.

The remaining dynamic writers have also been separated by subsystem.
`0x00498690` is the item/magic-option parameter dispatcher. Assembly at its
caller `0x0066C280` fixes the ABI: the packed option token is passed in `EAX`,
its magnitude in `EDX`, and three stack arguments carry the entity owner,
option level and an auxiliary group/option pointer. Ghidra had incorrectly
typed that last pointer as a secondary stat index. The dispatcher's only
two-slot branch maps its tokens to fixed slots `0x1B` through `0x20`; it never
writes `0xB4` or `0xB5`.

The preserved `gosro-sql-reference` database contains 87
`SRO_VT_SHARD.dbo._RefMagicOptGroup` rows. None stores `180` or `181` in
`Value`, `Param1` or `Param2`, independently agreeing with the corrected ABI.
Item-set and ordinary magic-option dispatch are therefore ruled out as
producers of the target final-damage factors.

The reference shard also has no table named for a character-parameter
catalog. RTTI identifies the vtable at `0x00B051EC` as
`CTJ_CharParamKeeper`; its generic writer is a different subsystem and is not
the `tisc`/`0xB4`/`0xB5` producer.

Direct inspection of `SRO_VT_SHARD.dbo._RefSkill` further rules out treating
raw operands `180` and `181` as internal stat indices: those values occur
hundreds of times as ordinary authored skill values. Actual `tisc` records
are packed effect sequences. For example, the TestIn rows for
`SKILL_EU_WARLOCK_RAZEA_INT_A_01` through `_08` begin with `tisc`, duration
`30000`, category `80`, skill level, `35`, then `tnat` and a level-scaled
magnitude. The `tisc` sequence contains no literal `0xB4`/`0xB5` slot
selector in SQL because the fixed mapping is implemented by callback
`0x004A50B0`.

### Darkness (`0x646E`) stat lifecycle

The server skill parser at `0x00587813` recognizes parameter token `0x646E`
(`25710`), retains exactly four operands at skill offset `+0x44C`, and advances
past duration, admission chance, abnormal level, and magnitude. The abnormal
builder at `0x00590680` consumes those fields, creates record type `13`, stores
the authored magnitude at record offset `+0x48`, and publishes abnormal mask
bit `0x00002000`.

The type-13 callback registered by `0x004A40C0` is `0x004A4E20`. On
application it negates the unsigned magnitude at record `+0x48`, commits it to
the character stat container through `0x004B3510`, and invokes the owning
player's virtual recalculation method at vtable offset `+0x538`. On removal it
deletes the same owner/key contribution through `0x004B3570` and invokes the
same recalculation method. Darkness is therefore an absolute Hit Rate
reduction, not a percentage, but it is also a client-visible derived-stat
change rather than a server-only variance modifier.

The pinned client independently parses `0x646E` at `0x009BBE23`, retains its
four operands at skill offset `+0x264`, and registers opcode `0x303D` to
`0x00880F80` at `0x0087021A`. That handler overwrites the local character's
four attack values, two defenses, Hit Rate at payload offset `20`, Parry Rate,
maximum HP/MP, Strength, and Intellect. A server implementation must therefore
send `0x303D` when Darkness is applied, removed, cured, or expires; publishing
only the `0x3057` abnormal mask leaves the character window stale.

### Remaining stat-changing abnormal callbacks

The adjacent abnormal callbacks distinguish client-visible derived statistics
from server-only combat factors:

- Shock's callback `0x004A4850` reads its magnitude from record `+0x28`,
  clamps `100 - magnitude` to `[0,100]`, commits that remaining-Parry factor to
  stat slot `9`, and invokes the player recalculation method at vtable
  `+0x538`. The `0x6573`/`25971` operand order is duration, admission chance,
  and magnitude; treating the duration as Shock power or hard-coding a
  one-half factor is incorrect.
- Physical Defense Down callback `0x004A5160` and Magical Defense Down callback
  `0x004A5240` read magnitude from record `+0x34`. Both cap the reduction to
  `base - 30% of base`, preserving a minimum 30% of the pre-debuff defense,
  mutate stat slots `5` and `6` respectively, and invoke vtable `+0x538` on
  application and removal.
- Short Sight callback `0x004A4C60` mutates server range factor slot `183`
  without invoking the player recalculation method. Disease callback
  `0x004A4F20` similarly mutates abnormal-admission slot `169` only.
- Attack Down callback `0x004A5000` mutates physical/magical attack factor
  slots `178/179` but does not invoke vtable `+0x538`; it remains a combat
  factor rather than a `0x303D` character-window rewrite.

Accordingly, Shock, Hit/Parry Down, physical/magical Defense Down, and Darkness
must refresh the applicable `0x303D` fields on application and teardown.
Short Sight, Disease, and Attack Down must not be fabricated into unrelated
displayed fields.

### Periodic and timing abnormal callbacks

The pinned `sro_client.exe` abnormal actor table exposes two additional runtime
contracts:

- Dull callback `0x004A4A90` installs `75%` in movement stat slots `23/24` and
  `125%` in action-time slot `140`; removal restores all three to `100%`.
  Dull therefore reduces walk/run speed by one quarter and stretches attack
  and skill timing by one quarter. It is not the same one-half speed/two-times
  timing effect as Frostbite callback `0x004A45D0`.
- Poison callback `0x004A4940` evaluates after `2000 ms`, takes the authored
  damage from record `+0x38`, and caps the result so the afflicted actor keeps
  at least one HP. Bleed callback `0x004A4CC0` uses the record interval at
  `+0x10` (the builder default is `2000 ms`), damage at `+0x38`, and an
  absolute physical-and-magical defense reduction at `+0x40`. Bleed invokes
  the same derived-stat recalculation boundary on application and removal.

The Bleed builder stores operand two as the abnormal level, operand three as
periodic damage, operand four as the dual-defense reduction, and operand five
as admission chance. Collapsing damage and defense reduction into one field,
or running all periodic states on a five-second cadence, loses stock behavior.

## Runtime policy

- Player basic attacks, reference skills, weapon imbues and transfer hits use
  the recovered variance sequence.
- Monster reference-skill attacks use the same sequence with the monster's
  authored Hit Rate and the player's derived Parry Rate.
- Accuracy variance never creates a synthetic miss or block packet.
- Shield Block Rate remains the independent zero-damage block outcome.
- Critical processing and physical/magical defense are applied after the
  variance result by the existing combat pipeline.
- If a legacy reference row required to provide level, Attack Rating or Parry
  context is absent, runtime retains the previous midpoint behavior instead
  of inventing missing reference values.

## Verification

`agent-server/model/combat_variance_test.go` fixes the recovered clamps,
three-roll minimum, sign selection and interpolation as deterministic golden
cases. `combat_final_damage_test.go` fixes final-factor multiplication and
truncation. The character-combat suite covers physical, magical and absolute
player attacks; the mob-AI suite covers physical, physical-critical and
magical damage against an affected player. Status-model tests cover parsing,
equal-record rejection, dispel, expiry, death persistence and reapplication.

From `go-sro/`:

```powershell
.\.tools\go\bin\go.exe test -count=1 ./agent-server/model ./agent-server/handler/character ./agent-server/manager
.\.tools\go\bin\go.exe test -count=1 ./agent-server/...
```

## Remaining boundary

This recovery establishes exact SR_GameServer damage **variance**, the core
defense/absorption equation, low-damage correction, final clamp, and their
positions in the final-damage routines. It does not yet claim semantic parity
for the unnamed stat-slot helpers, PvP adjustments, or every status
interaction; those remain gated until their producers and consumers are both
binary-confirmed.
