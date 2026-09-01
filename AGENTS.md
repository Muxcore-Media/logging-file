# AGENTS.md — logging-file

MuxCore sidecar module (`logging-file`). Workspace deploy and SSH: [`../AGENTS.md`](../AGENTS.md). Default ports: [`_mvp/PORTS.md`](../_mvp/PORTS.md).

## Module identity

| Field | Value |
|-------|-------|
| Directory | `logging-file` |
| Capabilities | `logging`, `logging.file`, `settings` |
| Contracts | `StructuredLogger` (v0.4.0) |

Enable in the MVP stack with `MVP_ENABLE_LOGGING_FILE=1` in `_mvp/.env` (see `_mvp/run-host.sh`).

## Agent rules

- Modules run as gRPC sidecars; capabilities are the security boundary.
- TLS required in production (`MUXCORE_INSECURE_DISABLE_TLS` is dev-only).
- Match existing Go patterns; run `gofmt` and package tests before finishing.
- Cross-module events: prefer `github.com/Muxcore-Media/contracts-media/events` over deprecated `core/pkg/contracts` aliases.
- Do not edit polluted workspace dumps (see `MASTER-ROADMAP.md` Appendix H).

## Build

```bash
cd logging-file
nix-shell -p go --run 'go test -race ./...'
```

Remote callers use `pkg/client` (StructuredLogger over LogService gRPC).
