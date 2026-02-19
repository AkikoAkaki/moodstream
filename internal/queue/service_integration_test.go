package queue

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	redisstore "github.com/AkikoAkaki/async-task-platform/internal/storage/redis"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	queueTCOnce      sync.Once
	queueTCContainer testcontainers.Container
	queueTCAddr      string
	queueTCSetupErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if queueTCContainer != nil {
		_ = queueTCContainer.Terminate(context.Background())
	}
	os.Exit(code)
}

func newIntegrationService(t *testing.T) (*Service, *redisstore.Store) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	addr := ensureQueueRedisContainer(t)
	store := redisstore.NewStore(addr)
	if err := store.GetClient().FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() {
		_ = store.GetClient().FlushDB(context.Background()).Err()
		_ = store.GetClient().Close()
	})

	return NewService(store), store
}

func ensureQueueRedisContainer(t *testing.T) string {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	queueTCOnce.Do(func() {
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
			queueTCSetupErr = fmt.Errorf("start redis container: %w", err)
			return
		}

		host, err := container.Host(ctx)
		if err != nil {
			_ = container.Terminate(ctx)
			queueTCSetupErr = fmt.Errorf("resolve redis host: %w", err)
			return
		}

		port, err := container.MappedPort(ctx, "6379/tcp")
		if err != nil {
			_ = container.Terminate(ctx)
			queueTCSetupErr = fmt.Errorf("resolve redis port: %w", err)
			return
		}

		queueTCContainer = container
		queueTCAddr = net.JoinHostPort(host, port.Port())
	})

	if queueTCSetupErr != nil {
		t.Skipf("skipping queue integration tests: %v", queueTCSetupErr)
	}

	return queueTCAddr
}

func TestServiceIntegration_EnqueueRetrieveAck(t *testing.T) {
	svc, store := newIntegrationService(t)
	ctx := context.Background()

	enqueueResp, err := svc.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "order",
		Payload:      `{"order_id":1001}`,
		DelaySeconds: 0,
	})
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}
	if !enqueueResp.Success || enqueueResp.Id == "" {
		t.Fatalf("Enqueue() invalid response: %+v", enqueueResp)
	}

	retrieveResp, err := svc.Retrieve(ctx, &pb.RetrieveRequest{
		Topic:     "order",
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Retrieve(): %v", err)
	}
	if len(retrieveResp.Tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(retrieveResp.Tasks))
	}
	if retrieveResp.Tasks[0].Id != enqueueResp.Id {
		t.Fatalf("task id = %q, want %q", retrieveResp.Tasks[0].Id, enqueueResp.Id)
	}

	ackResp, err := svc.Ack(ctx, &pb.AckRequest{Id: retrieveResp.Tasks[0].Id})
	if err != nil {
		t.Fatalf("Ack(): %v", err)
	}
	if !ackResp.Success {
		t.Fatalf("Ack() success = false")
	}

	runningCount, err := store.GetClient().HLen(ctx, "ddq:running").Result()
	if err != nil {
		t.Fatalf("HLen(ddq:running): %v", err)
	}
	if runningCount != 0 {
		t.Fatalf("running count = %d, want 0", runningCount)
	}
}

func TestServiceIntegration_Delete(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	enqueueResp, err := svc.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "cancel",
		Payload:      `{"task":"to-delete"}`,
		DelaySeconds: 60,
	})
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	deleteResp, err := svc.Delete(ctx, &pb.DeleteRequest{Id: enqueueResp.Id})
	if err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if !deleteResp.Success {
		t.Fatalf("Delete() success = false")
	}

	// Idempotent delete should still succeed.
	deleteResp, err = svc.Delete(ctx, &pb.DeleteRequest{Id: enqueueResp.Id})
	if err != nil {
		t.Fatalf("Delete(idempotent): %v", err)
	}
	if !deleteResp.Success {
		t.Fatalf("Delete(idempotent) success = false")
	}
}

func TestServiceIntegration_IdempotencyKey(t *testing.T) {
	svc, store := newIntegrationService(t)
	ctx := context.Background()

	first, err := svc.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:          "billing",
		Payload:        `{"invoice":1}`,
		DelaySeconds:   10,
		IdempotencyKey: "idem-123",
	})
	if err != nil {
		t.Fatalf("Enqueue(first): %v", err)
	}

	second, err := svc.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:          "billing",
		Payload:        `{"invoice":2}`,
		DelaySeconds:   20,
		IdempotencyKey: "idem-123",
	})
	if err != nil {
		t.Fatalf("Enqueue(second): %v", err)
	}

	if second.Id != first.Id {
		t.Fatalf("second id = %q, want %q", second.Id, first.Id)
	}

	count, err := store.GetClient().ZCard(ctx, "ddq:tasks").Result()
	if err != nil {
		t.Fatalf("ZCard(ddq:tasks): %v", err)
	}
	if count != 1 {
		t.Fatalf("pending count = %d, want 1", count)
	}
}

func TestServiceIntegration_NackRetryAndDLQ(t *testing.T) {
	svc, store := newIntegrationService(t)
	ctx := context.Background()

	resp, err := svc.Enqueue(ctx, &pb.EnqueueRequest{
		Topic:        "retry",
		Payload:      `{"job":"unstable"}`,
		DelaySeconds: 0,
		MaxRetries:   2,
	})
	if err != nil {
		t.Fatalf("Enqueue(): %v", err)
	}

	retrieved, err := svc.Retrieve(ctx, &pb.RetrieveRequest{Topic: "retry", BatchSize: 1})
	if err != nil {
		t.Fatalf("Retrieve(first): %v", err)
	}
	if len(retrieved.Tasks) != 1 {
		t.Fatalf("len(first tasks) = %d, want 1", len(retrieved.Tasks))
	}
	task := retrieved.Tasks[0]

	_, err = svc.Nack(ctx, &pb.NackRequest{
		Id:          task.Id,
		Topic:       task.Topic,
		Payload:     task.Payload,
		ExecuteTime: task.ExecuteTime,
		RetryCount:  task.RetryCount,
		MaxRetries:  task.MaxRetries,
		CreatedAt:   task.CreatedAt,
	})
	if err != nil {
		t.Fatalf("Nack(first): %v", err)
	}

	retried, err := svc.Retrieve(ctx, &pb.RetrieveRequest{Topic: "retry", BatchSize: 1})
	if err != nil {
		t.Fatalf("Retrieve(second): %v", err)
	}
	if len(retried.Tasks) != 1 {
		t.Fatalf("len(second tasks) = %d, want 1", len(retried.Tasks))
	}
	task = retried.Tasks[0]
	if task.Id != resp.Id {
		t.Fatalf("retried task id = %q, want %q", task.Id, resp.Id)
	}

	_, err = svc.Nack(ctx, &pb.NackRequest{
		Id:          task.Id,
		Topic:       task.Topic,
		Payload:     task.Payload,
		ExecuteTime: task.ExecuteTime,
		RetryCount:  task.RetryCount,
		MaxRetries:  task.MaxRetries,
		CreatedAt:   task.CreatedAt,
	})
	if err != nil {
		t.Fatalf("Nack(second): %v", err)
	}

	dlqCount, err := store.GetClient().LLen(ctx, "ddq:dlq").Result()
	if err != nil {
		t.Fatalf("LLen(ddq:dlq): %v", err)
	}
	if dlqCount != 1 {
		t.Fatalf("dlq count = %d, want 1", dlqCount)
	}
}
