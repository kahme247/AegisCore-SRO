# Player Trace / Follow

## Scope

This document records the vSRO 1.188 player **Trace** action and the GO-SRO implementation. Trace follows a selected live player while maintaining a server-controlled trailing distance. It is not the ordinary local destination-click action.

## Evidence

### Media.pk2

`VSRO_Client/Media.pk2:resinfo/ifaction.txt` defines the stock Action-window button:

```text
GDR_ACT_ORD_SLOT_TRACE:CIFSlotWithHelp
ID=103
CommandID=1003
```

The Trace control is therefore a stock Action-window command, not a GO-SRO or Aegis UI feature.

### Open-source clientless implementations

Both maintained clientless bot projects build the same Trace request:

- OasisBot: `Plugins/RSBot.Party/Bundle/Commands/CommandsBundle.cs`
- RSBot: `Plugins/RSBot.Party/Bundle/Commands/CommandsBundle.cs`

```text
0x7074 payload: 01 03 01 <targetUID:u32le>
```

Field meaning:

```text
01          action phase: start
03          action kind: Trace
01          entity target mode
targetUID   selected player unique ID
```

OasisBot's trade route implementation additionally establishes the expected follow model: resolve the named player, observe its current position, and issue movement toward that position. Its clientless mode has to synthesize movement itself; stock client behavior still uses the same Trace action request.

### Pinned client binary

Evidence binary: `VSRO_Client/sro_client.exe`, image base `0x00401000`.

- The client Action-window dispatcher is `0x0068CB90`.
- The dispatcher selects an action handler through the jump table at `0x0068CBF1`.
- The shared movement/action controller is `0x0081BA10`.
- Its movement-controller call is at `0x0081C561` (`sub_9EC660`).
- That local movement branch depends on target-action state fields at `+0xC8` and `+0xCC`.

A successful `0xB074` action-state response alone did not make the stock client populate those fields in the live GO-SRO test. GO-SRO therefore owns Trace movement authoritatively instead of relying on an incomplete client-side continuation.

### Stock GameServer

Evidence binary: `VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/SR_GameServer.exe`, image base `0x00401000`.

The `0x7074` action dispatcher at `0x004ACC40` accepts action kind `3` and creates an action object with byte `+0x5 = 3`. This confirms Trace is a valid member of the stock generic action family; it is not a missing custom opcode.

## GO-SRO implementation

`agent-server/handler/character/combat.go` owns the feature.

1. `CombatHandler.handlePacket` decodes and validates `01 03 01 <targetUID>`.
2. The target must be another live player in the follower's known-object list at Trace start.
3. The handler stores `followerUID -> targetUID` and runs a 250 ms follow cycle.
4. Each cycle re-resolves both live players, permitting the target to cross a region/city boundary or leave the follower's transient known-object list.
5. The follower destination is projected along the line to the target, stopping `traceFollowDistance` world units behind it rather than at the target's exact position.
6. The current deployed trailing distance is **30 world units**.
7. The trace ends on explicit action cancel, death, target loss, or an invalid target state.

The follow loop uses existing authoritative `MovementService`/world movement registration. It emits normal server movement updates (`0xB021`) and never teleports the follower.

## Validation

- `go test ./agent-server/handler/character`
- Docker image build runs `go test ./...`
- Live testing established that Trace follows through the city boundary; subsequent tuning adjusted only the trailing distance.
