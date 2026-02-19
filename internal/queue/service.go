// Package queue 实现了延迟队列的核心业务逻辑。
// 适用场景：处理 gRPC 请求，参数校验，并将任务调度下发至存储层。
package queue

import (
	"context"
	"fmt"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/common/errno"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service 延迟队列 gRPC 服务实现。
// @Description 充当业务网关（Gateway），负责输入校验、ID 生成、任务规整，最后通过 JobStore 接口实现持久化。
type Service struct {
	pb.UnimplementedDelayQueueServiceServer
	store storage.JobStore // 任务持久化后端实现
}

// NewService 创建延迟队列服务实例。
// @Param store: 任务存取引擎的实现，通常为 Redis 实现。
func NewService(store storage.JobStore) *Service {
	return &Service{
		store: store,
	}
}

// Enqueue 处理任务提交请求（入队）。
// @Complexity: O(log(N))，取决于存储实现。
// @Return: 成功则返回任务分配的唯一 ID；失败则返回 gRPC 错误码。
func (s *Service) Enqueue(ctx context.Context, req *pb.EnqueueRequest) (*pb.EnqueueResponse, error) {
	// 1. 参数校验。
	// @Validation: 检查 Topic、Payload 是否为空，延时时间是否合法。
	if req.Topic == "" || req.Payload == "" {
		return nil, status.Error(codes.InvalidArgument, errno.ErrInvalidParam.Message)
	}
	if req.DelaySeconds < 0 {
		return nil, status.Error(codes.InvalidArgument, "delay_seconds must be >= 0")
	}

	// 2. 身份标识分配。
	// @Note: 优先使用客户端传入的 ID 以支持幂等提交，否则由系统自动生成 UUID。
	taskID := req.Id
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// 3. 策略初始化。
	// @Default: 若未指定最大重试次数，则赋予系统预设默认值（3次）。
	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	// 4. 构造任务实体快照。
	task := &pb.Task{
		Id:          taskID,
		Topic:       req.Topic,
		Payload:     req.Payload,
		ExecuteTime: time.Now().Add(time.Duration(req.DelaySeconds) * time.Second).Unix(),
		RetryCount:  0,
		MaxRetries:  maxRetries,
		CreatedAt:   time.Now().Unix(),
	}

	// 5. 调用持久化层（支持幂等性）。
	// @Idempotency: 如果提供了 idempotency_key，相同 key 的请求只会创建一次任务。
	// @Note: 使用接口类型断言检查是否支持幂等性方法
	type IdempotentStore interface {
		AddWithIdempotency(ctx context.Context, task *pb.Task, idempotencyKey string) error
	}
	
	var err error
	if idempotentStore, ok := s.store.(IdempotentStore); ok && req.IdempotencyKey != "" {
		// 使用幂等性方法
		err = idempotentStore.AddWithIdempotency(ctx, task, req.IdempotencyKey)
	} else {
		// 标准方法
		err = s.store.Add(ctx, task)
	}

	if err != nil {
		return &pb.EnqueueResponse{
			Success:      false,
			ErrorMessage: "failed to store task",
		}, status.Error(codes.Internal, err.Error())
	}

	// 6. 返回结果（task.Id 可能已被幂等性逻辑修改）
	return &pb.EnqueueResponse{
		Success: true,
		Id:      task.Id,  // 返回实际的 task ID（可能是新创建的，也可能是已存在的）
	}, nil
}

// Retrieve 拉取任务（Worker 调用此接口获取待执行任务）。
// @Description 封装底层的 FetchAndHold 操作，通过 gRPC 向 Worker 提供统一的任务获取接口。
// @Complexity O(log(N)) + O(M) where M is batch_size
func (s *Service) Retrieve(ctx context.Context, req *pb.RetrieveRequest) (*pb.RetrieveResponse, error) {
	// 1. 参数校验
	if req.Topic == "" {
		req.Topic = "default" // 默认主题
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 10 // 默认批量大小
	}
	if req.BatchSize > 100 {
		req.BatchSize = 100 // 限制最大批量避免内存溢出
	}

	// 2. 调用存储层获取任务
	tasks, err := s.store.FetchAndHold(ctx, req.Topic, int64(req.BatchSize))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.RetrieveResponse{
		Tasks: tasks,
	}, nil
}

// Delete 撤销任务（取消尚未执行或正在执行的任务）。
// @Description 调用存储层的 Remove 方法，支持删除 Pending 和 Running 状态的任务。
// @UseCase 订单支付成功后取消"30分钟后自动关闭订单"的延迟任务
func (s *Service) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	// 1. 参数校验
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	// 2. 调用存储层删除
	err := s.store.Remove(ctx, req.Id)
	if err != nil {
		// 任务不存在也返回成功（幂等性）
		if err.Error() == fmt.Sprintf("task not found: %s", req.Id) {
			return &pb.DeleteResponse{Success: true}, nil
		}
		return &pb.DeleteResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.DeleteResponse{Success: true}, nil
}

// Ack 确认任务执行成功（Worker 完成任务后调用）。
// @Description 从 Running 队列中移除任务，标记为已完成。
func (s *Service) Ack(ctx context.Context, req *pb.AckRequest) (*pb.AckResponse, error) {
	// 1. 参数校验
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	// 2. 调用存储层确认
	err := s.store.Ack(ctx, req.Id)
	if err != nil {
		return &pb.AckResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.AckResponse{Success: true}, nil
}

// Nack 通知任务执行失败（Worker 执行失败后调用，触发重试逻辑）。
// @Description 根据重试次数决定重新入队或进入死信队列。
func (s *Service) Nack(ctx context.Context, req *pb.NackRequest) (*pb.NackResponse, error) {
	// 1. 参数校验
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	// 2. 构造 Task 对象（Nack 需要完整的任务信息）
	task := &pb.Task{
		Id:          req.Id,
		Topic:       req.Topic,
		Payload:     req.Payload,
		ExecuteTime: req.ExecuteTime,
		RetryCount:  req.RetryCount,
		MaxRetries:  req.MaxRetries,
		CreatedAt:   req.CreatedAt,
	}

	// 3. 调用存储层处理失败
	err := s.store.Nack(ctx, task)
	if err != nil {
		return &pb.NackResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.NackResponse{Success: true}, nil
}
