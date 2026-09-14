# vSRO 1.188 client: binary-confirmed lifecycle, opcodes, structures, and SQL boundary

Assessment date: 2026-07-20

Target: `VSRO_Client/sro_client.exe`

Target SHA-256: `3DF2282DAB84EE5439944B3E6CA8E8CFC69CB05963A45F0F7F4B8ADCA579C206`

Stock comparison authority: `.references/sro-client/vsro-188/sro_client.stock.exe`, SHA-256 `58B9DA3A218894F28E4C82A992B90997869CF331AB450929C8FCB7453327D114`

This document records static facts tied to the exact client image. `A` means directly observed in PE bytes, disassembly, or a focused decompilation. `B` means independently corroborated by the pinned SRO_DevKit address map or AegisCore implementation. `C` means a label or interpretation that still needs a stock wire capture. A packet field width can be `A` while its semantic name remains `C`.

## Executive result

The requested file is the compatibility-patched vSRO 1.188 client. It is byte-identical to the preserved stock client except for 40 bytes in two ATL/DEP patch sites and their executable code cave. All absolute addresses below therefore refer to the fixed-base stock image and remain valid for this patched file only because that comparison succeeded.

The client is a 32-bit, fixed-base, Visual C++ 2005-era Windows GUI process. Its lifecycle is network-driven:

```text
launcher / PK2 version check
        -> client process and resource initialization
        -> Gateway security/session exchange
        -> Agent admission and lobby
        -> character selection / 0x7001
        -> world bootstrap: 0x3017, 0x3019*, 0x3018
        -> movement, action, combat, inventory, quest, chat, and UI updates
        -> logout/reset/connection-error process transition
```

`0x3019*` is a repeated group-data message between the begin/end markers. Its exact envelope is `groupFamily:u8`, followed for family `1` or `2` by `recordCount:u16`; family `1` dispatches each record from an object/reference `u32`, while family `2` dispatches each record from an object UID `u32`. Family 1 then performs a runtime-class `+0x64` call. The recovered no-tail classes consume no additional bytes, while the `CICScriptObj` secondary-vtable path has a qualified conditional four-byte tail. Family 2 reuses the object-left path and its `+0x4C` call. See the [field-level decoder layout](vsro188-client-decoder-layouts.md#group-spawn-0x3019-parser) for the exact branches.

The SQL boundary is negative for this executable: no direct ODBC, MySQL, SQLite, or SQL imports were found; no SQL/database query literal was found in the ASCII or UTF-16 string scan; and the client imports networking and file/update APIs rather than a database driver. SQL in `go-sro/db` and server-side reverse-engineering documents belongs to Gateway/Agent/GameServer persistence, not to this client.

## Evidence and binary identity

### PE facts

| Property | Confirmed value |
| --- | --- |
| Format | PE32 x86 GUI executable |
| Machine | `0x014C` |
| Preferred image base | `0x00400000` |
| Entry-point RVA | `0x00749B52` |
| Entry-point VA | `0x00B49B52` |
| `DllCharacteristics` | `0x0000` |
| Sections | `.text`, `.rdata`, `.data`, `.rsrc` |
| Direct imports | 391 symbols from 16 DLLs |
| Embedded PDB path | `D:\vss-od\Silkroad\Client\Out\SRO_Client.pdb` |

The client has no relocation/ASLR support. A different regional rebuild can retain the same nominal game version while invalidating every absolute address, vtable, global, and handler map in this document.

### Patched-file comparison

The target and stock reference have the same file length, 11,485,184 bytes. The only differing file ranges are:

| File range | Image VA | Role |
| --- | --- | --- |
| `0x000C711A..0x000C711F` | `0x004C711A..0x004C711F` | client ATL thunk page-protection call |
| `0x000C7530..0x000C7535` | `0x004C7530..0x004C7535` | client ATL thunk page-protection call |
| `0x0097F24A..0x0097F267` | `0x00D7F24A..0x00D7F267` | compatibility stub in the `.text` code cave |

The patched binary is not a new protocol build. It preserves the stock code/data image and changes the ATL thunk setup so generated callbacks can execute under DEP. The patch calls `VirtualProtect` for the thunk, uses the stock `FlushInstructionCache` continuation, and leaves the containing page executable/writable. It has not been treated as evidence that a live end-to-end client session succeeds.

### Relevant imports

Confirmed functional groups include:

- `WS2_32.dll`: socket, connect, send/receive, event, overlapped I/O, and ioctl operations.
- `WININET.dll`, `urlmon.dll`: FTP/update and URL download operations.
- `iphlpapi.dll`: adapter and TCP-table inspection.
- `d3d9.dll`, `DDRAW.dll`, `DSOUND.dll`, `WINMM.dll`: graphics, sound, and timing.
- `GDI32.dll`, `USER32.dll`, `IMM32.dll`, `SHELL32.dll`, `ADVAPI32.dll`: windowing, input, shell, registry, and security support.

No imported DLL or symbol matched `SQL`, `ODBC`, `MySQL`, `SQLite`, or a database-driver API. `LoadLibraryA`/`GetProcAddress` exist, so a dynamically loaded private database component cannot be disproved solely by the import table; the executable also contains no matching database strings, and no such component is present in the inspected client dependency set.

## Lifecycle

### 1. Launcher boundary

The launcher, not `sro_client.exe`, owns the initial PK2/version/update conversation. The launcher opens `Media.pk2`, reads/decrypts `SV.T`, connects to Gateway, and uses `0x6100` for patch/version negotiation. It may download and import patch files before it starts the client. The launcher then starts the client suspended, injects the compatibility extension, checks the expected hook, and resumes it.

The launcher contract is documented in [`analysis/LAUNCHER_PATCH_SYSTEM_IDEA.md`](../../analysis/LAUNCHER_PATCH_SYSTEM_IDEA.md). Do not attribute its `0x6100`, `0x6104`, DownloadServer, or PK2-write operations to the client executable.

### 2. Client process/resource initialization

The PE imports and embedded strings confirm the client roles and resources:

- `Media.pk2`, `Map.pk2`, `Data.pk2`, and `Music.pk2` are opened by the client resource path.
- `GatewayServer`, `AgentServer`, and `DownloadServer` are embedded service identifiers.
- A launcher-state failure path contains `Please Execute the "Silkroad.exe."`.
- The client dynamically resolves `MiniDumpWriteDump` and references `SilkErrSender.exe` for crash reporting.

The pinned client wrappers identify the `CPSQuickStart` transition. `OnCreate` starts the network engine, loads text files, sets the process ready flag, shows the main window, and resizes it. This is address-map corroboration (`B`) around stock routines, not a claim that the wrapper source is original client source.

### 3. Security/session transport

The strongest corroborated frame shape is:

```text
payloadLength:u16le | opcode:u16le | sequence:u8 | crc:u8 | payload
```

The high bit of the length marks encrypted frames; the length bytes remain clear while the rest of the logical header and payload are encrypted/padded. The supplied PCAPNG has no packet blocks, so this remains `B+`, not capture-confirmed. TCP fragmentation/coalescing must be handled as a stream.

The shared security family uses `0x5000` and `0x9000` for negotiation, with `0x2001` as a module-identification message and `0x2002` as a keepalive in the corroborated implementation family. The client’s receive registration also contains internal/control values such as `0x2000`, `0xFFC`, and `0xFFD`; registration alone does not prove that every such value is a public socket opcode.

### 4. Quick-start and lobby transitions

The pinned client map confirms these focused transitions:

| Event | Opcode | Binary-confirmed behavior |
| --- | ---: | --- |
| IBUV challenge | `0x1002` | reads two `int` values, then sends an IBUV confirmation through the client network object |
| IBUV result | `0x1003` | reads two `int` values and records the result |
| Quick-start/server-ready | `0xB007` | flushes remaining data, constructs `0x7001`, writes one length-prefixed character name, and sends it |
| Character/lobby request | `0x7001` | client-side construction is `u16 string length` followed by raw character-name bytes; the same value is also used by the AegisCore lobby-selection alias |
| Agent ping extension | `0x7F00` | local DevKit extension sends a `u32` tick token; this is not stock-client behavior |
| Agent ping response | `0xBF00` | local DevKit extension accepts exactly one `u32` token; this is not stock-client behavior |

The `0x7F00/0xBF00` pair must remain labeled as a GO-SRO/DevKit extension. It is not evidence of an original stock opcode.

### 5. Character selection and world entry

`CPSCharacterSelect::OnCreate` initializes the character-selection interface, enables network-message processing, creates the character/lobby UI sections, loads the idol/map presentation, and enters the selection state. The exact stock routine addresses are retained in the SRO_DevKit wrappers.

After selection, the world bootstrap is the clearest client-confirmed ordering:

```text
0x3017  group-spawn begin
0x3019  group-spawn record list
0x3018  group-spawn end
```

`0x3015` is the single-object-enter path and `0x3016` is its object-left counterpart. The client has separate object, chest, and other grouped records; do not reuse an NPC record for a player, item, COS, or mob.

### 6. In-world loop and exit

The client receives movement and combat updates, mutates client-side world objects/UI, and sends action requests through the same `CMsgStreamBuffer` path. Focused stock-confirmed handlers include movement (`0xB021`, `0xB023`), action/combat (`0xB070`, `0xB071`), item result (`0xB034`, `0xB04C`), quest/dialogue (`0x30D4`, `0x30D5`, `0x3CA2`), chat (`0x3026`), and game-time/control paths. Logout/reset messages include `0xB005`, `0xB006`, and `0x3055`; the exact process-transition effect of every error branch remains to be traced.

Player/object selection is the `0x7045/0xB045` family. The client sender at `0x0078F5F0` writes exactly one `u32 targetUID`; `CNetProcessIn::OnB045` at `0x0088B200` resolves that UID and consumes a runtime-class-specific response tail. The end-to-end right-click/player branch is documented in [player-right-click-target-selection.md](player-right-click-target-selection.md). The similarly named `0x7445/0xB445` pair in preserved eSRO sources is not this binary profile.

The binary-confirmed prerequisite edges are:

```text
security/session established
    -> quick-start/server-ready
    -> character selected (0x7001)
    -> 0x3017 begin
    -> 0x3019 group-spawn record list
    -> 0x3018 end
    -> in-world movement/action requests
```

`0x3019` cannot be decoded as a single record type: its discriminator-dependent object branches must complete inside the `0x3017`/`0x3018` envelope. Movement/action traffic is therefore not treated as world-ready merely because the TCP session or Agent authentication succeeded.

## Opcode inventory

The exact-profile client handler index contains 321 registrations: 220 on `CNetProcessIn`, 39 on `CNetProcessSecond`, and 62 on `CNetProcessThird`. These are registration entries, not a claim that all 321 values are fully decoded or that all are public client-to-server wire requests. Many are server-to-client notifications, aliases, internal/control messages, or handlers that delegate to an undecoded stock routine.

The complete registration values, in source order, are below. The authoritative handler names and stock target addresses are in the pinned source files; the exact client address checks are summarized in the focused table that follows.

### `CNetProcessIn` — 220 entries

`0x3027, 0x3809, 0x3055, 0xB005, 0xB006, 0x300A, 0x3405, 0x34B5, 0x300C, 0x34A5, 0x3013, 0x34A6, 0x3020, 0x3017, 0x3019, 0x3018, 0x3047, 0x3049, 0x3048, 0x3015, 0x3016, 0x3076, 0x30BF, 0xB0CB, 0xB03C, 0x3092, 0x30DF, 0x30CD, 0x30CE, 0x30D3, 0x30C1, 0x30C2, 0x30C3, 0x30C8, 0xB0C6, 0x30C9, 0x30E7, 0xB0C7, 0x30CA, 0xB116, 0xB117, 0xB420, 0x30D4, 0x3054, 0x3056, 0x304E, 0x3052, 0x3057, 0xB045, 0x303D, 0x30D2, 0x30CF, 0x3011, 0x3058, 0xB025, 0x3026, 0x3091, 0x302D, 0xB021, 0xB024, 0xB023, 0x3028, 0xB04F, 0x30D0, 0x3200, 0xB070, 0xB071, 0xB0BD, 0xBF00, 0x330F, 0xB0BE, 0xB0BC, 0xB072, 0x30A6, 0x30D1, 0x3206, 0xB0A1, 0xB0A2, 0xB203, 0xB202, 0xB034, 0x3038, 0x3039, 0x3036, 0xB03E, 0xB03F, 0x304D, 0xB04C, 0x3068, 0x305C, 0x30E0, 0x3040, 0x3201, 0x3041, 0x3042, 0x3153, 0x3154, 0xB046, 0xB04B, 0xB0C0, 0xB050, 0xB051, 0xB0C5, 0xB059, 0xB05A, 0xB074, 0x30EB, 0xB0EA, 0xB05B, 0x34B1, 0xB060, 0x3864, 0x3065, 0xB062, 0xB067, 0xB069, 0xB06A, 0xB06B, 0xB06C, 0x706D, 0xB06D, 0xB081, 0x3080, 0x3085, 0xB082, 0xB083, 0xB084, 0x3089, 0x3086, 0x3087, 0x3088, 0x30E8, 0x308C, 0xB0B1, 0x30B8, 0xB0B2, 0x30B9, 0x30B7, 0xB0B3, 0xB0B5, 0xB0B4, 0xB0BA, 0x30BB, 0xB0A7, 0xB010, 0x30D6, 0x30D7, 0x30D5, 0xB0D8, 0xB0D9, 0x30EC, 0x30DC, 0xB515, 0x3514, 0x3CA2, 0x30E6, 0xB0E1, 0xB0E2, 0xB0E3, 0xB0E4, 0xB0E5, 0xB0E6, 0xB0F0, 0x38F5, 0xB0F1, 0xB0F8, 0xB0F3, 0x30FF, 0x3100, 0xB0F2, 0xB0F4, 0x34B3, 0x3101, 0x34B4, 0xB0F9, 0xB0F6, 0xB0FA, 0xB103, 0xB104, 0xB105, 0xB106, 0x3908, 0xB107, 0xB0FB, 0x3102, 0xB0FC, 0xB0FD, 0x3109, 0x7110, 0xB110, 0xB112, 0xB114, 0xB113, 0xB501, 0x30EF, 0x3305, 0xB302, 0xB304, 0x7302, 0xB30B, 0xB308, 0xB309, 0xB30A, 0x3B07, 0x3204, 0xB155, 0x3156, 0xB157, 0xB150, 0xB151, 0xB16A, 0x3207, 0xB0ED, 0x34BA, 0xB402, 0xFFC, 0x2000, 0xFFD, 0xB05E, 0x3078`

### `CNetProcessSecond` — 39 entries

`0xB118, 0xB119, 0x3120, 0x320C, 0x30DA, 0xB0DB, 0xB250, 0xB251, 0x3253, 0x3255, 0x3254, 0xB252, 0xB30C, 0xB256, 0x3256, 0x3257, 0xB258, 0xB259, 0xB25A, 0x7030, 0xB030, 0xB031, 0xB461, 0xB463, 0xB462, 0xB47A, 0xB47B, 0xB47C, 0xB47D, 0x747E, 0xB47E, 0xB0FF, 0x3103, 0x385F, 0xB05D, 0x3077, 0xB4D4, 0x34D5, 0xB516`

### `CNetProcessThird` — 62 entries

`0x3209, 0x320A, 0x320B, 0xB11A, 0x3159, 0x315A, 0x315B, 0x315C, 0x315D, 0x315E, 0x3161, 0xB15F, 0xB160, 0x325C, 0x325D, 0x325E, 0xB121, 0xB30D, 0xB168, 0xB470, 0x3C81, 0xB471, 0xB472, 0xB473, 0xB474, 0xB475, 0xB476, 0xB477, 0xB478, 0x3C80, 0x3C82, 0x3C86, 0xB483, 0x3C87, 0x3122, 0x34AA, 0x325F, 0x3261, 0x34BE, 0x34D2, 0xB4DF, 0xB4E0, 0x34E1, 0x34F2, 0xB519, 0x751A, 0xB51A, 0xB51C, 0xB51D, 0x351E, 0xB50C, 0xB50E, 0xB507, 0xB508, 0xB509, 0x350D, 0xB506, 0xB50A, 0xB50B, 0x3530, 0x3531, 0x3532`

### Focused address and handler map

| Opcode | Stock handler VA | Client-confirmed behavior |
| ---: | ---: | --- |
| `0x3017` | `0x008AC0E0` | group-spawn begin; must precede `0x3019` |
| `0x3019` | `0x008AC170` | group-spawn data; record layout is polymorphic |
| `0x3018` | `0x008AC3D0` | group-spawn end; must follow data |
| `0x3015` | `0x008A6CA0` | single object entered |
| `0x30C8` | `0x008A6650` | owner COS snapshot; subtype decoder `0x009A1900` |
| `0x30D4` | `0x00889D90` | reads `u8`, encoded string; types 4/5 add count and strings |
| `0x30D5` | `0x008812B0` | reads `u8`, `u32`; wire discriminators 1..4 branch |
| `0x3CA2` | `0x008ABCC0` | reads encoded string, then `u32` |
| `0xB021` | `0x008A5750` | reads UID plus movement union/control data |
| `0xB023` | `0x008A4F80` | reads UID, region, three floats, heading |
| `0xB070` | `0x008A5FD0` | result-dependent combat/action parser |
| `0xB071` | `0x008A4960` | combat continuation/result subsystem |
| `0xB04C` | `0x008A4EC0` | reads result byte; success reads `u8,u16,u16` |
| `0xB034` | `0x0087F370` | result envelope; success delegates to item-record decoder `0x0087EB50` |
| `0xB202` | `0x00880DB0` | dedicated potion/skill-restoration response; server failure body is `u8 2,u16` |
| `0xB203` | `0x00880E90` | sibling dedicated potion/skill-restoration response; server failure body is `u8 2,u16` |
| `0x3040` | `0x0088BF30` | flag-selected inventory/item state update; kind-gated tail |
| `0x3153` | `0x00882DE0` | reads three `u32` balance values; stores them at client state `+0x4AC/+0x4B0/+0x4B4` |
| `0x3154` | `0x00882E50` | reads `u8 reason, u8 cashType, u32 value`; formats Silk/Silk-point status text |

The complete field-level decoder contracts are documented in [`vsro188-client-decoder-layouts.md`](vsro188-client-decoder-layouts.md). The `0xB034` envelope is exact; its success body is discriminator-driven and is not one universal item struct.

## Confirmed packet layouts and structures

### `CMsgStreamBuffer`

The client packet stream object is 28 bytes:

| Offset | Width | Field/role |
| ---: | ---: | --- |
| `0x00` | `u32` | current read byte count |
| `0x04` | `u32` | bytes available for reading |
| `0x08` | 4 bytes | mode/flags; `ToggleBefore` and `ToggleAfter` state |
| `0x10` | pointer | first `SMsgStreamNode` |
| `0x14` | pointer | current read/write node |
| `0x18` | `u16` | message/opcode ID |
| `0x1A` | 2 bytes | tail/padding within the 28-byte fixture |

`CMsgStreamBuffer::Read` is at `0x004F7220`, raw write is at `0x00508FE0`, string extraction is at `0x004F7A70`, destructor target is at `0x005097A0`, opcode support checking is at `0x00841780`, and the send path is at `0x008418D0`.

Primitive encoding is little-endian. The client’s string writer emits `u16 byte-length` followed by exactly that many raw bytes. The reader can cross linked 4 KiB message nodes and rejects/diagnoses reads that exceed the available logical payload.

### Movement/action packets

These are the strongest focused request/response constructions recovered from the exact client:

```text
0x7021 directional request:
    u8  0
    u16 heading

0x7021 ordinary destination request:
    u8  1
    i16 region
    i16 x
    i16 y
    i16 z

0x7021 high-bit-region destination request:
    u8  1
    i16 region       // high bit set; signed branch discriminator
    u32 x
    u32 y
    u32 z

0x7023/0x7024 heading request:
    u16 heading

0xB021 movement response:
    u32 entityUID
    u8  0
    u8  source
    u16 heading
    u8  control

0xB023 authoritative position:
    u32 entityUID
    u16 region
    f32 x
    f32 y
    f32 z
    u16 heading

0x7074 targeted basic attack request:
    u8  1
    u8  4
    u32 targetUID
    u8  0

0x7074 entity-skill target request:
    u8  1
    u8  4
    u32 skillID
    u8  1
    u32 targetUID

0x7074 position-skill request:
    u8  1
    u8  4
    u32 skillID
    u8  2
    i16 region
    i32 x
    i32 z
    i32 y

0x7074 remove-own-buff request:
    u8  1
    u8  5
    u32 skillID
    u8  0

0xB070 success prefix:
    u8  result = 1
    u16 resultFamily = 0x3000 or 0x3002
    u32 directField0
    u32 directField1
    u32 directField2
    // delegated action/damage suffix begins here

0xB070 failure:
    u8  result != 1
    u16 failureCode

0xB071 success prefix:
    u8  result = 1
    u32 actionOrCastID
    u32 delegatedValue       // common writer emits zero; client reads f32
    u8  continuationFlags
    // delegated action/position suffix, if the selected action path has one

0xB071 failure:
    u8  result != 1
    u16 failureCode
    u32 actionOrCastID
```

### Dedicated item-use packets

The exact client senders at `0x00820E30` and `0x00820F10` provide two
additional item-use request contracts:

```text
0x7202:
    u32 itemRefID
    u32 targetSkillOrMasteryID
    u8  targetLevel

0x7203:
    u32 itemRefID
    u32 targetSkillOrMasteryID
    u8  targetLevel

0xB202 failure:
    u8  result = 2
    u16 errorCode

0xB202 success:
    u8  result = 1
    u32 resultingSkillID

0xB203 failure:
    u8  result = 2
    u16 errorCode

0xB203 success:
    u8  result = 1
    u32 masteryID
    u8  level
```

The field widths are binary-confirmed; semantic names remain deliberately
neutral. See the [item lifecycle report](sr-gameserver-items-inventory-mall.md)
for the matching GameServer item-name gates and mutation path.

The `0x7074` forms differ from some Vietnam-branch documentation. The exact-client constructor is authoritative for this profile; no cross-version form should replace it. The field names for action kind, target mode, and action/cast IDs are descriptive where the binary proves only widths and branch values. The B070/B071 delegated tails are action-family dependent; a target UID is not part of either fixed prefix.

### Quest/dialogue packets

The client parser proves widths and ordering, but not every field’s semantic name:

```text
0x30D5:
    u8  discriminator       // accepted wire values 1..4
    u32 value                // branch-dependent; semantic name unresolved

0x30D4:
    u8  dialogueType
    string encodedText
    // types 4 and 5:
    u8  selectionCount
    string selections[selectionCount]
    // type 5 has additional string/u32 data in the extended branch

0x3CA2:
    string encodedText
    u32 value                 // consumer-dependent
```

The focused evidence is preserved in [`analysis/decompiled/quest-retdec/FINDINGS.md`](../../analysis/decompiled/quest-retdec/FINDINGS.md) and the address-annotated RetDec exports beside it. Server-side semantic labels in [`sr-gameserver-quest-system.md`](sr-gameserver-quest-system.md) are useful correlation, but must not be mistaken for additional client-byte proof.

### Item result packets

The entity bootstrap sequence is `0x3017`, one or more `0x3019` data
packets, then `0x3018`; the registration table at `0x0086FBB0` binds these
to the object-list handlers. `0x3017` at `0x008AC0E0` reads
`groupAction:u8` and `objectCount:u16`. Action `1` is the spawn path and
action `2` is the removal path. `0x3019` at `0x008AC170` drains the
accumulator in `0x1000`-byte chunks and dispatches TypeID-dependent object
records. `0x3018` at `0x008A6F50` normally reads two status bytes, but a
client interface bootstrap flag takes a no-read internal-buffer branch.
Single spawn `0x3015` enters the `0x008A6BD0` record dispatcher through the
wrapper at `0x008A6CA0`; single removal `0x3016` reads `uniqueId:u32` at
`0x008A6CC0`. The four-byte ID is only the start of a spawn record, not its
complete schema.

Owner COS snapshot `0x30C8` is registered at `0x0086FBB0` with handler
`0x008A6650`. Its body decoder `0x009A1900` first reads
`uniqueId:u32, refObjectId:u32, health:u32, maxHealth:u32`, resolves the
reference TypeID, and then enters subtype-specific branches. Packed high
TypeID values `0x0800/0x1000/0x1800/0x2000/0x2800` identify ride, job,
growth, ability, and fellow COS families respectively. A ride (`TypeID4=1`)
has no experience, settings, name, owner, or instrument-slot fields; it
continues directly with `inventoryCapacity:u8`. The decoder reads
`itemCount:u8` and repeated `slot:u8 + item` records only when capacity is
nonzero. Consequently the ordinary empty-inventory horse packet is exactly
17 bytes and ends in a single zero capacity byte. A 16-byte common-prefix-only
packet deterministically overreads at this point; appending a second zero
count byte when capacity is zero is also not the decoded grammar.

Inventory operation response `0xB034` at `0x0087F370` has a fixed
`result:u8`, then `errorCode:u16` for result `2`; result `1` enters the
conditional `0x0087EB50(mode=0)` item grammar. That parser dispatches on a
wire discriminator byte and has explicit branches for equipment, expendable,
COS/container, and special item families. Its direct reads range from
single-byte fields through `u64` and repeated counted records; the same
helper family also exposes the `u16 length + raw bytes` path at
`0x00873560` in the alternate mode. It is unsafe to replace with a fixed
`CSOItem` image or to emit only the common reference ID.

Alchemy result decoders are split across three binary-confirmed handlers:
`0xB150` at `0x008729E0`, `0xB151` at `0x00872CD0`, and `0xB16A` at
`0x00872FA0`. `B150` and `B151` begin `result:u8`; error result `2` is
followed by `errorCode:u16`, while success result `1` is followed by a phase
byte. Phase `1` is cancellation. Phase `2` reads `status:u8` and a raw item
kind byte; `B150` then reads a record marker and only parses the variable
item when marker is zero, while `B151` parses it directly when status is
zero. `B16A` begins `result:u8,status:u8`, reads `errorCode:u16` on result
`2`, and reads `itemKind:u8` plus the variable item record for statuses `2`
through `4`. The shared record parser is `0x008BE4F0` and is conditional on
the reference item's TypeID; it is not a fixed `0x1D0`-byte packet.

The shared mode-1 item prefix is `0x008BB130`: `u32 rentFlags`, conditional
rent fields, then `u32 referenceItemId`. Equipment records continue with
`u8 optionLevel`, `u64 variance`, `u32 durability/value`, a counted magic
section (`u8 count` plus `u64` entries, maximum `12`), and two counted binding
sections (`u8 type`, `u8 count`, then `u8 slot` plus `u64` per entry). The
type-3 rent branch is ordered `u16, u32, u32, u16, u32`; this order is covered
by the AegisCore serializer golden test.

Within the shared `0x008BE4F0` record parser, TypeID2 `3` expendables are
not one shape: the client reads a `u32` amount for the TypeID3 `5` money
branch, `u16 quantity + u8 assimilation` for TypeID3 `11` magic/attribute
stones with TypeID4 `1/2`, `u16 quantity + counted u64 metadata` for the
TypeID3 `14` gacha branches, and only `u16 quantity` for ordinary
expendables. These exact predicates are at `0x00538410`, `0x00871DE0`,
`0x0056FCE0`, `0x0056FCA0`, and `0x00666FC0`; AegisCore's item serializer
matches the branch widths.

TypeID2 `2` container/COS records likewise begin with a `u8` state and, for
active states, conditionally carry a `u32` referenced object, a name/string
path, lifetime data, and counted buff records. The active buff record is
`u8 kind, u32 value, u32 value`, with `u32` plus `u8` appended for kind `5`.
The client predicates at `0x00666C40` and `0x00666CB0` select these branches;
they are not universal container fields.

The adjacent material/dismantle responses are also fixed: `0xB155` and
`0x3156` use `result:u8`, `errorCode:u16` on result `2`, and on success
`phase:u8`; phase `2` adds `status:u8`, with `value:u32` only when status is
`2`. `0xB157` uses `result:u8`, `errorCode:u16` on result `2`, and no success
tail. Their client wrappers are `0x00872810` and `0x00872940`.

`0x3040` is a separate inventory/item state-update message, not another
`0xB034` item-record body. Its exact client read order is documented in the
[field-level decoder layout](vsro188-client-decoder-layouts.md#0x3040-inventoryitem-state-update-at-0x0088bf30):
`itemKind:u8`, `flags:u8`, flag bits `0x01/0x02/0x04/0x08/0x10` selecting
`u32/u8/u64/u16/u32`, `0x20` selecting `u8 count` plus `count * u64`
magic-option entries, and the `0x40`/`0x80` fields only
when `itemKind >= 0x0D`. AegisCore has a corresponding exact-order codec and
golden test; the values remain semantic-neutral pending typed capture/runtime
identity.

`0xB04C` has a confirmed leading `u8 result`; its success branch reads `u8`, `u16`, `u16`. `0xB034` has a confirmed result/error branch in which the error path reads a `u16`, while success enters a variable item record. The variable item record is not documented as one fixed struct because item type, sockets, bindings, advanced attributes, COS state, and other flags select optional fields.

### Runtime object layouts

These are ABI/layout fixtures, not wire packet schemas.

| Type | Size | Confirmed fields |
| --- | ---: | --- |
| `CIObject` | `0x0DC` | region `+0x7C`; location `D3DVECTOR` `+0x80`; rotation `f32` `+0x8C` |
| `CIGIDObject` | `0x254` | unique ID `+0xF8`; name `std::n_wstring` `+0x110`; title `+0x194`; common-data pointer `+0x220` |
| `CICharactor` | `0x78C` | abnormal state `+0x2C4`; current MP `+0x454`; max HP `+0x458`; max MP `+0x45C`; current HP `+0x460`; job type `+0x50D`; character state `+0x65C` |
| `CICPlayer` | `0x2118` | character name `+0x84C`; level `u8` `+0x868`; current EXP `i64` `+0x870`; skill progress `+0x878`; strength `i16` `+0x87C`; intelligence `i16` `+0x87E`; skill points `+0x880`; available stat points `i16` `+0x884`; job alias `+0x2098`; world ID `i16` `+0x20E4` |
| `CSOItem` | `0x1D0` | valid `u8` `+0x28`; reference item ID `+0x34`; option level `u8` `+0x8C`; current durability `+0x98`; quantity `+0x9C`; physical/magical attack bounds `+0xE0..+0xEC`; blocking rate `+0xF0`; maximum durability `+0xF4`; physical/magical defense and absorption `+0xF8..+0x110`; reinforcement values `+0x114..+0x128`; attack rate `+0x130` |
| `CCharacterDependentData` | fixture offsets | party data `+0x20`; guild data `+0x148`; training-camp data `+0x248` |
| `CGame` | `0x960` | Media manager `+0x4C4`; Map manager `+0x4C8`; Data manager `+0x4CC`; Music manager `+0x4D4`; game config `+0x6F8`; bad-word filter pointer `+0x8E8` |

The `CSOItem` names and offsets are pinned by [`SOItem.h`](../../.references/sro-devkit/source/libs/ClientLib/src/SOItem.h). They describe the runtime object and must not be copied into every polymorphic wire record.

Important fixed globals include `CGame* 0x00EECE80`, global data manager `0x00EEDF08`, character-dependent data `0x00EEEBD8`, current player pointer `0x00EEF5EC`, current region `0x00EEF68C`, entity manager `0x0110F7D8`, Direct3D application `0x0110F7EC`, interface `0x0110F80C`, client network object `0x0115B0C0`, current navmesh `0x0115B1CC`, and new interface manager `0x0115DD40`.

The C++ ABI matters: these layouts use the client’s 32-bit VC80 object model, vtable order, custom allocator, and `std::n_*` types. The DevKit ABI test is false-green because its four string-layout checks fail while the test process exits zero; do not treat a successful test executable exit as proof that a modern compiler’s STL objects are compatible.

## SQL and persistence calls

### Client result: no direct SQL call graph

The client has no binary-confirmed SQL call sequence. Specifically:

1. The PE imports contain no ODBC, MySQL, SQLite, ADO, or database-driver symbols.
2. The direct DLL set contains no database client DLL.
3. The ASCII and UTF-16 string scans found no `SQL`, `ODBC`, `MySQL`, `SQLite`, `SELECT`, `INSERT`, `UPDATE`, `DELETE`, or database-query literal.
4. The client network path serializes opcodes and gameplay fields; it does not construct a server SQL job.

This is a static negative result, not a proof against an unknown dynamically loaded/private component. No such component or call edge appears in the inspected exact-client analysis, so the working conclusion is: **the stock client delegates persistence to the remote server over its protocol; it does not issue SQL itself.**

### Where SQL belongs in this repository

The current AegisCore SQL boundary is server-side:

- Gateway handles account/login and patch/catalog data.
- Agent handles shard/gameplay persistence through Go repositories and MySQL.
- Migration SQL is embedded under `db/migrations` and is never run by the client.
- The binary-confirmed server SQL catalogs and GameServer reverse-engineering notes are separate evidence sets; they must not be cited as client calls.

The checked-in profile remains `evidence-pending` because it has no client executable hash, locale/content ID, or sanitized stock capture. That status is correct even though the static client identity is now known: a capture is still required to promote semantic packet layouts and prove live end-to-end compatibility.

## Evidence map and next promotion steps

Primary artifacts:

- [`analysis/SRO_CLIENT_DEVKIT_FULL_REPORT.md`](../../analysis/SRO_CLIENT_DEVKIT_FULL_REPORT.md) — PE identity, imports, patched-vs-stock comparison, framing, focused opcodes, ABI, and limitations.
- [`analysis/decompiled/quest-retdec/FINDINGS.md`](../../analysis/decompiled/quest-retdec/FINDINGS.md) — exact-client quest/dialogue read order.
- [`analysis/decompiled/shop-client-exact/004F7220_FUN_004f7220.c`](../../analysis/decompiled/shop-client-exact/004F7220_FUN_004f7220.c) — bounds-checked message-buffer read.
- [Pinned `NetProcessIn.cpp`](../../.references/sro-devkit/source/libs/ClientLib/src/NetProcessIn.cpp) — exact-profile handler registrations and focused wrappers.
- [Pinned `NetProcessSecond.cpp`](../../.references/sro-devkit/source/libs/ClientLib/src/NetProcessSecond.cpp) and [Third](../../.references/sro-devkit/source/libs/ClientLib/src/NetProcessThird.cpp) — remaining handler registrations.
- [Pinned `MsgStreamBuffer.h`](../../.references/sro-devkit/source/libs/ClientNet/src/ClientNet/MsgStreamBuffer.h) — 28-byte ABI fixture and primitive serialization contract.
- [`protocol/captures/README.md`](../protocol/captures/README.md) — required provenance and sanitization rules for future wire fixtures.

The detailed field-level companion is [`vsro188-client-decoder-layouts.md`](vsro188-client-decoder-layouts.md).

The next evidence promotion is a sanitized capture set from this exact client/Data.pk2 pair covering Gateway login, Agent admission, `0x7001`, the `0x3017/0x3019/0x3018` bootstrap, movement, one basic attack, one item operation, and one quest dialogue branch. Until those fixtures exist, semantic names and server compatibility claims remain deliberately narrower than the static opcode and structure facts above.
