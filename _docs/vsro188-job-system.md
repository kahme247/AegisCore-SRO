# vSRO 1.188 tri-job system

This document records the Trader/Thief/Hunter contract recovered from the pinned TestIn GameServer and two local client copies. It separates confirmed binary behavior from AegisCore policy and leaves safe trade closed where the evidence is incomplete.

## Binary identities

| Artifact | SHA-256 | Role |
| --- | --- | --- |
| `SR_GameServer.exe` | `0efe7eafe6e0b8ae3284c377b59211f3a8864713d8674d274105739baac35e35` | authoritative server control-flow/SQL evidence |
| preserved `sro_client.stock.exe` | `58b9da3a218894f28e4c82a992b90997869cf331ab450929c8fcb7453327d114` | authoritative client address/read-order evidence |
| runtime `VSRO_Client/sro_client.exe` | `281345245694aecf1b73f845fd410f3f0a041e29cfee6457323c4e67ad6b9d48` | compatibility-patched runtime copy |

The clients have the same 11,485,184-byte PE layout and differ in 68 bytes across nine ranges (`0xC711A..0xC711F`, `0xC7530`, `0xC7532..0xC7535`, `0x43232F..0x432338`, `0x43297F`, and code-cave ranges under `0x97F24A..0x97F292`). None overlap the JOB handlers below. Address statements remain pinned to the stock hash; the runtime copy is behavior-equivalent only for unchanged bytes.

## Lifecycle

`TRIJOB_TYPE` is an 8-bit client state:

| Value | State |
| ---: | --- |
| `1` | Trader/Merchant |
| `2` | Thief |
| `3` | Hunter |
| `4` | No active job |

The client stores type at `CICharactor+0x50D`; the current player stores the mirrored active type at `CICPlayer+0x7BE`, level at `+0x7BF`, current job EXP at `+0x20B4`, reward/outcome value at `+0x20BC`, and job alias at `+0x2098`. `CICUser::GetJobLevel` is `0x009DB0E0`; the type getter is `0x009DB0D0`.

Confirmed/strong lifecycle:

1. **No job** is discriminator `4`, and player select/spawn omits the following job-level byte.
2. **Enroll** (`0x70E1`) requires an active type `1..3`. SR_GameServer checks the character is currently no-job, rejects conflicting world states, derives the fee from character level, persists the transition asynchronously, and only then emits `0xB0E1` success.
3. The recovered enrollment arithmetic is `(characterLevel - 20) * 5000`; levels below 20 are rejected. The fee is transmitted as the first request `u32`, but AegisCore recomputes it and rejects a mismatch rather than trusting it.
4. **Active state** is immutable until withdrawal. `0x30E6` updates type/level/EXP, and the client asserts that the update's type matches its current active type.
5. **Alias** (`0x70E3`) has action `0` (availability check) and `1` (create/modify). Durable uniqueness is case-insensitive.
6. **Withdraw** (`0x70E2`) archives current job state, charges a server-derived fee, clears type/level/EXP and safe-trade ownership, and publishes no-job state. The recovered withdrawal arithmetic is `characterLevel * 50000`.
7. **Previous info** (`0x70E6`) returns current and previous type/EXP/contribution fields; it is not an alias packet.
8. **Outcome** (`0x70E5`) claims a pending reward. AegisCore retains the existing exactly-once row-locking transaction. The recovered weekly distribution is an explicit maintenance action because the backup contains no SQL Agent schedule to establish a trustworthy weekday/time.

AegisCore adds transactional locking, checked arithmetic, parameterized SQL, and authenticated-character ownership. Those are defensive implementation choices, not claims about the legacy procedures' isolation level.

## Opcodes and structures

All integer fields are little-endian. Strings use the Silkroad `u16 byteLength + bytes` representation.

| Direction | Opcode | Structure / behavior | Evidence |
| --- | ---: | --- | --- |
| S -> C | `0x30E6` | `jobType:u8, jobLevel:u8, jobExp:u32` | confirmed at client `0x00884DD0` |
| C -> S | `0x70E1` | `enrollmentFee:u32, jobType:u8` | confirmed sender `0x0081F1A0` |
| S -> C | `0xB0E1` | `result:u8`; success adds `jobType:u8, jobLevel:u8, jobExp:u32`; failure adds `error:u16` | confirmed client `0x008890B0` and GameServer construction |
| C -> S | `0x70E2` | `withdrawalFee:u32` | confirmed sender `0x0081F270` |
| S -> C | `0xB0E2` | standard `result:u8` / failure `error:u16` | confirmed client `0x00889210` |
| C -> S | `0x70E3` | `fee/context:u32, action:u8, alias:string` | confirmed sender `0x00824030` |
| S -> C | `0xB0E3` | `result:u8`; success `action:u8`, and action `0` adds `available:u8`; failure `error:u16` then one action/context byte | confirmed client `0x00889370`; failure tail remains partially semantic |
| C -> S | `0x70E4` | `fee/context:u32, jobType:u8, kind:u8` | confirmed sender `0x0081F330` |
| S -> C | `0xB0E4` | `result:u8`; success `jobType:u8, kind:u8, count:u8`, then entries; kind `0` entries read `rank:u8, alias:string, level:u8, exp:u32, compatibility:u8`; kind `1` omits the final byte | confirmed client `0x0088A130` |
| C -> S | `0x70E5` | `fee/context:u32, action:u8` | confirmed sender `0x0081F410` |
| S -> C | `0xB0E5` | `result:u8`; success `action:u8, amount:u32`; failure `error:u16, action/context:u8` | confirmed client `0x008830F0` |
| C -> S | `0x70E6` | `fee/context:u32` | confirmed sender `0x0081F4E0` |
| S -> C | `0xB0E6` | `result:u8`; success tail `currentType:u8, currentExp:u32, currentLevel:u8, previousExp:u32, previousType:u8, previousContribution:u32`; failure `error:u16` | confirmed client `0x00881740` |
| S -> C | `0x30E0` | `count:u8`, then `count` entries of `refItemID:u32, price:u32` | confirmed client `0x00881600`; server builder `0x004C8A60` |
| S -> C | `0x30E7` | `reason:u8`; `1` means farther than 100 units from the trade cart, `2` is the generic 30-unit quest-target distance message | confirmed client `0x00883B10`; server builder `0x004F1230` |
| S -> C | `0x30E8` | `tradeScale:u8` | confirmed client `0x00880970`; server builder `0x00523810` |
| S -> C | `0x34D5` | success `result=1, code:u8, current:u8, limit:u8`; failure `result=0, error:u8` | confirmed client `0x00891080`; server builder `0x0051A960` |

The current AegisCore ranking row names are domain labels. In
`sro_client.exe` (image base `0x00401000`), reader `0x0088A130` consumes the
last kind-0 byte at `0x0088A23F–0x0088A248`, but list insertion
`0x00888B90` receives only the rank field and the later UI notification is
given only the job type. The byte is neither stored nor used in a branch.
AegisCore therefore emits zero as a client-ignored compatibility value rather
than assigning it an invented gameplay meaning.

## Player publication

Active types `1..3` carry a following `jobLevel:u8`. No-job type `4` does not. This conditional layout applies to the recovered player-selection branch and the player tail of single/group spawn. AegisCore stores the online snapshot on `model.Player`, so GameReady bootstrap, `0xB045`, and `0x3015/0x3019` use one source of truth instead of hard-coded bytes.

## SQL calls and durable mapping

Confirmed SR_GameServer call strings:

```sql
EXEC _AddTrijobReward %d, %d
EXEC _DeleteOldTrijobData %d
{?=CALL _InsertCharTrijobSafeTradeData (%d, %d, %d)}
{?=CALL _DeleteCharTrijobSafeTradeData (%d)}
{CALL _STRG_ADD_SPECIALTY (?, ?, %d,%d,%d,%d,%d, '%s')}
{CALL _STRG_RESTORE_SOLDITEM_SPECIALTY (?, ?, %d,%d,%d,%d,%d,%I64d, '%s')}
```

Confirmed table vocabulary includes `_CharTrijob`, `_CharTrijobSafeTrade`, and `_TrijobRewards`. SR_ShardManager also exposes `_SMC_EDIT_TRIJOB` and `_SMC_EDIT_TRIJOBNICK`, and queries `JobType, Level, Exp, Contribution, Reward` from `_CharTrijob`.

The safe-trade procedure call sites establish a narrower meaning than their names alone suggest:

- Trader enrollment (`0x70E1`, GameServer `0x005120F0`) schedules AQ action `6`, which calls `_InsertCharTrijobSafeTradeData(CharID, 0, 0)`. Hunter and Thief enrollment do not create this row.
- Job withdrawal (`0x70E2`, GameServer `0x005122B0`) schedules AQ action `7`, which calls `_DeleteCharTrijobSafeTradeData(CharID)` before clearing the in-memory tri-job state.
- The two zero-valued insert parameters are therefore per-Trader safe-trade state/counters. They are not evidence for cargo, origin market, destination market, price, or COS ownership columns.

The specialty procedures are branches of the generic asynchronous storage
updater, not procedures dedicated to `_CharTrijobSafeTrade`. Instruction-level
argument recovery at `0x0045AC0D` proves that `_STRG_ADD_SPECIALTY` receives,
after its two output parameters, the storage selector, owning character ID,
target slot, the value returned by item virtual `+0x04`, item-data field
`+0x38`, and the specialty item's creator string. The matching restore branch
at `0x0045BE08` supplies
the same fields plus the original sold-item `ID64`. The runtime creator string
is the same specialty metadata family used by the case-insensitive ownership
predicate at `0x00490930`; this connects the durable specialty row to the
buyer/creator marker without proving the stored procedure's column names.

`AQ_CharDataLoader` also conditionally loads one safe-trade record with
`WHERE CharID = %d` when the loaded tri-job state is Trader and stores the
result object at loader offset `+0x120`. A missing or multi-row result is a
hard load failure. This establishes a one-row-per-Trader runtime object, but
the selected table descriptor and its columns are not exposed by the query
fragment. It does not turn the generic sold-item restore operation into a
safe-trade replay ledger.

The executable proves names, parameter count/order, asynchronous query boundaries, and some callers. It does **not** contain the stored-procedure bodies, locks, isolation, rollback contract, or all result sets. AegisCore therefore maps the core to parameterized MySQL tables/transactions:

- `CHARACTER_JOB`: active type, alias, level, EXP, total/weekly contribution, pending reward, enrollment/leave state, and previous snapshot.
- `CHARACTER_JOB_HISTORY`: append-only withdrawal history.
- `REF_JOB_LEVEL_EXPERIENCE`: legacy Trader-threshold projection derived from
  imported v188 level data and constrained to the recovered levels 1–7;
  runtime progression selects the job-specific threshold directly from
  `REF_LEVEL_DATA`.

## Safe-trade boundary

The maintained safe-trade lifecycle is implemented across the recovered stock
boundaries: specialty quotation and package purchase, creator persistence,
cart delivery, Trader/Thief pickup admission, replacement-identity pickup,
cargo-loss world transition, terminal job-cart teardown, trade-scale updates,
safe-trade admission/reset/restore, and delivery/combat job progression. The
overall protocol profile remains evidence-pending because no sanitized stock
JOB capture exists; that evidence status does not make these runtime paths
framing-only.

`0x2C` is not the stock v188 sell half of `0x2B`. SR_GameServer function `0x004D8BF0` (item-operation structure construction) compares the discriminator against an inclusive maximum of `0x2B`; its `0x2B` jump-table entry parses the same fixed fields as operations `0x08` and `0x13`. Function `0x004F76E0` (the matching structure serializer) has the same inclusive maximum and a dedicated `0x2B` branch. A discriminator of `0x2C` is above both bounds and reaches their default/assert path. AegisCore therefore rejects `0x2C` explicitly and does not label it as a sell request.

The `0x2B` dispatch target is exact. The table initializer at `0x0050CE80` writes `0x005266A0` to entry `0x00CD3CB0` (`0x00CD3B58 + 0x2B * 8`). That handler is shared with operation `0x08`: record `+0x02` selects an entry from the active package vector, record `+0x03` selects the package member through `0x00616780`, and record `+0x06` is the positive quantity. It calls the generic package-item insertion engine `0x00618FE0` with character-inventory storage context `0x0D`. The pinned `_STRG_ADD_PACKAGEITEM_CORE` body names operation `43` (`0x2B`) `SP_BUY_ITEM_WITH_TOKEN` and debits token payment-device bits `64`, `128`, `256`, `512`, and `1024`; it is not the gold specialty purchase. Gold-priced specialty packages use the same four-byte package request under operation `0x08`, payment device `1`, and character inventory. Neither operation is a separate COS purchase or settlement protocol.

Both specialty-purchase validation (`0x006182A0`) and existing-specialty
validation (`0x00617F70`) resolve the active COS and measure player-to-COS
distance. The comparison at `0x006184A8` is an x87 `fcomp qword` against
`0x00B45C68`; the pinned image stores the IEEE-754 value `1000.0` there.
Thus the server threshold is exactly 1000 internal coordinate units. The
client's `0x30E7` reason-1 text calls the corresponding user-facing distance
100 units, consistent with the engine's ten-to-one internal/display scale.

### Recovered delivery and replay path

The actual sale path is the normal NPC sell operation, not `0x2C`. GameServer `0x004C8D10` is reached while pricing an item sold to an NPC:

1. It identifies special-merchant context through NPC capability and
   `_ITEMQUOTATION` lookups.
2. It explicitly rejects stolen goods at a special merchant (`"Trying to sell stolen Item to special merchant!"`).
3. For a Trader, a valid special-merchant sale invokes the safe-trade transition builder `0x0051A960` with raw transition `0x40` before calculating the payout.
4. The payout uses the server's runtime quotation entry:
   `(currentQuotation * quantity) - (baseQuotation * quantity)`, followed by
   the common gold-rate/conversion helpers. If no quotation entry exists, the
   server logs the missing `RefItemID` and falls back to the item's ordinary
   reference sell price.
5. Price changes are published through `0x30E0`; `0x004C89D0` fans the update out to players currently interacting with the relevant merchant.

The catalog boundary is now exact. NPC initialization at `0x004C8220` queries
`_ITEMQUOTATION` with `WHERE AssocNPC=%d` and builds a tree keyed by
`RefItemID`. Lookup `0x004C5D60` returns the value at tree-node `+0x10`:

| Runtime offset | Confirmed role |
| ---: | --- |
| `+0x00` | pointer to the loaded `_ITEMQUOTATION` reference row |
| `+0x04` | base quotation subtracted during special-merchant delivery |
| `+0x08` | mutable current quotation; serialized by `0x004C8A60` |
| `+0x0C` | quotation-update factor loaded from the referenced item |
| `+0x10` | quotation-update accumulator/bound |

`0x004C8A60` serializes the tree key (`RefItemID`) and runtime `+0x08`, proving
that the `0x30E0` price is the current quotation rather than the purchase cost
or final payout. `0x004C8810` updates this value and rounds an intermediate
ratio to hundredths using the binary constants `100.0` and `0.01`. The loaded
reference row contributes `AssocNPC` at `+0x20`, `RefItemID` at `+0x24`, float
bounds at `+0x28/+0x2C/+0x30`, integer quotation inputs at `+0x34/+0x38`, and
an upper accumulator/bound at `+0x3C`. The pinned v188 shard backup confirms
the complete `_ItemQuotation` schema as `ID`, `Service`, `AssocNPC`,
`RefItemID`, `BaseQuot`, `Quot_LB`, `Quot_UB`, `BaseStockAmount`,
`FluctuateAmount`, and `CurStockAmount`; its 492 rows satisfy the recovered
bounds and uniqueness invariants.

The purchase catalog itself is the ordinary package/shop catalog. The pinned
Media chain is `RefShopGoods -> RefPackageItem -> RefScrapOfPackageItem ->
RefObjItem`; for example `NPC_CH_SPECIAL` tab 0 slots 0..3 resolve package IDs
24823..24826 to item IDs 2147..2150. The maintained `ITEM_MALL_GOODS` snapshot contains the typed package/shop rows, preserving price, quantity, options, variance, and data. This is the correct source for ordinary one-member package entries, not a separate invented safe-trade goods catalog. Migration 91 and `import-vsro-items` now preserve the complete `_RefScrapOfPackageItem` member vector, its stock `Index`, shop-tab placement, and price-policy cost in the vSRO reference catalog.

The token-purchase response boundary is now exact: `0x2B` emits the primary
generated-slot vector and quantity used by `0x08`, followed by a token-debit
continuation vector whose entries are `child+0x03:u8, child+0x06:u16`; the
client constructs and applies a child operation record for every pair.
Migration 92 and `import-vsro-items` now
preserve `_RefShopGroup`, `_RefMappingShopGroup`,
`_RefMappingShopWithTab`, and `_RefShopTab`; the runtime resolver joins the
selected NPC through that authority chain, derives the tab selector by the
stock ID-ordered group vector, resolves the slot to its complete member vector,
and rejects missing or ambiguous results. The operation-`0x08` transaction
locks character balance, inventory, job creator state, and every destination;
it applies the recovered single-expandable versus clamped `[1,5]` multi-item
slot rule, validates stack bounds, creates every package member, debits
`Cost * Count`, reloads the committed inventory, and returns the exact primary
slot vector. Token operation `0x2B` uses the same expanded selector authority
but requires payment devices `64`, `128`, `256`, `512`, or `1024`. Its atomic
transaction consumes the exact token code name in ascending inventory-slot
order, deletes whole stacks, updates the final partial stack, and returns every
changed stack as the stock continuation `(slot, remaining:u16)` vector.

Migration 87 predates that recovered catalog authority and created
`SAFE_TRADE_MARKET`, `SAFE_TRADE_MARKET_GOOD`, `SAFE_TRADE_CARGO`, and
`SAFE_TRADE_SETTLEMENT`. Applied migration history is immutable, so those
tables remain in upgraded schemas, but no maintained runtime repository reads
or writes them. They are not an alternate purchase, cargo, or settlement
authority. `CHARACTER_SAFE_TRADE` remains live only for its attempt counter,
active status, and last-transition timestamp; its historical `PROGRESS`
column has no runtime semantics.

The server-only quotation catalog is maintained through `cmd/sro-data
import-specialty-quotations`. It accepts the exact ten-column export from the
pinned backup, preserves SQL Server `REAL` precision, validates bounds,
counters, IDs and NPC/item uniqueness, and replaces `ITEM_QUOTATION`
transactionally. The proprietary export remains an ignored local input.

Safe-trade replay/status is server-driven. `0x0051A960` emits raw transition codes `0x40`, `0x41`, `0x50`, and `0x51`. Its first byte is the persisted active flag rather than a generic success result. GameServer disassembly fixes the exact variable-length forms:

```text
inactive: 00 code:u8                 // code 0x40 or 0x41
active:   01 code:u8 current:u8 limit:u8 // code 0x50 or 0x51
```

The player safe-trade wrapper is at player `+0x1FB4`; its record pointer is
wrapper `+0x08`. The record stores the attempt counter as a 32-bit value at
`+0x1C` and the active flag at `+0x20`. The wire builder truncates the configured
`TRADE_SAFETY_NUM` and the persisted counter to their low bytes.

- success code `0x50` displays `UIIT_MSG_SAFETRADE_PROGRESS` using the following `current` and `limit` bytes;
- failure error `0x41` displays `UIIT_MSG_SAFETRADE_NUMBER_ERR_LIMIT`;
- every success enables the safe-trade guide/UI state and every failure disables it;
- transition `0x50` first sets the active flag to one, then sends the current
  counter and configured limit;
- transitions `0x40` and `0x41` first clear the active flag and emit only the
  two-byte inactive form;
- login/world restoration calls the builder with transition `0x51`. When the
  persisted active flag is one it sends `01 51 current limit` without changing
  the counter. When the flag is zero, the builder deliberately downgrades the
  packet to `00 40`;
- special-merchant sale, eligible zone crossing, and trade-scale loss/change
  paths call transition `0x40`.

Admission helper `0x0060E890` requires trade scale `1`. With
`TRADE_SAFETY_OPTION=0`, it rejects when `current >= TRADE_SAFETY_NUM`; otherwise
it increments the 32-bit counter by one (capped at the configured limit) when
the state was inactive. Option `2` admits without that counter increment. The
caller maps admission to `0x50`, exhaustion to `0x41`, and every other reset
path to `0x40`.

The pinned `_RefShardContentConfig` rows identify the profile values as
`TRADE_SAFETY_OPTION=0` and `TRADE_SAFETY_NUM=8`. Agent exposes them as
`AGENT_TRADE_SAFETY_OPTION` and `AGENT_TRADE_SAFETY_NUM`. During post-GameReady
bootstrap a Trader with persisted `STATUS=1` receives `01 51 current 08`; an
inactive Trader receives the stock downgrade `00 40`. Missing Trader state is
a hard bootstrap error, preserving the enrollment invariant.

Opening a maintained specialty merchant now runs the admission decision under
row locks for `CHARACTER_JOB`, `CHARACTER_SAFE_TRADE`, and the active job COS.
The transaction calculates the live cargo scale, applies the option matrix,
increments `ABLE_COUNT` only for a previously inactive counted admission, and
persists the active flag before publishing `0x50`, `0x41`, or `0x40`. Reopening
the merchant while already active cannot consume another counted attempt.

The separately scheduled `SCHEDULE_DEF_SPECIAL_GOODS_SELL` job does not supply
a per-character delivery transaction. Constructor `0x00674DD0` registers it
as `CGameSJ_SpecialGoodsSelling`; its start edge toggles a global server flag,
broadcasts inter-server opcode `0x300C`, calls the global special-goods manager,
and logs `Started Selling Special Goods`. It is therefore a global sale-window
lifecycle. No character ID, COS ID, specialty item ID, or payout is present in
that start edge.

The trade-scale setter (`0x005237B0`) publishes `0x30E8`. When the previous scale differs and the new scale is not `1`, it also triggers safe-trade transition `0x40`. Special-trade admission (`0x0060E800`) checks the Trader state, NPC capability `0x0C`, and configured attempt limit; it maps an admitted attempt to transition `0x50`, the exhausted limit to `0x41`, and other rejection/reset paths to `0x40`.

The classifier at `0x0060C330` returns the first scale whose configured maximum is greater than or equal to the normalized live-cargo score, or scale `5` when all four finite thresholds are exceeded. Initializer `0x0060C6C0` loads `TRADESCALE1_AMOUNT_MAX` through `TRADESCALE5_AMOUNT_MAX` and `TRADE_MON_FACTOR`; its defaults are `408`, `918`, `1428`, `2142`, `0xFFFFFFFF`, and `306`. Migration 90 corrects the maintained scale-1 default from the earlier provisional `510` to the stock `408` without rewriting applied migration 87.

The score source is also bounded. `0x004D2D20` sums stack quantities for every live cart item accepted by the specialty classifier/predicate. `0x0060C1F0` clamps character level to `[20,140]`, selects cart capacity `40` for reference rarity `1`, `30` for rarity `2`, or the reference capacity field otherwise, and evaluates conceptually:

```text
raw = qualifyingQuantity * 348
      / integer_conversion(levelLookup[level] * 1.25)
      / (40 / cartCapacity)
```

Positive results below one become one; other results use the stock float-to-integer helper `0x009FBB40`. The level-indexed table at global configuration `+0x268` is also used by stock level/gold/item scaling. The pinned client type-8 loader stores `LevelData.txt` column 6 at `CLevelData +0x30`, separately from the three legacy job-threshold columns 7–9 at `+0x24/+0x28/+0x2C`. Migration 93 corrects the earlier shifted import, preserves column 6 as `JOB_ACTIVITY_FACTOR`, and restores all three thresholds. AegisCore applies that factor to normalized trade score and combat job EXP.

Direct callers show that scale is derived from the live item storage rather than from `_CharTrijobSafeTrade`. NPC item-sale handler (`0x006194E0`) recalculates it after decrementing or removing the sold stack. Normal inventory drop/move handler `0x005264E0` snapshots the old scale, performs `0x004B2AB0`, and recalculates after success; `0x004B2AB0` detaches the existing runtime item and constructs its ground transition before the asynchronous `_STRG_DEL_ITEM` updater runs. The internal removal path `0x006185F0` also recalculates after detaching an item. These edges prove that adding, dropping, or removing qualifying stored items can change trade scale and transition `0x40`.

Ordinary ground pickup can preserve an existing item identity, but the COS-death specialty path does not. It preserves the runtime creator marker while COS cleanup releases the old persistence identity; pickup allocates a replacement through `_STRG_ADD_SPECIALTY`. Specialty stack compatibility (`0x00490230`) compares creator strings case-insensitively through `0x00426F30`, so theft cannot erase provenance by merging goods into a differently owned specialty stack. The destination special merchant rejects the mismatched creator through `0x00490930` and error `0x1868`.

COS death cargo disposition is now recovered. The common death edge `0x00529DE0` invokes virtual `+0x4DC`; the trade-cart override `0x004D2CD0` notifies owner/trade state and delegates to `0x004C42F0`. Event `0x1E` then reaches COS `+0x638`, whose cart implementation `0x004D1FD0` calls `0x004C3F60`. That function walks all `0x1C30` slots of the live COS storage at `+0x1C10`, calls `CGStorage::PopItem` (`0x004B7F10`) for every slot, and passes every accepted item to its world-transition virtual `+0x4DC` with the cart's current position. The cart's `+0x634` acceptance predicate is `0x009856E0`, which always returns one, so no occupied cargo slot is silently excluded. Both associated storage objects are cleared after the traversal.

The ground-owner rule deliberately distinguishes specialty goods. For an eligible player killer, ordinary items receive the killer UID through `0x004901B0`. The branch first calls the item's specialty classifier at virtual `+0x58`; a nonzero result skips that owner assignment. Specialty goods therefore enter the world without the ordinary killer-exclusive owner marker, allowing job theft, while their creator string remains attached to the runtime item. Pickup persists a newly allocated database item through `_STRG_ADD_SPECIALTY` operation `17` with that same creator string. This is why the later special-merchant creator comparison can reject stolen delivery with `0x1868`.

The player pickup preparer at GameServer image-base-relative effective address
`0x00525DC0` closes the job admission rule before the common transfer gate:

- Trader type `1` is accepted only when `0x00490930` says the specialty
  creator matches the current job alias; a mismatch returns `0x1863`.
- Thief type `2` is accepted only when that creator predicate does not match;
  a self-created item returns `0x186F`.
- Hunter type `3` returns `0x1863`, and no-job type `4` returns `0x1864`.
- Success additionally requires an active job COS and a strict distance below
  `1000.0` internal units. It rewrites the internal storage operation to `17`,
  records the active COS as the destination, and preserves the creator string.

AegisCore enforces the same matrix both against the online job snapshot and
again under the persistence transaction's `CHARACTER_JOB` row lock. Specialty
cart-death cargo can no longer enter the character inventory: it receives a
replacement `ITEM` identity in the owned active `COSJobTransport`, publishes
the stock `0xB034` operation-`0x17` COS/slot completion, and recalculates and
publishes `0x30E8`. Ordinary transient cart cargo retains the normal character
pickup path because the stock specialty predicate does not apply to it.

Database cleanup follows the runtime drop. Death release reaches `_DeleteCharCOS`; the pinned reference procedure calls `_DeleteCharCOS_NoTX`, which calls `_ClearCOSInvItems`, returns any still-linked item IDs to `_ItemPool`, clears `_InvCOS`, and deletes `_CharCOS`. Thus world-drop construction precedes deletion of the old COS persistence identities; pickup allocates replacement item identities. Ground scatter/lifetime remains part of the common item-world transition (`0x0048FDF0`), not a separate safe-trade transaction.

AegisCore now treats job-cart death as terminal. One transaction detaches all
`COS_INVENTORY` links, clears every old cargo `ITEM` identity and child rows,
clears the owning instrument's `ITEM.DATA` link, and deletes `CHAR_COS` (whose
foreign keys remove timed services and remaining cart-owned rows). Both
ordinary and specialty cargo enter the world as transient objects and allocate
new `ITEM` identities on pickup; specialty creator metadata is preserved.
Runtime teardown removes the cart from ownership/world registries and clears
the instrument snapshot, so the generic revival path cannot revive the deleted
identity.

The active job COS retains its last published trade scale. Specialty ground
pickup passes that value into the persistence transaction; when the newly
calculated scale differs and is not `1`, the transaction atomically clears
`CHARACTER_SAFE_TRADE.STATUS`. After commit the handler publishes the new
`0x30E8` scale followed by `00 40`, matching setter `0x005237B0` without
emitting resets for unchanged scales or transitions back to scale one.

### Delivery persistence and atomicity

The exact client operation for ordinary NPC delivery is `0x09` when selling
from character storage and `0x14` when selling from the active COS. Handler
`0x005267F0` validates the positive `u16` quantity, resolves the COS for
`0x14`, resolves the item slot, and calls `0x006194E0`. These are ordinary
item-sale operations; neither is `0x2C`.

The exact mode-0 `0x14` request body is
`operation:u8=0x14, activeCOSUniqueID:u32, cosSlot:u8, quantity:u16, npcUniqueID:u32`.
The success body echoes those fields and appends the assigned NPC-buyback
slot. AegisCore decodes and emits this shape and maps the recovered specialty
failures to `0x1860` (Trader only), `0x1861` (no cart), `0x1863` (not buyer),
`0x1865` (cart too far), and `0x1868` (creator/stolen mismatch).

`0x006194E0` performs the specialty delivery lifecycle in this order:

1. Validate the item, merchant, specialty ownership, and quantity.
2. Calculate the `0x004C8D10` quotation-margin payout and apply the common
   gold conversion helpers.
3. Credit the in-memory character through virtual `+0x16C` with reason
   `0x14`.
4. Compute `round(payout * 0.1)` and independently submit tri-job AQ action
   `3`. Its builder `0x00465220` executes `_AddTrijobReward 3, value` for a
   Trader or `_AddTrijobReward 2, value` for a Thief. Submission failure only
   logs `Failed To Post AsycQuery for Update Weekly Trijob Reward!!`; sale
   construction continues.
5. Record the sold-item entry used by ordinary NPC buyback, decrement or
   detach the stack, recalculate trade scale, and construct operation `0x09`
   or `0x14` storage persistence.

The storage updater maps operations `0x09` and `0x14` to
`{?=CALL _STRG_DEL_ITEM (%d, %I64d,%d, %I64d)}` at `0x0045A840`. The procedure
receives storage context, owning storage ID, slot, and item `ID64`; it does not
receive the calculated payout or weekly reward. Gold credit, weekly reward,
and item deletion are therefore not one stock stored-procedure transaction.
AegisCore may intentionally make its implementation atomic, but must describe
that as a stronger server guarantee rather than legacy replay behavior.
AegisCore applies that stronger guarantee to the safe-trade reset as well:
successful specialty COS delivery clears `CHARACTER_SAFE_TRADE.STATUS` in the
same transaction as cargo removal and gold credit, then publishes `00 40`
after commit. A failed or rolled-back sale therefore cannot expose a reset
whose delivery did not commit.

The same delivery transaction advances the Trader job state. GameServer
`0x006194E0 -> 0x00410520 -> 0x004E2830` awards job EXP from the pre-tax
quotation margin: scale one uses `floor(margin * 0.8)`, other scales use
`floor(margin * 0.5)`, with a minimum of one. It adds
`floor(payout * 0.1)` to contribution. `0x0060DD90` proves a maximum job level
of `7` and selects the threshold by job type from the three legacy LevelData
job columns. AegisCore applies EXP, lifetime contribution, and weekly
contribution atomically with cargo removal and publishes the committed
`0x30E6` snapshot after the sale response.

Combat job EXP is a separate path. GameServer `0x004E1F60` reaches
`0x004103E0 -> 0x004E2830`; it converts LevelData column 6 for both target and
recipient with `floor(value * 1.25)`, multiplies the target value by the
target/recipient ratio clamped to `[0.5,1.5]`, and halves the result for a
Trader. Party dispatcher `0x005BD7F0` keeps nearby eligible members, applies a
`1 + 0.05 * (memberCount-1)` bonus, and gives each member its character-level
share of that member's base award. Hunter-led sharing admits only Hunters.
AegisCore now loads the column-6 activity factor, applies these formulas on mob
kills, advances each active job transactionally, updates the online snapshot,
and publishes `0x30E6`; combat awards do not add trade contribution. Normal
character EXP and job EXP retain independent persistence boundaries. Once
normal EXP commits, a later job update or publication failure is logged and
does not suppress party rewards for later members or the remaining kill-side
quest/Hwan/pet lifecycle.

The pinned reference database supplies the previously missing reward-side SQL
body. `_AddTrijobReward(@JobType TINYINT,@Reward INT)` performs exactly:

```sql
UPDATE _TrijobRewards
SET Reward = Reward + @Reward
WHERE JobType = @JobType
```

`_TrijobRewards` has only `JobType tinyint NOT NULL` and
`Reward bigint NOT NULL DEFAULT 0`; its active rows are job types `2` and `3`.
There is no character identifier in this procedure. The reward is therefore a
global thief/trader pool counter, not character job EXP, weekly contribution,
or a per-character replay ledger. AegisCore migration 89 mirrors this boundary
as `TRIJOB_REWARDS` and deliberately keeps its update independent from item
sale persistence.

`_UpdateTriangleJobWeekly` supplies the distribution formula. For job types
`2` and `3`, each character receives
`floor(globalPool * weeklyContribution / totalWeeklyContribution)`, added to
its pending reward and capped at signed `INT_MAX`; the global pools are then
cleared. It snapshots rankings and resets contribution for all three jobs.
AegisCore preserves lifetime contribution separately, resets only
`WEEKLY_CONTRIBUTION`, and exposes the atomic operation as:

```text
sro-data distribute-weekly-job-rewards
```

The reference backup contains neither an SQL Agent job nor another executable
schedule for this procedure. `SR_GameServer.exe` (image base `0x00401000`) also
contains no `_UpdateTriangleJobWeekly` call or procedure string. Its only two
`Weekly Trijob Reward` diagnostics are referenced by specialty-sale function
`0x006194E0` at `0x00619765` and `0x00619803`; those are the per-sale global
pool update, not weekly distribution. Operators must therefore schedule the
maintenance command at their chosen weekly boundary instead of AegisCore
inventing a stock weekday. This operator-owned time choice does not leave the
`0x70E5/0xB0E5` claim transaction or distribution formula partially
implemented.

Operation `0x22` is ordinary buyback restoration. `0x0042AAE0` selects a
server-side sold-item entry, constructs a replacement item, debits its stored
price, and submits `_STRG_RESTORE_SOLDITEM_SPECIALTY` for specialty items.
This is not crash replay of a failed safe-trade settlement.

## Verification status

Focused Ghidra output is under `analysis/decompiled/job-system/client`. It is analysis evidence, not maintained source. Protocol tests are synthetic and exact-order; no sanitized stock JOB capture exists, so the overall vSRO 1.188 profile remains `evidence-pending`.
