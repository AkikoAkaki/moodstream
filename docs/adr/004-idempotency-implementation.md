# 4. Idempotency Support for Task Enqueue

Date: 2026-02-01  
Status: Accepted  
Supersedes: N/A

## Context

In distributed systems, network failures can cause clients to retry requests, potentially creating duplicate tasks:

**Problem Scenario:**
```
Time  Client Action                 Server State
T1    Send Enqueue(order-cancel)   Task created (ID: abc-123)
T2    Network timeout              Server sent response, client didn't receive
T3    Client retries               Task created AGAIN (ID: def-456) ❌
```

**Impact:**
- Order canceled twice
- Payment refunded twice
- Duplicate notifications sent

**Requirements:**
1. Same request should create task only once
2. Subsequent retries return original task ID
3. No client-side state management needed
4. TTL to prevent indefinite memory growth

## Decision

### Implement Server-Side Idempotency with Redis

**Mechanism:**

```
Client provides idempotency_key → Server checks Redis → Return cached ID or create new task
```

### Implementation Details

#### 1. Proto Definition

```protobuf
message EnqueueRequest {
  string idempotency_key = 6;  // Optional: key for deduplication
}
```

#### 2. Redis Data Structure

```
Key:   ddq:idempotency:{idempotency_key}
Value: task_id
TTL:   86400 seconds (24 hours)

Example:
  Key:   ddq:idempotency:payment-txn-12345
  Value: abc-123-def-456
  TTL:   86400
```

#### 3. Lua Script (Atomic Operation)

```lua
local pending_key = KEYS[1]          -- "ddq:tasks"
local idempotency_prefix = KEYS[2]   -- "ddq:idempotency:"
local task_json = ARGV[1]
local task_id = ARGV[3]
local idempotency_key = ARGV[4]
local ttl = ARGV[5]

-- Check if idempotency key exists
if idempotency_key ~= "" then
    local idempotency_redis_key = idempotency_prefix .. idempotency_key
    local existing_id = redis.call('GET', idempotency_redis_key)
    
    if existing_id then
        return existing_id  -- Return cached task ID
    end
end

-- Create new task
redis.call('ZADD', pending_key, score, task_json)

-- Save idempotency mapping
if idempotency_key ~= "" then
    redis.call('SET', idempotency_redis_key, task_id, 'EX', ttl)
end

return task_id
```

**Why Lua?**
- Atomicity: Check-and-create must be atomic
- Prevents race condition between two concurrent requests

#### 4. Service Layer Logic

```go
func (s *Service) Enqueue(ctx context.Context, req *pb.EnqueueRequest) {
    task := &pb.Task{
        Id:    uuid.New().String(),
        // ... other fields
    }
    
    // Call store with idempotency key
    if idempotentStore, ok := s.store.(IdempotentStore); ok {
        err = idempotentStore.AddWithIdempotency(ctx, task, req.IdempotencyKey)
        // task.Id may be modified to existing ID if duplicate
    }
    
    return &pb.EnqueueResponse{
        Success: true,
        Id:      task.Id,  // Returns existing ID on duplicate
    }
}
```

## Consequences

### Advantages

#### 1. **Prevents Duplicate Tasks**

```go
// Client code
resp1, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
    Topic:          "payment",
    Payload:        `{"amount": 100}`,
    IdempotencyKey: "payment-txn-12345",
})
// resp1.Id = "abc-123"

// Network retry (same idempotency key)
resp2, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
    Topic:          "payment",
    Payload:        `{"amount": 100}`,  // Even if payload differs
    IdempotencyKey: "payment-txn-12345",  // Same key
})
// resp2.Id = "abc-123"  ✅ Same ID returned
```

#### 2. **Server-Side Implementation**

- **No client-side caching** required
- **Stateless clients** can safely retry
- **Works across client instances**

#### 3. **Automatic Expiration**

- 24-hour TTL prevents Redis memory leaks
- Balances safety window vs memory usage

#### 4. **Backward Compatible**

```go
// Old code (no idempotency key) still works
client.Enqueue(ctx, &pb.EnqueueRequest{
    Topic:   "test",
    Payload: "{}",
    // idempotency_key omitted
})
```

### Disadvantages

#### 1. **Memory Overhead**

**Cost per idempotent request:**
```
Key:   ~50 bytes ("ddq:idempotency:payment-txn-12345")
Value: ~36 bytes (UUID)
Total: ~86 bytes per key
```

**At scale:**
```
1M requests/day × 86 bytes = 86 MB
10M requests/day × 86 bytes = 860 MB
```

**Mitigation**: Configurable TTL (default 24h)

#### 2. **TTL Expiration Edge Case**

```
T1: Client sends request with key "k1"
T2: Server creates task, saves idempotency mapping
T3: 24 hours pass, mapping expires
T4: Client retries with same key "k1"
T5: New task created ❌
```

**Acceptable because:**
- 24 hours is longer than typical retry window
- Edge case probability: < 0.001%
- Alternative (no TTL) causes unbounded memory growth

#### 3. **Key Design Responsibility**

**Client must choose good idempotency keys:**

✅ **Good:**
```go
// Based on business entity
idempotencyKey := fmt.Sprintf("order-cancel-%d", orderID)
idempotencyKey := fmt.Sprintf("payment-%s", transactionID)
```

❌ **Bad:**
```go
// Includes timestamp (every retry gets new key)
idempotencyKey := fmt.Sprintf("task-%d", time.Now().Unix())

// Too generic (conflicts across different operations)
idempotencyKey := fmt.Sprintf("user-%d", userID)
```

**Documentation requirement:** Provide best practices in API guide

#### 4. **Payload Mismatch Ignored**

If two requests have:
- Same idempotency key
- Different payloads

Second request returns first task, **ignoring payload difference.**

**Example:**
```go
// Request 1
Enqueue({
    IdempotencyKey: "order-1024",
    Payload: `{"status": "cancel"}`,  // ← First wins
})

// Request 2 (retry with bug, different payload)
Enqueue({
    IdempotencyKey: "order-1024",
    Payload: `{"status": "suspend"}`,  // ← Ignored!
})
```

**Rationale**: Idempotency assumes retries are identical. Payload changes indicate client bug.

### Performance Impact

| Metric | Without Idempotency | With Idempotency | Overhead |
|--------|---------------------|------------------|----------|
| **Enqueue Latency** | ~2ms | ~2.2ms | +0.2ms |
| **Redis Ops** | 1 (ZADD) | 2-3 (GET+ZADD+SET) | +1-2 |
| **Memory per Task** | ~200 bytes | ~286 bytes | +86 bytes |

**Conclusion**: Acceptable overhead for preventing duplicate tasks.

## Alternatives Considered

### Alternative 1: Client-Side Deduplication

**Implementation:**
```go
// Client maintains cache of sent task IDs
var sentTasks = make(map[string]string)  // key → taskID

func enqueue(key string, req *pb.EnqueueRequest) {
    if taskID, exists := sentTasks[key]; exists {
        return taskID  // Return cached
    }
    
    resp, _ := client.Enqueue(ctx, req)
    sentTasks[key] = resp.Id
    return resp.Id
}
```

**Pros:**
- No server-side storage overhead
- Lower latency (no Redis lookup)

**Cons:**
- **Stateful clients** (doesn't work across restarts)
- **Doesn't work in distributed systems** (Client A and Client B can't share cache)
- **Memory leak risk** (unbounded map growth)

**Rejected because:** Doesn't solve distributed retry problem

---

### Alternative 2: Database Unique Constraint

**Implementation:**
```sql
CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE,
    ...
);

INSERT INTO tasks VALUES (..., idempotency_key)
ON CONFLICT (idempotency_key) DO NOTHING;
```

**Pros:**
- Durable (survives server restart)
- No TTL issues

**Cons:**
- **Slower** (disk I/O vs Redis memory)
- **Couples design to SQL** (current system uses Redis)
- **Harder to implement TTL** (requires background job)

**Rejected because:** Contradicts Redis-first architecture

---

### Alternative 3: At-Least-Once Delivery (No Idempotency)

**Implementation:**
- Accept that retries create duplicates
- Make task handlers idempotent instead

**Example:**
```go
func handleOrderCancel(orderID string) {
    // Check if already canceled
    if order.Status == "canceled" {
        return  // Already done, skip
    }
    
    order.Cancel()
}
```

**Pros:**
- Simpler server implementation
- Moves complexity to business logic

**Cons:**
- **Every handler must be idempotent** (error-prone requirement)
- **Harder to reason about** (distributed consensus is complex)
- **Doesn't prevent duplicate DB writes** (e.g., duplicate audit logs)

**Rejected because:** Shifts burden to every task handler

## Design Decisions

### 1. TTL Choice: 24 Hours

**Rationale:**

| TTL | Pros | Cons |
|-----|------|------|
| 1 hour | Low memory | Doesn't cover long retry windows |
| 24 hours | Covers 99.9% of retries | Moderate memory |
| 7 days | Covers all retries | High memory cost |
| Forever | Perfect idempotency | Memory leak |

**Decision**: 24 hours balances safety and cost

---

### 2. Optional vs Required

**Decision**: `idempotency_key` is **optional**

**Rationale:**
- Backward compatibility with existing clients
- Not all use cases require idempotency
- Power users can opt-in

---

### 3. Key Namespace

**Decision**: Use prefix `ddq:idempotency:`

**Rationale:**
- Clearly separates from task data
- Easy to flush for testing: `DEL ddq:idempotency:*`
- Follows Redis naming conventions

## Validation

### Test Coverage

```go
func TestIdempotency(t *testing.T) {
    // First request
    resp1, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
        IdempotencyKey: "test-key-1",
        // ...
    })
    
    // Second request (same key)
    resp2, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
        IdempotencyKey: "test-key-1",
        // ...
    })
    
    assert.Equal(t, resp1.Id, resp2.Id)  // Same ID
    
    // Verify only one task in queue
    count, _ := redis.ZCard(ctx, "ddq:tasks")
    assert.Equal(t, 1, count)
}
```

### Production Metrics

**Recommended monitoring:**
```
idempotency_cache_hit_total   - Times existing ID was returned
idempotency_cache_miss_total  - Times new task was created
idempotency_key_expired_total - Times expired key was re-created
```

## Migration Guide

### For API Consumers

**Before (no idempotency):**
```go
resp, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
    Topic:   "payment",
    Payload: `{"amount": 100}`,
})
```

**After (with idempotency):**
```go
resp, _ := client.Enqueue(ctx, &pb.EnqueueRequest{
    Topic:          "payment",
    Payload:        `{"amount": 100}`,
    IdempotencyKey: fmt.Sprintf("payment-%s", transactionID),  // Add this
})
```

**Rollout strategy:**
1. Deploy server with idempotency support
2. Update client libraries with examples
3. Gradually migrate critical workflows

## References

- [Stripe Idempotency Guide](https://stripe.com/docs/api/idempotent_requests)
- [AWS S3 Conditional PUTs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/checking-object-integrity.html)
- [RFC 7231 - Safe Methods](https://httpwg.org/specs/rfc7231.html#safe.methods)

## Related ADRs

- [ADR-001](001-architecture-and-storage.md) - Architecture and Storage Selection
- [ADR-003](003-worker-grpc-client-pattern.md) - Worker gRPC Client Pattern
