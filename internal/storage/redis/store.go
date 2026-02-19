// Package redis 提供了基于 Redis 数据结构的 JobStore 接口实现。
// 核心设计：利用 Redis ZSet 结构实现延时优先级队列，并结合 Lua 脚本保障消费原子性。
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

// Store 实现了 storage.JobStore 接口，作为任务持久化的 Redis 适配器。
// @ThreadSafe: redis.Client 本身并发安全，Store 实例支持多协程共用。
type Store struct {
	client           *redis.Client // Redis 官方 Golang 客户端
	pendingKey       string        // 待处理任务的 Key 名称（业务隔离前缀）ZSet: ddq tasks
	runningKey       string        // 正在处理任务的 Key 名称（业务隔离前缀）Hash: ddq running
	dlqKey           string        // 死信队列 Key 名称（业务隔离前缀）List: ddq dlq
	idempotencyPrefix string       // 幂等性键前缀：ddq:idempotency:
}

// GetClient 返回底层的 Redis 客户端实例。
// @Warning: 仅用于测试脚本直接操作 Redis，生产代码应通过 JobStore 接口。
func (s *Store) GetClient() *redis.Client {
	return s.client
}

// 编译期校验：确保 Store 结构体完整实现了 JobStore 定义的所有契约。
var _ storage.JobStore = (*Store)(nil)

// NewStore 初始化并返回 Redis 存储实例。
// @Param addr: 格式为 "host:port" 的 Redis 连接地址。
func NewStore(addr string) *Store {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &Store{
		client:           rdb,
		pendingKey:       "ddq:tasks",       // Default namespace
		runningKey:       "ddq:running",     // Default namespace
		dlqKey:           "ddq:dlq",
		idempotencyPrefix: "ddq:idempotency:", // Idempotency key prefix
	}
}

// Add 将延时任务持久化至 Redis。
// @Algorithm: 基于 ZSet(Sorted Set) 实现，Score 为任务预定的执行 Unix 时间戳。
// @Complexity: O(log(N))，N 为队列中待处理任务的总数。
func (s *Store) Add(ctx context.Context, task *pb.Task) error {
	return s.AddWithIdempotency(ctx, task, "")
}

// AddWithIdempotency 将延时任务持久化至 Redis，支持幂等性。
// @Description 如果提供 idempotencyKey，则相同 key 的请求只会创建一次任务。
// @Param idempotencyKey: 幂等性键，空字符串表示不启用幂等性。
// @Returns: 如果是幂等请求且任务已存在，返回已有任务的 ID（通过修改 task.Id）
func (s *Store) AddWithIdempotency(ctx context.Context, task *pb.Task, idempotencyKey string) error {
	// 1. 序列化：使用标准 JSON 格式。
	// @Note: 追求性能时可替换为 Protobuf 二进制序列化以减少 Redis 内存占用。
	bytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	// 2. 使用 Lua 脚本执行幂等性入队
	const idempotencyTTL = 86400 // 24 小时
	
	result, err := s.client.Eval(ctx, luaEnqueueWithIdempotency,
		[]string{s.pendingKey, s.idempotencyPrefix},  // KEYS
		string(bytes), task.ExecuteTime, task.Id, idempotencyKey, idempotencyTTL, // ARGV
	).Result()

	if err != nil {
		return fmt.Errorf("redis eval enqueue failed: %w", err)
	}

	// 3. 获取返回的 task_id（可能是新创建的，也可能是已存在的）
	returnedID, ok := result.(string)
	if !ok {
		return fmt.Errorf("unexpected result type from lua script: %T", result)
	}

	// 4. 如果是幂等请求且任务已存在，更新 task.Id 为已存在的 ID
	if idempotencyKey != "" && returnedID != task.Id {
		task.Id = returnedID
	}

	return nil
}

// FetchAndHold 批量获取并从队列中弹出已到期的待执行任务。
// @Description 利用 Lua 脚本实现“查询+删除”的原子语义，确保在分布式水平扩展时，同一任务仅被下发一次。
// @Return: 返回解析成功的任务列表。若解析失败，将跳过损坏条目并继续处理，保障队列可用性。
func (s *Store) FetchAndHold(ctx context.Context, topic string, limit int64) ([]*pb.Task, error) {
	now := time.Now().Unix()

	// 1. 调用 Lua 脚本进行原子弹出。
	// @Optimization: MVP 版本暂不支持按 Topic 分片，全局共用一个 ZSet。
	val, err := s.client.Eval(ctx, luaFetchAndHold,
		[]string{s.pendingKey, s.runningKey},
		now, limit, now).Result()
	if err != nil {
		if err == redis.Nil {
			return []*pb.Task{}, nil
		}
		return nil, fmt.Errorf("redis eval failed: %w", err)
	}

	// 2. 返回值解析与反序列化。
	// Redis Lua 返回的是 interface{} 类型的 Slice。
	rawTasks, ok := val.([]interface{})
	if !ok {
		return []*pb.Task{}, nil
	}

	tasks := make([]*pb.Task, 0, len(rawTasks))
	for _, item := range rawTasks {
		str, ok := item.(string)
		if !ok {
			continue // 数据污染防御：跳过非字符串成员
		}

		var task pb.Task
		if err := json.Unmarshal([]byte(str), &task); err != nil {
			// @Security: 记录反序列化失败，防止单个异常数据造成整体消费阻塞（Poison Pill）。
			continue
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// Remove 根据任务 ID 删除任务（支持删除 Pending 和 Running 状态的任务）。
// @Description 使用 Lua 脚本保证原子性，会从 Pending ZSet 和 Running Hash 中查找并删除匹配的任务。
// @Complexity O(N) where N is the number of pending tasks (due to ZRANGE scan)
// @Optimization 未来可以引入 Hash 索引 (ddq:meta:{id}) 来实现 O(1) 删除
func (s *Store) Remove(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, luaDelete,
		[]string{s.pendingKey, s.runningKey},
		id,
	).Result()

	if err != nil {
		return fmt.Errorf("delete task failed: %w", err)
	}

	// 检查是否真的删除了任务
	deleted, ok := result.(int64)
	if !ok || deleted == 0 {
		return fmt.Errorf("task not found: %s", id)
	}

	return nil
}

// Ack 实现
func (s *Store) Ack(ctx context.Context, id string) error {
	// 简单直接：从 Hash 中删除即可
	return s.client.Eval(ctx, luaAck, []string{s.runningKey}, id).Err()
}

// Nack 实现
func (s *Store) Nack(ctx context.Context, task *pb.Task) error {
	// 1. 更新重试计数
	task.RetryCount++

	isDead := 0
	// 2. 检查是否超过最大重试次数
	if task.RetryCount >= task.MaxRetries {
		isDead = 1
	}

	// 3. 序列化更新后的 Task
	bytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task failed: %w", err)
	}

	// 4. 计算下次重试时间 (这里简单策略：立即重试，即 Now。工业级可以做指数退避)
	retryTime := time.Now().Unix()

	// 5. 执行 Lua
	err = s.client.Eval(ctx, luaNack,
		[]string{s.runningKey, s.pendingKey, s.dlqKey}, // KEYS
		task.Id, bytes, retryTime, isDead, // ARGV
	).Err()

	if err != nil {
		return fmt.Errorf("nack failed: %w", err)
	}
	return nil
}

// CheckAndMoveExpired 实现接口
func (s *Store) CheckAndMoveExpired(ctx context.Context, visibilityTimeout int64, maxRetries int32) error {
	now := time.Now().Unix()

	err := s.client.Eval(ctx, luaRecover,
		[]string{s.runningKey, s.pendingKey, s.dlqKey}, // KEYS
		now, visibilityTimeout, maxRetries, // ARGV
	).Err()

	if err != nil {
		return fmt.Errorf("recover failed: %w", err)
	}
	return nil
}
