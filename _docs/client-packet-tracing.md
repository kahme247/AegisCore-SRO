# Client packet trace contract

This contract defines diagnostics emitted by the optional AegisCore client extension. It is not a production telemetry channel and is disabled unless the diagnostic extension is injected into the pinned vSRO 1.188 client.

## Goals

- Preserve the last 64 protocol events in memory with no network I/O on the client thread.
- On an unhandled client exception, flush a sidecar file beside the client-generated `.dmp` file.
- Capture plaintext protocol information only after the client has decrypted an inbound message or immediately before it encrypts an outbound message.
- Never record passwords, session keys, security handshakes, or raw login/account payloads.

## Record shape

Each record is fixed-size and binary to remain safe inside an exception handler:

```text
sequence:u32le
tickMilliseconds:u32le
direction:u8                 # 1 inbound, 2 outbound
phase:u8                     # 0 unknown, 1 gateway, 2 agent, 3 world
flags:u16le                  # bit 0 payload omitted, bit 1 payload truncated
opcode:u16le
payloadLength:u16le          # original plaintext length
payloadHash[32]              # SHA-256 of the complete plaintext payload
payloadPrefix[128]           # only when the opcode is explicitly safe
```

The ring contains 64 records. The sidecar header includes the extension version, target executable SHA-256, record count, and write index.

## Privacy policy

The extension always records metadata and a SHA-256 payload digest. It records up to 128 bytes of plaintext only for inbound Agent/world messages. Gateway messages, every outbound message, and the security/login family are metadata-only. Sidecars are written beneath the ignored client-local diagnostics directory and must be sanitized under `protocol/captures/README.md` before becoming fixtures.

## Proven hook boundaries

For the pinned client image:

- Outbound plaintext serialization leaves through `SendMsg` at `0x008418D0`.
- `CMsgStreamBuffer` exposes the opcode at offset `0x18`, read offset at `0x00`, and available plaintext bytes at `0x04`.
- Agent packet registration begins at `0x0084C9BF`; the world handlers include `0x3017`, `0x3019`, `0x3018`, `0x3015`, and `0x3016`.

The inbound dispatch hook must be selected from a client-verified handler boundary. Socket `recv`/`WSARecv` is explicitly excluded because it sees encrypted frames and does not reliably preserve message boundaries.

## Acceptance criteria

1. A login and world-entry run produces metadata for both directions without altering gameplay traffic.
2. An intentional malformed test packet produces a sidecar with the last valid inbound opcode before the exception.
3. The extension neither stores authentication material nor exceeds a fixed 64-record/12 KiB in-memory budget.
4. A sanitized trace can be replayed by a Go protocol regression test.
