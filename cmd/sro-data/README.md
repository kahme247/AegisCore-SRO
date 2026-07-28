# `sro-data`

`sro-data verify` is an offline provenance check. It does not import reference
data, extract PK2 entries, connect to MySQL or alter the archive.

```powershell
go run ./cmd/sro-data verify --profile protocol/profiles/vsro188.json --archive Data/Data.pk2
```

The command compares an operator-supplied profile with the embedded checked-in
`vsro188` profile, validates archive size, SHA-256, fixed PK2 version field and
encryption flag, then writes one deterministic JSON manifest to standard
output.

The current checked-in archive identity is:

- size `2,136,580,096` bytes;
- SHA-256 `0F6191BA393DF32B2258DEE38CC5AB100DC91210D1FA121C1D3845FDC74D8C3B`;
- format `0x01000002`;
- encrypted `true`.

The profile status is still `evidence-pending`. A successful archive check does
not make the dataset or protocol profile publishable. Promotion also requires
the canonical client executable hash/version, locale, captured Content ID and
sanitized wire fixtures tied to the same archive hash.

Game archives are local inputs and must not be committed.

## Import progression and skills

After applying the shard migration catalog, import the exact extracted Media
reference data with an explicit maintenance command:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-reference --media Media/Media
```

This upserts all `LevelData.txt` rows into `REF_LEVEL_DATA` and enabled skill
definitions listed by `SkillData.txt` into `REF_SKILL_DATA`. The importer keeps
the complete source field array in `RAW_FIELDS` and the skill effect fields in
`EFFECTS`, so later gameplay work can use client-authored values without
re-parsing Media files. It never runs automatically during Agent or Gateway
startup.

## Import the full old vSRO skill catalog

`SRO_VT_SHARD` is the authoritative source for native skill relationships.
Export its `_RefSkill` family as JSON with `INCLUDE_NULL_VALUES`, then import
the seven files with:

```powershell
go run ./cmd/migrate --target=shard
go run ./cmd/sro-data import-vsro-skills --export-dir <export-directory>
```

The import preserves all 118 `_RefSkill` columns, all 50 parameters, groups,
masteries, item-opt-level links, character defaults, and extra-skill slots.
