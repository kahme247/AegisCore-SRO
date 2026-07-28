param(
    [Parameter(Mandatory = $true)]
    [string]$InputPath,
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$inputFile = (Resolve-Path -LiteralPath $InputPath).Path
$outputDirectory = Split-Path -Parent $OutputPath
if (-not (Test-Path -LiteralPath $outputDirectory)) {
    throw "Output directory does not exist: $outputDirectory"
}

$bytes = [IO.File]::ReadAllBytes($inputFile)
if ($bytes.Length -lt 0x100 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
    throw "Input is not a valid PE executable: $inputFile"
}

$peOffset = [BitConverter]::ToInt32($bytes, 0x3c)
if ($peOffset -lt 0 -or $peOffset + 24 -gt $bytes.Length -or
    $bytes[$peOffset] -ne 0x50 -or $bytes[$peOffset + 1] -ne 0x45) {
    throw "Input has an invalid PE header: $inputFile"
}

$characteristicsOffset = $peOffset + 22
$characteristics = [BitConverter]::ToUInt16($bytes, $characteristicsOffset)
$patchedCharacteristics = $characteristics -bor 0x20
$bytes[$characteristicsOffset] = $patchedCharacteristics -band 0xff
$bytes[$characteristicsOffset + 1] = ($patchedCharacteristics -shr 8) -band 0xff

[IO.File]::WriteAllBytes($OutputPath, $bytes)

$written = [IO.File]::ReadAllBytes($OutputPath)
$verified = [BitConverter]::ToUInt16($written, $characteristicsOffset)
if (($verified -band 0x20) -eq 0) {
    throw "Large Address Aware verification failed: $OutputPath"
}

Get-FileHash -Algorithm SHA256 -LiteralPath $OutputPath
