# vSRO 1.88 capture corpus

No stock-client captures are checked in yet. Until that changes, the
`vsro188` profile remains `evidence-pending` and inferred packet fields must be
treated as provisional.

## Authority

Sanitized wire bytes captured from the exact pinned client are the final
protocol authority. SilkroadDoc, RSBot, JellyBitz tools and legacy GO-SRO code
are research references used to explain or cross-check those bytes.
See the version and provenance classifications in
[`../../REFERENCES.md`](../../REFERENCES.md) before accepting a new source.

## Required fixture metadata

Every checked-in fixture must record:

- client executable SHA-256 and file version;
- Data.pk2 SHA-256 and protocol profile name;
- locale, Content ID, module name and version;
- connection branch and packet direction;
- opcode and whether security/encryption/massive framing was present;
- sanitization method;
- SHA-256 of the preserved wire bytes;
- the expected decoded fields or server response.

## Sanitization rules

Never commit raw captures containing credentials, admission tickets, account or
character names, public/private IP addresses, chat, hardware identifiers or
other personal data. Replace sensitive fields deterministically while
preserving lengths and packet structure, then verify and record the sanitized
byte hash.

Do not store full PCAP files when a minimal binary fixture is sufficient. A
fixture should cover one behavior and be small enough to audit in a test.

## Priority capture set

1. handshake/security and TCP fragmentation/coalescing;
2. patch/login massive `0x600D` flows and Gateway-to-Agent handoff;
3. character list/join and initial world state;
4. NPC/mob/COS/item group spawn `0x3019` and despawn;
5. movement, region transition, collision rejection and teleport;
6. NPC talk, mastery, skill/combat, quest and inventory operations;
7. party, exchange, storage, stalls and disconnect recovery.

Golden tests should consume sanitized bytes directly and prove both successful
decoding and exact response encoding.
