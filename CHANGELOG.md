# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0/).

## [Unreleased]

## [0.1.0] — 2026-08-09

### Added

- JSONL file logger with size-based rotation and sensitive-field redaction.
- Env-configurable rotation: `LOG_MAX_SIZE_MB` (default 100), `LOG_MAX_BACKUPS` (default 3).
- gRPC `Log` / `SetLevel`; capabilities `logging` / `logging.file`.
