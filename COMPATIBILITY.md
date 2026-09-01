# Compatibility

## Core Version

| Module Version | Core Version | Status |
|----------------|-------------|--------|
| v0.1.2         | v0.5.8+     | Current |

## Contracts

| Contract | Capability | Status |
|----------|-----------|--------|
| StructuredLogger / LogService | `logging`, `logging.file` | Current |

Rotation: `LOG_MAX_SIZE_MB` (default 100), `LOG_MAX_BACKUPS` (default 3). Buffer flush: `LOG_FLUSH_MS` (default 250).

## Breaking Changes

This is a pre-1.0 module. Interfaces may change without notice.
