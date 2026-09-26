# Handover: Temporary Enhancement Tooltip Timer

## Objective

Display the remaining temporary weapon, armor, or accessory enhancement time inside the target item tooltip while preserving:

- temporary projected `+N`;
- rejection of another scroll while the category service is active;
- expiration reversion to the permanent option level;
- teleport and relog persistence;
- stable world entry.

## Current status

### Working

- Enhancement scroll targeting works.
- Temporary projected levels work:
  - weapon `+2`;
  - armor `+1`;
  - accessory `+1`.
- Derived character stats receive the temporary effect.
- A second scroll for the same active category is rejected intentionally.
- Target metadata is persisted in the database.
- The Agent emits the apply-time packet:

  ```text
  0x3040
  flags = 0x82
  flag 0x02 = projected option level
  flag 0x80 = expiration value
  ```

- The patched client consumes ordinary in-world `0x3040/0x82` packets without crashing.
- Full Docker `go test ./...` passed after the latest server changes.

### Not working

- The equipment tooltip does not display the timer.
- The previously selected tooltip function, `0x007A3BD0`, was wrong. A breakpoint there did not fire while hovering the bow, bracer, or earring.
- Login/world-entry restoration has been unstable:
  - immediate restoration caused a client crash;
  - the current implementation delays restoration by two seconds after GameReady, but this still needs a live relog test.

## Current deployment state

### Client

Current patched executable:

```text
VSRO_Client/sro_client.exe
```

Prepared copy:

```text
PatchSource/sro_client_timed_tooltip.exe
```

Current SHA-256:

```text
2f49624db0c7c8d4d9cffc604429b7ec7584a74cfe2f08f3ee2bfa7d5b479342
```

Stock backup:

```text
VSRO_Client/sro_client.exe.stock-backup
```

The current client contains two modifications:

1. `0x3040` low-item-kind/equipment timer handling.
2. An experimental formatter eligibility patch based on the earlier, incorrect `0x007A3BD0` hypothesis.

The second patch has not produced a visible timer. Revert it before the next controlled experiment so the only live client change is the proven `0x3040` handler patch.

### Agent

Current restoration behavior is in:

```text
agent-server/handler/character/game_ready.go
```

After GameReady:

```go
time.AfterFunc(2*time.Second, func() {
    if player.Session == session && player.GetLifeState() != model.Spawning {
        publishRestoredItemServices(player)
    }
})
```

Risk: this two-second delay is heuristic, not tied to a protocol-confirmed post-world lifecycle event. It also runs from a timer goroutine. Verify the relevant player/session methods and packet writer are safe for this use, or route the operation through the existing serialized player/game scheduler.

## Confirmed crash evidence

A failed login session showed:

```text
0x3013 character bootstrap
0x34A6 character-data end
...
0x325F timed services
0x3040 size=7
client EOF approximately 382 ms later
```

The crashing restoration packet was sent before GameReady/world initialization was safely complete. The Agent process did not crash; the client disconnected.

## Database migration

Added and applied:

```text
db/migrations/shard/000143_item_service_target_projection.up.sql
db/migrations/shard/000143_item_service_target_projection.down.sql
```

New `CHAR_ITEM_SERVICE` columns:

```text
TARGET_SLOT
TARGET_BASE_OPT_LEVEL
TARGET_PROJECTED_OPT_LEVEL
```

Persistence was added to:

```text
agent-server/model/item_use_repository.go
```

`LoadItemServices` now restores:

- target slot;
- base option level;
- projected option level;
- `TargetSlotSet`.

Fresh-install snapshot and migration count were updated:

```text
db/create_sro_shard.sql
framework/db/migrations_test.go
```

Migration result:

```text
shard migrations applied successfully
```

## Packet implementation

Shared packet builder:

```text
agent-server/model/item_service_packets.go
```

Symbol:

```go
BuildItemServiceTargetLevelUpdate
```

Behavior:

- Active:

  ```text
  flags 0x82
  projected option level
  expiration Unix timestamp
  ```

- Inactive or expired:

  ```text
  flags 0x82
  permanent option level
  expiration zero
  ```

Apply and expiration publisher:

```text
agent-server/handler/inventory/item_use_handler.go
```

Codec extension:

```text
protocol/vsro188/item_update_codec.go
```

`Flag80U32` is accepted for equipped/low item kinds under the patched-client contract.

## Live service evidence

At one point, the live database contained:

```text
kind 6 weapon
source 3790
slot 30
base 0
projected 2

kind 7 armor
source 3791
slot 35
base 0
projected 1

kind 8 accessory
source 3793
slot 36
base 0
projected 1
```

These rows may have expired. Query again before testing:

```powershell
docker compose exec -T db sh -lc 'mysql -usro -p"$MYSQL_PASSWORD" -N -e "
USE SRO_SHARD;
SELECT SERVICE_KIND,SOURCE_REF_ITEM_ID,EFFECT_VALUE,
       TARGET_SLOT,TARGET_BASE_OPT_LEVEL,TARGET_PROJECTED_OPT_LEVEL,
       ACTIVATED_AT,EXPIRES_AT,UTC_TIMESTAMP(6)
FROM CHAR_ITEM_SERVICE
WHERE CHAR_ID=7 AND SERVICE_KIND IN (6,7,8)
ORDER BY SERVICE_KIND"'
```

Bow reapplication was rejected because the weapon service was already active. That is expected, not a target-parser failure.

## Confirmed apply-time packet

A captured weapon update:

```text
payload_prefix_hex = 1e82024942686a
```

Decoded:

```text
slot       = 0x1E / 30
flags      = 0x82
projected  = 2
expiration = 0x6A684249
```

The client stayed connected after ordinary in-world `0x3040/0x82` packets.

## SRO_DevKit research

DevKit was cloned to:

```text
.local/sro-devkit
```

Source:

```text
https://gitlab.com/florian0/sro_devkit
branch: devel
```

Useful files:

```text
.local/sro-devkit/source/libs/ClientLib/src/IFSlotWithHelp.h
.local/sro-devkit/source/libs/ClientLib/src/IFSlotWithHelp.cpp
.local/sro-devkit/source/libs/ClientLib/src/SOItem.h
.local/sro-devkit/source/libs/ClientLib/src/SOItem.cpp
.local/sro-devkit/source/libs/ClientLib/src/IFHelperBubbleWindow.h
```

DevKit mapping:

```cpp
CIFSlotWithHelp::SetSlotData(CSOItem*)
→ 0x006871D0
```

`CIFSlotWithHelp` layout:

```text
m_itemSourceParentWindowId  +0x370
m_inventorySlotIndex        +0x378
m_pItem                     +0x390
m_myItem                    +0x398
```

`CSOItem` known fields:

```text
m_refObjItemId      +0x34
m_OptLevel          +0x8C
m_CurrDurability    +0x98
m_quantity          +0x9C
```

## Correct runtime path discovered

IDA decompilation of `0x006871D0` shows the slot update path directly querying item expiration:

```cpp
if (sub_55EDF0(itemData) && sub_8BA580(item) == 1) {
    expiration = sub_8BA5B0(item);

    if (expiration > 0) {
        ...
        sub_666640(this);
    }
}
```

Relevant addresses:

```text
0x006871D0  CIFSlotWithHelp::SetSlotData
0x0055EDF0  item eligibility predicate
0x008BA580  timed-state flag getter
0x008BA5B0  expiration getter
0x00666640  item/slot refresh callback
```

Expiration getter:

```asm
0x008BA5B0  mov eax, [ecx+60h]
0x008BA5B3  mov edx, [ecx+64h]
0x008BA5B6  retn
```

It returns the 64-bit value from:

```text
item + 0x60
item + 0x64
```

in `EDX:EAX`.

## IDA debugger state

The user attached IDA's native Windows debugger successfully. The bridge reported:

```text
active: true
can_query: true
```

The MCP bridge exposes debugger status and current address, but not breakpoint creation, resume, register reads, or stack inspection. Those actions currently must be performed in the IDA UI.

## Next exact debugging experiment

Set these breakpoints in IDA:

```text
0x006871D0
0x0055EDF0
0x008BA5B0
```

Then:

1. Resume with `F9`.
2. Hover an enhanced earring or bracer.
3. When `0x006871D0` fires, record:
   - `this`;
   - `a2`, the `CSOItem*`;
   - raw `[item+0x60]`;
   - raw `[item+0x64]`;
   - call stack.
4. Step through `sub_55EDF0` and record its boolean return.
5. At `0x008BA580`, record the timed-state flag.
6. At `0x008BA5B0`, record returned `EDX:EAX`.
7. Confirm whether `sub_666640` executes.

### Interpretation

#### Case A: `sub_55EDF0` returns false

The item-category eligibility gate excludes equipment. Reverse `0x0055EDF0` and patch only the proven branch.

#### Case B: `sub_8BA580` is not `1`

The packet handler writes the expiration value but does not set the client timed-state flag for this equipment item.

#### Case C: `[item+0x60/+0x64]` or `EDX:EAX` is zero

The `0x3040` handler did not store the expiration in the same `CSOItem` instance used by the slot. Trace `0x0088BF30` and compare its selected item pointer with the `CSOItem*` at `0x006871D0`.

#### Case D: expiration is valid and `sub_666640` runs

Timer state is correct. The remaining problem is the final equipment tooltip text/layout function called from `sub_666640` or a subsequent slot refresh. Use the runtime call stack instead of patching `0x007A3BD0`.

## Do not repeat

- Do not patch `0x007A3BD0`; the equipment hover breakpoint did not hit it.
- Do not send restoration `0x3040` before GameReady.
- Do not change client and server variables in the same test.
- Do not clear the active-service rejection; it is correct.
- Do not send flag `0x80` to an unpatched stock client.
- Do not assume a visible `+N` proves the timer field was stored in the same `CSOItem`.
- Do not treat the two-second `AfterFunc` restoration as final until its concurrency and lifecycle safety are proven.

## Completed implementation

The tooltip timer was implemented in the injected Aegis client extension rather
than as another permanent `sro_client.exe` formatter patch.

### Aegis source and build locations

Primary source:

```text
tools/sro-client-diagnostics/src/compat/temporary_enhancement_tooltip.cpp
tools/sro-client-diagnostics/src/compat/temporary_enhancement_tooltip.h
```

Initialization:

```text
tools/sro-client-diagnostics/src/core/runtime.cpp
```

Build registration:

```text
tools/sro-client-diagnostics/CMakeLists.txt
```

DevKit-enabled build directory and output:

```text
.local/sro-client-diagnostics-devkit-build/
.local/sro-client-diagnostics-devkit-build/Release/Aegis.dll
```

Deployed client extension:

```text
VSRO_Client/Aegis.dll
```

Do not deploy the smaller `plain-build` DLL over the client copy. The deployed
artifact is the full DevKit-enabled build and retains the existing Aegis
integration.

### What changed

The restored `VSRO_Client/sro_client.exe` remains the clean compatibility base.
At client startup, Aegis installs:

1. The proven equipment-compatible `0x3040` kind-gate patch at runtime. This
   lets the client consume the server's `flags = 0x82` update for weapon,
   armor, and accessory items without requiring the old prepared executable.
2. A hook on the real helper-bubble line builder at `0x00776D20`.

The hook follows the live equipment tooltip objects:

```text
CIFHelperBubbleWindow +0x7DC -> owner CIFSlotWithHelp
CIFSlotWithHelp       +0x390 -> CSOItem
```

It then calls the native item accessors:

```text
0x008BA580 -> timed-state flag
0x008BA5B0 -> 64-bit absolute expiration from item +0x60/+0x64
```

For `timed-state == 1` and an unexpired value, Aegis appends a gold line:

```text
Temporary enhancement: 10h 15m remaining
```

Durations of at least one day use:

```text
Temporary enhancement: 1d 02h 14m remaining
```

### Time-basis correction

The first live version treated `0x008BA5B0` as an already-relative duration.
The tooltip consequently displayed an epoch-sized value such as:

```text
Temporary enhancement: 20662d 10h 15m remaining
```

The live result established that the equipment value is an absolute Unix
expiration in milliseconds. The deployed implementation now computes:

```text
remaining = absolute expiration milliseconds - current Unix milliseconds
```

The user restarted the client and confirmed that the corrected tooltip appears
correctly.

### Build and verification

The full extension was built with the configured Visual Studio CMake binary:

```text
"C:/Program Files (x86)/Microsoft Visual Studio/2022/BuildTools/Common7/IDE/CommonExtensions/Microsoft/CMake/CMake/bin/cmake.exe" \
  --build .local/sro-client-diagnostics-devkit-build --config Release
```

Observed verification:

- the DevKit-enabled `Aegis.dll` compiled successfully;
- `AegisClientTest.exe` completed successfully;
- the restored executable matched the expected `0x3040` gate, empty code cave,
  and tooltip-hook prologue before runtime patching;
- the equipment tooltip hook fired for the enhanced bow;
- the tooltip displayed the temporary `+2` and remaining time;
- the corrected time basis was confirmed visually by the user.

### Trade-window limitation

The ordinary owner equipment tooltip is verified. Trade presentation is not
yet verified end to end:

- the owner's offered-item slot will likely reuse the hooked
  `CIFSlotWithHelp` helper-bubble path;
- the counterparty will only see the timer if the exchange item record carries
  the temporary option level and expiration into its `CSOItem`;
- the service itself is character/target-slot scoped and must not transfer as a
  permanent property of the item.

Until the exchange packet and both clients are tested, do not claim that the
counterparty sees the temporary `+N` and timer.

### Remaining lifecycle work

The tooltip objective is complete. Separate lifecycle work remains:

1. Verify teleport behavior.
2. Verify relog restoration with the delayed post-GameReady publication.
3. Replace the heuristic two-second `AfterFunc` with a protocol-confirmed,
   serialized post-world lifecycle hook.
