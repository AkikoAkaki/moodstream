# Development Log (AI + Human Contributors)

This log keeps execution context, release semantics, and milestone status aligned across collaborators.

## Project Snapshot

**Async Task Platform** is currently in the `v0.2.0` hardening milestone:
- Keep delayed-task API path complete and stable.
- Replace manual verification scripts with repeatable integration tests.
- Normalize config loading for all runtime entrypoints.

## Working Agreements

### Branching and Release
- Git Flow: `develop` for integration, `main` for production mirror.
- Release branches follow `release/vX.Y.Z`.
- Tags are annotated and created on `main`.

### Versioning
- Semantic Versioning (`MAJOR.MINOR.PATCH`).
- `v0.2.0` is the current release target and the only active milestone for this phase.

### Quality Gate
- Required before PR merge: `make fmt && make lint && make test`.
- Integration tests are mandatory for core path regression protection (`Enqueue -> Retrieve -> Ack/Nack`).
- Script-only validation under `scripts/` is not accepted as release evidence.

## Decision Index

| ADR | Title | Status |
|-----|-------|--------|
| [ADR-001](docs/adr/001-architecture-and-storage.md) | Redis-based MVP with gRPC surface | Accepted |
| [ADR-002](docs/adr/002-gitflow-and-versioning.md) | Git Flow adoption and SemVer policy | Accepted |
| [ADR-003](docs/adr/003-worker-grpc-client-pattern.md) | Worker gRPC client pattern | Accepted |
| [ADR-004](docs/adr/004-idempotency-implementation.md) | Idempotency implementation | Accepted |

## Milestones

| Version | Status | Focus |
|---------|--------|-------|
| v0.1.0 | Completed | Redis-backed delay queue, gRPC API, watchdog recovery |
| v0.2.0 | In Progress | Baseline hardening: docs alignment, real integration tests, config bootstrap cleanup |
| v0.3.0 | Planned | Cron scheduling, leader election, queue sharding |
| v1.0.0 | Future | Production-grade observability and HA |

## v0.2.0 Exit Criteria

- [ ] `CHANGELOG.md`, `DEVELOPMENT_LOG.md`, and `docs/CONSTITUTION.md` agree on current milestone status.
- [ ] `internal/queue/service.go` has real Redis integration tests via `testcontainers-go`.
- [ ] `internal/storage/redis/store.go` has real Redis integration tests via `testcontainers-go`.
- [ ] `cmd/server` and `cmd/worker` remove hardcoded relative config fallback and use standardized flag/env loading.

## Change Journal

| Date | Summary |
|------|---------|
| 2026-02-19 | Baseline audit initiated for v0.2.0: aligned milestone narrative, replaced script-based verification with containerized integration tests, and standardized startup configuration loading path. |
| 2026-02-01 | Project vision moved from “Distributed Delay Queue” to “Async Task Platform”; docs and ADR references updated. |
| 2026-01-08 | Git Flow + SemVer governance introduced (ADR-002), changelog scaffolding and release-readiness pipeline defined. |
