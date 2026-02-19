# Changelog

All notable changes to this project are tracked here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

## [0.2.0] - Unreleased

### Added
- Redis-backed idempotent enqueue path (`idempotency_key`) in queue service and Redis storage.
- Containerized integration tests for `internal/queue/service.go` and `internal/storage/redis/store.go` using `testcontainers-go` and a real Redis container.

### Changed
- Standardized configuration loading: `flag` + Viper environment variables + file/default fallback via `internal/conf.LoadWithOptions`.
- `cmd/server` and `cmd/worker` startup paths now use `-config`/`-config-dir` options instead of hardcoded relative fallbacks.
- `internal/conf` now provides explicit defaults to keep startup behavior deterministic when config files are missing.

### Deprecated
- Manual script-based lifecycle verification in `scripts/test_lifecycle` and `scripts/test_submit`.

### Notes
- `v0.2.0` is the active release target and baseline-hardening milestone.
- Run `git-chglog --next-tag v0.2.0` to preview commit grouping before cutting the release branch.

## [0.1.0] - 2026-01-08

### Added
- Redis Sorted Set `JobStore` with Lua-based atomic pop for delayed-task scheduling.
- `DelayQueueService` gRPC contract with initial `cmd/server` and `cmd/worker` reference flow.
- Watchdog recovery loop for visibility timeout handling.

## [0.0.1] - 2026-01-07

### Build
- Fixed Dockerfile and aligned structure with coding standards.

### Chore
- Initialized project skeleton and development environment.

### Feat
- Implemented Redis storage with Lua script.
- Defined API contract and storage interface.

### Fix
- Hardcoded Redis image version in CI configuration to stabilize CI.
