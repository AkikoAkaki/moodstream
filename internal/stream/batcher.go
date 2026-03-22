package stream

import (
	"context"
	"log"
	"sync"
	"time"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
)

// Batcher is an in-memory "high-frequency repetition word merging" interceptor
// that sits between the gRPC receiver and Redis writes. It buffers incoming
// events, merges identical raw_text entries within each flush interval, and
// writes the deduplicated batch to Redis via pipeline — preventing BigKey
// bloat and excessive network I/O on Redis's single thread.
type Batcher struct {
	store      storage.EventStore
	flushEvery time.Duration

	mu      sync.Mutex
	buffers map[string]map[string]*mergeEntry // videoID -> rawText -> entry
	seen    map[string]struct{}               // all video IDs ever submitted

	done chan struct{}
	wg   sync.WaitGroup
}

type mergeEntry struct {
	event *pb.InteractionEvent
	count int32
}

// NewBatcher creates a batcher that flushes merged events every flushEvery.
func NewBatcher(store storage.EventStore, flushEvery time.Duration) *Batcher {
	return &Batcher{
		store:      store,
		flushEvery: flushEvery,
		buffers:    make(map[string]map[string]*mergeEntry),
		seen:       make(map[string]struct{}),
		done:       make(chan struct{}),
	}
}

// Start begins the periodic flush goroutine. Call Stop to shut down.
func (b *Batcher) Start() {
	b.wg.Add(1)
	go b.loop()
}

// Stop flushes remaining events and stops the background goroutine.
func (b *Batcher) Stop() {
	close(b.done)
	b.wg.Wait()
}

// Submit adds an event to the in-memory buffer. Thread-safe.
func (b *Batcher) Submit(event *pb.InteractionEvent) {
	vid := event.VideoId
	text := event.RawText

	b.mu.Lock()
	defer b.mu.Unlock()

	b.seen[vid] = struct{}{}

	vidBuf, ok := b.buffers[vid]
	if !ok {
		vidBuf = make(map[string]*mergeEntry)
		b.buffers[vid] = vidBuf
	}

	if entry, exists := vidBuf[text]; exists {
		entry.count++
		if event.TimestampMs > entry.event.TimestampMs {
			entry.event.TimestampMs = event.TimestampMs
		}
	} else {
		vidBuf[text] = &mergeEntry{event: event, count: 1}
	}
}

// ForgetVideoID removes a video from the active set. Called by the aggregator
// when a FetchWindow returns empty, so stale IDs don't accumulate indefinitely.
// New events for the same video re-add it via Submit.
func (b *Batcher) ForgetVideoID(videoID string) {
	b.mu.Lock()
	delete(b.seen, videoID)
	b.mu.Unlock()
}

// ActiveVideoIDs returns all video IDs that have ever been submitted.
func (b *Batcher) ActiveVideoIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	ids := make([]string, 0, len(b.seen))
	for id := range b.seen {
		ids = append(ids, id)
	}
	return ids
}

func (b *Batcher) loop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.flushEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.flush()
		case <-b.done:
			b.flush() // drain remaining events
			return
		}
	}
}

// flush swaps the buffer under lock, then writes outside the lock to
// minimize contention on the hot Submit path.
func (b *Batcher) flush() {
	b.mu.Lock()
	snapshot := b.buffers
	b.buffers = make(map[string]map[string]*mergeEntry)
	b.mu.Unlock()

	for videoID, entries := range snapshot {
		events := make([]*pb.InteractionEvent, 0, len(entries))
		for _, e := range entries {
			events = append(events, &pb.InteractionEvent{
				VideoId:     e.event.VideoId,
				TimestampMs: e.event.TimestampMs,
				RawText:     e.event.RawText,
				UserId:      e.event.UserId,
				RepeatCount: e.count,
			})
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := b.store.PushEvents(ctx, videoID, events); err != nil {
			log.Printf("batcher: flush failed for video %q (%d events): %v",
				videoID, len(events), err)
		}
		cancel()
	}
}
