[CmdletBinding()]
param(
    [switch]$Race
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$bundledGo = Join-Path $repositoryRoot '.tools\go\bin\go.exe'

if (Test-Path -LiteralPath $bundledGo) {
    $go = $bundledGo
} else {
    $go = (Get-Command go -ErrorAction Stop).Source
}

$version = (& $go version).Trim()
if ($version -notmatch '^go version go1\.26\.5 ') {
    throw "AegisCore requires Go 1.26.5; found '$version'."
}

$goBin = Split-Path -Parent $go
$gofmt = Join-Path $goBin 'gofmt.exe'
$cacheRoot = Join-Path $repositoryRoot '.tools'
$goCache = Join-Path $cacheRoot 'gocache'
$goModCache = Join-Path $cacheRoot 'gomodcache'

New-Item -ItemType Directory -Force -Path $goCache, $goModCache | Out-Null
$env:GOCACHE = $goCache
$env:GOMODCACHE = $goModCache

Push-Location $repositoryRoot
try {
    $goFiles = @(Get-ChildItem -Recurse -File -Filter '*.go' |
        Where-Object { $_.FullName -notlike "$repositoryRoot\.tools\*" } |
        ForEach-Object FullName)
    # Windows limits the native command line to roughly 32 KiB. Keep each
    # gofmt invocation comfortably below that limit as the repository grows.
    $unformatted = @()
    $batch = @()
    $batchLength = 0
    foreach ($goFile in $goFiles) {
        $argumentLength = $goFile.Length + 3
        if ($batch.Count -gt 0 -and ($batchLength + $argumentLength) -gt 16000) {
            $unformatted += @(& $gofmt -l $batch)
            if ($LASTEXITCODE -ne 0) { throw 'gofmt check failed.' }
            $batch = @()
            $batchLength = 0
        }
        $batch += $goFile
        $batchLength += $argumentLength
    }
    if ($batch.Count -gt 0) {
        $unformatted += @(& $gofmt -l $batch)
        if ($LASTEXITCODE -ne 0) { throw 'gofmt check failed.' }
    }
    if ($unformatted.Count -gt 0) {
        throw "Run gofmt on:`n$($unformatted -join "`n")"
    }

    & $go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }

    & $go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed.' }

    if ($Race) {
        $racePackages = 'go test -race ./framework/network ./framework/server ./framework/client ./agent-server/engine/geo ./agent-server/handler/character ./agent-server/model ./agent-server/service'
        $docker = Get-Command docker -ErrorAction Stop
        # Keep the checkout read-only: Linux race validation may download Go
        # modules and build artifacts inside the container, but must never
        # rewrite source, fixtures, or user-owned workspace state.
        & $docker run --rm --mount "type=bind,source=$repositoryRoot,target=/workspace,readonly" -w /workspace -e 'GOMODCACHE=/go/pkg/mod' -e 'GOCACHE=/tmp/go-build' 'golang:1.26.5' bash -c $racePackages
        if ($LASTEXITCODE -ne 0) { throw 'core race tests failed.' }
    }
} finally {
    Pop-Location
}
