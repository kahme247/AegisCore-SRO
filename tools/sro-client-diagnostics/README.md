# Aegis client diagnostics

This is a separate x86 DLL for the pinned vSRO 1.188 client. It is not a replacement for the reference DevKit and it is not copied into the client directory automatically.

Use this tool only when diagnosing a stock-client crash. It is not part of the
server build or first-time installation, and its output may contain sensitive
packet data.

## Build and test

The current milestone provides the bounded trace ring and `.actrace` sidecar writer. It has no live hook installed yet, so it cannot affect normal game traffic. The next milestone will connect the ring only to client-verified plaintext packet boundaries.

Build from `go-sro` in an x86 Visual Studio developer shell:

```powershell
cmake -S tools/sro-client-diagnostics -B .local/sro-client-diagnostics-build -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build .local/sro-client-diagnostics-build
.local/sro-client-diagnostics-build/AegisClientDiagnosticsTest.exe
```

The resulting DLL is `.local/sro-client-diagnostics-build/AegisClientDiagnostics.dll`. It is a diagnostic build artifact and remains local/ignored.

The launcher can inject `Aegis.dll` first and then `AegisClientDiagnostics.dll`. In that order, diagnostics hooks the client's later unhandled-exception-filter registration and preserves the normal Aegis/client startup flow. If the launcher does not yet load the diagnostics DLL, launch the client normally and arm it manually:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\Arm-SroClientDiagnostics.ps1
```

The injected DLL preserves the client crash handler. When an unhandled client exception occurs, it writes two sidecars beside the stock `.dmp` file in `VSRO_Client\Dump`, then the stock crash flow continues:

- `AegisCrash-*.json` contains the exception code, faulting address, x86 register values, and module-relative return addresses captured from the exception filter.
- `AegisTrace-*.actrace` contains the bounded packet trace ring. Packet hooks are not enabled until their client boundary has been verified, so this file can be empty until that milestone is complete.

Trace sidecars use the contract in [`_docs/client-packet-tracing.md`](../../_docs/client-packet-tracing.md). Do not commit their plaintext data; follow `protocol/captures/README.md` to sanitize anything promoted to a fixture.
