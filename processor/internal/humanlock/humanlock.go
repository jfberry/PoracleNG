// Package humanlock provides a process-wide per-human write lock, so that
// entry points mutating the same human's data (API, bot, reconcilers) are
// serialised against each other without a global bottleneck.
package humanlock

import "sync"

type entry struct {
	mu   sync.Mutex
	refs int
}

var (
	mapMu   sync.Mutex
	entries = map[string]*entry{}
)

// Lock blocks until the caller holds the lock for humanID and returns the
// matching unlock func. An empty id returns a no-op unlock without locking.
// Entries are reference-counted and removed when the last holder unlocks.
func Lock(humanID string) (unlock func()) {
	if humanID == "" {
		return func() {}
	}
	mapMu.Lock()
	e, ok := entries[humanID]
	if !ok {
		e = &entry{}
		entries[humanID] = e
	}
	e.refs++
	mapMu.Unlock()

	e.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Unlock()
			mapMu.Lock()
			e.refs--
			if e.refs == 0 {
				delete(entries, humanID)
			}
			mapMu.Unlock()
		})
	}
}

// held returns the number of live entries (for tests).
func held() int {
	mapMu.Lock()
	defer mapMu.Unlock()
	return len(entries)
}
