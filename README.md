# Logging File

File-backed structured logging provider with JSONL output, log rotation, and dynamic level control.

## Key Features

- JSONL (JSON Lines) log format with timestamps
- Automatic log rotation (size + backup count; env-configurable)
- Dynamic log level adjustment at runtime via gRPC (`SetLevel`)
- Optional stdout mirroring
- Sensitive field redaction (`contracts.SensitiveLogFieldNames`)
- `pkg/client` StructuredLogger for mesh modules dialing LogService

## MVP stack

Enable the sidecar in the local/vault MVP stack:

```bash
# _mvp/.env
MVP_ENABLE_LOGGING_FILE=1
```

`run-host.sh` writes to `$RUN/logging-file.log` when enabled.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LOG_FILE_PATH` | `/var/lib/logging-file/module.log` | Log file path |
| `LOG_ALLOWED_ROOT` | parent of `LOG_FILE_PATH` | Allowed directory for runtime `log_path` changes |
| `LOG_GRPC_ADDR` | `127.0.0.1:9625` | gRPC listen address |
| `LOG_STDOUT` | `false` | Mirror logs to stdout (`true`, `1`, `yes`, or `on`) |
| `LOG_LEVEL` | `info` | Minimum log level (`debug`, `info`, `warn`, `error`) |
| `LOG_FLUSH_MS` | `250` | Background buffer flush interval (ms); each log also flushes |
| `LOG_MAX_SIZE_MB` | `100` | Rotate when active file reaches this size (MiB) |
| `LOG_MAX_BACKUPS` | `3` | Number of rotated files to keep (`module.log.1` … `.N`) |
| `LOG_MODULE_TOKEN` | — | Bearer token accepted on LogService when mesh identity is absent |
| `MUXCORE_MODULE_TOKEN` | — | Alias for `LOG_MODULE_TOKEN` |
| `MUXCORE_MODULE_ID` | `logging-file` | Module identity (SDK) |
| `MUXCORE_INSECURE_DISABLE_TLS` | `false` | Disable TLS for module↔core gRPC (`true` for local dev) |

Invalid or non-positive `LOG_MAX_SIZE_MB`, `LOG_MAX_BACKUPS`, or `LOG_FLUSH_MS` values log a warning and keep defaults.

### Admin settings keys

`log_path`, `level`, `stdout`, `flush_ms`, `max_size_mb`, `max_backups` (mirrors env vars above).

## Capability

`logging`, `logging.file` — File-backed structured logging

## Dependencies

- `github.com/Muxcore-Media/core` — contracts, module SDK, logging proto
- `google.golang.org/grpc` — LogService gRPC server

## Docker

The image defaults `LOG_FILE_PATH=/tmp/logging-file/module.log` (writable by distroless `nonroot`). Mount a volume and override for persistence.
