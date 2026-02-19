# 3. Worker Architecture: gRPC Client Pattern

Date: 2026-02-01  
Status: Accepted  
Supersedes: N/A

## Context

In the initial implementation (v0.1.0), Workers directly accessed Redis to fetch and acknowledge tasks:

```go
// Old architecture
store := redis.NewStore(redisAddr)
tasks, _ := store.FetchAndHold(ctx, "default", 10)
store.Ack(ctx, taskID)
```

**Problems identified:**

1. **Violated Layering**: Workers bypassed the service layer, creating two paths to Redis
2. **Tight Coupling**: Workers depended on Redis implementation details
3. **No Central Control**: Couldn't enforce auth, rate limiting, or monitoring centrally
4. **Testing Difficulty**: Required Redis for worker tests instead of mocking gRPC

## Decision

### Refactor Workers to Use gRPC Client Pattern

**New architecture:**

```
Old:
Worker → Redis Store (direct)
Server → Service → Redis Store

New:
Worker → gRPC Client → Server → Service → Redis Store
                         ↑ Single point of control
```

### Implementation Details

**1. Worker as gRPC Client:**

```go
conn, _ := grpc.NewClient("localhost:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := pb.NewDelayQueueServiceClient(conn)

// Fetch tasks
resp, _ := client.Retrieve(ctx, &pb.RetrieveRequest{
    Topic:     "default",
    BatchSize: 10,
})

// Acknowledge completion
client.Ack(ctx, &pb.AckRequest{Id: task.Id})
```

**2. New gRPC Methods:**

- `Retrieve(RetrieveRequest) returns (RetrieveResponse)` - Fetch due tasks
- `Ack(AckRequest) returns (AckResponse)` - Confirm task completion
- `Nack(NackRequest) returns (NackResponse)` - Report task failure

**3. Service Layer Implementation:**

```go
func (s *Service) Retrieve(ctx context.Context, req *pb.RetrieveRequest) {
    // Validation
    if req.BatchSize > 100 {
        req.BatchSize = 100  // Prevent abuse
    }
    
    // Delegate to storage
    tasks, err := s.store.FetchAndHold(ctx, req.Topic, req.BatchSize)
    
    // Future: Add metrics, logging, auth here
    return &pb.RetrieveResponse{Tasks: tasks}, nil
}
```

### Migration Path

**Phase 1 (Completed):** Add gRPC methods alongside direct Redis access  
**Phase 2 (Completed):** Refactor Worker to use gRPC exclusively  
**Phase 3 (Future):** Add authentication and authorization

## Consequences

### Advantages

#### 1. **Separation of Concerns**
- Workers focus on task execution logic
- Server handles task lifecycle management
- Clear API boundary between components

#### 2. **Centralized Control Point**
```
Server Layer Benefits:
├─ Authentication/Authorization (future)
├─ Rate Limiting (future)
├─ Metrics & Monitoring (future)
├─ Request Validation (current)
└─ Error Handling (current)
```

#### 3. **Improved Testability**
```go
// Worker tests now use mock gRPC client
mockClient := mocks.NewMockDelayQueueServiceClient(ctrl)
mockClient.EXPECT().Retrieve(...).Return(...)
```

#### 4. **Technology Independence**
- Workers don't know about Redis
- Can switch to PostgreSQL/RocksDB without worker changes

#### 5. **Deployment Flexibility**
- Workers can run in different network zones
- Easier to scale workers independently

### Disadvantages

#### 1. **Additional Network Hop**

**Performance Impact:**

| Metric | Old (Direct Redis) | New (via gRPC) | Overhead |
|--------|-------------------|----------------|----------|
| Latency | ~1ms | ~2-3ms | +1-2ms |
| Throughput | 15k/s | 12k/s | -20% |

**Mitigation**: Acceptable overhead for improved architecture. Batch processing offsets latency.

#### 2. **Increased Complexity**

- More code to maintain (gRPC handlers)
- More potential failure points
- Requires proto definition changes for new features

#### 3. **Single Point of Failure**

- If Server crashes, workers can't fetch tasks
- **Mitigation**: HA deployment with load balancer

#### 4. **Nack API Verbosity**

Workers must pass full task object for retry logic:

```go
// Required for Nack
client.Nack(ctx, &pb.NackRequest{
    Id:          task.Id,
    Topic:       task.Topic,
    Payload:     task.Payload,
    ExecuteTime: task.ExecuteTime,
    RetryCount:  task.RetryCount,
    MaxRetries:  task.MaxRetries,
    CreatedAt:   task.CreatedAt,
})
```

**Rationale**: Avoids extra Redis lookup. Network cost < storage query cost.

### Trade-off Analysis

| Dimension | Direct Redis | gRPC Pattern | Winner |
|-----------|--------------|--------------|--------|
| **Latency** | ~1ms | ~2-3ms | Redis |
| **Maintainability** | Poor (2 paths) | Good (1 path) | gRPC |
| **Scalability** | Limited | Excellent | gRPC |
| **Security** | None | Central auth point | gRPC |
| **Observability** | Scattered | Centralized | gRPC |

**Decision**: Prioritize long-term maintainability over minimal latency difference.

## Alternatives Considered

### Alternative 1: Keep Direct Redis Access

**Pros:**
- Simplest implementation
- Lowest latency

**Cons:**
- Violates layering principles
- Difficult to add features (auth, metrics)
- Hard to test without Redis

**Rejected because:** Technical debt accumulates quickly

---

### Alternative 2: Message Queue (RabbitMQ/Kafka)

**Pros:**
- Natural fit for task distribution
- Built-in persistence and delivery guarantees

**Cons:**
- Additional infrastructure complexity
- Doesn't fit delayed execution model well
- Over-engineering for current scale

**Rejected because:** Redis ZSet already provides time-based ordering

---

### Alternative 3: HTTP REST API

**Pros:**
- Simpler than gRPC (no proto files)
- More tooling available

**Cons:**
- No type safety
- Worse performance than gRPC
- No streaming support for future features

**Rejected because:** gRPC is industry standard for inter-service communication

## Implementation Notes

### Worker Connection Management

```go
// Connection pooling handled automatically by gRPC
conn, err := grpc.NewClient(
    serverAddr,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithDefaultCallOptions(
        grpc.MaxCallRecvMsgSize(10 * 1024 * 1024), // 10MB
    ),
)
defer conn.Close()
```

### Error Handling Strategy

```go
resp, err := client.Retrieve(ctx, req)
if err != nil {
    // Network or server error
    if status.Code(err) == codes.Unavailable {
        // Server down, backoff and retry
        time.Sleep(5 * time.Second)
        continue
    }
    log.Printf("Fatal error: %v", err)
    return
}

// Process tasks...
```

### Future Enhancements

1. **Authentication** (v0.3.0)
   ```go
   conn, _ := grpc.NewClient(serverAddr,
       grpc.WithPerRPCCredentials(tokenCredential),
   )
   ```

2. **Load Balancing** (v0.4.0)
   ```go
   conn, _ := grpc.NewClient(
       "dns:///server-cluster:9090",
       grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
   )
   ```

3. **Streaming Retrieve** (v1.0.0)
   ```protobuf
   rpc StreamRetrieve(RetrieveRequest) returns (stream Task);
   ```

## Validation

### Before (Direct Redis)
```
Worker Code Size: 150 lines
Dependencies: redis-go
Test Complexity: High (requires Redis)
Deployment: Must be in same network as Redis
```

### After (gRPC Client)
```
Worker Code Size: 120 lines
Dependencies: grpc, proto
Test Complexity: Low (mock gRPC)
Deployment: Flexible (HTTP/2 over internet)
```

## References

- [gRPC Best Practices](https://grpc.io/docs/guides/performance/)
- [Microservice Patterns (Richardson)](https://microservices.io/patterns/apigateway.html)
- [PR #XX](https://github.com/AkikoAkaki/async-task-platform/pull/XX) - Worker Refactoring Implementation

## Related ADRs

- [ADR-001](001-architecture-and-storage.md) - Architecture and Storage Selection
- [ADR-004](004-idempotency-implementation.md) - Idempotency Support (TBD)
