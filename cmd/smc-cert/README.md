# SMC development certificates

This maintenance command creates a dedicated local CA, Agent management server
identity, and SMC client identity. It refuses to overwrite any existing file.
Generated files belong under ignored `.local/` directories and must never be
committed or reused as production credentials.

From `go-sro/`:

```powershell
go run ./cmd/smc-cert --agent-dir .local/smc-agent-tls --smc-dir .local/smc-tls --server-name agent-server
```

Set `AGENT_MANAGEMENT_TLS_SERVER_NAME=agent-server`, enable both management
flags, and recreate Agent and SMC Server during a maintenance window.
