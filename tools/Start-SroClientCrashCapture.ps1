[CmdletBinding()]
param(
    [string]$ClientDirectory,
    [ValidateRange(1, 10)]
    [int]$MaxDumps = 3,
    [switch]$FullMemory,
    [switch]$DoNotLaunch
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($ClientDirectory)) {
    $ClientDirectory = Join-Path $PSScriptRoot '..\VSRO_Client'
}

$clientDirectory = (Resolve-Path -LiteralPath $ClientDirectory).Path
$launcherPath = Join-Path $clientDirectory 'Silkroad.exe'
$procDumpPath = Join-Path $PSScriptRoot '..\..\tools\Procdump\procdump.exe'

if (-not (Test-Path -LiteralPath $launcherPath)) {
    throw "Silkroad.exe was not found in $clientDirectory"
}
if (-not (Test-Path -LiteralPath $procDumpPath)) {
    throw "ProcDump was not found at $procDumpPath"
}

$captureDirectory = Join-Path $PSScriptRoot ("..\.local\sro-client-crashes\{0}" -f (Get-Date -Format 'yyyyMMdd-HHmmss'))
[System.IO.Directory]::CreateDirectory($captureDirectory) | Out-Null

$metadataPath = Join-Path $captureDirectory 'capture.txt'
@(
    "Started: $(Get-Date -Format o)"
    "Client directory: $clientDirectory"
    "Launcher: $launcherPath"
    'Target process: sro_client.exe'
    "Maximum crash dumps: $MaxDumps"
) | Set-Content -LiteralPath $metadataPath -Encoding utf8

$dumpMode = if ($FullMemory) { '-ma' } else { '-mp' }
$procDumpArguments = "-accepteula -e $dumpMode -n $MaxDumps -w sro_client.exe `"$captureDirectory`""
$procDump = Start-Process -FilePath $procDumpPath -ArgumentList $procDumpArguments -WorkingDirectory $captureDirectory -RedirectStandardOutput (Join-Path $captureDirectory 'procdump.stdout.log') -RedirectStandardError (Join-Path $captureDirectory 'procdump.stderr.log') -PassThru

Write-Host "Crash capture is armed (ProcDump PID $($procDump.Id))."
Write-Host "Expanded crash dumps and logs will be written to: $captureDirectory"
if (-not $FullMemory) {
    Write-Host 'Using Mini Plus dumps (thread stacks, modules, handles, and useful private memory).'
    Write-Host 'Use -FullMemory only when a complete process-memory dump is needed.'
}
Write-Host 'After a crash, run: .\tools\Get-SroClientCrashReport.ps1'

if (-not $DoNotLaunch) {
    Start-Process -FilePath $launcherPath -WorkingDirectory $clientDirectory | Out-Null
    Write-Host 'Launched Silkroad.exe. Reproduce the crash normally.'
}
