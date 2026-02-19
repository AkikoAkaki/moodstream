package scheduler

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AkikoAkaki/async-task-platform/internal/conf"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
)

// Watchdog periodically checks and recovers expired running tasks.
type Watchdog struct {
	store    storage.JobStore
	interval time.Duration
	timeout  int64
	maxRetry int32

	quit chan struct{}
	wg   sync.WaitGroup

	stopOnce sync.Once
	stopped  atomic.Bool
}

func NewWatchdog(cfg conf.QueueConfig, store storage.JobStore) *Watchdog {
	maxRetries := cfg.MaxRetries
	if maxRetries > 2147483647 {
		maxRetries = 2147483647
	}

	visibilityTimeout := cfg.VisibilityTimeout
	if visibilityTimeout < 0 {
		visibilityTimeout = 60
	}

	return &Watchdog{
		store:    store,
		interval: time.Duration(cfg.WatchdogInterval) * time.Second,
		timeout:  int64(visibilityTimeout),
		maxRetry: int32(maxRetries),
		quit:     make(chan struct{}),
	}
}

func (w *Watchdog) Start() {
	if w.stopped.Load() {
		// Do not restart after Stop() has been finalized.
		return
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		log.Printf("Watchdog started. Interval: %v, Timeout: %ds, MaxRetries: %d", w.interval, w.timeout, w.maxRetry)

		for {
			select {
			case <-w.quit:
				return
			case <-ticker.C:
				w.recover()
			}
		}
	}()
}

// Stop is idempotent and safe to call multiple times.
func (w *Watchdog) Stop() {
	w.stopOnce.Do(func() {
		w.stopped.Store(true)
		close(w.quit)
		w.wg.Wait()
		log.Println("Watchdog stopped")
	})
}

func (w *Watchdog) recover() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.store.CheckAndMoveExpired(ctx, w.timeout, w.maxRetry); err != nil {
		log.Printf("Watchdog recover error: %v", err)
	}
}
