[CmdletBinding()]
param(
    [int]$ProcessId
)

$ErrorActionPreference = 'Stop'

$buildDirectory = Join-Path $PSScriptRoot '..\.local\sro-client-diagnostics-build'
$dllPath = Join-Path $buildDirectory 'AegisClientDiagnostics.dll'
$injectorPath = Join-Path $buildDirectory 'AegisClientDiagnosticsInjector.exe'
if (-not (Test-Path -LiteralPath $dllPath) -or -not (Test-Path -LiteralPath $injectorPath)) {
    throw 'Build the x86 client diagnostics first; see tools\sro-client-diagnostics\README.md.'
}

if ($ProcessId -eq 0) {
    $client = Get-Process -Name sro_client -ErrorAction SilentlyContinue | Sort-Object StartTime -Descending | Select-Object -First 1
    if ($null -eq $client) {
        throw 'sro_client.exe is not running. Launch the game normally, then run this command.'
    }
    $ProcessId = $client.Id
}

& $injectorPath $ProcessId (Resolve-Path -LiteralPath $dllPath).Path
if ($LASTEXITCODE -ne 0) {
    throw "Client diagnostics injection failed with exit code $LASTEXITCODE."
}
