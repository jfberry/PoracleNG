package mute

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Sweeper runs Store.Sweep on a fixed cadence. Mute entries store an
// absolute expiry — the matcher already skips expired entries on the read
// path — so the sweep is purely a memory-reclaim concern. A long-lived
// process with bursty muting would accumulate dead entries until restart
// without this; the cadence isn't latency-critical.
//
// Cadence defaults to a minute. Matchers tolerate up to one sweep
// interval of "phantom" stale entries between expiry and reap, which is
// invisible to users because the matcher's Match call respects ExpiresAt
// directly.
type Sweeper struct {
	store    *Store
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

// NewSweeper builds a sweeper. interval <= 0 falls back to one minute.
func NewSweeper(store *Store, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Sweeper{
		store:    store,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the sweep goroutine. The first sweep runs after one
// interval — fresh stores have nothing to reap.
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
			now := time.Now().Unix()
			if removed := s.store.Sweep(now); removed > 0 {
				log.Debugf("mute: sweep reclaimed %d expired entries", removed)
			}
		}
	}
}

// Stop signals the sweep goroutine to exit and blocks until it has. Safe
// to call from multiple goroutines; only the first call has any effect.
func (s *Sweeper) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}
