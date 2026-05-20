package snapshots

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
)

// Sweeper runs Store.Sweep on a fixed cadence in a background goroutine.
// It's the safety net for snapshots whose normal per-message delete callback
// didn't fire (process restart between TTL expiry and the delete callback, a
// destination that was removed, etc.). Steady-state operation should produce
// near-zero deletions per sweep — anything large is a signal that the normal
// path isn't running.
//
// A Sweeper is created by NewSweeper and started by Start. Stop signals the
// goroutine to exit and blocks until it has. The goroutine catches up on its
// own — Stop is safe to call from any goroutine, including one that's
// shutting down concurrently with the rest of the process.
type Sweeper struct {
	store    Store
	interval time.Duration
	maxAge   time.Duration

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewSweeper builds a sweeper that runs every interval, deleting snapshots
// whose ExpiresAt is more than maxAge in the past. interval and maxAge must
// both be positive; zero values fall back to sensible defaults (1h sweep
// interval, 7d max age) per #108's recommendation.
//
// The grace period (maxAge) lets operators investigating "why didn't this
// button work yesterday" find the snapshot in the store — long enough for
// triage, short enough that orphans don't accumulate.
func NewSweeper(store Store, interval, maxAge time.Duration) *Sweeper {
	if interval <= 0 {
		interval = time.Hour
	}
	if maxAge <= 0 {
		maxAge = 7 * 24 * time.Hour
	}
	return &Sweeper{
		store:    store,
		interval: interval,
		maxAge:   maxAge,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the sweep goroutine. The first sweep runs after one
// interval — there's no need to sweep immediately at startup because any
// snapshots accumulated since the last process exit will get cleaned up on
// their natural TTL or by the next periodic tick.
func (s *Sweeper) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *Sweeper) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

// sweepOnce computes the threshold timestamp and invokes Store.Sweep. The
// store treats records with ExpiresAt < threshold as deletable; threshold is
// (now - maxAge), giving the configured grace period beyond ExpiresAt.
func (s *Sweeper) sweepOnce(ctx context.Context) {
	threshold := time.Now().Add(-s.maxAge).Unix()
	deleted, err := s.store.Sweep(ctx, threshold)
	if err != nil {
		log.Warnf("snapshots: sweep error: %v", err)
		return
	}
	if deleted > 0 {
		metrics.SnapshotSweepDeletionsTotal.Add(float64(deleted))
		// Non-zero deletions are worth surfacing — the steady state should
		// be near-zero because the normal per-message delete callback runs.
		log.Infof("snapshots: sweep deleted %d expired snapshots", deleted)
	} else {
		log.Debugf("snapshots: sweep deleted 0 snapshots")
	}
	// Refresh the entries gauge after each sweep. The store impl exposes
	// Count() but only via the concrete type — try a type assertion.
	if c, ok := s.store.(interface{ Count() uint32 }); ok {
		metrics.SnapshotStoreEntries.Set(float64(c.Count()))
	}
}

// Stop signals the sweep goroutine to exit and waits for it to finish. Safe
// to call multiple times; only the first invocation has any effect.
func (s *Sweeper) Stop() {
	s.once.Do(func() {
		close(s.stop)
	})
	<-s.done
}
