package queue

import (
	"context"
	"errors"
	"testing"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/storage/mocks"
	"go.uber.org/mock/gomock"
)

func TestEnqueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)
	svc := NewService(mockStore)

	tests := []struct {
		name    string
		req     *pb.EnqueueRequest
		mock    func()
		wantErr bool
		checkID bool // 是否检查返回的 ID
	}{
		{
			name: "Success",
			req: &pb.EnqueueRequest{
				Topic:        "test",
				Payload:      "{}",
				DelaySeconds: 10,
			},
			mock: func() {
				mockStore.EXPECT().
					Add(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			wantErr: false,
			checkID: true,
		},
		{
			name: "Success with custom ID",
			req: &pb.EnqueueRequest{
				Topic:        "test",
				Payload:      "{}",
				DelaySeconds: 10,
				Id:           "custom-id-123",
			},
			mock: func() {
				mockStore.EXPECT().
					Add(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "Success with max retries",
			req: &pb.EnqueueRequest{
				Topic:        "test",
				Payload:      "{}",
				DelaySeconds: 10,
				MaxRetries:   5,
			},
			mock: func() {
				mockStore.EXPECT().
					Add(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "Invalid Param - Empty Topic",
			req: &pb.EnqueueRequest{
				Topic:   "",
				Payload: "{}",
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Invalid Param - Empty Payload",
			req: &pb.EnqueueRequest{
				Topic:   "test",
				Payload: "",
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Invalid Param - Negative Delay",
			req: &pb.EnqueueRequest{
				Topic:        "test",
				Payload:      "{}",
				DelaySeconds: -1,
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Store Error",
			req: &pb.EnqueueRequest{
				Topic:        "test",
				Payload:      "{}",
				DelaySeconds: 10,
			},
			mock: func() {
				mockStore.EXPECT().
					Add(gomock.Any(), gomock.Any()).
					Return(errors.New("redis connection failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mock()
			resp, err := svc.Enqueue(context.Background(), tt.req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Enqueue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp != nil {
				if !resp.Success {
					t.Errorf("Enqueue() success = false, want true")
				}
				if tt.checkID && resp.Id == "" {
					t.Errorf("Enqueue() returned empty ID")
				}
			}
		})
	}
}

func TestRetrieve(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)
	svc := NewService(mockStore)

	tests := []struct {
		name      string
		req       *pb.RetrieveRequest
		mock      func()
		wantErr   bool
		wantTasks int
	}{
		{
			name: "Success - With Tasks",
			req: &pb.RetrieveRequest{
				Topic:     "test",
				BatchSize: 10,
			},
			mock: func() {
				mockStore.EXPECT().
					FetchAndHold(gomock.Any(), "test", int64(10)).
					Return([]*pb.Task{
						{Id: "task-1", Topic: "test", Payload: "{}"},
						{Id: "task-2", Topic: "test", Payload: "{}"},
					}, nil)
			},
			wantErr:   false,
			wantTasks: 2,
		},
		{
			name: "Success - No Tasks",
			req: &pb.RetrieveRequest{
				Topic:     "test",
				BatchSize: 10,
			},
			mock: func() {
				mockStore.EXPECT().
					FetchAndHold(gomock.Any(), "test", int64(10)).
					Return([]*pb.Task{}, nil)
			},
			wantErr:   false,
			wantTasks: 0,
		},
		{
			name: "Default Topic and BatchSize",
			req:  &pb.RetrieveRequest{},
			mock: func() {
				mockStore.EXPECT().
					FetchAndHold(gomock.Any(), "default", int64(10)).
					Return([]*pb.Task{}, nil)
			},
			wantErr: false,
		},
		{
			name: "BatchSize Limit",
			req: &pb.RetrieveRequest{
				Topic:     "test",
				BatchSize: 1000, // 超过限制
			},
			mock: func() {
				// 应该被限制为 100
				mockStore.EXPECT().
					FetchAndHold(gomock.Any(), "test", int64(100)).
					Return([]*pb.Task{}, nil)
			},
			wantErr: false,
		},
		{
			name: "Store Error",
			req: &pb.RetrieveRequest{
				Topic:     "test",
				BatchSize: 10,
			},
			mock: func() {
				mockStore.EXPECT().
					FetchAndHold(gomock.Any(), "test", int64(10)).
					Return(nil, errors.New("redis error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mock()
			resp, err := svc.Retrieve(context.Background(), tt.req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Retrieve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp != nil {
				if len(resp.Tasks) != tt.wantTasks {
					t.Errorf("Retrieve() tasks count = %d, want %d", len(resp.Tasks), tt.wantTasks)
				}
			}
		})
	}
}

func TestDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)
	svc := NewService(mockStore)

	tests := []struct {
		name    string
		req     *pb.DeleteRequest
		mock    func()
		wantErr bool
	}{
		{
			name: "Success",
			req: &pb.DeleteRequest{
				Id: "task-123",
			},
			mock: func() {
				mockStore.EXPECT().
					Remove(gomock.Any(), "task-123").
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "Task Not Found - Idempotent",
			req: &pb.DeleteRequest{
				Id: "task-999",
			},
			mock: func() {
				mockStore.EXPECT().
					Remove(gomock.Any(), "task-999").
					Return(errors.New("task not found: task-999"))
			},
			wantErr: false, // 幂等性：不存在也返回成功
		},
		{
			name: "Invalid Param - Empty ID",
			req: &pb.DeleteRequest{
				Id: "",
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Store Error",
			req: &pb.DeleteRequest{
				Id: "task-123",
			},
			mock: func() {
				mockStore.EXPECT().
					Remove(gomock.Any(), "task-123").
					Return(errors.New("redis connection failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mock()
			resp, err := svc.Delete(context.Background(), tt.req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp != nil {
				if !resp.Success {
					t.Errorf("Delete() success = false, want true")
				}
			}
		})
	}
}

func TestAck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)
	svc := NewService(mockStore)

	tests := []struct {
		name    string
		req     *pb.AckRequest
		mock    func()
		wantErr bool
	}{
		{
			name: "Success",
			req: &pb.AckRequest{
				Id: "task-123",
			},
			mock: func() {
				mockStore.EXPECT().
					Ack(gomock.Any(), "task-123").
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "Invalid Param - Empty ID",
			req: &pb.AckRequest{
				Id: "",
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Store Error",
			req: &pb.AckRequest{
				Id: "task-123",
			},
			mock: func() {
				mockStore.EXPECT().
					Ack(gomock.Any(), "task-123").
					Return(errors.New("redis error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mock()
			resp, err := svc.Ack(context.Background(), tt.req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Ack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp != nil {
				if !resp.Success {
					t.Errorf("Ack() success = false, want true")
				}
			}
		})
	}
}

func TestNack(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)
	svc := NewService(mockStore)

	tests := []struct {
		name    string
		req     *pb.NackRequest
		mock    func()
		wantErr bool
	}{
		{
			name: "Success",
			req: &pb.NackRequest{
				Id:          "task-123",
				Topic:       "test",
				Payload:     "{}",
				ExecuteTime: 1000,
				RetryCount:  1,
				MaxRetries:  3,
				CreatedAt:   900,
			},
			mock: func() {
				mockStore.EXPECT().
					Nack(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			wantErr: false,
		},
		{
			name: "Invalid Param - Empty ID",
			req: &pb.NackRequest{
				Id: "",
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "Store Error",
			req: &pb.NackRequest{
				Id:          "task-123",
				Topic:       "test",
				Payload:     "{}",
				ExecuteTime: 1000,
				RetryCount:  1,
				MaxRetries:  3,
			},
			mock: func() {
				mockStore.EXPECT().
					Nack(gomock.Any(), gomock.Any()).
					Return(errors.New("redis error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mock()
			resp, err := svc.Nack(context.Background(), tt.req)

			if (err != nil) != tt.wantErr {
				t.Errorf("Nack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp != nil {
				if !resp.Success {
					t.Errorf("Nack() success = false, want true")
				}
			}
		})
	}
}
