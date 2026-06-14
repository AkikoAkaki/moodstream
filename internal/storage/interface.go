package storage

import (
	"context"

	pb "github.com/AkikoAkaki/moodstream/api/proto"
)

// EventStore is the persistence contract for the stream processing layer.
// Implementations must guarantee atomic fetch-and-delete in FetchWindow.
//
//go:generate mockgen -source=interface.go -destination=mocks/store_mock.go -package=mocks
type EventStore interface {
	// PushEvent writes a single event into the ZSet for the given video.
	// Score = TimestampMs (video playback position, not wall clock).
	PushEvent(ctx context.Context, videoID string, event *pb.InteractionEvent) error

	// PushEvents writes a batch of events using a Redis pipeline for efficiency.
	PushEvents(ctx context.Context, videoID string, events []*pb.InteractionEvent) error

	// FetchWindow atomically retrieves and removes all events with
	// timestamps in [fromMs, toMs] for the given video.
	FetchWindow(ctx context.Context, videoID string, fromMs, toMs int64) ([]*pb.InteractionEvent, error)
}
