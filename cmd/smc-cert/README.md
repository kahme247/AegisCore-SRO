# SMC development certificates

This maintenance command creates a dedicated local CA, Agent management server
identity, and SMC client identity. It refuses to overwrite any existing file.
Generated files belong under ignored `.local/` directories and must never be
committed or reused as production credentials.

From `go-sro/`:

```powershell
go run ./cmd/smc-cert --agent-dir .local/smc-agent-tls --smc-dir .local/smc-tls --server-name agent-server
```

The Agent directory receives `agent-management-cert.pem`,
`agent-management-key.pem` and `smc-ca.pem`. The SMC directory receives
`smc-cert.pem`, `smc-key.pem` and `agent-management-ca.pem`. The generated CA
is dedicated to the SMC management channel and is separate from Gateway
admission TLS.

Set `AGENT_MANAGEMENT_TLS_SERVER_NAME=agent-server`, enable both management
flags (`AGENT_MANAGEMENT_ENABLED=1` and `SMC_AGENT_MANAGEMENT_ENABLED=1`), and
recreate Agent and SMC Server during a maintenance window. The command is for
local development only; it does not rotate or inspect existing credentials.
