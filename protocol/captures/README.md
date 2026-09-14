# vSRO 1.188 capture corpus

No stock-client captures are checked in. The `vsro188` profile remains
`evidence-pending`; inferred packet fields are provisional.

Use this guide only when turning a local packet capture into a sanitized test
fixture. Never commit raw captures, credentials, account data, chat, character
names, IP addresses, or proprietary client files.

The shortest safe path is: record provenance, choose the representation,
sanitize, replay locally, then pass the acceptance gate below.

Sanitized wire evidence becomes the project's highest protocol authority only
when it is traceable to a client executable whose name, SHA-256, file version,
locale and Content ID are recorded alongside the matching Data.pk2 identity.
Until then, use the evidence order and provenance classifications in
[`../../REFERENCES.md`](../../REFERENCES.md).

## Representations

A fixture must identify its representation:

- `tcp-stream-chunks`: ordered chunks delivered to a stream decoder;
- `wire-frame`: complete secured or unsecured frame bytes;
- `decrypted-frame`: decoded frame before security reconstruction;
- `logical-payload`: opcode payload independent of framing.

TCP packetization observed in one PCAP is not a protocol boundary. Stream tests
must store a chunk plan separately and replay split-header, split-payload, exact
frame and coalesced-frame variants where relevant.

## Required metadata

Every checked-in fixture must record:

1. Stable fixture ID, metadata schema version, behavior and evidence status.
2. Client executable name, SHA-256 and file version.
3. Data.pk2 path, size, SHA-256, archive format and profile.
4. Locale and Content ID, marking observed versus inferred values.
5. Connection branch, target service, direction and ordered frame index.
6. Opcode, logical length, encryption/EDC state and massive role.
7. Representation layer and, for streams, ordered chunk lengths.
8. Private source-capture SHA-256 and capture tool/version, retained outside Git.
9. Public fixture SHA-256, sanitizer/version and sanitized-field manifest.
10. Reconstruction details for lengths, sequence, CRC, padding, encryption and
    massive framing.
11. Expected decoded fields, state transition, response bytes or rejection.
12. External source revisions and a reviewer confirmation that secrets and
    personal data are absent.

The current profile model stores capture paths only; fixture schema and replay
validation are implemented in `protocol/captures`, while a sanitizer and raw
capture corpus are intentionally not included. Metadata enforcement applies to
the first fixture set. Gateway also has a vSRO 1.274 negotiation
profile, but Agent/world behavior and this priority corpus remain anchored to
vSRO 1.188 until a separate 1.274 evidence set exists.

## Fixture schema and replay

The `protocol/captures` package now provides the metadata-validated JSON
envelope and deterministic replay boundary. Decode or load a fixture with
`DecodeFixture`/`LoadFixture`, then call `Replay` with a codec callback. The
callback receives each validated logical payload and returns the payload
produced by the typed protocol codec. When `expected_payload_hex` is present,
replay requires an exact byte-for-byte match.

The minimum checked-in shape is:

```json
{
  "schema_version": "1",
  "fixture_id": "stable-id",
  "behavior": "modern-mall-purchase",
  "evidence_status": "reconstructed-sanitized-wire",
  "client": { "executable_name": "sro_Client.exe", "sha256": "...", "file_version": "...", "locale": "...", "content_id": "..." },
  "data_pk2": { "path": "Data/Data.pk2", "size": 0, "sha256": "...", "format": "pk2", "profile": "vsro188" },
  "connection": { "branch": "agent", "target_service": "game-server" },
  "representation": "logical-payload",
  "private_source_capture_sha256": "...",
  "public_fixture_sha256": "...",
  "sanitizer": "tool/version",
  "sanitized_fields": ["field names or none"],
  "reconstruction": { "details": "how lengths and security fields were rebuilt" },
  "frames": [{
    "index": 0, "direction": "client->server", "opcode": 28724,
    "logical_length": 0, "payload_hex": "...", "expected_payload_hex": "...",
    "encryption_state": "none", "edc_state": "none", "massive_role": "none"
  }],
  "expected": [{ "state_transition": "...", "rejected": false }],
  "external_sources": ["evidence reference"],
  "reviewer_confirmed": true
}
```

The package validates metadata, frame ordering, directions, hex payloads and
logical lengths. It intentionally does not assign SQL meanings, infer item
state, or treat a synthetic unit test as stock evidence. A fixture should be
marked `synthetic` until executable/archive provenance and sanitized capture
review are complete.

## Sanitization

Never commit original PCAP/PCAPNG files, TLS material, packet logs, credentials,
tickets, identities, endpoint addresses, chat, hardware identifiers or live
session secrets. Keep originals in access-controlled storage and record their
hashes in a private evidence ledger.

Sanitize decoded fields or logical payloads, then regenerate every affected
length, sequence, CRC, padding and ciphertext field using fixed synthetic test
material. A changed secured frame is a `reconstructed-sanitized-wire` fixture,
not preserved original wire evidence. Keep an expected logical decode beside it
so tests prove both decode and re-encode behavior.

Use deterministic fixture-scoped replacements. Use RFC 5737 IPv4 ranges and
`2001:db8::/32` for endpoints. Preserve fixed-width fields; when a
length-prefixed field changes size, regenerate and test all affected lengths.
Do not normalize unknown bytes merely to make a decoder pass.

For massive traffic, preserve target opcode, body count/order and final-body
length. Validate each physical wrapper before reassembly and keep malformed,
interleaved and timeout cases separate from successful flows.

## Acceptance gate

A fixture is ready only when:

1. Its recorded profile identity matches the checked-in canonical profile.
2. Forbidden-value scans and metadata/hash validation pass.
3. The decoder consumes intended bytes without unexplained trailing data.
4. Re-encoding matches expected sanitized wire bytes where exact encoding is
   claimed.
5. Positive and negative tests exist and the fixture is minimal or justified.
6. A reviewer can reproduce sanitization from private evidence without adding
   the source capture to Git.

## Priority set

1. Handshake/security and TCP fragmentation/coalescing.
2. Patch/login massive `0x600D` and Gateway-to-Agent handoff.
3. Character list/join, configuration and initial world state.
4. Player/NPC/mob/COS/item group spawn `0x3019` and despawn.
5. Movement, region transition, collision rejection and teleport.
6. NPC talk, mastery, skill/combat, resurrection, quest and inventory.
7. Party, exchange, storage, stalls and disconnect recovery.

## Current verification

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
go test ./framework/... ./protocol/...
go run ./cmd/sro-data verify `
  --profile protocol/profiles/vsro188.json `
  --archive Data/Data.pk2
```

These commands test synthetic transport behavior and archive identity. They do
not make the profile publication-ready or provide capture-backed compatibility.
The full repository gate can additionally require the ignored local Media
fixture used by `cmd/sro-data`; absence of that local tree is an environment
limitation, not evidence that a capture fixture is valid.
