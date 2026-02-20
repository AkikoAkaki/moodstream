package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	storeTCOnce      sync.Once
	storeTCContainer testcontainers.Container
	storeTCAddr      string
	storeTCSetupErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if storeTCContainer != nil {
		if err := storeTCContainer.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "terminate redis test container: %v\n", err)
		}
	}
	os.Exit(code)
}

func testStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	addr := ensureRedisContainer(t)

	store := NewStore(addr)
	prefix := testKeyPrefix(t.Name())
	store.pendingKey = prefix + ":tasks"
	store.runningKey = prefix + ":running"
	store.dlqKey = prefix + ":dlq"
	store.idempotencyPrefix = prefix + ":idempotency:"
	store.taskIDPrefix = prefix + ":taskid:"
	store.watchdogLeaderKey = prefix + ":watchdog:leader"

	ctx := context.Background()
	if err := store.client.Del(ctx, store.pendingKey, store.runningKey, store.dlqKey, store.watchdogLeaderKey).Err(); err != nil {
		t.Fatalf("cleanup before test: %v", err)
	}

	t.Cleanup(func() {
		if err := store.client.Del(context.Background(), store.pendingKey, store.runningKey, store.dlqKey, store.watchdogLeaderKey).Err(); err != nil {
			t.Logf("cleanup redis keys: %v", err)
		}
		if err := store.client.Close(); err != nil {
			t.Logf("close redis client: %v", err)
		}
	})

	return store
}

func ensureRedisContainer(t *testing.T) string {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	storeTCOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7.2-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(90 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			storeTCSetupErr = fmt.Errorf("start redis container: %w", err)
			return
		}

		host, err := container.Host(ctx)
		if err != nil {
			if termErr := container.Terminate(ctx); termErr != nil {
				storeTCSetupErr = fmt.Errorf("resolve redis host: %w; terminate container: %v", err, termErr)
				return
			}
			storeTCSetupErr = fmt.Errorf("resolve redis host: %w", err)
			return
		}

		port, err := container.MappedPort(ctx, "6379/tcp")
		if err != nil {
			if termErr := container.Terminate(ctx); termErr != nil {
				storeTCSetupErr = fmt.Errorf("resolve redis port: %w; terminate container: %v", err, termErr)
				return
			}
			storeTCSetupErr = fmt.Errorf("resolve redis port: %w", err)
			return
		}

		storeTCContainer = container
		storeTCAddr = net.JoinHostPort(host, port.Port())
	})

	if storeTCSetupErr != nil {
		t.Skipf("skipping redis integration tests: %v", storeTCSetupErr)
	}

	return storeTCAddr
}

func testKeyPrefix(name string) string {
	s := strings.ToLower(name)
	replacer := strings.NewReplacer("/", "_", " ", "_", "-", "_")
	return "it:" + replacer.Replace(s)
}

func newTask(id, topic, payload string, executeAt time.Time, maxRetries int32) *pb.Task {
	now := time.Now().Unix()
	return &pb.Task{
		Id:          id,
		Topic:       topic,
		Payload:     payload,
		ExecuteTime: executeAt.Unix(),
		RetryCount:  0,
		MaxRetries:  maxRetries,
		CreatedAt:   now,
	}
}

func TestStoreIntegration_AddFetchAndHold(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	ready := newTask("ready-task", "email", `{"type":"ready"}`, time.Now().Add(-2*time.Second), 3)
	future := newTask("future-task", "email", `{"type":"future"}`, time.Now().Add(2*time.Minute), 3)

	if err := store.Add(ctx, ready); err != nil {
		t.Fatalf("Add(ready): %v", err)
	}
	if err := store.Add(ctx, future); err != nil {
		t.Fatalf("Add(future): %v", err)
	}

	tasks, err := store.FetchAndHold(ctx, "email", 10)
	if err != nil {
		t.Fatalf("FetchAndHold(): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].Id != "ready-task" {
		t.Fatalf("task id = %q, want ready-task", tasks[0].Id)
	}

	pendingCount, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	runningCount, err := store.client.HLen(ctx, store.runningKey).Result()
	if err != nil {
		t.Fatalf("HLen(running): %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending count = %d, want 1", pendingCount)
	}
	if runningCount != 1 {
		t.Fatalf("running count = %d, want 1", runningCount)
	}
}

func TestStoreIntegration_AddWithIdempotency(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	first := newTask("task-a", "order", `{"order_id":1}`, time.Now().Add(time.Second), 3)
	if err := store.AddWithIdempotency(ctx, first, "idem-key-1"); err != nil {
		t.Fatalf("AddWithIdempotency(first): %v", err)
	}
	firstID := first.Id

	second := newTask("task-b", "order", `{"order_id":2}`, time.Now().Add(2*time.Second), 3)
	if err := store.AddWithIdempotency(ctx, second, "idem-key-1"); err != nil {
		t.Fatalf("AddWithIdempotency(second): %v", err)
	}

	if second.Id != firstID {
		t.Fatalf("second task id = %q, want %q", second.Id, firstID)
	}

	count, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	if count != 1 {
		t.Fatalf("pending count = %d, want 1", count)
	}
}

func TestStoreIntegration_AddWithCustomIDDedup(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	first := newTask("custom-task-id", "order", `{"v":1}`, time.Now().Add(time.Second), 3)
	second := newTask("custom-task-id", "order", `{"v":2}`, time.Now().Add(2*time.Second), 3)

	if err := store.Add(ctx, first); err != nil {
		t.Fatalf("Add(first): %v", err)
	}
	if err := store.Add(ctx, second); err != nil {
		t.Fatalf("Add(second duplicate id): %v", err)
	}

	count, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	if count != 1 {
		t.Fatalf("pending count = %d, want 1 for duplicate custom id", count)
	}
}

func TestStoreIntegration_Remove(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	task := newTask("task-delete", "cleanup", `{}`, time.Now().Add(3*time.Minute), 3)
	if err := store.Add(ctx, task); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	if err := store.Remove(ctx, task.Id); err != nil {
		t.Fatalf("Remove(existing): %v", err)
	}

	if err := store.Remove(ctx, task.Id); err == nil {
		t.Fatalf("Remove(missing) expected error")
	}
}

func TestStoreIntegration_RemoveFromRunning(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	task := newTask("task-running-delete", "cleanup", `{}`, time.Now().Add(-time.Second), 3)
	if err := store.Add(ctx, task); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	tasks, err := store.FetchAndHold(ctx, "cleanup", 1)
	if err != nil {
		t.Fatalf("FetchAndHold(): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}

	if err := store.Remove(ctx, task.Id); err != nil {
		t.Fatalf("Remove(running): %v", err)
	}

	runningCount, err := store.client.HLen(ctx, store.runningKey).Result()
	if err != nil {
		t.Fatalf("HLen(running): %v", err)
	}
	if runningCount != 0 {
		t.Fatalf("running count = %d, want 0", runningCount)
	}

	pendingCount, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending count = %d, want 0", pendingCount)
	}
}

func TestStoreIntegration_Ack(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	task := newTask("task-ack", "worker", `{}`, time.Now().Add(-time.Second), 3)
	if err := store.Add(ctx, task); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	tasks, err := store.FetchAndHold(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("FetchAndHold(): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}

	if err := store.Ack(ctx, task.Id); err != nil {
		t.Fatalf("Ack(): %v", err)
	}

	runningCount, err := store.client.HLen(ctx, store.runningKey).Result()
	if err != nil {
		t.Fatalf("HLen(running): %v", err)
	}
	if runningCount != 0 {
		t.Fatalf("running count = %d, want 0", runningCount)
	}
}

func TestStoreIntegration_NackRetryAndDLQ(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	task := newTask("task-nack", "worker", `{}`, time.Now().Add(-time.Second), 2)
	if err := store.Add(ctx, task); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	tasks, err := store.FetchAndHold(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("FetchAndHold(first): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}

	// First Nack requeues the task.
	if err := store.Nack(ctx, tasks[0]); err != nil {
		t.Fatalf("Nack(first): %v", err)
	}
	pendingCount, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending count after first nack = %d, want 1", pendingCount)
	}

	tasks, err = store.FetchAndHold(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("FetchAndHold(second): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) second = %d, want 1", len(tasks))
	}

	// Second Nack reaches max retries and moves task to DLQ.
	if err := store.Nack(ctx, tasks[0]); err != nil {
		t.Fatalf("Nack(second): %v", err)
	}
	dlqCount, err := store.client.LLen(ctx, store.dlqKey).Result()
	if err != nil {
		t.Fatalf("LLen(dlq): %v", err)
	}
	if dlqCount != 1 {
		t.Fatalf("dlq count = %d, want 1", dlqCount)
	}
}

func TestStoreIntegration_CheckAndMoveExpired(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	task := newTask("task-expired", "worker", `{}`, time.Now().Add(-time.Second), 3)
	if err := store.Add(ctx, task); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	tasks, err := store.FetchAndHold(ctx, "worker", 1)
	if err != nil {
		t.Fatalf("FetchAndHold(): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}

	if err := store.CheckAndMoveExpired(ctx, 0, 3); err != nil {
		t.Fatalf("CheckAndMoveExpired(): %v", err)
	}

	runningCount, err := store.client.HLen(ctx, store.runningKey).Result()
	if err != nil {
		t.Fatalf("HLen(running): %v", err)
	}
	if runningCount != 0 {
		t.Fatalf("running count = %d, want 0", runningCount)
	}

	pendingCount, err := store.client.ZCard(ctx, store.pendingKey).Result()
	if err != nil {
		t.Fatalf("ZCard(pending): %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending count = %d, want 1", pendingCount)
	}

	raw, err := store.client.ZRange(ctx, store.pendingKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange(pending): %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("len(raw) = %d, want 1", len(raw))
	}

	var recovered pb.Task
	if err := json.Unmarshal([]byte(raw[0]), &recovered); err != nil {
		t.Fatalf("unmarshal recovered task: %v", err)
	}
	if recovered.Id != "task-expired" {
		t.Fatalf("recovered id = %q, want task-expired", recovered.Id)
	}
}

func TestStoreIntegration_WatchdogLeaderElection(t *testing.T) {
	storeA := testStore(t)
	storeB := NewStore(ensureRedisContainer(t))
	storeB.watchdogLeaderKey = storeA.watchdogLeaderKey
	t.Cleanup(func() {
		if err := storeB.client.Del(context.Background(), storeB.watchdogLeaderKey).Err(); err != nil {
			t.Logf("cleanup watchdog key: %v", err)
		}
		if err := storeB.client.Close(); err != nil {
			t.Logf("close redis client B: %v", err)
		}
	})

	ctx := context.Background()
	ttl := 400 * time.Millisecond

	isLeader, err := storeA.TryAcquireWatchdogLeader(ctx, "node-a", ttl)
	if err != nil {
		t.Fatalf("node-a acquire leader error: %v", err)
	}
	if !isLeader {
		t.Fatalf("node-a should acquire leader lock")
	}

	isLeader, err = storeB.TryAcquireWatchdogLeader(ctx, "node-b", ttl)
	if err != nil {
		t.Fatalf("node-b acquire leader error: %v", err)
	}
	if isLeader {
		t.Fatalf("node-b should not acquire while node-a owns lock")
	}

	// Current owner can renew lease.
	time.Sleep(100 * time.Millisecond)
	isLeader, err = storeA.TryAcquireWatchdogLeader(ctx, "node-a", ttl)
	if err != nil {
		t.Fatalf("node-a renew leader error: %v", err)
	}
	if !isLeader {
		t.Fatalf("node-a should renew leader lock")
	}

	// Simulate owner crash by not renewing further; lock should auto-expire by TTL.
	time.Sleep(ttl + 100*time.Millisecond)
	isLeader, err = storeB.TryAcquireWatchdogLeader(ctx, "node-b", ttl)
	if err != nil {
		t.Fatalf("node-b acquire after ttl error: %v", err)
	}
	if !isLeader {
		t.Fatalf("node-b should acquire after TTL expiration")
	}
}
