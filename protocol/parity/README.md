# vSRO 1.188 parity manifest

`vsro188.json` is the maintained acceptance contract for the consolidated
Agent implementation of the legacy `SR_GameServer` and `SR_ShardManager`
responsibilities. It records only metadata and evidence locators; the legacy
binaries, extracted strings, captures and proprietary data remain outside the
maintained Go source tree.

From the `go-sro` worktree, audit the embedded manifest against the local
read-only evidence catalogs with:

```powershell
.\.tools\go\bin\go.exe run ./cmd/parity-audit -workspace .
```

The audit fails when the unique opcode, procedure or table inventory changes.
It also hashes all eight legacy executables contributing to those catalogs and
compares the current evidence bytes with the manifest before trusting any row.
An inventory match does not prove that extracted names are callable contracts:
the SQL catalog includes string-extraction candidates whose call sites still
need function-level confirmation.

The same command builds a row-level matrix from the read-only evidence inputs.
It preserves every opcode alias instead of collapsing numeric values, groups
the exact SQL object spellings used by the catalog, inventories GameServer and
ShardManager configuration directives, includes disabled schedule definitions,
fingerprints the five compiled Lua artifacts, and adds explicit entity-family
and external-integration obligations. The `matrix unresolved` count is expected
to remain nonzero until owners, primary function/dispatch evidence, tests and
capture validation have been assigned per row.

The audit prints a count for every missing required field. Use
`-matrix-json <path>` to export the complete, deterministically ordered matrix,
or `-matrix-json -` for standard output. Empty aggregate, repository, function,
or test fields remain explicit unresolved obligations and are never inferred.

Use `-require-complete` only as a release gate. It fails until every capability
is either `verified`, with implementation and test references, or `replaced`,
with a documented secure replacement and tests. Disabled-by-default legacy
features remain part of the gate, and any unresolved matrix row also fails the
strict gate. `implemented` never means evidence-exact stock-client parity.

Capability evidence must identify a reviewable source and locator. Client-wire
features additionally require sanitized vSRO 1.188 captures following
`protocol/captures/README.md`; stateful features require transaction, retry,
disconnect and recovery tests before promotion to `verified`.
