package webhook

import (
	"sync"
	"time"
)

// RateCounter tracks webhook arrival counts over a rolling 60-minute window,
// broken down by webhook type. Safe for concurrent use.
//
// Implementation: two parallel 60-slot ring buffers, one for per-minute totals
// and one for per-minute per-type maps. The slot index is (unix/60 % 60), so
// each slot covers exactly one calendar minute. Slots are overwritten when a
// new minute arrives, giving automatic O(1) decay with no background goroutine.
type RateCounter struct {
	mu sync.Mutex

	// now is used for all time reads; defaults to time.Now for production,
	// injectable for tests.
	now func() time.Time

	// totals[i] is the (minute, count) pair for slot i.
	totals [60]minuteSlot

	// byType[i] holds per-type counts for minute slot i.
	byType [60]map[string]int
}

type minuteSlot struct {
	minute int64 // unix timestamp / 60
	count  int
}

// NewRateCounter returns a ready-to-use RateCounter backed by time.Now.
func NewRateCounter() *RateCounter {
	return newRateCounterWithClock(time.Now)
}

// newRateCounterWithClock creates a RateCounter with an injectable clock, used
// in tests to control time without wall-clock dependencies.
func newRateCounterWithClock(now func() time.Time) *RateCounter {
	rc := &RateCounter{now: now}
	for i := range rc.byType {
		rc.byType[i] = make(map[string]int)
	}
	return rc
}

// Record bumps the counter for the given webhook type at the current time.
// Cheap — one lock acquisition, one slot update, one map write.
func (r *RateCounter) Record(webhookType string) {
	now := r.now()
	min := now.Unix() / 60
	idx := int(min % 60)

	r.mu.Lock()
	defer r.mu.Unlock()

	// If the slot belongs to a different (older) minute, reset it.
	if r.totals[idx].minute != min {
		r.totals[idx] = minuteSlot{minute: min, count: 0}
		r.byType[idx] = make(map[string]int)
	}

	r.totals[idx].count++
	r.byType[idx][webhookType]++
}

// Snapshot returns the current rate statistics as a value-typed snapshot.
// Safe to call from any goroutine.
func (r *RateCounter) Snapshot() RateSnapshot {
	now := r.now()
	currentMin := now.Unix() / 60

	r.mu.Lock()
	defer r.mu.Unlock()

	var per5, per15, per60 int
	perType := make(map[string]int)

	for i := range r.totals {
		slot := &r.totals[i]
		if slot.minute == 0 {
			// Never written.
			continue
		}
		age := currentMin - slot.minute // minutes ago
		if age < 0 || age >= 60 {
			continue
		}
		per60 += slot.count
		if age < 15 {
			per15 += slot.count
		}
		if age < 5 {
			per5 += slot.count
		}
		for typ, cnt := range r.byType[i] {
			perType[typ] += cnt
		}
	}

	return RateSnapshot{
		Per5Min:  per5,
		Per15Min: per15,
		Per60Min: per60,
		PerType:  perType,
	}
}

// RateSnapshot is a point-in-time read of RateCounter state.
type RateSnapshot struct {
	Per5Min  int            // total webhooks across all types in last 5 min
	Per15Min int            // total in last 15 min
	Per60Min int            // total in last 60 min
	PerType  map[string]int // per-webhook-type count in last 60 min
}
