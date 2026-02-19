package storage

import (
	"context"
	"time"
)

// WatchdogLeaderElector provides a lightweight leader election primitive for watchdog instances.
// Implementations should guarantee that at most one owner can hold the lease key at a time.
type WatchdogLeaderElector interface {
	// TryAcquireWatchdogLeader tries to acquire or renew leadership for the given owner.
	// Returns true if this owner is the current leader.
	TryAcquireWatchdogLeader(ctx context.Context, owner string, ttl time.Duration) (bool, error)
}
