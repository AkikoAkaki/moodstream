package stream

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/ai"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
)

// Aggregator implements a wall-clock tumbling window that periodically
// drains all accumulated events from Redis, sends them to the LLM, and
// broadcasts the resulting WindowResult via the Broadcaster.
type Aggregator struct {
	store       storage.EventStore
	ai          *ai.Client
	broadcaster *Broadcaster
	batcher     *Batcher

	windowSize   time.Duration
	maxBatchSize int

	done chan struct{}
	wg   sync.WaitGroup
}

// AggregatorConfig holds the settings for the aggregator.
type AggregatorConfig struct {
	Store        storage.EventStore
	AI           *ai.Client
	Broadcaster  *Broadcaster
	Batcher      *Batcher
	WindowSize   time.Duration
	MaxBatchSize int
}

// NewAggregator creates an aggregator from the given config.
func NewAggregator(cfg AggregatorConfig) *Aggregator {
	return &Aggregator{
		store:        cfg.Store,
		ai:           cfg.AI,
		broadcaster:  cfg.Broadcaster,
		batcher:      cfg.Batcher,
		windowSize:   cfg.WindowSize,
		maxBatchSize: cfg.MaxBatchSize,
		done:         make(chan struct{}),
	}
}

// Start begins the tumbling window loop.
func (a *Aggregator) Start() {
	a.wg.Add(1)
	go a.loop()
}

// Stop gracefully shuts down the aggregator.
func (a *Aggregator) Stop() {
	close(a.done)
	a.wg.Wait()
}

func (a *Aggregator) loop() {
	defer a.wg.Done()
	ticker := time.NewTicker(a.windowSize)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.tick()
		case <-a.done:
			return
		}
	}
}

func (a *Aggregator) tick() {
	videoIDs := a.batcher.ActiveVideoIDs()
	for _, vid := range videoIDs {
		a.processVideo(vid)
	}
}

func (a *Aggregator) processVideo(videoID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Drain all events regardless of video timestamp.
	events, err := a.store.FetchWindow(ctx, videoID, 0, math.MaxInt64)
	if err != nil {
		log.Printf("aggregator: fetch failed for %q: %v", videoID, err)
		return
	}
	if len(events) == 0 {
		return
	}

	// Cap batch size for LLM.
	if a.maxBatchSize > 0 && len(events) > a.maxBatchSize {
		events = events[:a.maxBatchSize]
	}

	// Compute window bounds from event timestamps.
	var minTs, maxTs int64
	minTs = math.MaxInt64
	for _, e := range events {
		if e.TimestampMs < minTs {
			minTs = e.TimestampMs
		}
		if e.TimestampMs > maxTs {
			maxTs = e.TimestampMs
		}
	}

	// Count total events (accounting for repeat_count).
	var totalCount int32
	for _, e := range events {
		c := e.RepeatCount
		if c <= 0 {
			c = 1
		}
		totalCount += c
	}

	result, err := a.ai.Analyze(ctx, events)
	if err != nil {
		log.Printf("aggregator: LLM analysis failed for %q: %v", videoID, err)
		return
	}

	wr := &pb.WindowResult{
		VideoId:     videoID,
		WindowStart: minTs,
		WindowEnd:   maxTs,
		EmotionTag:  result.EmotionTag,
		CoreTopic:   result.CoreTopic,
		EventCount:  totalCount,
		ProcessedAt: time.Now().UnixMilli(),
	}

	a.broadcaster.Publish(wr)
	log.Printf("aggregator: %q window [%d–%d] → %s / %s (%d events)",
		videoID, minTs, maxTs, result.EmotionTag, result.CoreTopic, totalCount)
}
