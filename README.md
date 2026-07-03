# Logging File

File-backed structured logging provider with JSONL output, log rotation, and dynamic level control.

## Key Features

- JSONL (JSON Lines) log format with timestamps
- Automatic log rotation with configurable max size and backup count
- Dynamic log level adjustment at runtime via gRPC
- Optional stdout mirroring
- Sensitive field redaction

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LOG_FILE_PATH` | `/var/lib/logging-file/module.log` | Log file path |
| `LOG_GRPC_ADDR` | `:9620` | gRPC listen address |
| `LOG_STDOUT` | `false` | Mirror logs to stdout |
| `LOG_LEVEL` | `info` | Minimum log level |

## Capability

`logging.file` — File-backed structured logging

## Dependencies

- `github.com/Muxcore-Media/core` — MuxCore SDK
