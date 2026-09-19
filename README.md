# AegisCore

AegisCore is an open-source Go server for Silkroad Online. Its primary target is
vSRO 1.188.

The goal is a stable, secure, and maintainable server that can run the original
client without relying on the legacy closed-source server binaries.

> AegisCore is under active development. Core gameplay works, but stock-client
> compatibility is not complete or production-proven.

## What runs today

The default Docker Compose stack contains:

| Service | Port | Purpose |
| --- | ---: | --- |
| MySQL | `3306` on localhost | Account, character, world, and gameplay data |
| Gateway | `15779` | Client version checks, shard list, login, and Agent handoff |
| Agent | `15882` | Character lobby, world, movement, combat, and gameplay |
| Admission API | `15883` internal only | Secure Gateway-to-Agent handoff over mTLS |

SMC management and launcher patching are optional profiles. They are not needed
for a basic server.

## Roadmap

### 1. Client stability and protocol accuracy

- Validate packets against sanitized vSRO 1.188 captures.
- Remove remaining provisional packet fields.
- Prevent malformed data from crashing the client or server.
- Keep the automated two-client spawn/despawn matrix green for players, mobs,
  ground drops, growth pets, ride transports, service NPCs, and verified
  specialized structures. This protects packet boundaries but does not replace
  stock-client capture validation.

### 2. Playable core

- Complete combat, quests, items, parties, guilds, jobs, pets, and economy.
- Improve world collision, spawning, persistence, and reconnect behavior.
- Add automated coverage for every confirmed client flow.

### 3. Production operations

- Simplify installation, backup, restore, upgrades, and monitoring.
- Reduce Agent memory use and improve startup time.
- Complete the optional management and patch-delivery tools.

## First-time installation

Run all commands from the `go-sro` directory.

### 1. Install Docker

Windows requires:

- Windows 10 or 11 with WSL 2;
- Docker Desktop using Linux containers;
- Git.

Linux requires:

- a current x86-64 Linux distribution;
- Docker Engine;
- Docker Compose v2;
- Git.

Confirm Docker is ready:

```text
docker version
docker compose version
```

Agent needs an 8 GiB container memory limit. Leave additional memory available
for MySQL, Docker, and the operating system.

### 2. Add the private server files

These files are not included in Git:

| Input | Required location or format |
| --- | --- |
| Game archive | `Data/Data.pk2` |
| Database backup | Plain `mysqldump --databases` file containing `SRO_ACCOUNT` and `SRO_SHARD` |
| Admission certificates | Files shown in the TLS layout below |

Do not commit game assets, database backups, certificates, private keys, or
`.env`.

Create the admission TLS directories:

```text
.local/admission-tls/agent/
  agent-cert.pem
  agent-key.pem
  gateway-ca.pem

.local/admission-tls/gateway/
  agent-ca.pem
  gateway-cert.pem
  gateway-key.pem
```

The Agent certificate must:

- have `serverAuth` usage;
- contain the DNS SAN `agent-server`.

The Gateway certificate must have `clientAuth` usage. Use an
operator-controlled CA in production. Never reuse the optional SMC certificate
chain for admission.

### 3. Create `.env`

Create `go-sro/.env` and replace every `CHANGE_ME` value:

```dotenv
MYSQL_PASSWORD=CHANGE_ME_LONG_RANDOM_PASSWORD
MYSQL_DATA_VOLUME=aegiscore-mysql-data

DB_BACKUP_FILE=CHANGE_ME_ABSOLUTE_SQL_BACKUP_PATH

SRO_AGENT_TLS_DIR=./.local/admission-tls/agent
SRO_GATEWAY_TLS_DIR=./.local/admission-tls/gateway
AGENT_ADMISSION_TLS_SERVER_NAME=agent-server

AGENT_PUBLIC_IP=CHANGE_ME_CLIENT_REACHABLE_IPV4
GATEWAY_PROTOCOL_CONTENT_ID=CHANGE_ME
GATEWAY_PROTOCOL_MODULE_NAME=CHANGE_ME
GATEWAY_PROTOCOL_VERSION=CHANGE_ME
```

Windows paths should use forward slashes:

```dotenv
DB_BACKUP_FILE=C:/aegiscore-private/bootstrap.sql
```

Linux example:

```dotenv
DB_BACKUP_FILE=/srv/aegiscore-private/bootstrap.sql
```

For a same-machine development server, add:

```dotenv
AGENT_PUBLIC_IP=127.0.0.1
GATEWAY_ENVIRONMENT=development
```

For a remote server, use the IPv4 address reachable by clients and leave
`GATEWAY_ENVIRONMENT` unset.

### 4. Start the server

Windows PowerShell and Linux use the same Docker commands.

Validate the configuration and start MySQL:

```text
docker compose config --quiet
docker compose up --build -d db
docker compose ps
docker compose logs --tail=200 db
```

Wait until MySQL is healthy, then start Gateway and Agent:

```text
docker compose up --build -d
docker compose ps
docker compose logs --tail=200 agent-server gateway-server
```

A successful start leaves `db`, `gateway-server`, and `agent-server` running.
The first Agent world load can take time and use more than 5 GiB of memory.

If a service exits:

```text
docker compose logs --tail=200 <service-name>
```

### 5. Apply migrations when required

Normal server startup never changes the schema. Back up the database and run
one migration target at a time during maintenance:

```text
docker compose --profile maintenance up --build migrate-account
docker compose --profile maintenance up --build migrate-shard
```

Check the migration container's exit status. These commands may remain attached
because MySQL is a long-running dependency.

Database bootstrap runs only when `MYSQL_DATA_VOLUME` is empty. Changing the SQL
backup or password does not reinitialize an existing volume. Never delete a
database volume as a routine restart or upgrade step.

## Daily commands

```text
# Start or update
docker compose up --build -d

# Check status
docker compose ps

# Follow logs
docker compose logs --follow agent-server gateway-server

# Stop without deleting database data
docker compose stop

# Start existing containers
docker compose start

# Remove containers without deleting the named database volume
docker compose down
```

## Developer verification

AegisCore requires exactly Go `1.26.5`.

Windows:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
```

The script checks formatting, runs `go vet ./...`, and runs `go test ./...`.
Use `-Race` for the selected Linux-container race suite; it requires Docker
Desktop.

## Documentation

| Need | Guide |
| --- | --- |
| Gateway configuration | [Gateway](gateway-server/README.md) |
| Agent configuration and world loading | [Agent](agent-server/README.md) |
| Database operations | [Database](db/README.md) |
| Apply or create migrations | [Migration command](cmd/migrate/README.md) and [migration rules](db/migrations/README.md) |
| Verify or import reference data | [`sro-data`](cmd/sro-data/README.md) |
| Optional launcher patching | [DownloadServer](download-server/README.md) and [patch administration](cmd/patch-admin/README.md) |
| Optional management UI | [SMC Server](smc-server/README.md) |
| Shared Go packages | [Framework](framework/README.md) |
| Protocol evidence | [Capture guide](protocol/captures/README.md) and [parity manifest](protocol/parity/README.md) |

## License

See [LICENSE](LICENSE). The original project was created by Ferdoran and is
maintained here as AegisCore.
