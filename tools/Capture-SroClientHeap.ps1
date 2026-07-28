param(
    [int]$DurationSeconds = 180,
    [string]$OutputPath = ".local\sro-client-heap.etl"
)

if (-not (Get-Process -Name sro_client -ErrorAction SilentlyContinue)) {
    throw "sro_client is not running"
}

$outputDirectory = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outputDirectory)) {
    throw "Output directory does not exist: $outputDirectory"
}

$wpr = "$env:SystemRoot\System32\wpr.exe"
& $wpr -cancel 2>$null
& $wpr -start Heap -start VirtualAllocation -filemode
if ($LASTEXITCODE -ne 0) {
    throw "Failed to start WPR heap tracing"
}

try {
    Start-Sleep -Seconds $DurationSeconds
    & $wpr -stop $OutputPath "sro_client active heap-growth reproduction"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to save WPR heap trace"
    }
}
catch {
    & $wpr -cancel 2>$null
    throw
}
