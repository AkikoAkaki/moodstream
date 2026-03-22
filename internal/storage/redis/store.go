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

// Options configures a Redis store.
type Options struct {
	Addr     string
	Password string
	DB       int
}

var _ storage.EventStore = (*Store)(nil)

// NewStore creates a store connecting to the given address (password-less, DB 0).
func NewStore(addr string) *Store {
	return NewStoreWith(Options{Addr: addr})
}

// NewStoreWith creates a store with full Redis options.
func NewStoreWith(opts Options) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{
			Addr:     opts.Addr,
			Password: opts.Password,
			DB:       opts.DB,
		}),
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
		Member: data,
	}).Err()
}

// PushEvents writes a batch of events in a single Redis pipeline round-trip.
func (s *Store) PushEvents(ctx context.Context, videoID string, events []*pb.InteractionEvent) error {
	if len(events) == 0 {
		return nil
	}
	key := s.eventKey(videoID)
	pipe := s.client.Pipeline()
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		pipe.ZAdd(ctx, key, redis.Z{
			Score:  float64(event.TimestampMs),
			Member: data,
		})
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("pipeline push events: %w", err)
	}
	return nil
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
			log.Printf("store: FetchWindow: dropping corrupt event for video %q: %v (raw: %.80s)", videoID, err, raw)
			continue
		}
		events = append(events, &e)
	}
	return events, nil
}
