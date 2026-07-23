# Logging File

File-backed structured logging provider with JSONL output, log rotation, and dynamic level control.

## Key Features

- JSONL (JSON Lines) log format with timestamps
- Automatic log rotation (default 100 MB max size, 3 backups)
- Dynamic log level adjustment at runtime via gRPC (`SetLevel`)
- Optional stdout mirroring
- Sensitive field redaction (`contracts.SensitiveLogFieldNames`)

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LOG_FILE_PATH` | `/var/lib/logging-file/module.log` | Log file path |
| `LOG_GRPC_ADDR` | `:9620` | gRPC listen address |
| `LOG_STDOUT` | `false` | Mirror logs to stdout (`true` to enable) |
| `LOG_LEVEL` | `info` | Minimum log level (`debug`, `info`, `warn`, `error`) |
| `MUXCORE_MODULE_ID` | `logging-file` | Module identity (SDK) |
| `MUXCORE_INSECURE_DISABLE_TLS` | `false` | Disable TLS for module↔core gRPC (`true` for local dev) |

Rotation size and backup count are code defaults (`MaxSizeMB=100`, `MaxBackups=3`); there are no env vars for them.

## Capability

`logging`, `logging.file` — File-backed structured logging

## Dependencies

- `github.com/Muxcore-Media/core` — contracts, module SDK, logging proto
- `google.golang.org/grpc` — LogService gRPC server
