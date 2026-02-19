# Architecture Decision Records (ADR)

This directory contains Architecture Decision Records for the Async Task Platform.

## What is an ADR?

An Architecture Decision Record (ADR) captures an important architectural decision made along with its context and consequences.

## ADR Index

| # | Title | Date | Status |
|---|-------|------|--------|
| [001](001-architecture-and-storage.md) | Architecture and Storage Selection | 2026-01-04 | ✅ Accepted |
| [002](002-gitflow-and-versioning.md) | GitFlow and Versioning Strategy | 2026-01-11 | ✅ Accepted |
| [003](003-worker-grpc-client-pattern.md) | Worker Architecture: gRPC Client Pattern | 2026-02-01 | ✅ Accepted |
| [004](004-idempotency-implementation.md) | Idempotency Support for Task Enqueue | 2026-02-01 | ✅ Accepted |

## ADR Lifecycle

```
Proposed → Accepted → Deprecated → Superseded
```

- **Proposed**: Under discussion
- **Accepted**: Currently in use
- **Deprecated**: Still in use but being phased out
- **Superseded**: Replaced by a newer ADR

## Creating a New ADR

1. Copy the [template](TEMPLATE.md) (if exists, otherwise use existing ADR as reference)
2. Number it sequentially (e.g., `005-my-decision.md`)
3. Fill in all sections:
   - **Context**: What problem are we solving?
   - **Decision**: What did we decide to do?
   - **Consequences**: What are the trade-offs?
   - **Alternatives**: What else did we consider?
4. Submit for review via Pull Request
5. Update this index

## Key Decisions Summary

### System Architecture (ADR-001)
- **Language**: Go
- **Protocol**: gRPC with Protobuf
- **Storage**: Redis (Sorted Set for time-based scheduling)
- **Rationale**: Performance, simplicity, operational maturity

### Version Control (ADR-002)
- **Branching**: GitFlow (main/develop/feature/release)
- **Versioning**: Semantic Versioning (MAJOR.MINOR.PATCH)
- **Rationale**: Industry standard, clear release process

### Worker Communication (ADR-003)
- **Pattern**: gRPC Client (Workers → Server → Redis)
- **Key Decision**: Workers no longer directly access Redis
- **Rationale**: Separation of concerns, centralized control, testability

### Idempotency (ADR-004)
- **Mechanism**: Server-side deduplication using Redis
- **TTL**: 24 hours for idempotency keys
- **Rationale**: Prevents duplicate tasks from network retries

## Cross-References

### Related to Storage
- ADR-001: Primary storage decision
- ADR-004: Idempotency storage strategy

### Related to API Design
- ADR-001: gRPC protocol selection
- ADR-003: Worker API design
- ADR-004: Idempotency key design

### Related to System Evolution
- ADR-002: How we version changes
- ADR-003: How we migrate workers

## Questions?

For questions about these decisions, see:
- [Technical Design Doc](../ARCHITECTURE.md)
- [API Reference](../API.md)
- [Contributing Guide](../../CONTRIBUTING.md)
