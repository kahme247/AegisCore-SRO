[CmdletBinding()]
param(
    [string]$ClientPath
)

$ErrorActionPreference = 'Stop'

$expectedHash = '3DF2282DAB84EE5439944B3E6CA8E8CFC69CB05963A45F0F7F4B8ADCA579C206'
$patchedHash = '55A1F40D369F3652477F7AB0315C092ECC1204447F3459327646597696D909E0'
$patchOffset = 0x43297F
$expectedByte = 0x75 # JNE: preserve the stock single-instance rejection.
$patchedByte = 0xEB  # JMP: continue startup when the mutex already exists.

if ([string]::IsNullOrWhiteSpace($ClientPath)) {
    $ClientPath = Join-Path $PSScriptRoot '..\VSRO_Client\sro_client.exe'
}

$resolvedPath = (Resolve-Path -LiteralPath $ClientPath).Path
$hash = (Get-FileHash -LiteralPath $resolvedPath -Algorithm SHA256).Hash
$bytes = [System.IO.File]::ReadAllBytes($resolvedPath)

if ($hash -eq $patchedHash -and $bytes[$patchOffset] -eq $patchedByte) {
    Write-Host "Multi-instance patch is already applied: $resolvedPath"
    exit 0
}

if ($hash -ne $expectedHash -or $bytes[$patchOffset] -ne $expectedByte) {
    throw "Refusing to patch an unexpected client image. Expected SHA-256 $expectedHash and byte 0x$('{0:X2}' -f $expectedByte) at file offset 0x$('{0:X}' -f $patchOffset); got SHA-256 $hash and byte 0x$('{0:X2}' -f $bytes[$patchOffset])."
}

$bytes[$patchOffset] = $patchedByte
[System.IO.File]::WriteAllBytes($resolvedPath, $bytes)

if ([System.IO.File]::ReadAllBytes($resolvedPath)[$patchOffset] -ne $patchedByte) {
    throw 'The multi-instance patch could not be verified after writing.'
}

Write-Host "Enabled multiple simultaneous sro_client.exe instances: $resolvedPath"
