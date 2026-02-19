package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/storage/mocks"
	"go.uber.org/mock/gomock"
)

func TestNewWatchdog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)

	tests := []struct {
		name        string
		cfg         conf.QueueConfig
		wantTimeout int64
		wantRetries int32
	}{
		{
			name: "Normal Config",
			cfg: conf.QueueConfig{
				VisibilityTimeout: 120,
				WatchdogInterval:  30,
				MaxRetries:        5,
			},
			wantTimeout: 120,
			wantRetries: 5,
		},
		{
			name: "Negative Timeout - Use Default",
			cfg: conf.QueueConfig{
				VisibilityTimeout: -1,
				WatchdogInterval:  30,
				MaxRetries:        3,
			},
			wantTimeout: 60, // 默认值
			wantRetries: 3,
		},
		{
			name: "Zero Values",
			cfg: conf.QueueConfig{
				VisibilityTimeout: 0,
				WatchdogInterval:  0,
				MaxRetries:        0,
			},
			wantTimeout: 0,
			wantRetries: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watchdog := NewWatchdog(tt.cfg, mockStore)

			if watchdog == nil {
				t.Fatal("NewWatchdog() returned nil")
			}

			if watchdog.timeout != tt.wantTimeout {
				t.Errorf("timeout = %d, want %d", watchdog.timeout, tt.wantTimeout)
			}

			if watchdog.maxRetry != tt.wantRetries {
				t.Errorf("maxRetry = %d, want %d", watchdog.maxRetry, tt.wantRetries)
			}

			if watchdog.quit == nil {
				t.Error("quit channel is nil")
			}
		})
	}
}

func TestWatchdog_StartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)

	// 不期望调用 CheckAndMoveExpired（因为我们会立即停止）
	// 但为了容错，允许调用
	mockStore.EXPECT().
		CheckAndMoveExpired(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	cfg := conf.QueueConfig{
		VisibilityTimeout: 60,
		WatchdogInterval:  1, // 1 秒间隔
		MaxRetries:        3,
	}

	watchdog := NewWatchdog(cfg, mockStore)

	// 启动
	watchdog.Start()

	// 等待一小段时间确保 goroutine 启动
	time.Sleep(100 * time.Millisecond)

	// 停止
	done := make(chan bool)
	go func() {
		watchdog.Stop()
		done <- true
	}()

	// 确保在合理时间内停止
	select {
	case <-done:
		// 成功停止
	case <-time.After(2 * time.Second):
		t.Fatal("Watchdog.Stop() timeout")
	}
}

func TestWatchdog_RecoverCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)

	// 设置期望：CheckAndMoveExpired 至少被调用一次
	called := make(chan bool, 1)
	mockStore.EXPECT().
		CheckAndMoveExpired(gomock.Any(), int64(60), int32(3)).
		DoAndReturn(func(ctx context.Context, timeout int64, maxRetries int32) error {
			select {
			case called <- true:
			default:
			}
			return nil
		}).
		MinTimes(1)

	cfg := conf.QueueConfig{
		VisibilityTimeout: 60,
		WatchdogInterval:  1, // 1 秒间隔，快速触发
		MaxRetries:        3,
	}

	watchdog := NewWatchdog(cfg, mockStore)
	watchdog.Start()
	defer watchdog.Stop()

	// 等待至少一次调用
	select {
	case <-called:
		// 成功：CheckAndMoveExpired 被调用
	case <-time.After(3 * time.Second):
		t.Fatal("CheckAndMoveExpired was not called within timeout")
	}
}

func TestWatchdog_RecoverError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)

	// 模拟存储层错误
	mockStore.EXPECT().
		CheckAndMoveExpired(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("redis connection failed")).
		AnyTimes()

	cfg := conf.QueueConfig{
		VisibilityTimeout: 60,
		WatchdogInterval:  1,
		MaxRetries:        3,
	}

	watchdog := NewWatchdog(cfg, mockStore)
	watchdog.Start()

	// 等待一段时间让 watchdog 运行
	time.Sleep(500 * time.Millisecond)

	// Watchdog 应该能够容错继续运行
	watchdog.Stop()
	// 如果能正常停止，说明即使有错误也不会 panic
}

func TestWatchdog_MultipleStartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStore := mocks.NewMockJobStore(ctrl)

	mockStore.EXPECT().
		CheckAndMoveExpired(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()

	cfg := conf.QueueConfig{
		VisibilityTimeout: 60,
		WatchdogInterval:  1,
		MaxRetries:        3,
	}

	watchdog := NewWatchdog(cfg, mockStore)

	// 多次启动/停止
	for i := 0; i < 3; i++ {
		watchdog.Start()
		time.Sleep(200 * time.Millisecond)
		watchdog.Stop()
	}

	// 如果没有 panic 或死锁，测试通过
}
