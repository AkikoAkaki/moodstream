# TaskService API Reference

The Async Task Platform exposes a gRPC service for task management. This document provides the complete API reference with examples.

## Service Definition

```protobuf
service DelayQueueService {
  // Submit a delayed task for future execution
  rpc Enqueue(EnqueueRequest) returns (EnqueueResponse);
  
  // Retrieve due tasks (called by workers)
  rpc Retrieve(RetrieveRequest) returns (RetrieveResponse);
  
  // Cancel a pending or running task by ID
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  
  // Acknowledge successful task completion
  rpc Ack(AckRequest) returns (AckResponse);
  
  // Report task failure and trigger retry logic
  rpc Nack(NackRequest) returns (NackResponse);
}
```

> **Note**: The service is currently named `DelayQueueService` for backward compatibility. It will be renamed to `TaskService` in a future major version.

## Messages

### Task

The core task entity:

```protobuf
message Task {
  string id = 1;           // Unique identifier
  string topic = 2;        // Logical grouping (e.g., "order-cancel", "email-send")
  string payload = 3;      // Business data as JSON string
  int64  execute_time = 4; // Scheduled execution time (Unix timestamp)
  int32  retry_count = 5;  // Current retry attempt (0 = first attempt)
  int32  max_retries = 6;  // Maximum retries before moving to DLQ
  int64  created_at = 7;   // Task creation timestamp
}
```

### EnqueueRequest / EnqueueResponse

```protobuf
message EnqueueRequest {
  string topic = 1;           // Required: business topic
  string payload = 2;         // Required: JSON payload
  int64  delay_seconds = 3;   // Required: delay before execution (>= 0)
  string id = 4;              // Optional: client-provided ID for idempotency
  int32  max_retries = 5;     // Optional: custom retry limit (default: 3)
  string idempotency_key = 6; // Optional: key for idempotent enqueue (v0.2.0+)
}

message EnqueueResponse {
  bool   success = 1;         // Whether the task was enqueued
  string id = 2;              // Assigned task ID (may be from cache if idempotent)
  string error_message = 3;   // Error details if success=false
}
```

### RetrieveRequest / RetrieveResponse

```protobuf
message RetrieveRequest {
  string topic = 1;           // Topic to retrieve from (default: "default")
  int32  batch_size = 2;      // Maximum tasks to return (default: 10, max: 100)
}

message RetrieveResponse {
  repeated Task tasks = 1;    // List of due tasks (moved to running state)
}
```

### DeleteRequest / DeleteResponse

```protobuf
message DeleteRequest {
  string id = 1;              // Task ID to cancel
}

message DeleteResponse {
  bool success = 1;           // Whether deletion succeeded (idempotent)
}
```

### AckRequest / AckResponse

```protobuf
message AckRequest {
  string id = 1;              // Task ID to acknowledge
}

message AckResponse {
  bool success = 1;           // Whether acknowledgment succeeded
}
```

### NackRequest / NackResponse

```protobuf
message NackRequest {
  string id = 1;              // Task ID to report failure
  string topic = 2;           // Task topic
  string payload = 3;         // Task payload
  int64  execute_time = 4;    // Original execute time
  int32  retry_count = 5;     // Current retry count
  int32  max_retries = 6;     // Maximum retry limit
  int64  created_at = 7;      // Task creation time
}

message NackResponse {
  bool success = 1;           // Whether nack was processed
}
```

---

## API Examples

### Prerequisites

Install `grpcurl` for command-line testing:

```powershell
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

---

## 1. Enqueue: Submit a Delayed Task

### Basic Usage

```powershell
grpcurl -plaintext -d '{
  "topic": "order-cancel",
  "payload": "{\"order_id\": 1024, \"user_id\": 42}",
  "delay_seconds": 1800
}' localhost:9090 api.queue.DelayQueueService/Enqueue
```

**Response:**

```json
{
  "success": true,
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

### With Custom ID (for tracking)

```powershell
grpcurl -plaintext -d '{
  "topic": "order-cancel",
  "payload": "{\"order_id\": 1024}",
  "delay_seconds": 1800,
  "id": "order-1024-cancel"
}' localhost:9090 api.queue.DelayQueueService/Enqueue
```

### With Idempotency Key (v0.2.0+)

**Prevents duplicate task creation on network retries:**

```powershell
grpcurl -plaintext -d '{
  "topic": "payment-process",
  "payload": "{\"transaction_id\": \"txn-12345\"}",
  "delay_seconds": 60,
  "idempotency_key": "payment-txn-12345"
}' localhost:9090 api.queue.DelayQueueService/Enqueue
```

**If the same request is sent again (e.g., due to retry), it returns the same task ID:**

```json
{
  "success": true,
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"  
}
```

> **Note**: Idempotency keys expire after 24 hours by default.

### With Custom Retry Limit

```powershell
grpcurl -plaintext -d '{
  "topic": "critical-job",
  "payload": "{}",
  "delay_seconds": 60,
  "max_retries": 5
}' localhost:9090 api.queue.DelayQueueService/Enqueue
```

---

## 2. Retrieve: Fetch Due Tasks (Worker API)

**Fetch up to 10 tasks from the "order-cancel" topic:**

```powershell
grpcurl -plaintext -d '{
  "topic": "order-cancel",
  "batch_size": 10
}' localhost:9090 api.queue.DelayQueueService/Retrieve
```

**Response:**

```json
{
  "tasks": [
    {
      "id": "task-123",
      "topic": "order-cancel",
      "payload": "{\"order_id\": 1024}",
      "execute_time": 1738454400,
      "retry_count": 0,
      "max_retries": 3,
      "created_at": 1738452600
    }
  ]
}
```

**Fetch from default topic:**

```powershell
grpcurl -plaintext -d '{}' localhost:9090 api.queue.DelayQueueService/Retrieve
```

> **Note**: Tasks returned by `Retrieve` are moved to the "running" state. You must call `Ack` or `Nack` to complete them.

---

## 3. Delete: Cancel a Task

**Cancel a pending task:**

```powershell
grpcurl -plaintext -d '{
  "id": "order-1024-cancel"
}' localhost:9090 api.queue.DelayQueueService/Delete
```

**Response:**

```json
{
  "success": true
}
```

**Idempotent behavior:**

- If the task doesn't exist (already deleted), still returns `success: true`
- Can be safely retried without side effects

---

## 4. Ack: Acknowledge Task Completion

**After successfully processing a task, acknowledge it:**

```powershell
grpcurl -plaintext -d '{
  "id": "task-123"
}' localhost:9090 api.queue.DelayQueueService/Ack
```

**Response:**

```json
{
  "success": true
}
```

**Effect:**
- Task is removed from the "running" state
- Task will not be retried or recovered

---

## 5. Nack: Report Task Failure

**When a task fails and should be retried:**

```powershell
grpcurl -plaintext -d '{
  "id": "task-123",
  "topic": "order-cancel",
  "payload": "{\"order_id\": 1024}",
  "execute_time": 1738454400,
  "retry_count": 0,
  "max_retries": 3,
  "created_at": 1738452600
}' localhost:9090 api.queue.DelayQueueService/Nack
```

**Response:**

```json
{
  "success": true
}
```

**Effect:**
- If `retry_count < max_retries`: Task is requeued with incremented `retry_count`
- If `retry_count >= max_retries`: Task moves to Dead Letter Queue (DLQ)

---

## Error Handling

The API uses standard gRPC status codes:

| Code | Meaning | Example |
|------|---------|---------|
| `OK` | Success | Task enqueued |
| `INVALID_ARGUMENT` | Bad input | Empty topic, negative delay, empty ID |
| `NOT_FOUND` | Resource missing | (Reserved for future use) |
| `INTERNAL` | Server error | Redis connection failed |

**Example error response:**

```json
{
  "code": 3,
  "message": "topic and payload are required",
  "details": []
}
```

---

## Validation Rules

| Field | Rule | Default |
|-------|------|---------|
| `topic` | Required, non-empty ASCII string | - |
| `payload` | Required, valid JSON string | - |
| `delay_seconds` | Required, must be >= 0 | - |
| `batch_size` | Optional, capped at 100 | 10 |
| `id` | Optional, must be unique | Auto-generated UUID |
| `idempotency_key` | Optional, max 24h TTL | - |

---

## Complete Workflow Example

### Producer (Task Creation)

```go
import (
    pb "github.com/AkikoAkaki/moodstream/api/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func submitTask() {
    conn, _ := grpc.NewClient("localhost:9090", 
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    defer conn.Close()
    
    client := pb.NewDelayQueueServiceClient(conn)
    
    // Submit with idempotency
    resp, err := client.Enqueue(context.Background(), &pb.EnqueueRequest{
        Topic:          "order-cancel",
        Payload:        `{"order_id": 1024}`,
        DelaySeconds:   1800,
        IdempotencyKey: "order-1024-cancel",  // Prevents duplicates
    })
    
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Task ID: %s\n", resp.Id)
}
```

### Worker (Task Consumption)

```go
func processTask() {
    conn, _ := grpc.NewClient("localhost:9090",
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    defer conn.Close()
    
    client := pb.NewDelayQueueServiceClient(conn)
    
    // Retrieve tasks
    resp, _ := client.Retrieve(context.Background(), &pb.RetrieveRequest{
        Topic:     "order-cancel",
        BatchSize: 10,
    })
    
    for _, task := range resp.Tasks {
        // Process task
        err := processOrderCancel(task.Payload)
        
        if err != nil {
            // Report failure (triggers retry)
            client.Nack(context.Background(), &pb.NackRequest{
                Id:          task.Id,
                Topic:       task.Topic,
                Payload:     task.Payload,
                ExecuteTime: task.ExecuteTime,
                RetryCount:  task.RetryCount,
                MaxRetries:  task.MaxRetries,
                CreatedAt:   task.CreatedAt,
            })
        } else {
            // Acknowledge success
            client.Ack(context.Background(), &pb.AckRequest{
                Id: task.Id,
            })
        }
    }
}
```

---

## Code Generation

After modifying `api/proto/queue.proto`, regenerate Go code:

```powershell
make proto
```

This requires:
- `protoc` (Protocol Buffer compiler)
- `protoc-gen-go` (Go code generator)
- `protoc-gen-go-grpc` (gRPC code generator)

---

## API Evolution & Versioning

### Version History

| Version | Date | Changes |
|---------|------|---------|
| v0.1.0 | 2026-01-11 | Initial release with Enqueue |
| v0.2.0 | 2026-02-01 | Added Delete, Retrieve, Ack, Nack, Idempotency |

### Backward Compatibility

- Follow semantic versioning for proto package via git tags
- Additive fields are backward compatible
- Breaking changes require bumping major version or new service name
- Use `reserved` declarations when removing fields

### Deprecation Policy

- Deprecated fields marked with `[deprecated=true]` for 6 months
- Remove only after major version bump
- Migration guide provided in release notes

---

## Performance Characteristics

| Operation | Latency (p50) | Latency (p99) | Throughput |
|-----------|---------------|---------------|------------|
| Enqueue | ~2ms | ~5ms | 10,000/s |
| Retrieve | ~3ms | ~8ms | 5,000/s |
| Delete | ~2ms | ~6ms | 8,000/s |
| Ack | ~1ms | ~3ms | 15,000/s |
| Nack | ~3ms | ~8ms | 5,000/s |

> **Note**: Benchmarks on single Redis instance (localhost). Production performance varies.

---

## Best Practices

### 1. Idempotency Key Design

✅ **Good**:
```go
idempotencyKey := fmt.Sprintf("order-cancel-%d", orderID)
```

❌ **Bad** (includes timestamp):
```go
idempotencyKey := fmt.Sprintf("order-cancel-%d-%d", orderID, time.Now().Unix())
```

### 2. Batch Processing

Retrieve tasks in batches for better throughput:

```go
// ✅ Good: Batch processing
resp, _ := client.Retrieve(ctx, &pb.RetrieveRequest{BatchSize: 50})

// ❌ Bad: One at a time
resp, _ := client.Retrieve(ctx, &pb.RetrieveRequest{BatchSize: 1})
```

### 3. Error Handling

Always handle both network errors and business errors:

```go
resp, err := client.Enqueue(ctx, req)
if err != nil {
    // Network/gRPC error
    log.Printf("gRPC error: %v", err)
    return
}
if !resp.Success {
    // Business error
    log.Printf("Enqueue failed: %s", resp.ErrorMessage)
    return
}
```

---

## References

- [gRPC Documentation](https://grpc.io/docs/)
- [Protocol Buffers Guide](https://protobuf.dev/)
- [Project Architecture ADR](../adr/001-architecture-and-storage.md)
