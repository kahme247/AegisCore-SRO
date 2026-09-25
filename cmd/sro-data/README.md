# `sro-data`

`sro-data` provides offline archive verification and explicit reference-data
maintenance. None of its commands run during Agent or Gateway startup.

Use `verify` for a read-only archive check. `audit-item-coverage`,
`verify-drop-catalog`, and `verify-dimension-hole` are read-only shard audits.
Every `import-*` command writes to `SRO_SHARD` and must be treated as database
maintenance.

| Need | Start here |
| --- | --- |
| Check `Data.pk2` without changing the database | [Verify Data.pk2](#verify-datapk2) |
| Find the database table for a Media file, or the reverse | [Map Media files to tables](#map-media-files-to-tables) |
| Audit usable items, linked skills, and loot sources | [Audit item and drop coverage](#audit-item-and-drop-coverage) |
| Prove Dimension Hole startup data is complete | [Verify Dimension Hole readiness](#verify-dimension-hole-readiness) |
| Export database-backed Media files | [Export Media files](#export-database-backed-media-files) |
| Load Media reference catalogs | [Import Media references](#import-media-references) |
| Load drops, Item Mall, skills, items, or quests | Open the matching import section below |
| Review write and replacement behavior | [Import safety](#import-safety) |

`distribute-weekly-job-rewards` is an explicit shard maintenance write. It
proportionally distributes the accumulated Thief/Hunter global pools by weekly
contribution, caps pending rewards, clears both pools, and resets weekly
contribution atomically. The reference backup does not preserve the legacy
execution schedule; invoke it from an operator-owned weekly scheduler.

## Map Media files to tables

Use `media-map` before editing a file or table:

```powershell
go run ./cmd/sro-data media-map --file teleportdata.txt
go run ./cmd/sro-data media-map --table REF_LEVEL_DATA
go run ./cmd/sro-data media-map --file gacha
```

The tab-separated output identifies the related table, current authority,
database update command, and Media synchronization support. `MEDIA_SYNC` is
`export-media` only when the current exporter regenerates that file from the
database; `not-supported` means the DB and PK2 file must not be assumed aligned.
The authority column also marks superseded, client/file-only, candidate, and
unmapped catalogs. Filename patterns such as `itemdata_*.txt` and
`skilldata_*.txt` cover their chunk families.

Migration `000136` stores this mapping in
`SRO_SHARD.MEDIA_CATALOG_MAPPING`; `media-map` reads that registry rather
than a compiled list. `MEDIA_*` views provide filename-aligned discovery over
the normalized tables. `RECONCILE=snapshot` means a fresh export represents
inserts, updates, and deletions from the mapped database catalog.

Use the fail-closed synchronization wrapper when rebuilding an extracted Media
tree after database edits:

```powershell
go run ./cmd/sro-data media-sync --table REF_LEVEL_DATA `
  --output ./.local/media-sync-levels
go run ./cmd/sro-data media-sync --supported-only `
  --output ./.local/media-sync-supported
```

The first form refuses a mapped table if any selected file is not a complete
snapshot export. With no table filter, `media-sync` validates the full registry
and lists every unsupported database-backed file instead of producing a
partially aligned tree. `--supported-only` explicitly exports only the catalogs
whose registry rows are both `export-media` and `snapshot`.

## Verify Data.pk2

```powershell
go run ./cmd/sro-data verify `
  --profile protocol/profiles/vsro188.json `
  --archive Data/Data.pk2 > provenance.json
```

Verification requires the supplied profile to match the embedded canonical
profile, checks archive size before hashing, validates the fixed PK2 version and
encryption fields, streams SHA-256, and writes deterministic manifest schema 1.
It does not parse PK2 directories or entries, validate captures, or connect to
MySQL.

Current checked-in archive identity:

- size `2,136,580,096` bytes;
- SHA-256 `0F6191BA393DF32B2258DEE38CC5AB100DC91210D1FA121C1D3845FDC74D8C3B`;
- format `0x01000002`;
- encrypted `true`.

The profile remains `evidence-pending`. Successful archive verification does
not establish a canonical client executable, locale, Content ID or sanitized
wire corpus. Game archives are local inputs and must not be committed.

## Export database-backed Media files

After shard migration `000081`, seed the catalogs that do not yet have a full
typed schema. This stores only an explicit filename whitelist and replaces the
previous passthrough snapshot transactionally:

```powershell
go run ./cmd/sro-data import-media-catalogs --media ./Media/Media
```

Character, world, shop, siege, magic-option, and teleport text rows are then
stored in `REF_MEDIA_TEXT_CATALOG*`. They remain editable database JSON rows
until dedicated typed tables are available.

Create a new extracted Media tree from one consistent, read-only snapshot of
`SRO_SHARD`:

```powershell
go run ./cmd/sro-data export-media --output ./.local/media-export-189
```

The output directory must not already exist. The command generates level data,
plain and client-encrypted skill chunks, item chunks, package/price/set data,
quest/reward data, and seeded passthrough catalogs as UTF-16LE files. Normalized database values are written
over preserved source fields, so supported database edits appear without
losing unknown client columns. `media-export-manifest.json` records row counts,
sizes, SHA-256 hashes, and the catalogs that cannot yet be reconstructed from
the current schema or have not yet been seeded.

The command never edits `Media.pk2` or publishes a release. Connect the verified
manifest to the review-only patch scanner:

```powershell
go run ./cmd/patch-admin scan-media `
  --archive ./VSRO_Client/Media.pk2 `
  --target-root ./.local/media-export-189 `
  --export-manifest ./.local/media-export-189/media-export-manifest.json `
  --output ./PatchSource/188-to-189.json
```

Only manifest-listed files are compared; the manifest itself is not shipped to
the client. Review the resulting specification before building or publishing.

## Import Media references

Apply the shard schema first, then import extracted Media data:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-reference --media Media/Media
```

The importer expects `server_dep/silkroad/textdata/leveldata.txt`,
`levelgold.txt`, `gameworlddata.txt`, and `skilldata.txt`, follows the filenames listed by
`skilldata.txt`, and upserts levels plus enabled skills in one transaction.
`levelgold.txt` must contain the complete 140-row level/GoldMin/GoldMax catalog;
its values populate the restoration-price columns added by shard migration
`000072`. Duplicate levels/skill IDs, missing gold levels, and short rows are
rejected. UTF-16LE is recognized only with a BOM. `RAW_FIELDS` preserves parsed
fields, not original whitespace. Rows absent from the current source are not
deleted, so this is not strict synchronization.

Shard migration `000015` provides `REF_LEVEL_DATA` and `REF_SKILL_DATA`;
`000072` extends the level catalog with `DROP_GOLD_MIN` and `DROP_GOLD_MAX`.
Shard migration `000128` extends `REF_GAME_WORLD` with the stock lifecycle
contract. Because the six-field Media export omits those server-only columns,
the importer supplies the reference-database values only for the pinned
Dimension Hole worlds: entry/entrance/leave type `0`, duration `7200000 ms`,
and empty-remain time `600000 ms`. Other worlds retain zero until an
authoritative server-side source is imported.

Shard migrations `000130`, `000131`, and `000135` add the normalized
allowed-region, instance-start, and item-to-world definition catalogs.
`REF_DIMENSION_HOLE` is the authority that selects enabled Dimension Hole
items and worlds; `import-reference` refreshes their Media-derived metadata.

## Verify Dimension Hole readiness

After the shard migrations and reference import, run the read-only gate:

```powershell
go run ./cmd/sro-data verify-dimension-hole
```

Success requires every enabled `REF_DIMENSION_HOLE` mapping to resolve a valid
item and world, with at least one allowed region and validated start position
for every world. Missing tables or rows, altered lifecycle metadata, invalid
regions, duplicate starts, or unsupported nonzero `Param` values return a
non-zero status. Agent performs the same preflight before enabling Dimension
Hole item use.

## Audit item and drop coverage

Create deterministic item and loot reports from one repeatable-read,
read-only shard snapshot:

```powershell
go run ./cmd/sro-data audit-item-coverage `
  --output-dir ./.local/reports/item-coverage
```

The command writes `items.csv`, `drop_sources.csv`, and `summary.md`. The item
report includes TypeIDs, parameters, the resolved active `REF_SKILL` link,
decoded effect family, implementation status, reason, and loot-source count.
The normalized drop report follows direct monster assignments, random groups,
and level-profile item assignments while retaining missing references as
broken rows. References absent from the active runtime catalogs but present as
`SERVICE=0` in `REF_OBJ_COMMON_VSRO` are reported as disabled and are excluded
from effective loot-source counts; they are not data corruption. `CanDrop`
remains the player's permission to drop an inventory item; it is not treated as
proof that a monster produces that item.

Use a gate only after reviewing the baseline report. Reports are still written
before the command returns a failing status:

```powershell
go run ./cmd/sro-data audit-item-coverage `
  --output-dir ./.local/reports/item-coverage `
  --fail-on broken

go run ./cmd/sro-data audit-item-coverage `
  --output-dir ./.local/reports/item-coverage `
  --fail-on unsupported
```

Validate the normalized drop catalogs without writing reports or changing the
database:

```powershell
go run ./cmd/sro-data verify-drop-catalog
```

The verifier checks item, monster, group, magic-option, probability, range,
classification, duplicate, and orphan boundaries. It reports normalized edges
skipped because their authoritative item or monster has `SERVICE=0`; only
genuinely missing, wrong-type, or enabled-but-unloaded references fail. Empty
optional profile catalogs are reported as warnings; broken edges or invalid
values return a non-zero exit status.

## Import event rewards

After applying shard migration `000077`, import the event catalogs independently
from the normal level/skill import:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-event-rewards --media Media/Media
```

The command reads `refeventreward.txt` and `refeventrewarditems.txt`, resolves
their event rows into `REF_EVENT_REWARD` and `REF_EVENT_REWARD_ITEM`, and
replaces each event's item rows transactionally. The current checked-in Media
snapshot contains BOM-only files, so it correctly imports zero rows; a populated
server reference export is required to enable event rewards.

## Import server-event rewards

The broader binary-confirmed `_RefServerEventReward` families are kept in
their own catalog. After shard migration `000079`, run:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-server-event-rewards --media Media/Media
```

The importer accepts UTF-16 tab-separated exports named
`refservereventreward.txt`, `refservereventreward_expupforplayers.txt`,
`refservereventreward_spawnmonster.txt`, and `refservereventreward_event.txt`.
Missing files are treated as an empty catalog. Base rows contain the confirmed
reward ID, skill ID, class, flags, apply time, three reference values, and the
experience/skill ratios; subtype and `(ServerEventID, RewardID, AQType)` rows
are validated and committed together. These records are not quest rewards and
are not consumed by the quest completion transaction.

## Import monster drops

Apply shard migrations `000078` and `000080`, then import the four client-matched drop
catalogs from a semicolon-delimited export directory:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-drop-catalog --data-dir <drop-table-directory>
```

The directory must contain `_RefDropGold.csv`, `_RefDropItemGroup.csv`,
`_RefMonster_AssignedItemRndDrop.csv`, `_RefMonster_AssignedItemDrop.csv`,
`_RefDropItemAssign.csv`, `_RefDropOptLvlSel.csv`, and all ten
`_RefDropClassSel_*.csv` files, including their header rows. The import
replaces all four normalized tables in one transaction and preserves the
binary field boundaries for amount ranges, ratios, overlap, option IDs, and
custom values. Drop selection remains disabled until this catalog is loaded
successfully during Agent startup.

## Import Item Mall

Replace the Item Mall catalog from extracted Media files:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-item-mall --media Media/Media
```

The importer follows the enabled country `15` shop group, shop, tab, goods,
package, scrap and price-policy chain under `server_dep/silkroad/textdata`.
Only `MALL_` tabs with positive Silk (device `2`), Gift Silk (device `4`) or
Silk Point (device `16`) prices are imported. Source order defines group and
tab indices. Item IDs are resolved by exact `ITEMDATA.CodeName`; unresolved or
duplicate joins, ambiguous mapped prices, out-of-range values and an empty
result abort without changing the existing catalog. A successful import
deletes and inserts `ITEM_MALL_GOODS` in one transaction. For stackable items,
the package scrap data field becomes `QUANTITY` (for example, the reverse-return
scroll package contains 11); non-stackable items retain it as item data.
`ENABLED` is set to `1`, and opt level and variance come from the scrap row.

The scrap `Data` column is a union in the Media format. When the resolved
`ITEMDATA.Stack_Size` is greater than one, a positive scrap value is the
package's delivered stack quantity and is imported as `QUANTITY` with item
`DATA=0`. For non-stackable items it remains item data (for example, equipment
durability) and `QUANTITY` remains one. This distinction prevents the client
from displaying a Media count that disagrees with the persisted stack count.

Re-run the import after changing this mapping; editing the importer does not
update an existing catalog. Restart Agent after a successful import:

```powershell
go run ./cmd/sro-data import-item-mall --media Media/Media
docker compose restart agent-server
```

Shard migration `000030` provides `ITEM_MALL_GOODS`.

## Import specialty quotations

`_ItemQuotation` is a server database catalog; it is not present in client
Media. Export the ten stock columns from the pinned v188 shard backup without
headers, then replace the maintained quotation catalog transactionally:

```powershell
$quotationExport = ".local/itemquotation.psv"
docker exec gosro-sql-reference /opt/mssql-tools18/bin/sqlcmd -C -S localhost -U sa -P "GoSroReference!2026" -d SRO_VT_SHARD -h -1 -W -s "|" -Q "SET NOCOUNT ON; SELECT ID,Service,AssocNPC,RefItemID,BaseQuot,Quot_LB,Quot_UB,BaseStockAmount,FluctuateAmount,CurStockAmount FROM dbo._ItemQuotation ORDER BY ID" | Set-Content -Encoding utf8 $quotationExport
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-specialty-quotations --source $quotationExport
```

The importer requires exactly ten pipe-delimited fields per row, preserves
SQL Server `REAL` precision, rejects duplicate IDs and NPC/item pairs, checks
quotation bounds and stock counters, and deletes/inserts `ITEM_QUOTATION` in
one MySQL transaction. Foreign keys require the matching `CHAR_REF_DATA` and
`ITEMDATA` catalogs to be imported first. The export remains an ignored local
artifact; do not commit the proprietary rows. Migration `000088` provides the
destination schema.

## Import reverse-return map locations

After applying shard migration `000067`, import the client-matched optional
teleport catalog used by the reverse-return scroll's map selection:

```powershell
go run ./cmd/sro-data import-optional-teleports --file Media/Media/server_dep/silkroad/textdata/refoptionalteleport.txt
```

The command replaces `REF_OPTIONAL_TELEPORT` transactionally without copying
the proprietary Media input into the repository.

## Import native vSRO skills

Export the SQL Server `_RefSkill` family as JSON arrays with
`INCLUDE_NULL_VALUES`, then run:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-vsro-skills --export-dir <export-directory>
```

Required files:

```text
ref_skill.json
ref_skill_by_item_opt_level.json
ref_skill_group.json
ref_skill_group_by_item.json
ref_skill_mastery.json
ref_default_skill.json
ref_char_default_skill.json
```

This is a destructive full replacement of seven target tables in one
transaction, not an upsert. Empty arrays are accepted and can leave a table
empty. Columns come from each file's first row; incorrect names or omitted null
columns can fail insertion. Migrations `000016`-`000019` provide the schema and
source-compatibility adjustments.

## Import native vSRO items

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-vsro-items --export-dir <export-directory>
```

Required files and targets:

| File | Table |
| --- | --- |
| `ref_obj_common.json` | `REF_OBJ_COMMON_VSRO` |
| `ref_obj_item.json` | `REF_OBJ_ITEM_VSRO` |
| `ref_obj_struct.json` | `REF_OBJ_STRUCT_VSRO` |
| `ref_package_item.json` | `REF_PACKAGE_ITEM_VSRO` |
| `ref_price_policy_item.json` | `REF_PRICE_POLICY_ITEM_VSRO` |
| `ref_scrap_of_package_item.json` | `REF_SCRAP_OF_PACKAGE_ITEM_VSRO` |
| `ref_shop_group.json` | `REF_SHOP_GROUP_VSRO` |
| `ref_mapping_shop_group.json` | `REF_MAPPING_SHOP_GROUP_VSRO` |
| `ref_mapping_shop_with_tab.json` | `REF_MAPPING_SHOP_WITH_TAB_VSRO` |
| `ref_shop_tab.json` | `REF_SHOP_TAB_VSRO` |
| `ref_shop_goods.json` | `REF_SHOP_GOODS_PACKAGE_VSRO` |
| `ref_ability_item_opt_level.json` | `REF_ABILITY_ITEM_OPT_LEVEL_VSRO` |
| `ref_magic_opt_item_opt_level.json` | `REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO` |
| `ref_set_item_group.json` | `REF_SET_ITEM_GROUP_VSRO` |

Shard migrations `000020`, `000091`, `000092`, and `000123` provide these tables.
The common export must include `_RefObjCommon.Link`. The importer validates
one-to-one `_RefObjCommon.Link` mappings for both TypeID1=3 items and
TypeID1=4 structures before opening the replacement transaction. For example,
the stock `INS_QUEST_TELEPORT` common row has public `ID=19556` and private
`Link=7`, so its structure row is `ref_obj_struct.json` `ID=7`, not `ID=19556`.
`_RefObjCommon.ID` remains the public object reference and must not be used as
the detail-row join. The structure row's source fields are preserved in
`RAW_DATA`; the importer does not synthesize teleport interaction/task bytes
from `teleportbuilding.txt`.

The package
import retains every `_RefScrapOfPackageItem` member and its stock `Index`, so
multi-member vectors are not flattened to one item. It also retains shop-tab
placement, the NPC-to-shop-to-tab authority chain, and the actual price-policy
`Cost`. No checked-in exporter exists;
operators must generate JSON arrays with exact filenames and source columns.

## Import native vSRO quests

Export `_RefQuest`, `_RefQuestReward`, and `_RefQuestRewardItems` from the
matching `SRO_VT_SHARD` database as JSON arrays named `ref_quest.json`,
`ref_quest_reward.json`, and `ref_quest_reward_items.json`, then run:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-vsro-quests --export-dir .local/vsro-quest-export
```

The import replaces all three quest reference catalogs in one transaction.
Agent discovery and completion consume the original Chulsan delivery flow plus
variant-zero, single-objective kill quests whose active-record and reward
layouts pass the runtime compatibility filters. Unsupported objective,
qualification and reward mechanisms fail closed instead of being silently
ignored. Shard migration `000054` provides the target tables.

The supplied `Quest.sct` is a standard little-endian Lua 5.1 binary chunk. It
can be decoded without executing Lua or loading native extensions:

```powershell
go run ./cmd/sro-data inspect-vsro-quest-script --script Script/VIETNAM/Quest.sct
go run ./cmd/sro-data inspect-vsro-quest-script --script Script/VIETNAM/Quest.sct --runtime-coverage
go run ./cmd/sro-data inspect-vsro-quest-script --script Script/VIETNAM/Quest.sct --extended-kill
go run ./cmd/sro-data inspect-vsro-quest-script --script Script/VIETNAM/Quest.sct --kill-player
go run ./cmd/sro-data import-vsro-quest-script --script Script/VIETNAM/Quest.sct
```

The importer stores the bounded declarative call stream, source name and exact
script SHA-256 in `REF_QUEST_SCRIPT_DECLARATION`. Multiple authored variants
of one quest remain separate rows. Migration `000055` provides this catalog;
storing declarations does not enable mission types whose runtime and packets
are not yet proven.

The same import normalizes the exact ten-argument
`LuaSetMissionData(...MISSION_TYPE_KILL_MONSTER...)` form into
`REF_QUEST_KILL_OBJECTIVE`. All 126 recovered objectives are retained. The
importer runtime-enables only variant-zero kill quests that contain exactly one
objective across every recovered mission family; Agent applies additional NPC,
level, dependency, policy and reward filters before displaying them. Migration
`000056` provides the typed catalog.

`LuaSetAchievedItem` declarations are normalized into script reward and reward
item tables. For runtime-enabled quests, completion requires the script EXP,
skill EXP, gold, item identity and quantity to agree with the SQL reference
catalog before any character or inventory mutation. Migration `000058`
provides these cross-check tables.

The variable-length `LuaSetDeliverMissionData` form is decoded as an exact
item count followed by quantity/code-name pairs. Migration `000059` normalizes
all 64 standard deliver-item objectives and their 66 item entries. The importer
runtime-enables only variant-zero quests with mission index zero and exactly
one objective across all recovered mission families. Agent locks the active
quest and inventory together at the authored target NPC, verifies and consumes
the required stacks (including partial stacks) only after the authenticated
delivery prompt and final `0x30D4` selection, promotes the quest to reward
ready, then publishes inventory changes and the typed `0x30D5` delta. Initial
NPC contact only lists eligible deliveries and never consumes an item. Other
standard delivery variants remain disabled.

Migration `000060` similarly normalizes all 48 fixed-layout
`LuaSetDialLogMissionData` objectives, preserving their objective, parameter and
dialogue keys. The binary-confirmed event completes only after the final
authenticated `0x30D4` dialogue selection, rather than on initial NPC contact.
Migration `000095` preserves the matching NPC/menu route. The importer enables
only variant-zero, mission-zero, single-objective dialog quests; Agent retains
the selected route in its authenticated NPC interaction and publishes progress
only after the final confirmation. Multi-page and multi-objective shapes stay
disabled.

Migration `000061` normalizes the nine `LuaSetKillPlayerMissionData`
declarations and their variable job-state filter lists. Region and other filter
parameters are preserved exactly; the scalar after the Temple region is the
minimum victim level (`105` for all nine rows). The importer enables only variant-zero,
mission-zero, single-objective rows using the recovered `MKP_LEVEL`, `MKP_JOB`,
and `MKP_REGION` trio with recognized trader, robber/thief, and hunter states.
Agent qualifies lethal PvP events from the victim's authoritative level, job,
and signed navmesh region, persists the counter, and publishes it through the
ordinary `0x30D5` quest delta. Unknown filter vocabularies remain disabled.

Migration `000062` normalizes all 258 standard and two extended
monster-item collection declarations plus their 726 monster-source entries.
The source numeric field and all EX trailing fields retain their exact script
values. The importer runtime-enables only the recovered standard,
variant-zero, mission-zero, single-objective shape when every source chance is
within `1..100` and the generated stack count fits the wire/materializer
width. Extended and multi-objective shapes remain disabled. Agent now resolves
the active objective at monster death, rolls its authored source percentage,
materializes the quest item as ground loot, and advances progress after the
committed pickup.

Migration `000063` normalizes all 77 extended kill declarations and their 280
target references. The 20 classified and 57 multi-target layouts remain
distinct, and the complete positional arguments preserve the authored
fractional value. GameServer converts that value to an integer goal (the lone
`1.5` Giant value becomes `1`) and keeps separate normal, champion and giant
counters. Classified declarations remain runtime-disabled because their
multi-mission task-state projection is not implemented.

Migration `000064` normalizes all four `LuaSetTalismanMissionData`
declarations. Each exact seven-argument layout retains its objective key,
mission NPC, Forgotten World theme code and final positional numeric field.
GameServer `0x0049ACA0` and `0x004A0780` prove that using a
`TypeID3=12, TypeID4=5` talisman registers and consumes it; ordinary ground
pickup does not register it. The registration is persisted through
`_AddCollectionBook_Item(CharID, ThemeID, SlotIndex)`, then internal event
`243` reaches `0x00922080`. `0x0091E050` proves that completion is the collection-book
theme-accomplished predicate; the final numeric field is only a non-zero
callback gate. Migration `000096` installs the four stock themes and 32 item
slots; successful `0x704C` use now persists the registration, consumes one
talisman, completes matching enabled quests when all eight slots exist, and
publishes the zero-valued `0x30D5` delta confirmed by the binary. The same
safe single-objective shape participates in normal quest offer discovery,
script-reward validation and reconnect hydration.

Migration `000065` normalizes the 534 `LuaSetStartCodition` level-window
declarations, 534 conversation start methods and their one or two NPC routes,
plus 239 dependency groups. Dependency groups retain the authored all-clear or
one-clear mode and resolve every referenced quest by identity. Original
positional arguments and the script checksum remain available for evidence
auditing. Non-empty variant-zero groups are runtime-enabled; Agent evaluates
both modes during offer discovery against completed
status-`4` rows with a positive achievement count. Rewarded completion
increments that count, reconnect retains the completed-ID projection, and
repeat activation remains bounded by the authored achievement limit.

Migration `000066` normalizes 534 completion policies, including authored
achievement, division, real-time, day, time-limit and Hwan fields, plus 39 job
and four race start conditions. The exact variant-zero `QSC_RACE` forms for
`COUNTRY_CHINA` and `COUNTRY_EUROPE` are runtime-enabled and evaluated against
the character reference's stock country discriminator (`0`/`1`); unknown
condition or country keys remain disabled. The four variant-zero
`LuaSetHwanLevel` declarations (levels `3`/`4`) are also enabled and require an
exact match with the character's durable Hwan level; real-time, day/night and
day-limit policies remain disabled. The two exact `LuaSetLimitTimeStartCondition(120)`
rows are enabled with durable ten-second checkpoints and reconnect resume via
migration `000098`. It also stores all 433 skill-point and 36 named
point reward declarations as 469 typed rows. The 433 variant-zero fixed
skill-point rows are enabled and cross-checked against the SQL reward scalar.
All 36 variant-zero named-point rows use the binary-whitelisted
`TEMPLE_ANUBIS` or `TEMPLE_HAROERIS` key and are enabled; migration `000097`
stores their additive balances independently from skill and Academy points.

All 39 job declarations also share the recovered variant-zero
`(1, OBJ_JOBSTATE_TRADER|ROBBER|HUNTER, 1)` form. They are runtime-enabled and
offer discovery matches them against `CHARACTER_JOB.JOB_TYPE` for every
independently supported objective family.

## Import safety

Unlike `cmd/migrate`, importers do not verify the exact active schema, acquire
an advisory lock, inspect the migration ledger, checksum source files or stop
runtime writes. Before any import:

1. Stop dependent writes and back up `SRO_SHARD`.
2. Confirm `DB_SHARD_DATABASE=SRO_SHARD` and apply all current migrations.
3. Validate filenames, JSON shape and expected source row counts.
4. Run one importer.
5. Verify target row counts and representative relationships before restoring
   writes.

## Verification

```powershell
go test ./cmd/sro-data/...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
```
