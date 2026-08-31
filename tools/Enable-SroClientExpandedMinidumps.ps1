[CmdletBinding()]
param(
    [string]$ClientPath
)

$ErrorActionPreference = 'Stop'

# This baseline includes the repository's compatibility patch and the optional
# multi-instance patch from Enable-SroClientMultiInstance.ps1.
$expectedHash = '55A1F40D369F3652477F7AB0315C092ECC1204447F3459327646597696D909E0'
$instrumentedHash = '281345245694AECF1B73F845FD410F3F0A041E29CFEE6457323C4E67AD6B9D48'
$hookOffset = 0x43232F
$hookAddress = 0x0083232F
$caveOffset = 0x97F280
$caveAddress = 0x00D7F280
$resumeAddress = 0x00832339
$expectedHook = [byte[]](0x53, 0x57, 0x89, 0x44, 0x24, 0x50, 0x89, 0x6C, 0x24, 0x54)

if ([string]::IsNullOrWhiteSpace($ClientPath)) {
    $ClientPath = Join-Path $PSScriptRoot '..\VSRO_Client\sro_client.exe'
}

$resolvedPath = (Resolve-Path -LiteralPath $ClientPath).Path
$hash = (Get-FileHash -LiteralPath $resolvedPath -Algorithm SHA256).Hash
$bytes = [System.IO.File]::ReadAllBytes($resolvedPath)

if ($hash -eq $instrumentedHash) {
    Write-Host "Expanded built-in minidumps are already enabled: $resolvedPath"
    exit 0
}

if ($hash -ne $expectedHash) {
    throw "Refusing to patch an unexpected client image. Expected SHA-256 $expectedHash; got $hash."
}
for ($i = 0; $i -lt $expectedHook.Length; $i++) {
    if ($bytes[$hookOffset + $i] -ne $expectedHook[$i]) {
        throw "Unexpected crash-handler bytes at file offset 0x$('{0:X}' -f ($hookOffset + $i))."
    }
}
for ($i = 0; $i -lt 19; $i++) {
    if ($bytes[$caveOffset + $i] -ne 0) {
        throw "The selected code cave is not empty at file offset 0x$('{0:X}' -f ($caveOffset + $i))."
    }
}

# MiniDumpWithDataSegs | WithHandleData | ScanMemory | WithUnloadedModules |
# WithIndirectlyReferencedMemory | WithProcessThreadData | WithFullMemoryInfo |
# WithThreadInfo. This deliberately excludes MiniDumpWithFullMemory.
$dumpType = 0x1975
$cave = [System.Collections.Generic.List[byte]]::new()
$cave.AddRange([byte[]](0x68))
$cave.AddRange([BitConverter]::GetBytes([uint32]$dumpType))
$cave.Add(0x57) # push edi: dump file handle
$cave.AddRange([byte[]](0x89, 0x44, 0x24, 0x50)) # exception information
$cave.AddRange([byte[]](0x89, 0x6C, 0x24, 0x54)) # user stream pointer = null
$cave.Add(0xE9)
$cave.AddRange([BitConverter]::GetBytes([int32]($resumeAddress - ($caveAddress + $cave.Count + 4))))

$hook = [System.Collections.Generic.List[byte]]::new()
$hook.Add(0xE9)
$hook.AddRange([BitConverter]::GetBytes([int32]($caveAddress - ($hookAddress + 5))))
while ($hook.Count -lt $expectedHook.Length) { $hook.Add(0x90) }

[Array]::Copy($hook.ToArray(), 0, $bytes, $hookOffset, $hook.Count)
[Array]::Copy($cave.ToArray(), 0, $bytes, $caveOffset, $cave.Count)
[System.IO.File]::WriteAllBytes($resolvedPath, $bytes)

$verified = [System.IO.File]::ReadAllBytes($resolvedPath)
if (-not [Linq.Enumerable]::SequenceEqual([byte[]]$hook, [byte[]]$verified[$hookOffset..($hookOffset + $hook.Count - 1)]) -or -not [Linq.Enumerable]::SequenceEqual([byte[]]$cave, [byte[]]$verified[$caveOffset..($caveOffset + $cave.Count - 1)])) {
    throw 'The expanded-minidump patch could not be verified after writing.'
}

Write-Host "Enabled expanded built-in minidumps: $resolvedPath"
