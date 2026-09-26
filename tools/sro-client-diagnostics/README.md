# Aegis client runtime

This project builds the single maintained x86 `Aegis.dll` for the pinned vSRO
1.188 client. Client compatibility, launcher authentication, packet boundaries,
and diagnostics belong behind this one composition root. It is not copied into
the client directory automatically.

The runtime is organized by responsibility beneath `src/`:

- `core/` owns initialization and module composition;
- `auth/` owns the versioned, process-bound launcher handoff and erases its
  shared mapping immediately after consumption;
- `diagnostics/` owns crash integration;
- `network/` owns pinned-client packet boundaries, including the signature-
  checked automatic Gateway login on the client UI thread;
- `client_trace.*` owns the bounded, credential-redacting trace ring;
- `compat/` owns signature-checked client compatibility hooks, including
  navigation policy and temporary-enhancement tooltip rendering; modules are
  registered only through the core composition root.

## Source map

The maintained source root is:

```text
tools/sro-client-diagnostics/
```

The temporary weapon, armor, and accessory enhancement tooltip is implemented
in:

```text
src/compat/temporary_enhancement_tooltip.cpp
src/compat/temporary_enhancement_tooltip.h
```

`src/core/runtime.cpp` calls `InstallTemporaryEnhancementTooltip()` during
Aegis initialization. `CMakeLists.txt` registers the implementation in the
`Aegis` shared-library target.

The module installs two verified runtime changes against the pinned 32-bit
vSRO 1.188 client:

1. An equipment-compatible `0x3040` kind-gate patch. This permits the
   server-provided `flags = 0x82` update to project the temporary option level
   and store the expiration on weapon, armor, and accessory `CSOItem` objects.
2. A hook at `sro_client.exe:0x00776D20`, the helper-bubble line builder used
   by item tooltips.

The tooltip hook resolves:

```text
CIFHelperBubbleWindow +0x7DC -> owner CIFSlotWithHelp
CIFSlotWithHelp       +0x390 -> CSOItem
```

It uses the native client accessors:

```text
0x008BA580 -> timed-state flag
0x008BA5B0 -> absolute expiration at CSOItem +0x60/+0x64
```

For an active timed item, it subtracts the current Unix time in milliseconds
from the absolute expiration and appends one gold line:

```text
Temporary enhancement: 10h 15m remaining
Temporary enhancement: 1d 02h 14m remaining
```

The first live implementation incorrectly formatted the absolute expiration as
a duration and displayed an epoch-sized value such as `20662d`. Do not remove
the Unix-millisecond subtraction.

The hook affects item helper bubbles whose `CSOItem` contains the timed fields.
The owner's ordinary equipment tooltip is verified. A counterparty trade
tooltip is not guaranteed until the exchange item record is verified to carry
the projected option level and expiration.

### Pinned-client compatibility

All addresses, structure offsets, and expected instruction bytes above are
specific to the pinned vSRO 1.188 `sro_client.exe`:

```text
SHA-256: 281345245694AECF1B73F845FD410F3F0A041E29CFEE6457323C4E67AD6B9D48
Image base: 0x00400000
```

`temporary_enhancement_tooltip.cpp` stores image-relative RVAs and verifies the
original bytes with `memcmp` before writing. A mismatch fails closed; never
remove the byte guards to support another client build.

For a different executable, re-derive every value in IDA:

1. Locate `CIFSlotWithHelp::SetSlotData` and follow the item timed-state and
   expiration calls to recover the equivalents of `0x008BA580` and
   `0x008BA5B0`.
2. Confirm the expiration getter's field offsets and time basis at runtime.
3. Locate the helper-bubble line append method reached by equipment tooltips;
   do not reuse `0x007A3BD0`, which is not the equipment-hover path.
4. Recover `CIFHelperBubbleWindow::m_ownerWindow` and
   `CIFSlotWithHelp::m_pItem` from actual memory accesses.
5. Re-trace the `0x3040` decoder's item-kind gate and its branch targets.
6. Replace the RVAs and expected byte arrays together, then verify a weapon,
   armor, and accessory tooltip in a disposable client.

## Build and test

The maintained runtime contains live, signature-checked compatibility,
authentication, diagnostics, and tooltip hooks. Every new hook must verify the
pinned bytes before mutation and fail closed when the client does not match.

Build from `go-sro` in an x86 Visual Studio developer shell:

```powershell
cmake -S tools/sro-client-diagnostics -B .local/sro-client-diagnostics-build -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build .local/sro-client-diagnostics-build
.local/sro-client-diagnostics-build/AegisClientTest.exe
```

The resulting DLL is `.local/sro-client-diagnostics-build/Aegis.dll`. Build
artifacts remain local/ignored.

## Guarded reconstructed DevKit integration

The normal Aegis build does not include or enable DevKit. A combined candidate
can be built explicitly without copying reverse-engineering artifacts into the
maintained source tree:

```powershell
cmake -S tools/sro-client-diagnostics `
  -B .local/sro-client-diagnostics-devkit-build -A Win32 `
  -DAEGIS_ENABLE_DEVKIT_INTEGRATION=ON `
  -DAEGIS_DEVKIT_SOURCE_DIR=C:/Users/Khaled/Documents/GO-SRO/analysis/DevKit_DLL_reconstructed
cmake --build .local/sro-client-diagnostics-devkit-build --config Release
```

Every build produced with `AEGIS_ENABLE_DEVKIT_INTEGRATION=ON` schedules DevKit
initialization automatically; no launcher context or feature flag is required.
Ordinary Aegis builds keep this CMake option off and contain no DevKit code.

Activation is deferred outside `DllMain`. Before any DevKit mutation, Aegis
requires the pinned `sro_client.exe` SHA-256
`281345245694AECF1B73F845FD410F3F0A041E29CFEE6457323C4E67AD6B9D48` and exact
original bytes at all 33 mutation sites. Installation snapshots every target,
logs each hook to `AegisDevKit.log`, and rolls back in reverse order if any
write fails. The existing trace exports remain unchanged.

The current automatic test build uses the proven lifecycle trio. Macro
bootstrap at `0x00DB95CC` is disabled after a controlled launch reached
character selection and crashed in host runtime-type handling at `0x00B94B4C`.

Do not copy an untested candidate over `VSRO_Client/Aegis.dll`. The deployed
client uses the full DevKit-enabled output:

```text
.local/sro-client-diagnostics-devkit-build/Release/Aegis.dll
```

The smaller plain-build DLL omits the reconstructed DevKit integration.

The launcher injects only `Aegis.dll`. For manual diagnostic arming, launch the
client normally and run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\Arm-SroClientDiagnostics.ps1
```

The injected DLL preserves the client crash handler. When an unhandled client exception occurs, it writes two sidecars beside the stock `.dmp` file in `VSRO_Client\Dump`, then the stock crash flow continues:

- `AegisCrash-*.json` contains the exception code, faulting address, x86 register values, and module-relative return addresses captured from the exception filter.
- `AegisTrace-*.actrace` contains the bounded packet trace ring. Packet hooks are not enabled until their client boundary has been verified, so this file can be empty until that milestone is complete.

Trace sidecars use the contract in [`_docs/client-packet-tracing.md`](../../_docs/client-packet-tracing.md). Do not commit their plaintext data; follow `protocol/captures/README.md` to sanitize anything promoted to a fixture.
