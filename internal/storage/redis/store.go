package redis

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
	"github.com/AkikoAkaki/async-task-platform/internal/storage"
	"github.com/redis/go-redis/v9"
)

// Store implements storage.EventStore using Redis ZSet.
// Key scheme: stream:{video_id}:events (score = timestamp_ms)
type Store struct {
	client *redis.Client
}

var _ storage.EventStore = (*Store)(nil)

func NewStore(addr string) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (s *Store) GetClient() *redis.Client {
	return s.client
}

func eventKey(videoID string) string {
	return fmt.Sprintf("stream:%s:events", videoID)
}

func (s *Store) PushEvent(ctx context.Context, videoID string, event *pb.InteractionEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return s.client.ZAdd(ctx, eventKey(videoID), redis.Z{
		Score:  float64(event.TimestampMs),
		Member: string(data),
	}).Err()
}

func (s *Store) FetchWindow(ctx context.Context, videoID string, fromMs, toMs int64) ([]*pb.InteractionEvent, error) {
	results, err := s.client.Eval(ctx, luaFetchWindow, []string{eventKey(videoID)},
		fromMs, toMs,
	).StringSlice()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("fetch window: %w", err)
	}

	events := make([]*pb.InteractionEvent, 0, len(results))
	for _, raw := range results {
		var e pb.InteractionEvent
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			continue
		}
		events = append(events, &e)
	}
	return events, nil
}
