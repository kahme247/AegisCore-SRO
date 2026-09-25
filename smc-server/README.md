# AegisCore SMC Server

SMC Server is the dedicated management control plane for AegisCore. It is a
separate process from Gateway and Agent and owns operator authentication,
management APIs, dashboard aggregation, and security auditing. Its embedded web
application talks only to SMC Server; browsers never connect directly to MySQL,
Gateway, Agent, or the private admission endpoint.

SMC Server is optional. Use this guide when enabling the management dashboard;
it is not required for a normal Gateway + Agent installation.

Quick check from `go-sro/` after enabling the `smc` profile:

```text
docker compose ps smc-server
docker compose logs --tail=200 smc-server
```

The current implementation keeps general database views read-only and exposes
explicitly audited operations for live controls and database-backed Media
exports. It provides:

- authenticated, expiring operator sessions;
- login rate limiting and same-origin request enforcement;
- an append-only JSONL security audit file;
- account, character, guild, item, notice, shard, and capacity totals;
- bounded account and persisted-character directory searches;
- Gateway and Agent TCP reachability probes;
- capability-gated navigation for the SMC modules;
- an audited Servers & Media interface that selects a new export directory and
  optionally limits synchronization to one mapped database table;
- `/healthz`, `/api/v1/session`, `/api/v1/dashboard`, `/api/v1/modules`,
  `/api/v1/accounts`, `/api/v1/characters`, `/api/v1/online-players`,
  `GET /api/v1/media-exports/config`, and the audited
  `POST /api/v1/media-exports` and `POST /api/v1/player-disconnects` commands.

TCP reachability means that a listener accepted a connection. It is not full
Gateway or Agent readiness. Future live controls must use separate private mTLS
admin APIs; Agent admission port `15883` must not be reused for SMC commands.
The Players screen shows both authoritative Agent presence when its private
channel is enabled and persisted character search results as a separate data
source.

Live disconnect commands require a 3..200 character operator reason and the
runtime UID from the current snapshot. SMC writes an attempt event before
contacting Agent and a terminal success/failure event before responding. Agent
rejects a stale UID so an old page cannot disconnect a later reconnection.

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `SMC_HOST` | `127.0.0.1` | HTTP bind address |
| `SMC_PORT` | `15900` | HTTP port |
| `SMC_ADMIN_USERNAME` | `admin` | Bootstrap operator name |
| `SMC_ADMIN_PASSWORD` | none | Bootstrap password, minimum 12 characters |
| `SMC_ADMIN_PASSWORD_HASH` | none | Bcrypt alternative to the plaintext bootstrap password |
| `SMC_ALLOW_WEAK_PASSWORD` | `false` | Permit a short password for isolated local development only |
| `SMC_SESSION_TTL` | `8h` | Session duration, between 5 minutes and 24 hours |
| `SMC_COOKIE_SECURE` | `false` | Require HTTPS when sending the session cookie |
| `SMC_AUDIT_PATH` | `smc-audit.jsonl` | Append-only audit path |
| `SMC_MEDIA_EXPORT_ROOT` | `media-exports` | Server-side root that contains generated Media directories |
| `SMC_MEDIA_EXPORT_DISPLAY_ROOT` | `media-exports` | Host-visible root returned to the browser after export |
| `SMC_MEDIA_EXPORT_BINARY` | `sro-data` | Path to the bundled `sro-data` maintenance executable |
| `SMC_GATEWAY_ADDRESS` | `127.0.0.1:15779` | Private Gateway probe target |
| `SMC_AGENT_ADDRESS` | `127.0.0.1:15882` | Private Agent probe target |
| `SMC_AGENT_MANAGEMENT_ENABLED` | `false` | Enable authoritative Agent player snapshots |
| `SMC_AGENT_MANAGEMENT_ENDPOINT` | `https://127.0.0.1:15884` | Private Agent management base URL |
| `SMC_AGENT_MANAGEMENT_TLS_CERT_FILE` | none | SMC client certificate |
| `SMC_AGENT_MANAGEMENT_TLS_KEY_FILE` | none | SMC client private key |
| `SMC_AGENT_MANAGEMENT_TLS_CA_FILE` | none | Agent management server CA |
| `SMC_AGENT_MANAGEMENT_TLS_SERVER_NAME` | none | Required Agent certificate DNS SAN |

Exactly one of `SMC_ADMIN_PASSWORD` and `SMC_ADMIN_PASSWORD_HASH` is required.
Passwords shorter than 12 characters additionally require
`SMC_ALLOW_WEAK_PASSWORD=1`. Never enable that override on a remote or shared
deployment.
The shared `DB_ACCOUNT_*`, `DB_SHARD_*`, and `DATABASE_*_DRIVER` settings are
also used. SMC Server never runs migrations during startup.

Live Agent state requires `AGENT_MANAGEMENT_ENABLED=1` and
`SMC_AGENT_MANAGEMENT_ENABLED=1`, a dedicated Agent server certificate, and a
dedicated SMC client certificate. Compose never publishes port `15884` to the
host. These certificates must be separate identities from Gateway admission.

For local Compose use, add a strong `SMC_ADMIN_PASSWORD` to the ignored `.env`
and start the opt-in profile:

```powershell
docker compose --profile smc up --build -d smc-server
docker compose ps smc-server
docker compose logs --tail=100 smc-server
```

Open `http://127.0.0.1:15900`. The supplied Compose binding is loopback-only;
`SMC_PUBLIC_PORT` changes the host port without changing the container port.

Compose mounts `${SRO_MEDIA_EXPORT_DIR:-./.local/media-exports}` at `/exports`.
It supplies `SMC_MEDIA_EXPORT_DISPLAY_ROOT` from the same value, so completion
and the copy button show the host-visible address instead of `/exports`. Set an
absolute `SRO_MEDIA_EXPORT_DIR` in the ignored `.env`, then recreate SMC:

```powershell
$env:SRO_MEDIA_EXPORT_DIR = "C:\Silkroad\MediaExports"
docker compose --profile smc up --build -d smc-server
```

Open **Servers & Media**, enter a new directory name, and optionally enter a
database table such as `REF_LEVEL_DATA`. An empty table exports every currently
supported snapshot catalog. SMC never overwrites an existing directory and
records attempt, success, and failure outcomes as `media.export` audit events.
For remote administration, place SMC Server behind an authenticated HTTPS/VPN
boundary and set `SMC_COOKIE_SECURE=1`; do not publish it directly to the
Internet.

## Verification

From the `go-sro/` worktree:

```powershell
go test ./smc-server/...
docker compose config --quiet
```
