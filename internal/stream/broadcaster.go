package stream

import (
	"sync"

	pb "github.com/AkikoAkaki/async-task-platform/api/proto"
)

// Broadcaster is a thread-safe fan-out hub for WindowResult messages.
// SSE handlers subscribe to receive results; the aggregator publishes them.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan *pb.WindowResult]struct{}
}

// NewBroadcaster creates an empty broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[chan *pb.WindowResult]struct{}),
	}
}

// Subscribe registers a new listener and returns its channel.
// The caller must call Unsubscribe when done.
func (b *Broadcaster) Subscribe() chan *pb.WindowResult {
	ch := make(chan *pb.WindowResult, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a listener channel.
func (b *Broadcaster) Unsubscribe(ch chan *pb.WindowResult) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish sends a result to all current subscribers (non-blocking per client).
func (b *Broadcaster) Publish(result *pb.WindowResult) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- result:
		default:
			// Slow consumer — drop this message to avoid blocking.
		}
	}
}
