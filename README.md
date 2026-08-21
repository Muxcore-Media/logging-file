# Logging File

File-backed structured logging provider with JSONL output, log rotation, and dynamic level control.

## Key Features

- JSONL (JSON Lines) log format with timestamps
- Automatic log rotation (size + backup count; env-configurable)
- Dynamic log level adjustment at runtime via gRPC (`SetLevel`)
- Optional stdout mirroring
- Sensitive field redaction (`contracts.SensitiveLogFieldNames`)

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LOG_FILE_PATH` | `/var/lib/logging-file/module.log` | Log file path |
| `LOG_GRPC_ADDR` | `:9625` | gRPC listen address |
| `LOG_STDOUT` | `false` | Mirror logs to stdout (`true` to enable) |
| `LOG_LEVEL` | `info` | Minimum log level (`debug`, `info`, `warn`, `error`) |
| `LOG_MAX_SIZE_MB` | `100` | Rotate when active file reaches this size (MiB) |
| `LOG_MAX_BACKUPS` | `3` | Number of rotated files to keep (`module.log.1` … `.N`) |
| `MUXCORE_MODULE_ID` | `logging-file` | Module identity (SDK) |
| `MUXCORE_INSECURE_DISABLE_TLS` | `false` | Disable TLS for module↔core gRPC (`true` for local dev) |

Invalid or non-positive `LOG_MAX_SIZE_MB` / `LOG_MAX_BACKUPS` values are ignored (defaults apply).

## Capability

`logging`, `logging.file` — File-backed structured logging

## Dependencies

- `github.com/Muxcore-Media/core` — contracts, module SDK, logging proto
- `google.golang.org/grpc` — LogService gRPC server
