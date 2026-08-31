# SR_GameServer aggro recovery

This note records the currently recovered SR_GameServer threat behavior. Only
the arithmetic and branches below are claimed as binary-confirmed.

## Evidence identity

- Binary: `SR_GameServer.exe`
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- skill-parameter parser: `0x00587630`
- combat threat-event construction: `0x0058E5F0`
- threat-reduction event construction: `0x00593800`
- GameServer configuration loader: `0x00427050`
- linked-monster propagation routine: `0x00547570`
- healing application routine: `0x005A0850`
- healing threat relation routine: `0x005A0650`

## Skill parameter mapping

The parser stores the known parameters at these skill-object offsets:

| Tag | Decimal | Skill offset | Meaning |
| --- | ---: | ---: | --- |
| `tntd` | `1685352052` | `+0x3D0` | authored threat reduction values |
| `agkl` | `1818976615` | `+0x47C` | aggro absorption/link behavior |
| `alop` | `1886350433` | `+0x304` | normal aggro suppression/block behavior |
| `2tnt` | `1953395762` | `+0x3CC` | flat and percentage threat increase |

`2tnt` has two authored operands. The previous AegisCore parser retained only
the first; it now preserves both as `AggroIncrease` and
`AggroIncreasePercent`.

## Confirmed threat arithmetic

At `0x0058E5F0`, SR_GameServer first accumulates the attack's base threat. When
`2tnt` is present it applies the second operand as a percentage, rounds using
the x87 nearest-even mode, and then adds the first operand:

```text
threat = round_to_even(baseThreat * (1 + bonusPercent / 100)) + flatThreat
```

Negative results are not admitted into AegisCore's threat table. The recovered
arithmetic is implemented by `model.CombatThreat` and is used before aggro
absorption splits the generated threat.

At `0x00593800`, the `tntd` operands are subtracted while constructing the
threat-reduction event. This confirms reduction is an operation on existing
threat, not a miss, damage reduction or target evasion effect.

## Linked-monster propagation

The GameServer configuration includes
`MONSTER_AGGRO_LINK_DECREASE_RATIO`. Its loader at `0x00427050` initializes the
ratio to `0.5` and accepts an integer percentage divided by `100`.

The only reader is `0x00547570` in `AITactics_ExtRoutine.cpp`. That routine:

1. enumerates the source monster's authored AI-tactics link collection;
2. retains the source and linked members within `1000` world units;
3. clamps the number processed to the available authored link slots;
4. dispatches threat event type `0x0E` to each eligible member; and
5. multiplies the carried integer threat by the configured decrease ratio
   after each dispatch.

At the default ratio, authored order therefore receives `T`, `T/2`, `T/4`,
and so on, with the floating-point product converted back to an integer after
each member. The routine exits without propagation when its AI-tactics
blocked/reset predicate is active.

AegisCore must not approximate the authored collection as every nearby NPC.
The current spawn projection does not retain enough tactics-link identity to
reconstruct that ordered collection, so runtime wiring remains intentionally
blocked on recovering and importing the link membership fields.

## Healing threat

The `heal` skill tag is stored at skill offset `+0x324`. `0x005A0850` computes
the actual HP and MP restored, including the relevant authored additions, and
passes their sum to `0x005A0650`.

`0x005A0650` does not scan the world or every nearby monster. It obtains the
healed target's active combat-relation collection, retains monster relations
that already have a threat record, and prepares one healing-threat update per
related monster. Consequently:

- overhealing cannot generate threat from the skill's unapplied amount;
- restored HP and restored MP both contribute to the base healing event; and
- healing does not pull unrelated monsters that have no combat relation with
  the healed target.

The final per-monster mutation lies in a Ghidra-unreachable exception block in
this build. Its multiplier and rounding remain unclaimed until the assembly
path is reconstructed; AegisCore therefore does not yet emit healing threat.

## AegisCore integration

- `agent-server/model/ref_skill.go` parses all four tags and both `2tnt`
  operands.
- `agent-server/model/combat_threat.go` owns the confirmed threat arithmetic.
- `agent-server/handler/character/combat.go` applies it to attack-generated
  threat and then performs an active aggro-absorption split.
- `agent-server/model/npc.go` owns the concurrent per-NPC threat table and
  highest-threat target selection.
- `agent-server/manager/mob_ai_manager.go` validates and pursues the selected
  target and clears invalid/dead targets.

## Verification

```powershell
.\.tools\go\bin\go.exe test -count=1 ./agent-server/model ./agent-server/handler/character ./agent-server/manager
.\.tools\go\bin\go.exe test -count=1 ./agent-server/...
```

Golden tests cover two-operand parsing, nearest-even percentage arithmetic,
flat addition, non-negative clamping, attack integration and absorbed-threat
splitting.

## Remaining recovery boundary

The following must not yet be described as exact SR_GameServer behavior:

- the healing-threat multiplier/rounding and non-damage support actions;
- authored linked-monster membership/order and its spawn-data projection;
- forced-target/taunt duration and priority rules;
- target-switch thresholds and equal-threat ordering;
- periodic decay, leash reset and return-home timing;
- party, pet, transport and owner attribution in every branch;
- death, teleport, world-transfer and disconnect cleanup ordering.

Those behaviors remain isolated behind the existing threat-table and mob-AI
interfaces until their routines and callers are recovered.
