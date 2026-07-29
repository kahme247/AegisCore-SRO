# GO-SRO Gateway server

The Gateway server is the vSRO 1.88 entry point. It serves the configured client
profile, notices and shard list, validates credentials and admission policy,
then installs a one-time ticket on Agent before returning a successful handoff.

## Implemented behavior

- patch/profile response from one explicit `vsro188` profile;
- DB-backed notices and shard list plus shard ping;
- normalized credential lookup;
- admission checks against account state, shard state and capacity;
- bounded login-attempt tracking;
- one-time Agent tickets installed over mutual TLS;
- success response only after Agent acknowledges the ticket.

The Gateway intentionally does not guess Content ID, module name or version.
`protocol/profiles/vsro188.json` remains `evidence-pending`, so provide values
from the exact client build being tested:

```text
GATEWAY_PROTOCOL_PROFILE=vsro188
GATEWAY_PROTOCOL_CONTENT_ID=<captured value>
GATEWAY_PROTOCOL_MODULE_NAME=<captured value>
GATEWAY_PROTOCOL_VERSION=<captured value>
AGENT_PUBLIC_IP=<externally reachable IPv4>
```

Loopback Agent addresses are accepted only with the explicit local override
`GATEWAY_ENVIRONMENT=development`.

## Gateway-to-Agent mTLS

The private endpoint defaults to Agent port `15883` and is not published by
Compose. Gateway needs a client certificate/key and the Agent CA. Agent needs a
server certificate/key and the Gateway client CA. The Agent certificate DNS SAN
must match `AGENT_ADMISSION_TLS_SERVER_NAME`.

The repository's Compose file mounts separate read-only TLS directories through
`SRO_GATEWAY_TLS_DIR` and `SRO_AGENT_TLS_DIR`. Missing files, an invalid chain,
a wrong server name or an unavailable Agent listener causes startup/admission to
fail closed; there is no shared-secret fallback.

## Known gaps

- canonical client executable hash, file version, locale and Content ID are not
  recorded;
- patch and login massive-message behavior needs sanitized `0x600D` fixtures;
- live capacity, maintenance and ban-policy operations need broader smoke tests;
- account activation/email verification is not implemented;
- public deployment still needs an operator-owned patch service and TLS/network
  perimeter.

## Verification

```powershell
go test ./gateway-server/...
docker compose up -d --build --force-recreate gateway-server
docker compose logs --tail=200 gateway-server
```

See the root [data/evidence rules](../README.md#evidence-and-references) and
[capture policy](../protocol/captures/README.md).

## License

The original project acknowledgement to DaxterSoul and the repository
[LICENSE](../LICENSE) are retained.
