[CmdletBinding()]
param(
    [int]$Hours = 2
)

$ErrorActionPreference = 'Stop'

$captureRoot = Join-Path $PSScriptRoot '..\.local\sro-client-crashes'
$builtInDumpRoot = Join-Path $PSScriptRoot '..\VSRO_Client\Dump'

Write-Host 'Latest expanded crash captures:'
if (Test-Path -LiteralPath $captureRoot) {
    Get-ChildItem -LiteralPath $captureRoot -Recurse -Filter '*.dmp' -File |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 5 FullName, Length, LastWriteTime |
        Format-Table -AutoSize
} else {
    Write-Host '  No ProcDump captures yet. Start one with Start-SroClientCrashCapture.ps1.'
}

Write-Host "`nLatest client-built minidumps:"
if (Test-Path -LiteralPath $builtInDumpRoot) {
    Get-ChildItem -LiteralPath $builtInDumpRoot -Filter '*.dmp' -File |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 5 FullName, Length, LastWriteTime |
        Format-Table -AutoSize
}

Write-Host "`nWindows crash events from the last $Hours hour(s):"
$startTime = (Get-Date).AddHours(-$Hours)
$events = Get-WinEvent -FilterHashtable @{ LogName = 'Application'; StartTime = $startTime } -ErrorAction SilentlyContinue |
    Where-Object {
        $_.ProviderName -in 'Application Error', 'Windows Error Reporting' -and
        $_.Message -match '(?i)sro_client\.exe|silkroad\.exe'
    } |
    Select-Object -First 5 TimeCreated, ProviderName, Id, Message

if ($events) {
    $events | Format-List
} else {
    Write-Host '  No matching Windows error events were recorded.'
}
