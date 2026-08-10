# AegisCore Gateway server

Gateway is the public vSRO 1.188 or 1.274 entry point. It handles patch negotiation,
notices, shard listing, login policy and one-time handoff to Agent. Runtime
compatibility values come from explicit configuration; Gateway does not load or
transmit the client profile JSON.

## Protocol surface

After the Silkroad core handshake and module identification, Gateway admits:

| Opcode | Request |
| --- | --- |
| `0x6100` | Patch/version negotiation |
| `0x6101` | Shard list |
| `0x6102` | Login |
| `0x6104` | Notices |
| `0x6106` | Shard ping |

Patch negotiation compares one configured Content ID/module/version tuple.
Exact clients receive massive `0xA100 [1]`; unknown or newer clients receive
`[2,1]`; older clients receive `[2,3]`. No patch manifest or patch service is
implemented.

Notices and shards are read from `SRO_ACCOUNT` for each request. Notices are
capped at 255. Shards are filtered by the configured Content ID, validated and
sorted by ID. A one-second cached mTLS readiness probe projects listed shards as
offline while Agent is unavailable. Shard ping is configuration-backed, does
not perform readiness, and returns farm ID plus Agent IPv4 bytes without a port.

The client-visible server name comes from `SRO_ACCOUNT.SHARD.NAME` and is read
for every shard-list request, so a valid database update takes effect without a
Gateway restart:

```sql
UPDATE SRO_ACCOUNT.SHARD SET NAME = 'AegisCore' WHERE ID = 1;
```

Names must be nonblank, trimmed, valid UTF-8 and at most 32 characters. The
separate farm heading is controlled by `GATEWAY_FARM_NAME` and requires a
Gateway restart after changing it.

## Login and admission

Login performs these steps:

1. Parse Content ID, username, password and shard ID without trailing bytes.
2. Apply account/IP failed-attempt preflight using the direct TCP source IP.
3. Query the exact supplied username and verify its stored password hash.
4. Require `USER.STATUS=1` and a matching online shard with positive free
   capacity and the requested Content ID.
5. Issue a random nonzero Queue ID and bind a 45-second ticket to account,
   normalized username, shard, Content ID and source IP.
6. Install the ticket on Agent over mTLS and wait for acknowledgement.
7. Return Agent address, port and Queue ID only after successful installation.

Agent atomically rejects duplicate pending or active accounts. Tickets are
single-use, process-local and bounded to 8,192 entries; Agent restart discards
pending tickets. There is no separate ban, punishment, maintenance, activation,
email-verification or trusted-proxy policy.

Credential SQL uses the username exactly as received. Normalization applies to
rate-limit and ticket identity keys only. Patch/notice and shard-list paths
enforce the configured profile; login currently checks Content ID against the
selected DB shard rather than directly against `GATEWAY_PROTOCOL_CONTENT_ID`.

## Limits

- The traffic filter defaults to 200 validated physical frames per connection
  per one-second window, including handshake and `0x600D` wrapper frames.
- Failed-credential tracking defaults to four attempts per normalized account
  and 20 per source IP over 15 minutes, with 4,096 shared records.
- Only credential failures increment counters. Successful login clears the
  account counter but retains source-IP history.

The public `15779` listener uses legacy Silkroad encrypted framing, key exchange
and EDC; it is not TLS. TLS 1.2 or newer is used only for the private
Gateway-to-Agent admission API.

## Configuration

Compose requires these operator-supplied values:

| Variable | Purpose |
| --- | --- |
| `GATEWAY_PROTOCOL_CONTENT_ID` | Accepted runtime Content ID |
| `GATEWAY_PROTOCOL_MODULE_NAME` | Accepted patch module |
| `GATEWAY_PROTOCOL_VERSION` | Accepted patch version |
| `AGENT_PUBLIC_IP` | IPv4 advertised to clients |
| `AGENT_ADMISSION_TLS_SERVER_NAME` | Agent certificate DNS name |
| `SRO_GATEWAY_TLS_DIR` | Gateway certificate/key/CA host directory |
| `SRO_AGENT_TLS_DIR` | Agent certificate/key/CA host directory |

The Gateway TLS directory must contain `agent-ca.pem`, `gateway-cert.pem`, and
`gateway-key.pem`. The Agent directory must contain `agent-cert.pem`,
`agent-key.pem`, and `gateway-ca.pem`.

Important defaults:

| Variable | Default |
| --- | --- |
| `GATEWAY_HOST` / `GATEWAY_PORT` | `127.0.0.1` / `15779` |
| `GATEWAY_PROTOCOL_PROFILE` | `vsro188` |
| `GATEWAY_ENVIRONMENT` | `production` |
| `GATEWAY_FARM_ID` / `GATEWAY_FARM_NAME` | `1` / `AegisCore` |
| `AGENT_HOST` / `AGENT_PORT` | `127.0.0.1` / `15882` |
| `AGENT_ADMISSION_PORT` | `15883` |
| `AGENT_ADMISSION_TIMEOUT_SECONDS` | `5` |
| `GATEWAY_LOGIN_ATTEMPT_WINDOW_SECONDS` | `900` |
| `GATEWAY_LOGIN_ACCOUNT_ATTEMPT_LIMIT` | `4` |
| `GATEWAY_LOGIN_IP_ATTEMPT_LIMIT` | `20` |
| `GATEWAY_LOGIN_ATTEMPT_TRACKER_CAPACITY` | `4096` |

Loopback advertised Agent addresses require
`GATEWAY_ENVIRONMENT=development`. Gateway opens and pings both configured MySQL
pools at startup. Normal startup does not apply migrations.

To use the supplied v274 client, select `GATEWAY_PROTOCOL_PROFILE=vsro274` and
configure its patch tuple: `GATEWAY_PROTOCOL_CONTENT_ID=22`,
`GATEWAY_PROTOCOL_MODULE_NAME=SR_Client`, and `GATEWAY_PROTOCOL_VERSION=274`.
The core `0x5000`/`0x9000` handshake is shared by Gateway and Agent and does
not require a v274-specific configuration branch.

## mTLS and deployment

Admission port `15883` exposes `GET /v1/readiness` and
`POST /v1/admission-tickets` on the private Compose network. Agent requires a
Gateway client certificate chaining to its configured CA; Gateway verifies the
Agent chain and `AGENT_ADMISSION_TLS_SERVER_NAME`.

Missing or malformed local TLS material fails construction. Gateway does not
contact Agent during construction: reachability, live certificate and readiness
errors are discovered on readiness or ticket requests, then shards are projected
offline and login fails closed. There is no shared-secret fallback.

Compose publishes Gateway `15779`, Agent `15882`, MySQL `3306`, and MySQL X
on loopback; only admission `15883` stays private. The supplied topology has no
Gateway/Agent health checks and is not a complete public network perimeter.

## Known limitations

- The v188 executable identity, locale, Content ID and sanitized wire corpus
  are not checked in. The v274 executable/archive identities and seed-database
  Content ID are recorded, but its complete wire corpus is still pending.
- `vsro188.json` and `vsro274.json` are evidence metadata, not Gateway runtime
  configuration.
- Shard discovery is a DB catalog read for one Agent endpoint, not service
  registration or multi-Agent discovery.
- Shard ping does not check readiness or serialize Agent port.
- Source-IP ticket binding uses the direct peer and has no proxy-header support.
- Login does not directly enforce the configured protocol Content ID.
- Pending tickets and duplicate-account reservations are non-durable.

## Verification

Run from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify.ps1
go test ./gateway-server/... ./framework/security/auth ./framework/server `
  ./protocol/profiles ./agent-server/handler/lobby ./deployment
docker compose config --quiet
```

For a live rebuild:

```powershell
docker compose up -d --build --force-recreate agent-server gateway-server
docker compose logs --tail=200 agent-server gateway-server
Test-NetConnection 127.0.0.1 -Port 15779
Test-NetConnection 127.0.0.1 -Port 15882
```

Open ports prove listener state, not stock-client compatibility. See the
[capture policy](../protocol/captures/README.md) and [LICENSE](../LICENSE).
