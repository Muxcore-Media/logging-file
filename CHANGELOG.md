# Changelog

## [0.1.2] — 2026-08-10

### Added

- Advertise `settings` capability so admin-ui discovers SettingsProvider without ListAll probing.

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0/).

## [Unreleased]

## [0.1.3] - 2026-10-05

### Changed
- CI runs on GitHub-hosted runners from the umbrella template; retired-origin workflows removed.
- Dependencies resolve from published GitHub tags (no filesystem `replace`); requires core v0.6.0.

## [0.1.1] — 2026-08-10

### Added

- `RegisterSettings` / `SettingsUpdater` for live `log_path`, `level`, `stdout`, `max_size_mb`, `max_backups`
- Pin `core` / contracts / `sdk/go/module` to **v0.5.2**

## [0.1.0] — 2026-08-09

### Added

- JSONL file logger with size-based rotation and sensitive-field redaction.
- Env-configurable rotation: `LOG_MAX_SIZE_MB` (default 100), `LOG_MAX_BACKUPS` (default 3).
- gRPC `Log` / `SetLevel`; capabilities `logging` / `logging.file`.
