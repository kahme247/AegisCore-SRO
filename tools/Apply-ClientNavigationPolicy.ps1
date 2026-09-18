param(
    [Parameter(Mandatory = $true)][string]$ClientRoot,
    [Parameter(Mandatory = $true)][string]$LauncherAssembly,
    [Parameter(Mandatory = $true)][string]$ResourceRoot
)

$ErrorActionPreference = 'Stop'
$clientRootPath = (Resolve-Path -LiteralPath $ClientRoot).Path
$mediaPath = Join-Path $clientRootPath 'Media.pk2'
$assemblyPath = (Resolve-Path -LiteralPath $LauncherAssembly).Path
$resourceRootPath = (Resolve-Path -LiteralPath $ResourceRoot).Path

$assembly = [Reflection.Assembly]::LoadFrom($assemblyPath)
$streamType = $assembly.GetType('SRO.PK2API.Pk2Stream', $true)
$constructor = $streamType.GetConstructor([Type[]]@([string], [string], [bool]))
if ($null -eq $constructor) {
    throw 'Pk2Stream constructor was not found'
}
[object[]]$constructorArguments = @(
    [string][IO.Path]::GetFullPath($mediaPath),
    [string]'169841',
    [bool]$false
)
$stream = $constructor.Invoke($constructorArguments)
try {
    $entries = @(
        @('resinfo\ifsystemwnd.txt', 'ifsystemwnd.txt'),
        @('resinfo\pscharactercreatechina.txt', 'pscharactercreatechina.txt'),
        @('resinfo\pscharactercreate_europe.txt', 'pscharactercreate_europe.txt'),
        @('resinfo\pscharacterselect.txt', 'pscharacterselect.txt'),
        @('resinfo\pscharacterselect_europe.txt', 'pscharacterselect_europe.txt')
    )
    $addFile = $streamType.GetMethod('AddFile')
    foreach ($entry in $entries) {
        $sourcePath = Join-Path $resourceRootPath $entry[1]
        $bytes = [IO.File]::ReadAllBytes($sourcePath)
        if (-not $addFile.Invoke($stream, @($entry[0], $bytes))) {
            throw "Failed to import $($entry[0])"
        }
        Write-Output "Imported $($entry[0])"
    }
}
finally {
    $streamType.GetMethod('Dispose').Invoke($stream, @()) | Out-Null
}
