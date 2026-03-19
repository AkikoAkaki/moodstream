package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
	"github.com/redis/go-redis/v9"
)

// Store implements storage.EventStore using Redis ZSet.
// Key scheme: stream:{video_id}:events (score = timestamp_ms)
type Store struct {
	client    *redis.Client
	namespace string // non-empty in tests for per-test key isolation
}

var _ storage.EventStore = (*Store)(nil)

func NewStore(addr string) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) eventKey(videoID string) string {
	if s.namespace != "" {
		return fmt.Sprintf("stream:%s:%s:events", s.namespace, videoID)
	}
	return fmt.Sprintf("stream:%s:events", videoID)
}

func (s *Store) PushEvent(ctx context.Context, videoID string, event *pb.InteractionEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return s.client.ZAdd(ctx, s.eventKey(videoID), redis.Z{
		Score:  float64(event.TimestampMs),
		Member: data, // pass []byte directly — avoids string copy
	}).Err()
}

func (s *Store) FetchWindow(ctx context.Context, videoID string, fromMs, toMs int64) ([]*pb.InteractionEvent, error) {
	results, err := s.client.Eval(ctx, luaFetchWindow, []string{s.eventKey(videoID)},
		fromMs, toMs,
	).StringSlice()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("fetch window: %w", err)
	}

	events := make([]*pb.InteractionEvent, 0, len(results))
	for _, raw := range results {
		var e pb.InteractionEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			// Data was already removed from Redis by the Lua script — log so it's visible.
			log.Printf("store: FetchWindow: dropping corrupt event for video %q: %v (raw: %.80s)", videoID, err, raw)
			continue
		}
		events = append(events, &e)
	}
	return events, nil
}
