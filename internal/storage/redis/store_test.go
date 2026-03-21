package redis

import (
	"context"
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

// testStore returns a Store with a per-test namespace so each test owns
// its own key prefix. Cleanup deletes only those keys, not the whole DB.
func testStore(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}
	store := NewStore(ensureRedisContainer(t))
	store.namespace = strings.ToLower(
		strings.NewReplacer("/", "_", " ", "_", "-", "_").Replace(t.Name()),
	)
	t.Cleanup(func() {
		ctx := context.Background()
		pattern := "stream:" + store.namespace + ":*"
		keys, err := store.client.Keys(ctx, pattern).Result()
		if err == nil && len(keys) > 0 {
			store.client.Del(ctx, keys...)
		}
		if err := store.client.Close(); err != nil {
			t.Logf("store cleanup close error: %v", err)
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
				storeTCSetupErr = fmt.Errorf("terminate container after host error: %w", termErr)
			} else {
				storeTCSetupErr = fmt.Errorf("resolve redis host: %w", err)
			}
			return
		}

		port, err := container.MappedPort(ctx, "6379/tcp")
		if err != nil {
			if termErr := container.Terminate(ctx); termErr != nil {
				storeTCSetupErr = fmt.Errorf("terminate container after port error: %w", termErr)
			} else {
				storeTCSetupErr = fmt.Errorf("resolve redis port: %w", err)
			}
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

func TestStoreIntegration_PushAndFetchWindow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	videoID := "v1"

	events := []*pb.InteractionEvent{
		{VideoId: videoID, TimestampMs: 1000, RawText: "hello"},
		{VideoId: videoID, TimestampMs: 3000, RawText: "world"},
		{VideoId: videoID, TimestampMs: 6000, RawText: "outside window"},
	}
	for _, e := range events {
		if err := store.PushEvent(ctx, videoID, e); err != nil {
			t.Fatalf("PushEvent(%d): %v", e.TimestampMs, err)
		}
	}

	got, err := store.FetchWindow(ctx, videoID, 0, 5000)
	if err != nil {
		t.Fatalf("FetchWindow: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("FetchWindow returned %d events, want 2", len(got))
	}

	// Verify atomic delete: second fetch of same range returns empty.
	got2, err := store.FetchWindow(ctx, videoID, 0, 5000)
	if err != nil {
		t.Fatalf("FetchWindow (second): %v", err)
	}
	if len(got2) != 0 {
		t.Fatalf("expected empty second fetch, got %d events", len(got2))
	}

	// Event outside window still present.
	got3, err := store.FetchWindow(ctx, videoID, 6000, 6000)
	if err != nil {
		t.Fatalf("FetchWindow (outside window): %v", err)
	}
	if len(got3) != 1 {
		t.Fatalf("expected 1 outside-window event, got %d", len(got3))
	}
}

func TestStoreIntegration_EmptyWindow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	got, err := store.FetchWindow(ctx, "no-such-video", 0, 9999)
	if err != nil {
		t.Fatalf("FetchWindow on empty key: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 events, got %d", len(got))
	}
}
