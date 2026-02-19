package storage

import "context"

// QueueDepthProvider exposes queue depth reads for observability.
type QueueDepthProvider interface {
	QueueDepth(ctx context.Context, topic string) (int64, error)
}
