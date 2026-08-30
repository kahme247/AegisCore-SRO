# `sro-data`

`sro-data` provides offline archive verification and explicit reference-data
maintenance. None of its commands run during Agent or Gateway startup.

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

## Import Media references

Apply the shard schema first, then import extracted Media data:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-reference --media Media/Media
```

The importer expects `server_dep/silkroad/textdata/leveldata.txt` and
`skilldata.txt`, follows the filenames listed by `skilldata.txt`, and upserts
levels plus enabled skills in one transaction. Duplicate levels/skill IDs and
short rows are rejected. UTF-16LE is recognized only with a BOM. `RAW_FIELDS`
preserves parsed fields, not original whitespace. Rows absent from the current
source are not deleted, so this is not strict synchronization.

Shard migration `000015` provides `REF_LEVEL_DATA` and `REF_SKILL_DATA`.

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
| `ref_ability_item_opt_level.json` | `REF_ABILITY_ITEM_OPT_LEVEL_VSRO` |
| `ref_magic_opt_item_opt_level.json` | `REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO` |
| `ref_set_item_group.json` | `REF_SET_ITEM_GROUP_VSRO` |

Shard migration `000020` provides these tables. No checked-in exporter exists;
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
Agent completion currently consumes only the two quest flows whose script and
stock-client packet layouts are recovered; unsupported reward mechanisms fail
closed instead of being silently ignored. Shard migration `000054` provides
the target tables.

The supplied `Quest.sct` is a standard little-endian Lua 5.1 binary chunk. It
can be decoded without executing Lua or loading native extensions:

```powershell
go run ./cmd/sro-data inspect-vsro-quest-script --script Script/VIETNAM/Quest.sct
go run ./cmd/sro-data import-vsro-quest-script --script Script/VIETNAM/Quest.sct
```

The importer stores the bounded declarative call stream, source name and exact
script SHA-256 in `REF_QUEST_SCRIPT_DECLARATION`. Multiple authored variants
of one quest remain separate rows. Migration `000055` provides this catalog;
storing declarations does not enable mission types whose runtime and packets
are not yet proven.

The same import normalizes the exact ten-argument
`LuaSetMissionData(...MISSION_TYPE_KILL_MONSTER...)` form into
`REF_QUEST_KILL_OBJECTIVE`. All 126 recovered objectives are retained, but only
the capture-backed Chulsan objective is runtime-enabled. Migration `000056`
provides the typed catalog.

`LuaSetAchievedItem` declarations are normalized into script reward and reward
item tables. For runtime-enabled quests, completion requires the script EXP,
skill EXP, gold, item identity and quantity to agree with the SQL reference
catalog before any character or inventory mutation. Migration `000058`
provides these cross-check tables.

The variable-length `LuaSetDeliverMissionData` form is decoded as an exact
item count followed by quantity/code-name pairs. Migration `000059` normalizes
all 64 standard deliver-item objectives and their 66 item entries. They remain
runtime-disabled until their dialogue and turn-in packet transitions have
capture-backed codecs.

Migration `000060` similarly normalizes all 48 fixed-layout
`LuaSetDialLogMissionData` objectives, preserving their objective, parameter and
dialogue keys. They remain runtime-disabled pending the same packet evidence.

Migration `000061` normalizes the nine `LuaSetKillPlayerMissionData`
declarations and their variable job-state filter lists. Region and other filter
parameters are preserved exactly; runtime remains disabled until PvP target
qualification and quest-result packets are capture-backed.

Migration `000062` normalizes all 258 standard and two extended
monster-item collection declarations plus their 726 monster-source entries.
The source numeric field and all EX trailing fields retain their exact script
values without assigning unproven probability semantics; runtime remains
disabled pending drop-generation and quest-progress packet evidence.

Migration `000063` normalizes all 77 extended kill declarations and their 280
target references. The 20 classified and 57 multi-target layouts remain
distinct, and the complete positional arguments preserve the authored
fractional value without assigning unproven count semantics. Runtime remains
disabled pending target-selection and progress packet evidence.

Migration `000064` normalizes all four `LuaSetTalismanMissionData`
declarations. Each exact seven-argument layout retains its objective key,
mission NPC, Forgotten World theme code and final positional numeric field.
Runtime remains disabled pending talisman collection-event and progress-packet
evidence.

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
