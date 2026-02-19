# Agentic Development Workflow (v0.3.0 Baseline)

## Purpose
This document defines a practical Human + AI Agent delivery model for the Async Task Platform.
The goal is faster iteration without weakening correctness, especially for distributed concurrency behavior.

## Scope
- Applies to all new features and refactors from `v0.3.0` onward.
- Primary focus: protocol evolution, scheduler/storage correctness, integration tests, and release readiness.

## Responsibility Boundary
### Human Responsibilities (must be authored by humans)
- Define interfaces and contracts:
  - `api/proto/*.proto`
  - `internal/storage/interface.go` and cross-module interfaces
- Write ADRs for architecture-impacting decisions:
  - consistency model changes
  - lock/lease semantics
  - retry and idempotency guarantees
- Define acceptance criteria before implementation:
  - functional behavior
  - failure behavior
  - concurrency/distributed invariants
- Final review gate:
  - race-safety
  - lock correctness
  - backward compatibility

### Agent Responsibilities (implementation-heavy execution)
- Implement code from approved interfaces and ADR constraints.
- Generate/complete Testcontainers integration tests (real Redis).
- Handle repetitive mapping and glue code:
  - Protobuf request/response mapping
  - validation boilerplate
  - wiring in `cmd/server` and `cmd/worker`
- Produce minimal, reviewable diffs with test evidence.

## Standard Delivery Loop (TDD with Agent)
1. Human defines skeleton and failing tests.
2. Human writes acceptance criteria in the PR/issue description.
3. Agent implements code until tests pass (`red -> green`).
4. Human reviews:
   - data races (`go test -race`)
   - distributed safety (leader lease, duplicate dispatch, crash recovery)
   - API compatibility and migration notes
5. Agent applies review feedback and updates docs/changelog.

## Required Inputs Before Agent Implementation
- Interface contract committed (or included in the same PR as the first commit).
- ADR (for non-trivial design changes).
- Failing tests that represent acceptance criteria.
- Explicit non-goals (to prevent scope creep).

## Definition of Done (DoD)
- `go test ./...` passes.
- New distributed logic has integration tests using Testcontainers.
- `go test -race` passes for touched concurrency-sensitive packages.
- Observability is updated when behavior changes:
  - counters/histograms/gauges or rationale for no metric change.
- Docs updated:
  - API contract docs
  - workflow/ADR references
  - milestone notes (`CHANGELOG.md`, `DEVELOPMENT_LOG.md`).

## Review Checklist (Human Gate)
- Are lease keys separated by responsibility (watchdog vs scheduler)?
- Is every lock protected by TTL and owner-check renewal/release?
- Is duplicate dispatch prevented under retries, restart, and split-brain windows?
- Are proto changes backward-compatible?
- Is rollback strategy clear?

## Branch and PR Pattern
- Branch naming: `feature/*`, `bugfix/*`, `release/*`, `hotfix/*`.
- Small PRs: one behavior change per PR.
- PR template must include:
  - acceptance criteria
  - test evidence
  - risk and rollback notes.

## Anti-Patterns (Forbidden)
- Agent coding before interfaces/acceptance criteria are defined.
- Merging distributed changes without integration tests.
- Using only unit mocks for Redis lock/idempotency semantics.
- Mixing protocol change + major storage migration + runtime rewiring in one PR.
