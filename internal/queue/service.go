package queue

import (
	"context"
	"strings"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/common/errno"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type idempotentStore interface {
	AddWithIdempotency(ctx context.Context, task *pb.Task, idempotencyKey string) error
}

type Service struct {
	pb.UnimplementedDelayQueueServiceServer
	store storage.JobStore
}

func NewService(store storage.JobStore) *Service {
	return &Service{store: store}
}

func (s *Service) Enqueue(ctx context.Context, req *pb.EnqueueRequest) (*pb.EnqueueResponse, error) {
	if req.Topic == "" || req.Payload == "" {
		return nil, status.Error(codes.InvalidArgument, errno.ErrInvalidParam.Message)
	}
	if req.DelaySeconds < 0 {
		return nil, status.Error(codes.InvalidArgument, "delay_seconds must be >= 0")
	}

	taskID := req.Id
	if taskID == "" {
		taskID = uuid.New().String()
	}

	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	now := time.Now()
	task := &pb.Task{
		Id:          taskID,
		Topic:       req.Topic,
		Payload:     req.Payload,
		ExecuteTime: now.Add(time.Duration(req.DelaySeconds) * time.Second).Unix(),
		RetryCount:  0,
		MaxRetries:  maxRetries,
		CreatedAt:   now.Unix(),
	}

	var err error
	if store, ok := s.store.(idempotentStore); ok {
		err = store.AddWithIdempotency(ctx, task, req.IdempotencyKey)
	} else {
		err = s.store.Add(ctx, task)
	}
	if err != nil {
		return &pb.EnqueueResponse{
			Success:      false,
			ErrorMessage: "failed to store task",
		}, status.Error(codes.Internal, err.Error())
	}

	return &pb.EnqueueResponse{
		Success: true,
		Id:      task.Id,
	}, nil
}

func (s *Service) Retrieve(ctx context.Context, req *pb.RetrieveRequest) (*pb.RetrieveResponse, error) {
	if req.Topic == "" {
		req.Topic = "default"
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 10
	}
	if req.BatchSize > 100 {
		req.BatchSize = 100
	}

	tasks, err := s.store.FetchAndHold(ctx, req.Topic, int64(req.BatchSize))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.RetrieveResponse{Tasks: tasks}, nil
}

func (s *Service) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	err := s.store.Remove(ctx, req.Id)
	if err != nil {
		// Idempotent delete behavior: missing task is considered success.
		if strings.Contains(err.Error(), "task not found") {
			return &pb.DeleteResponse{Success: true}, nil
		}
		return &pb.DeleteResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.DeleteResponse{Success: true}, nil
}

func (s *Service) Ack(ctx context.Context, req *pb.AckRequest) (*pb.AckResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	if err := s.store.Ack(ctx, req.Id); err != nil {
		return &pb.AckResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.AckResponse{Success: true}, nil
}

func (s *Service) Nack(ctx context.Context, req *pb.NackRequest) (*pb.NackResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "task ID is required")
	}

	task := &pb.Task{
		Id:          req.Id,
		Topic:       req.Topic,
		Payload:     req.Payload,
		ExecuteTime: req.ExecuteTime,
		RetryCount:  req.RetryCount,
		MaxRetries:  req.MaxRetries,
		CreatedAt:   req.CreatedAt,
	}

	if err := s.store.Nack(ctx, task); err != nil {
		return &pb.NackResponse{Success: false}, status.Error(codes.Internal, err.Error())
	}

	return &pb.NackResponse{Success: true}, nil
}
