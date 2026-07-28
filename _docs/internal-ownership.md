# Internal ownership after the behavior-preserving refactor

The runtime topology remains MySQL + Gateway + Agent. These boundaries describe
in-process ownership only; they are not new services and do not change vSRO
1.88 traffic.

## Shared framework

`server.Session` remains the public session facade. Internally:

- `sessionLifetime` exclusively owns cancellation, inbound worker accounting,
  the immediate `Done` signal, and the terminal `Drained` signal;
- `inboundMassive` exclusively owns synchronized `0x600D` assembly, expiry,
  and shutdown validation;
- `Sender` remains the single ordered outbound writer;
- protocol security and handshake state remain on `Session`, guarded by its
  security lock and ordered phase values.

The reconnecting framework client continues to own one lifecycle and at most
one active socket generation. Each generation owns its reader, socket,
massive-message assembler, and write lock.

## Process composition

`NewGatewayServer` and `NewAgentServer` remain compatibility composition roots
that load configuration and infrastructure. The dependency constructors build
the lifecycle facades from already-created resources, allowing tests and future
entrypoints to avoid configuration, TLS, filesystem, or database side effects.

The Agent database package now owns concrete SQL repository composition through
`GameplayRepositories`. Handlers and services consume the existing narrow
repository interfaces; `AgentServer` owns only resources needed for ordered
shutdown.

## World

`WorldService` remains the handler- and manager-facing facade. Its internal
responsibilities are separated as follows:

- `worldIndex` maintains player identity and visible-object indexes as one
  atomic admission operation;
- `worldRegions` owns immutable terrain lookup and detached public snapshots;
- `movementCoordinator` owns movement timing and the authoritative movement
  algorithm entrypoint;
- the existing visibility reconciler and position-persistence queue retain
  their independent serialization and drain rules.

The world mutex, visibility mutex, unique-ID counter, collision behavior,
eager navmesh loading, region membership, and persistence cadence are unchanged.

## Gameplay request flow

Refactored request paths follow this order:

1. handler validates and decodes the client packet;
2. handler resolves the authenticated session/player;
3. typed use case applies rules, repository transaction, and committed state;
4. handler emits the existing response packets.

Recovery-item consumption and mastery upgrades use this boundary now. Their
compatibility constructors and packet fixtures preserve existing call sites and
wire bytes. Other handlers can migrate to the same shape independently, with a
green verification gate after each domain.

## Migrations

The migration runner now separates catalog/ledger reconciliation into an
immutable `migrationPlan` and delegates pinned-connection advisory-lock
ownership to `migrationLockCoordinator`. SQL text, checksum rules, ledger
states, target validation, lock names, and up/down execution order are unchanged.
