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

## Import safety

Unlike `cmd/migrate`, importers do not verify the exact active schema, acquire
an advisory lock, inspect the migration ledger, checksum source files or stop
runtime writes. Before any import:

1. Stop dependent writes and back up `SRO_SHARD`.
2. Confirm `DB_SHARD_DATABASE=SRO_SHARD` and apply migrations through `000029`.
3. Validate filenames, JSON shape and expected source row counts.
4. Run one importer.
5. Verify target row counts and representative relationships before restoring
   writes.

## Verification

```powershell
go test ./cmd/sro-data/...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
```
