# vSRO 1.188 opcode online research

Research date: 2026-07-16

This report surveys the public vSRO/Silkroad protocol material that can help
AegisCore. It does not treat an online opcode list as protocol proof. The target
remains the pinned Vietnam vSRO **1.188** client, and sanitized bytes from that
exact client outrank every source below.

## Executive result

There is no trustworthy, complete, version-safe public vSRO opcode database.
The useful material is split between:

- SilkroadDoc's vSRO 1.188 tables and packet-layout pages;
- RSBot's executable readers and writers, with client-version branches;
- Phoenix and modern network libraries for transport/security behavior;
- capture/analyzer tools for producing our own authoritative fixtures;
- the local TestIn binaries for original-server handler confirmation.

AegisCore currently declares **173 opcode constants / 169 unique numeric values**
and registers **55 unique application-level inbound handlers**, in addition to
the four framework handlers for `0x5000`, `0x9000`, `0x2001`, and `0x2002`.
For comparison, the inspected SilkroadDoc summary tables mention **414 unique
numeric values**, while the inspected RSBot tree has **123 unique values in
explicit handler declarations**. These counts measure catalog breadth, not
implementation completeness: many documented packets are server-to-client
notifications, internal framework messages, newer-version branches, or packet
families that AegisCore intentionally does not implement yet.

The highest-value improvement is therefore not to paste hundreds of numbers
into Go constants. It is to build one provenance-aware registry, generate the
code/docs/audits from it, and promote packet layouts only after capture or
function-level TestIn confirmation.

## Evidence hierarchy

1. Sanitized wire bytes from the exact pinned client executable and Data.pk2.
2. Function-level behavior recovered from the local read-only TestIn servers.
3. SilkroadDoc pages explicitly based on vSRO 1.188.
4. RSBot behavior when the relevant path is confirmed to apply to vSRO 188.
5. Agreement among independent implementations such as Phoenix,
   Silkroad.Net, and skrillax-network.
6. Historical emulator or analyzer behavior.
7. Unversioned forum lists, which are discovery leads only.

An opcode record is incomplete unless it includes service, direction, session
phase, version/profile, framing/encryption requirements, payload status,
source, and confidence. The high nibble is a useful naming convention, not a
safe substitute for explicit direction: vSRO has exceptions such as the
party-matching player-join pair `0x306E` (client to server) and `0x706D`
(server to client).

## Primary opcode and layout sources

### SilkroadDoc wiki

- Repository: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc>
- Wiki: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki>
- Packet index: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/Packets>
- Gateway table: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/Gateway-packets>
- Agent table: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/Agent-packets>
- Split packets: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/Split-packets>
- Security: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/silkroad-security>
- Inspected wiki snapshot: commit
  `21e8ccdeaecb7ad7d641352ffc78da287377ec9c` (2025-05-13).

This is the strongest public, version-pinned documentation. It defines the
message-type nibble convention and exposes Gateway, Agent, global/framework,
massive, and manual split packet families. It is still incomplete, contains
work-in-progress entries, and occasionally conflicts with executable clients.
The wiki/repository does not provide a clear license for copying content into
the server, so use facts and independent implementation rather than copied
code or prose.

High-value families for AegisCore include:

- handshake/framework: `0x5000`, `0x9000`, `0x2001`, `0x2002`;
- massive framing: `0x600D`;
- Gateway: `0x6100`/`0xA100`, `0x6101`/`0xA101`,
  `0x6102`/`0xA102`, `0x6104`/`0xA104`, `0x6106`/`0xA106`;
- Agent admission: `0x6103`/`0xA103`;
- character streaming: `0x34A5`, `0x3013`, `0x34A6`;
- entity group spawn: `0x3017`, `0x3019`, `0x3018`;
- movement/action: `0x7021`, `0xB021`, `0x7074`, `0xB070`,
  `0xB071`, `0xB074`;
- NPC interaction: `0x7045`, `0x7046`, `0x704B` and responses;
- inventory, mastery, party, exchange, stall, COS, storage, and quest tables.

### RSBot

- Repository: <https://github.com/myildirimofficial/RSBot>
- Inspected snapshot: commit
  `1723fed61b7c75cdb7db58cf04cd5a19dc560fc5` (2026-05-22).
- License: AGPL-3.0; do not copy implementation into AegisCore.

RSBot is the broadest executable client-side cross-check. Its value is not
only the numeric `Opcode` declarations: readers reveal field order and
conditional parsing, writers reveal client request bodies, and client-type
branches expose version drift. Always prove that a branch covers Vietnam
vSRO 188 before using it.

Useful paths in the inspected tree:

- `Library/RSBot.Core/Network/Handler/Agent` for response readers;
- `Library/RSBot.Core/Network/Hooks` for requests observed from the client;
- `Library/RSBot.Core/Network/Protocol/SecurityProtocol.cs` for framing,
  handshake, count/CRC, encryption, and massive-message behavior;
- `Plugins/RSBot.General/PacketHandler` for Gateway/Agent login flows;
- `Library/RSBot.Core/Objects/Spawn` for entity/COS/item parsing;
- `Library/RSBot.Core/Components` for packet writers and sequencing.

RSBot independently confirms many AegisCore anchors, including `0x3012`, the
`0x3017/0x3019/0x3018` group-spawn sequence, `0x6100`-`0x6103`, `0x7001`,
`0x7021`, `0x7045`, `0x7046`, `0x705A`, `0x7074`, `0xB0A2`, and the
`0x34B5`/`0x34B6` game-reset exchange.

### Phoenix

- Repository: <https://gitlab.com/Chernobyl_/phoenix>
- Target stated by the project: Vietnam v188.
- Inspected snapshot: commit
  `95d3e4bde1e28b88ea22bcc2802615577bdd64d9` (2019-09-08).

The public branch is mostly framework/transport code rather than a complete
opcode implementation. It is useful for the security handshake, stream
decoder, message lifecycle, pooling, and server/client protocol separation.
No clear license was found in the inspected tree, so treat it as behavioral
research only.

### Silkroad.Net

- Repository: <https://github.com/halimsamy/Silkroad.Net>
- Inspected snapshot: commit
  `6378edd752017c29b1bcda698c5df5cb56bc83cc` (2024-10-19).
- License: MIT.

This is a modern independent transport implementation. It is useful for
checking stream boundaries, client/server handshake state machines, message
size/ID/direction types, invalid-message handling, and async session design.
It is not a vSRO 1.188 opcode authority.

### skrillax-network

- Repository: <https://github.com/kumpelblase2/skrillax-network>
- Inspected snapshot: commit
  `20b525964959996957f3d4d13dbbdfe0d539a9c2` (2026-03-30).
- License: MIT.

Its strongest contribution is architecture: typed packet definitions,
serialization derives, stream registries, handshake state, checksum/count
security, and bounded codecs. Use these ideas to improve AegisCore's type safety
and tests, not as v188 packet-layout evidence.

## Capture and analysis sources

- pickpocket: <https://github.com/skrillax-dev/pickpocket> — packet observation
  at the client read/write boundary; hooks and offsets are version-specific.
- phAnalyzer: <https://gitlab.com/ryan-projecthax/phAnalyzer> — historical
  analyzer workflow; old code, but useful for packet-trace process.
- XSniff discussion:
  <https://forum.ragezone.com/threads/xsniff-silkroad-online-packet-sniffer.1135553/>
  — reports its Agent redirection behavior was tested on vSRO v188; binaries
  remain untrusted and should not be run on the development machine.
- Basic Wireshark dissector:
  <https://gist.github.com/kumpelblase2/92857a16220baa4a65b2294f6b1028a1>
  — useful for six-byte header triage and TCP desegmentation, not decryption or
  semantic decoding.
- pySilkroadSecurity: <https://github.com/ProjectHax/pySilkroadSecurity> — MIT
  wrapper and proxy examples around the historical SilkroadSecurity API; useful
  for interoperability tests, not an opcode catalog.
- phBot packet API: <https://plugins.phbot.org/phbot-api/packet-injection> —
  convenient request injection examples; version must be established per
  example.
- Thessium/open-documentation PDF:
  <https://app.readthedocs.org/projects/open-documentation/downloads/pdf/latest/>
  — vSRO 188 client offsets and reverse-engineering notes. Use only when the
  executable hash matches; offsets are not portable across builds.

## Secondary sources and exclusion rules

Historical emulators, clientless bots, filter projects, and forum posts can
identify a packet family or expected state transition. They cannot establish
vSRO 1.188 bytes by themselves. In particular, recent generic opcode lists are
not automatically better: one 2026 list labels `0x7025` as movement, while
SilkroadDoc, RSBot, and AegisCore all identify `0x7025` as the Agent chat request.
That source is suitable only as a warning about unversioned aggregation.

Do not download or execute leaked server/client archives, filter binaries, or
forum attachments. Do not import code with absent/unclear licensing. Record
the URL and claim, then confirm it through captures, TestIn, or independent
licensed implementations.

## Concrete conflicts and risks found

### 1. Game reset opcode conflict

AegisCore's local opcode corpus and RSBot use server `0x34B5` followed by client
`0x34B6`. The current SilkroadDoc Agent table lists `0x35B5` and `0x35B6`.
AegisCore currently names `0x34B6` `TeleportReadyResponse`, although the other
sources describe it as game-reset completion. Preserve the current behavior
until captured, but rename/document the semantic alias and add the conflict to
the canonical registry.

### 2. Opcode aliases need direction and phase

AegisCore intentionally or historically has numeric aliases:

- `0x3080`: party invitation and generic player invitation;
- `0x7001`: lobby join and character-selection alias;
- `0x706D`: party matching join request and the response side of player join.

A map keyed only by `uint16` cannot explain these safely. The key needs at
least service, direction, and phase, and aliases should be explicit records
rather than duplicate constants with no provenance.

### 3. Client opcodes and internal service opcodes are mixed

The `E1xx/F1xx/E201/F201` Agent/Game service messages share the same package as
stock-client opcodes. They were not found in the inspected public client
tables/handlers and should live in a separate internal-service namespace with
TestIn/legacy provenance, preventing them from being mistaken for client wire
support.

### 4. The local text list is broad but not machine-verifiable

`framework/docs/opcodes.txt` contains valuable names, duplicates, unknown
files, misspellings, and direction aliases, but it is a directory listing
rather than a canonical schema. It cannot automatically prove that registered
handlers, response writers, docs, and tests agree.

### 5. Breadth is ahead of proof

AegisCore has a useful central constant package and 55 registered application
handlers, but the pinned protocol profile remains evidence-pending and no
sanitized stock-client fixtures are checked in. Adding more handler breadth
from an online table would increase drift faster than confidence.

## Prioritized improvements for AegisCore

### P0 — build the evidence-backed opcode registry

Create a machine-readable registry under `protocol/vsro188` with one row per
meaning, not merely per number. Suggested fields:

`opcode`, `name`, `service`, `direction`, `phase`, `request_response_pair`,
`encryption`, `framing`, `payload_status`, `client_profile`, `source`,
`source_revision`, `confidence`, `implementation`, `tests`, `notes`.

Seed it from the existing Go constants and TestIn catalog, then import public
references as separate claims. Never overwrite a conflict. Generate:

- Go constants grouped by client/Gateway/Agent/internal service;
- the human opcode reference;
- duplicate/alias/conflict reports;
- registered-handler coverage and unhandled-server-writer coverage tests.

Exit gate: every declared or registered opcode has provenance, direction,
service, and confidence, and CI fails on an unexplained duplicate.

### P0 — settle high-risk flows with captures

Capture and sanitize these flows first:

1. `0x5000`/`0x9000` handshake and `0x2001` identification;
2. `0x600D` massive patch/login messages;
3. `0x6102` Gateway login to `0x6103` Agent admission;
4. `0x34A5`/`0x3013`/`0x34A6` character stream and `0x3012` ready;
5. `0x3017`/`0x3019`/`0x3018` for player, NPC, mob, item, and COS;
6. movement/region transition and teleport, including the
   `0x34B5`/`0x34B6` versus `0x35B5`/`0x35B6` conflict;
7. NPC talk, `0xB0A2` mastery, combat, quest, inventory, party, and exchange.

Exit gate: golden decode/encode tests consume exact sanitized fixture bytes and
record executable/Data.pk2 hashes and direction/framing metadata.

### P1 — enforce opcode contracts at runtime

- Bind allowed opcodes to session lifecycle states and service endpoints.
- Add minimum/exact/maximum body lengths to the registry and reject trailing
  or truncated bytes consistently.
- Record unknown/disallowed opcode metrics by service, phase, direction, size,
  encryption, and client profile without logging sensitive payloads.
- Rate-limit by opcode family as well as physical frames; keep the existing
  frame budget as the outer defense.
- Test duplicate registration, illegal phase transitions, and disconnect
  behavior.

### P1 — separate codecs from gameplay handlers

Define typed request/response codecs for the stabilized P0 flows. Handlers
should receive validated domain values rather than reading arbitrary packet
bytes. This makes RSBot/SilkroadDoc comparisons testable, keeps server
authority separate from parsing, and enables fuzzing of every decoder.

### P1 — preserve source snapshots and licensing

Record source URL, exact commit, retrieval date, target client, and license in
the registry/reference catalog. Keep research clones ignored under
`.references`; do not vendor them. Add a small refresh script that reports
upstream revision changes without automatically changing protocol truth.

### P2 — expand by complete gameplay slices

After P0/P1 are green, use the public tables to identify complete request,
response, notification, persistence, rollback, and visibility slices. Prefer
finishing existing inventory, mastery/combat, party, exchange, storage, stall,
COS, and quest flows over declaring every Academy/Guild/Siege/Consignment
opcode with no implementation.

## Immediate next implementation slice

The safest first change is the canonical registry plus CI audits. It is
behavior-preserving, exposes current ambiguities, and creates the structure
needed to absorb captures and TestIn confirmations. The first registry entries
should cover framework/Gateway/auth, character-ready, group spawn, movement,
teleport/game-reset, and mastery because those have the highest operational
impact and the strongest existing cross-source evidence.
