package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	client            *redis.Client
	pendingKey        string
	runningKey        string
	dlqKey            string
	idempotencyPrefix string
	watchdogLeaderKey string
}

func (s *Store) GetClient() *redis.Client {
	return s.client
}

var _ storage.JobStore = (*Store)(nil)
var _ storage.WatchdogLeaderElector = (*Store)(nil)

func NewStore(addr string) *Store {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &Store{
		client:            rdb,
		pendingKey:        "ddq:tasks",
		runningKey:        "ddq:running",
		dlqKey:            "ddq:dlq",
		idempotencyPrefix: "ddq:idempotency:",
		watchdogLeaderKey: "ddq:watchdog:leader",
	}
}

func (s *Store) Add(ctx context.Context, task *pb.Task) error {
	return s.AddWithIdempotency(ctx, task, "")
}

func (s *Store) AddWithIdempotency(ctx context.Context, task *pb.Task, idempotencyKey string) error {
	bytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	const idempotencyTTL = 86400
	result, err := s.client.Eval(
		ctx,
		luaEnqueueWithIdempotency,
		[]string{s.pendingKey, s.idempotencyPrefix},
		string(bytes), task.ExecuteTime, task.Id, idempotencyKey, idempotencyTTL,
	).Result()
	if err != nil {
		return fmt.Errorf("redis eval enqueue failed: %w", err)
	}

	returnedID, ok := result.(string)
	if !ok {
		return fmt.Errorf("unexpected result type from lua script: %T", result)
	}

	if idempotencyKey != "" && returnedID != task.Id {
		task.Id = returnedID
	}

	return nil
}

func (s *Store) FetchAndHold(ctx context.Context, topic string, limit int64) ([]*pb.Task, error) {
	now := time.Now().Unix()

	val, err := s.client.Eval(
		ctx,
		luaFetchAndHold,
		[]string{s.pendingKey, s.runningKey},
		now, limit, now,
	).Result()
	if err != nil {
		if err == redis.Nil {
			return []*pb.Task{}, nil
		}
		return nil, fmt.Errorf("redis eval failed: %w", err)
	}

	rawTasks, ok := val.([]interface{})
	if !ok {
		return []*pb.Task{}, nil
	}

	tasks := make([]*pb.Task, 0, len(rawTasks))
	for _, item := range rawTasks {
		raw, ok := item.(string)
		if !ok {
			continue
		}

		var task pb.Task
		if err := json.Unmarshal([]byte(raw), &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

func (s *Store) Remove(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, luaDelete, []string{s.pendingKey, s.runningKey}, id).Result()
	if err != nil {
		return fmt.Errorf("delete task failed: %w", err)
	}

	deleted, ok := result.(int64)
	if !ok || deleted == 0 {
		return fmt.Errorf("task not found: %s", id)
	}

	return nil
}

func (s *Store) Ack(ctx context.Context, id string) error {
	return s.client.Eval(ctx, luaAck, []string{s.runningKey}, id).Err()
}

func (s *Store) Nack(ctx context.Context, task *pb.Task) error {
	task.RetryCount++

	isDead := 0
	if task.RetryCount >= task.MaxRetries {
		isDead = 1
	}

	bytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task failed: %w", err)
	}

	retryTime := time.Now().Unix()
	if err := s.client.Eval(
		ctx,
		luaNack,
		[]string{s.runningKey, s.pendingKey, s.dlqKey},
		task.Id, bytes, retryTime, isDead,
	).Err(); err != nil {
		return fmt.Errorf("nack failed: %w", err)
	}

	return nil
}

func (s *Store) CheckAndMoveExpired(ctx context.Context, visibilityTimeout int64, maxRetries int32) error {
	now := time.Now().Unix()
	if err := s.client.Eval(
		ctx,
		luaRecover,
		[]string{s.runningKey, s.pendingKey, s.dlqKey},
		now, visibilityTimeout, maxRetries,
	).Err(); err != nil {
		return fmt.Errorf("recover failed: %w", err)
	}

	return nil
}

const luaRefreshWatchdogLeaderLease = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`

// TryAcquireWatchdogLeader acquires leadership using SETNX+TTL and renews the lease for the same owner.
func (s *Store) TryAcquireWatchdogLeader(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	if owner == "" {
		return false, fmt.Errorf("owner is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}

	acquired, err := s.client.SetNX(ctx, s.watchdogLeaderKey, owner, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire watchdog leader lock: %w", err)
	}
	if acquired {
		return true, nil
	}

	ttlMS := ttl.Milliseconds()
	if ttlMS <= 0 {
		ttlMS = 1
	}

	renewed, err := s.client.Eval(
		ctx,
		luaRefreshWatchdogLeaderLease,
		[]string{s.watchdogLeaderKey},
		owner,
		ttlMS,
	).Int64()
	if err != nil {
		return false, fmt.Errorf("refresh watchdog leader lease: %w", err)
	}

	return renewed == 1, nil
}
