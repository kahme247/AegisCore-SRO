# SR_GameServer mastery and skill lifecycle

This note is the binary-first reference for Chinese and European mastery
growth, ordinary skill learning, and item-authorized skill/mastery restoration
in the pinned TestIn `SR_GameServer.exe`. It deliberately does not use emulator
handler names or AegisCore behavior as proof.

Execution of an already learned combat skill—including targeting, casting,
cooldowns, damage/effects, cancellation, and its persistence boundary—is
documented in `sr-gameserver-skill-execution.md`.

## Evidence identity and scope

- Binary: `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`
- Size: `8,925,184` bytes
- SHA-256: `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35`
- Architecture: 32-bit Windows x86
- Saved program: `analysis/ghidra-projects/vsro-testin-gameserver:/SR_GameServer.exe`
- Focused exports: `analysis/decompiled/gameserver-mastery-skills/`

**Confirmed** means that an instruction, field access, comparison, call,
serialization operation, diagnostic string, or SQL literal exists in that
binary. **Derived** means simple arithmetic or control-flow interpretation from
confirmed instructions. Names for otherwise anonymous functions and fields are
descriptive, not recovered symbols.

“Item skills” has a narrow meaning here. The binary-confirmed `0x7202` and
`0x7203` paths authorize skill or mastery withdrawal using one of three named
items. They are not the general inventory-item use or combat-skill cast paths.

## Dispatcher and opcode map

The GameServer dispatcher uses eight-byte entries rooted at `0x00CD3D08`.
These entries are present in the binary-populated table:

| Direction | Opcode | Slot | Handler | Confirmed role |
|---|---:|---:|---:|---|
| client to GameServer | `0x70A1` | `0x00CD4210` | block `0x00515A60` | learn an ordinary skill |
| client to GameServer | `0x70A2` | `0x00CD4218` | block `0x00515AB0` | increase a mastery |
| GameServer to client | `0xB0A1` | constructed at `0x0059C3F2` | - | skill-learn result |
| GameServer to client | `0xB0A2` | constructed at `0x0059C737` | - | mastery-increase result |
| client to GameServer | `0x7202` | `0x00CD4D18` | `0x005169D0` | item-authorized skill withdrawal |
| client to GameServer | `0x7203` | `0x00CD4D20` | `0x00516BB0` | item-authorized mastery withdrawal |
| GameServer to client | `0xB202` | constructed in `0x005169D0` | - | skill-withdraw result |
| GameServer to client | `0xB203` | constructed in `0x00516BB0` | - | mastery-withdraw result |

`0x70A1` reads one `u32` skill ID. `0x70A2` reads a `u32` mastery ID followed by
one `u8` increment. The wrappers select the packet reader according to the
packet object's `+0x1048` state and then invoke the per-character skill manager
at character offset `+0xA30`.

The ordinary failure shape is binary-confirmed as a leading byte `2` followed
by a little-endian `u16` error. The dedicated withdrawal handlers send that
form directly on non-`0x7800` status; successful restoration is deferred
through the database/change-result path. The matching stock client handlers
prove the success bodies exactly: `0xB202` reads `1, u32 resultingSkillID`,
and `0xB203` reads `1, u32 masteryID, u8 level`. Their failure branches read
`2,u16`. The remaining semantic names of the success fields and the final
asynchronous server serializer are still kept separate from the width proof;
the neighboring skill-learning and mastery-growth events remain `0x1B` and
`0x1A`, respectively.

Client response evidence: [`00880DB0_FUN_00880db0.c`](../../analysis/decompiled/client-skill-restoration/00880DB0_FUN_00880db0.c)
and [`00880E90_FUN_00880e90.c`](../../analysis/decompiled/client-skill-restoration/00880E90_FUN_00880e90.c).

## Recovered structures

### Per-character skill manager

The handler wrapper passes `character + 0xA30` as the manager object. Confirmed
manager-relative state includes:

| Offset | Shape | Use |
|---:|---|---|
| `+0x220` (`+0x8B * 4`) | tree/sentinel | learned skill objects inspected for dependency conflicts |
| `+0x228` (`+0x8A * 4`) | container | active skill/effect bookkeeping |
| `+0x268` (`+0x9A * 4`) | container | skill-object insertion/removal bookkeeping |
| `+0x26C` | sentinel/link | paired with the `+0x268` container |

The exact STL types are unresolved; the offsets and their iteration/mutation
roles are confirmed.

### Learned mastery object

`0x0059D6F0` resolves a mastery entry and callers dereference its object at
node `+0x08`:

| Object offset | Shape | Confirmed use |
|---:|---|---|
| `+0x08` / `+0x0C` | flags/change metadata | cleared before persistence |
| `+0x1C` | `u32` | mastery reference ID |
| `+0x20` | `u8` | current mastery level |

### Reference skill fields used by learning

`0x0059E450` validates the requested reference skill returned by
`0x0046C1F0`:

| Ref-skill offset | Shape | Confirmed rule |
|---:|---|---|
| `+0x08` | `u32` | previous/basic-original skill lookup used to enforce the next level |
| `+0x64` | `u8` | authored skill level |
| `+0x65` | `u8` | selects the passive/effect construction branch after learning |
| `+0xA0`, `+0xA4` | two `u32` | required masteries |
| `+0xA8`, `+0xA9` | two `u8` | required mastery levels |
| `+0xAA` | `u16` | required STR |
| `+0xAC` | `u16` | required INT |
| `+0xB0..+0xB8` | three `u32` | prerequisite skill IDs |
| `+0xBC..+0xBE` | three `u8` | prerequisite skill levels |
| `+0xC0` | `u32` | required skill points |
| `+0xC4` | `u8` | required race (`3` means shared) |

These offsets explain the corresponding vSRO reference columns; they do not
prove every column in AegisCore's imported record layout.

## Ordinary skill learning lifecycle (`0x70A1`)

The exact GameServer sequence is:

1. The wrapper reads the requested `skillID:u32` and calls `0x0059BFE0` on the
   character's skill manager.
2. `0x0059E450` resolves the reference skill and checks, in order, both mastery
   prerequisites, STR, INT, race, level-chain continuity, three prerequisite
   skills, and available SP.
3. A request that resolves to the first level while an entry already exists is
   treated as an abuse condition (`0x340C`). The server logs
   `Skill Learn Abuser!!`, invokes its punitive/disconnect path, and returns.
4. When the new skill replaces an earlier level, the old learned object and
   associated active effect are detached. An active three-state effect also
   causes a `0xB072` notification before removal.
5. The new learned-skill object is allocated, linked into the character's
   containers, and—when ref-skill byte `+0x65` is zero—its effect-side objects
   are constructed and engaged.
6. The object/change accumulator is prepared, `_skill_manage` is queued with
   operation `0`, and the change set is applied.
7. Only after that path succeeds does the server debit the reference skill's
   `+0xC0` SP cost, mark the points dirty, log the new balance, and dispatch
   result event `0x1B` with the requested skill ID.

Confirmed immediate learn result codes are:

| Code | Trigger in `0x0059E450` / caller |
|---:|---|
| `0x3400` | validation success |
| `0x3401` | required mastery entry missing |
| `0x3402` | required mastery level too low |
| `0x3403` | insufficient STR |
| `0x3404` | insufficient INT |
| `0x3405` | race mismatch |
| `0x3406` | invalid level chain or missing prior level |
| `0x3407` | prerequisite skill missing/below required level |
| `0x3409` | invalid object/state or apply failure |
| `0x340A` | insufficient SP (derived from the final comparison) |
| `0x340C` | attempted first-level insertion when the skill already exists |

### Chinese and European skill admission

The race byte is read from the character's reference object through
`character -> +0x34 -> +0x18 -> +0x88`. A reference skill with `+0xC4 == 3`
is accepted for every race. Otherwise the reference byte must exactly equal
the character byte. The binary therefore applies the same validation engine to
Chinese and European skills; their authored reference prerequisites and race
byte choose the tree. No separate CH or EU learn opcode exists.

## Mastery growth lifecycle (`0x70A2`)

`0x0059C4E0` resolves the mastery, validates the requested increment, mutates
the mastery object, persists it, and then charges SP:

1. Reject an unknown mastery with `0x3801`.
2. Apply the race-specific total-mastery cap below.
3. Require `current mastery level + increment <= character level`; otherwise
   return `0x3804`.
4. Sum the SP costs for every newly unlocked mastery level. The lookup is
   repeated from `currentLevel` through `newLevel - 1`, and each cost is read
   from the referenced level record at `+0x10`.
5. Reject an SP shortfall with `0x3802`.
6. Increment the in-memory mastery level, clear its change metadata, prepare
   the change accumulator, queue `_skill_manage` with operation `1`, and apply
   the change set.
7. If apply fails, return `0x3803`. On success debit the summed SP, mark points
   dirty, log the balance, and dispatch result event `0x1A` with mastery ID and
   resulting level.

The binary does not restrict the increment byte to one before this calculation;
it sums every crossed level. AegisCore currently chooses the narrower stock
client contract of exactly one level per request.

### Chinese cap

For character race byte `0`, the server admits the request only when:

```text
sum(all current mastery levels) + requested increment < 331
```

The effective Chinese total is therefore at most `330`.

### European cap

For character race byte `1`, the candidate total must satisfy both:

```text
candidate <= 240
candidate <= 2 * character level
```

The first bound is the little-endian IEEE-754 double `240.0` stored at
`0x00B46130` (`00 00 00 00 00 00 6E 40`). The effective European cap is
therefore `min(240, 2 * character level)`.

Race byte `2` takes the common level/SP path without either of those two
total-cap branches. That observation is not evidence that a normal client can
create a third playable race.

## Item-authorized withdrawal (`0x7202` / `0x7203`)

Both handlers resolve an inventory/reference item from the request and accept
only these exact code names:

| Item code | Skill withdrawal mode | Mastery withdrawal |
|---|---:|---:|
| `ITEM_QSP_ALL_POTION_1_01` | mode flags `3` | accepted |
| `ITEM_MALL_SKILL_RESTORATION_POTION` | mode flags `6` | accepted |
| `ITEM_QNO_RM_OLDWOMAN_2_02` | mode flags `6` | accepted |

Every other item returns from the handler without entering the restoration
engine. String equality uses `CompareStringA` with locale `0x400` and flags
`0x10001`.

Both requests have the exact nine-byte body:

```text
itemRefID:u32 | targetSkillOrMasteryID:u32 | targetLevel:u8
```

The first field is an `ITEMDATA` reference ID, not an inventory slot or item
instance ID. The server resolves its code name and then locates/consumes the
matching inventory quantity.

### Skill withdrawal

`0x0059F8B0` / `0x0059F410` validate the item, target skill and target level,
check the skill-restoration currency/item requirement selected by the mode
bits, reject dependent learned skills, detach the learned object/effect, and
queue `_skill_manage` operation `2`. The SP basis is the sum of `+0xC0` for
every removed skill-line level. Successful application refunds that basis,
marks points dirty, consumes one matching restoration item per removed level,
and dispatches event `0x1C`.

### Mastery withdrawal

`0x0059FD60` / `0x0059F610` reject a target level not below the current mastery
level, optionally validate/consume the restoration resource, and reject the
change when any learned skill would have a now-unsatisfied mastery prerequisite.
The mastery object is changed and `_skill_manage` operation `3` is applied
before refunded SP is credited and event `0x1D` is dispatched. The refund
basis is the sum of the level-table SP costs for every crossed level, and one
matching restoration item is consumed per removed mastery level.

The mode flags distinguish refund policy:

| Item | Mode | SP refund | Additional cost path |
|---|---:|---:|---|
| `ITEM_QSP_ALL_POTION_1_01` | `3` | `80%` (integer arithmetic) | enabled |
| `ITEM_MALL_SKILL_RESTORATION_POTION` | `6` | `100%` | disabled |
| `ITEM_QNO_RM_OLDWOMAN_2_02` | `6` | `100%` | disabled |

`0x00410EA0` computes the skill refund and `0x00410F10` the mastery refund.
When mode bit `4` is clear they subtract `(basis * 20) / 100`; when it is set
they return the full basis. `0x00410C80` and `0x00410DF0` calculate the
additional paid-path charge for skill and mastery restoration respectively.

The paid path selects `_RefDropGold.GoldMin` by the character's historical
`MAX_LEVEL`, not current level. For each removed level the pinned GameServer
computes:

```text
round(GoldMin / 0.8 * 80 * (1 + masteryLevel / 10))
```

All inputs make this exactly `GoldMin * 10 * (10 + masteryLevel)`. Skill
withdrawal uses each removed skill row's first mastery requirement level
(`ReqCommon_MasteryLevel1`); mastery withdrawal uses every crossed mastery
level from current down to target+1. The charge is summed, checked against the
locked character gold balance, and deducted atomically with the SP refund and
per-level item consumption. Insufficient gold returns `0x7804`.

The old SQL Server reference function `CalcWithdrwaGold_Skill_n_Mastery`
applies an additional `0.5` multiplier. The pinned `SR_GameServer.exe`
assembly does not, so AegisCore follows the runtime binary rather than that
divergent helper function. The source catalog is the 140-row `_RefDropGold`
table (`MonLevel`, `DropProb`, `GoldMin`, `GoldMax`), represented in Media by
`server_dep/silkroad/textdata/levelgold.txt`.

The restored `SRO_VT_SHARD.dbo._RefObjCommon` row for
`ITEM_QSP_ALL_POTION_1_01` (ID `3673`) has `Price = 0` and `SellPrice = 1`.
That independent database check confirms the item catalog is not the paid
restoration fee source; using its price would be an incorrect fallback.

The common restoration result family is:

| Code | Confirmed meaning |
|---:|---|
| `0x7800` | success |
| `0x7801` | missing/invalid item or target object |
| `0x7802` | target level is not below the current level |
| `0x7803` | generic invalid/apply/resource failure |
| `0x7804` | insufficient restoration currency/resource |
| `0x7805` | insufficient inventory quantity |
| `0x7806` | another learned skill depends on the level being removed |

On the mastery path, `0x7800` additionally invokes character vfunc `+0x50C`
with dirty flag `0x20`; non-success codes are returned in `0xB203`. The skill
path emits `0xB202` for a non-success code. The success bodies are
`1, resultingSkillID:u32` and `1, masteryID:u32, level:u8`; failures are
`2, errorCode:u16`.

## SQL and asynchronous result contract

The only direct SQL literal reached by these learn/increase/withdraw paths is:

```sql
exec _skill_manage %d, %d, %d, %d
```

The formatter is at `0x004587A0`. Before queuing the command it records the
character ID at job offset `+0xC28`, operation at `+0xC2C`, reference ID at
`+0xC30`, and level at `+0xC34`. Confirmed operation values are:

| Operation | Mutation |
|---:|---|
| `0` | learn/replace ordinary skill |
| `1` | increase mastery |
| `2` | withdraw ordinary skill level |
| `3` | withdraw mastery level |

The function also invokes character vfunc `+0x380` with the reference ID and
level before formatting the command. Because one formatted value is produced
by that virtual call and Ghidra has not recovered the formatter prototype, the
four stored job fields are the reliable semantic contract; the exact textual
argument order after the operation value remains unresolved.

The GameServer contains no separate `_AddSkill`, `_UpdateMastery`, or
item-specific skill-restoration SQL literal. In-memory mutation and the generic
change accumulator precede the `_skill_manage` job. Result handling later logs
`Insert Skill Info` / `Erased Skill Info`, corroborating operation-specific
asynchronous completion rather than direct synchronous table updates in the
packet handler.

## AegisCore implications

- `0x70A1` and `0x70A2` are correctly modeled as one skill ID and one mastery
  ID plus increment, respectively.
- CH and EU ordinary skills share one admission algorithm; reference race and
  prerequisites distinguish them.
- The Chinese mastery total of `330` and European
  `min(240, 2 * character level)` total are binary-confirmed.
- Persist-before-success and atomic SP/mutation behavior are important
  compatibility properties even though AegisCore uses normalized MySQL tables
  rather than `_skill_manage`.
- AegisCore now implements `0x7202`/`0x7203` for both mode-`6` full-refund
  items and the mode-`3` paid item with exact codecs, dependency analysis,
  per-level item consumption, binary-compatible SP refunds and gold pricing,
  and serializable persistence. `cmd/sro-data import-reference` imports the
  required `levelgold.txt` base-price catalog.

## Reproduction anchors and unresolved edges

- dispatcher initializer: `0x0050D0C0`
- learn wrapper: block `0x00515A60`
- mastery wrapper: block `0x00515AB0`
- skill learn/mutation: `0x0059BFE0`
- skill admission: `0x0059E450`
- mastery increase: `0x0059C4E0`
- skill withdrawal handler/core: `0x005169D0`, `0x0059F8B0`
- mastery withdrawal handler/core: `0x00516BB0`, `0x0059FD60`
- withdrawal validators: `0x0059F410`, `0x0059F610`, `0x0059F7B0`
- SQL job formatter: `0x004587A0`
- reference skill parser: `0x00587630`

Do not yet claim the following as exact:

- textual `_skill_manage` argument order beyond the confirmed operation and
  stored job fields;
- applicability of these addresses or layouts to the separate 1.274 binary.
