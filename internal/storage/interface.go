package storage

import "context"

// InteractionEvent represents a single danmu/comment event.
// Replaced by proto-generated type (api/proto/stream.proto) in Phase 2.
type InteractionEvent struct {
	VideoID     string `json:"video_id"`
	TimestampMs int64  `json:"timestamp_ms"`
	RawText     string `json:"raw_text"`
	UserID      string `json:"user_id,omitempty"`
}

// EventStore is the persistence contract for the stream processing layer.
// Implementations must guarantee atomic fetch-and-delete in FetchWindow.
//
//go:generate mockgen -source=interface.go -destination=mocks/store_mock.go -package=mocks
type EventStore interface {
	// PushEvent writes a single event into the ZSet for the given video.
	// Score = TimestampMs (video playback position, not wall clock).
	PushEvent(ctx context.Context, videoID string, event *InteractionEvent) error

	// FetchWindow atomically retrieves and removes all events with
	// timestamps in [fromMs, toMs] for the given video.
	FetchWindow(ctx context.Context, videoID string, fromMs, toMs int64) ([]*InteractionEvent, error)
}
