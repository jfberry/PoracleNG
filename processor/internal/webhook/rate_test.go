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
// minutes ago and asserts that each window boundary is respected.
func TestRateCounter_WindowBoundaries(t *testing.T) {
	base := time.Now().Truncate(time.Minute)

	offsets := []int{0, 4, 14, 30, 59} // minutes ago

	rc := newRateCounterWithClock(fixedClock(base)) // dummy; we'll vary per record
	for _, ago := range offsets {
		rc.now = fixedClock(minutesAgo(base, ago))
		rc.Record(fmt.Sprintf("type%d", ago))
	}
	// Snapshot is taken "at" base.
	rc.now = fixedClock(base)

	snap := rc.Snapshot()

	// Per5Min: only events at 0 and 4 minutes ago (age < 5).
	if snap.Per5Min != 2 {
		t.Errorf("Per5Min = %d, want 2 (events at 0m and 4m ago)", snap.Per5Min)
	}
	// Per15Min: 0, 4, 14 minutes ago (age < 15).
	if snap.Per15Min != 3 {
		t.Errorf("Per15Min = %d, want 3 (events at 0m, 4m, 14m ago)", snap.Per15Min)
	}
	// Per60Min: all five (age < 60).
	if snap.Per60Min != 5 {
		t.Errorf("Per60Min = %d, want 5 (all events within 60 min)", snap.Per60Min)
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
