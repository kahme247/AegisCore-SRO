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
- future `compat/` modules are registered only through
  the core composition root.

## Build and test

The current milestone provides the bounded trace ring and `.actrace` sidecar writer. It has no live hook installed yet, so it cannot affect normal game traffic. The next milestone will connect the ring only to client-verified plaintext packet boundaries.

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

Do not copy the candidate over `VSRO_Client/Aegis.dll` until a disposable
client test has reviewed `AegisDevKit.log` and the normal crash sidecars.

The launcher injects only `Aegis.dll`. For manual diagnostic arming, launch the
client normally and run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\Arm-SroClientDiagnostics.ps1
```

The injected DLL preserves the client crash handler. When an unhandled client exception occurs, it writes two sidecars beside the stock `.dmp` file in `VSRO_Client\Dump`, then the stock crash flow continues:

- `AegisCrash-*.json` contains the exception code, faulting address, x86 register values, and module-relative return addresses captured from the exception filter.
- `AegisTrace-*.actrace` contains the bounded packet trace ring. Packet hooks are not enabled until their client boundary has been verified, so this file can be empty until that milestone is complete.

Trace sidecars use the contract in [`_docs/client-packet-tracing.md`](../../_docs/client-packet-tracing.md). Do not commit their plaintext data; follow `protocol/captures/README.md` to sanitize anything promoted to a fixture.
