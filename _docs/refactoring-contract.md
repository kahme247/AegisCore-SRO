# Behavior-preserving refactoring contract

This document records the compatibility boundary for the refactoring branch.
It is intentionally narrower than the project roadmap: the refactor may change
internal ownership and file/package layout, but it may not silently change game
rules or the vSRO 1.88 contract.

## Runtime boundary

- The deployable topology remains MySQL, Gateway, and Agent.
- Gateway remains responsible for profile/patch replies, notices, shard list,
  credentials, admission policy, and one-time Agent ticket installation.
- Agent remains responsible for lobby, world ownership, movement, visibility,
  gameplay handlers, and persistence checkpoints.
- Ports, environment variables, Compose service names, admission mTLS, startup
  timeouts, shutdown timeouts, and session idle/massive-message limits remain
  unchanged.

## Protocol boundary

- Opcode values, packet fields, byte order, response codes, packet ordering,
  encryption, EDC policy, and massive `0x600D` framing are compatibility data.
- The checked-in packet fixture tests are golden contracts. A byte change must
  be an isolated bug fix backed by a sanitized stock-client capture or another
  authoritative vSRO 1.88 invariant.
- Unsupported entity and quest formats continue to fail closed; refactoring is
  not permission to guess wire layouts.

## Gameplay and persistence boundary

- Movement generation, collision correction, region changes, visibility,
  unique-ID allocation, inventory slots, formulas, locking, and disconnect
  cleanup retain their current semantics.
- Existing transaction boundaries remain authoritative: repository operations
  either commit their complete state transition or return without publishing a
  partial in-memory result.
- Migration filenames, versions, checksums, SQL text, advisory lock names, and
  target selection are unchanged by this refactor.

## Change discipline

- Refactoring commits must keep `scripts/verify.ps1` green.
- Intentional behavior fixes require a separate `fix:` commit and a regression
  test that fails against the previous behavior.
- Dependency upgrades, schema redesign, new gameplay, navmesh memory strategy,
  and process extraction are outside this branch.
