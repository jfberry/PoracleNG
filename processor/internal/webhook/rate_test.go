package webhook

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fixedClock returns a clock function that always returns t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// minutesAgo returns a clock function that returns a time n minutes before base.
func minutesAgo(base time.Time, n int) time.Time {
	return base.Add(-time.Duration(n) * time.Minute)
}

// TestRateCounter_SingleMinute_RecordsTotal records 5 pokemon events in the
// same minute and checks all three windows report 5.
func TestRateCounter_SingleMinute_RecordsTotal(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	rc := newRateCounterWithClock(fixedClock(now))

	for i := 0; i < 5; i++ {
		rc.Record("pokemon")
	}

	snap := rc.Snapshot()
	if snap.Per5Min != 5 {
		t.Errorf("Per5Min = %d, want 5", snap.Per5Min)
	}
	if snap.Per15Min != 5 {
		t.Errorf("Per15Min = %d, want 5", snap.Per15Min)
	}
	if snap.Per60Min != 5 {
		t.Errorf("Per60Min = %d, want 5", snap.Per60Min)
	}
	if snap.PerType["pokemon"] != 5 {
		t.Errorf("PerType[pokemon] = %d, want 5", snap.PerType["pokemon"])
	}
}

// TestRateCounter_MultipleTypes_Breakdown records mixed types and checks both
// the per-type breakdown and the total.
func TestRateCounter_MultipleTypes_Breakdown(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	rc := newRateCounterWithClock(fixedClock(now))

	for i := 0; i < 3; i++ {
		rc.Record("pokemon")
	}
	for i := 0; i < 2; i++ {
		rc.Record("raid")
	}
	rc.Record("quest")

	snap := rc.Snapshot()
	if snap.Per60Min != 6 {
		t.Errorf("Per60Min = %d, want 6", snap.Per60Min)
	}
	if snap.PerType["pokemon"] != 3 {
		t.Errorf("PerType[pokemon] = %d, want 3", snap.PerType["pokemon"])
	}
	if snap.PerType["raid"] != 2 {
		t.Errorf("PerType[raid] = %d, want 2", snap.PerType["raid"])
	}
	if snap.PerType["quest"] != 1 {
		t.Errorf("PerType[quest] = %d, want 1", snap.PerType["quest"])
	}
}

// TestRateCounter_OldEventsDecay records 5 events 70 minutes ago and 3 events
// now; the old ones must not appear in any window.
func TestRateCounter_OldEventsDecay(t *testing.T) {
	base := time.Now().Truncate(time.Minute)
	old := minutesAgo(base, 70)

	// Record old events with an old clock.
	rc := newRateCounterWithClock(fixedClock(old))
	for i := 0; i < 5; i++ {
		rc.Record("pokemon")
	}

	// Advance the clock to now and record 3 new events.
	rc.now = fixedClock(base)
	for i := 0; i < 3; i++ {
		rc.Record("raid")
	}

	snap := rc.Snapshot()
	if snap.Per60Min != 3 {
		t.Errorf("Per60Min = %d, want 3 (old events must have decayed)", snap.Per60Min)
	}
	if snap.PerType["pokemon"] != 0 {
		t.Errorf("PerType[pokemon] = %d, want 0 (old events must be gone)", snap.PerType["pokemon"])
	}
	if snap.PerType["raid"] != 3 {
		t.Errorf("PerType[raid] = %d, want 3", snap.PerType["raid"])
	}
}

// TestRateCounter_WindowBoundaries records one event at 0, 4, 14, 30, and 59
// minutes ago and asserts that each window boundary is respected. Also records
// events at exactly 5 and 15 minutes ago to verify strict-< exclusion: those
// events must NOT appear in Per5Min or Per15Min respectively.
func TestRateCounter_WindowBoundaries(t *testing.T) {
	base := time.Now().Truncate(time.Minute)

	// Inner events (must be counted in all applicable windows).
	offsets := []int{0, 4, 14, 30, 59} // minutes ago

	rc := newRateCounterWithClock(fixedClock(base)) // dummy; we'll vary per record
	for _, ago := range offsets {
		rc.now = fixedClock(minutesAgo(base, ago))
		rc.Record(fmt.Sprintf("type%d", ago))
	}

	// Boundary events at exactly 5 and 15 minutes ago: must be EXCLUDED from
	// Per5Min and Per15Min respectively (Snapshot uses strict age < 5 / < 15),
	// but must still be counted in Per60Min.
	rc.now = fixedClock(minutesAgo(base, 5))
	rc.Record("type5")
	rc.now = fixedClock(minutesAgo(base, 15))
	rc.Record("type15")

	// Snapshot is taken "at" base.
	rc.now = fixedClock(base)

	snap := rc.Snapshot()

	// Per5Min: only events at 0 and 4 minutes ago (age < 5).
	// The event at age=5 is excluded (strict <).
	if snap.Per5Min != 2 {
		t.Errorf("Per5Min = %d, want 2 (events at 0m and 4m ago; 5m ago excluded)", snap.Per5Min)
	}
	// Per15Min: 0, 4, 5, 14 minutes ago (age < 15).
	// The event at age=5 is INCLUDED here (5 < 15 is true).
	// The event at age=15 is excluded (strict <).
	if snap.Per15Min != 4 {
		t.Errorf("Per15Min = %d, want 4 (events at 0m, 4m, 5m, 14m ago; 15m ago excluded)", snap.Per15Min)
	}
	// Per60Min: all seven (age < 60), including the boundary events at 5m and 15m.
	if snap.Per60Min != 7 {
		t.Errorf("Per60Min = %d, want 7 (all events within 60 min, including 5m and 15m ago)", snap.Per60Min)
	}
}

// TestRateCounter_SnapshotIsIsolated verifies that a snapshot is a true
// value-typed copy: subsequent Record calls must not mutate the snapshot's
// maps or totals.
func TestRateCounter_SnapshotIsIsolated(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	rc := newRateCounterWithClock(fixedClock(now))
	rc.Record("pokemon")
	snap := rc.Snapshot()
	rc.Record("pokemon")
	rc.Record("raid")
	if snap.Per60Min != 1 {
		t.Errorf("snapshot mutated by subsequent Record: Per60Min = %d, want 1", snap.Per60Min)
	}
	if snap.PerType["pokemon"] != 1 {
		t.Errorf("snapshot.PerType mutated: pokemon = %d, want 1", snap.PerType["pokemon"])
	}
	if _, ok := snap.PerType["raid"]; ok {
		t.Errorf("snapshot.PerType leaked post-snapshot type: %v", snap.PerType)
	}
}

// TestRateCounter_ConcurrentRecord spawns 100 goroutines each calling Record
// 100 times with mixed types. The total and per-type breakdown must both sum
// to exactly 10000 with no data races.
func TestRateCounter_ConcurrentRecord(t *testing.T) {
	rc := NewRateCounter()
	types := []string{"pokemon", "raid", "quest", "invasion", "lure"}

	var wg sync.WaitGroup
	const goroutines = 100
	const recordsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < recordsPerGoroutine; i++ {
				rc.Record(types[i%len(types)])
			}
		}(g)
	}
	wg.Wait()

	snap := rc.Snapshot()

	const total = goroutines * recordsPerGoroutine // 10000
	if snap.Per60Min != total {
		t.Errorf("Per60Min = %d, want %d", snap.Per60Min, total)
	}

	typeSum := 0
	for _, cnt := range snap.PerType {
		typeSum += cnt
	}
	if typeSum != total {
		t.Errorf("sum of PerType = %d, want %d", typeSum, total)
	}
}
