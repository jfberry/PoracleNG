package logbuffer

import (
	"fmt"
	"sync"
	"testing"
)

// TestBuffer_StartupCaptured verifies that entries captured before
// MarkStartupComplete appear in Startup() and not in Recent().
func TestBuffer_StartupCaptured(t *testing.T) {
	b := New(10, 10)
	b.Capture("WARN", "a", "")
	b.Capture("ERROR", "b", "")
	b.Capture("WARN", "c", "")

	if got := len(b.Startup()); got != 3 {
		t.Fatalf("Startup() len = %d, want 3", got)
	}
	if got := len(b.Recent()); got != 0 {
		t.Fatalf("Recent() len = %d, want 0", got)
	}
}

// TestBuffer_StartupBoundedAtCap verifies that the startup buffer retains
// only the first startupCap entries; excess entries are silently dropped.
func TestBuffer_StartupBoundedAtCap(t *testing.T) {
	b := New(5, 10)
	for i := 0; i < 10; i++ {
		b.Capture("WARN", fmt.Sprintf("msg%d", i), "")
	}

	entries := b.Startup()
	if len(entries) != 5 {
		t.Fatalf("Startup() len = %d, want 5", len(entries))
	}
	// The FIRST 5 should be retained (msg0–msg4).
	for i, e := range entries {
		want := fmt.Sprintf("msg%d", i)
		if e.Message != want {
			t.Errorf("entries[%d].Message = %q, want %q", i, e.Message, want)
		}
	}
}

// TestBuffer_RoutesToRollingAfterMarkComplete verifies that after
// MarkStartupComplete, new entries go to Recent() only.
func TestBuffer_RoutesToRollingAfterMarkComplete(t *testing.T) {
	b := New(10, 10)
	b.Capture("WARN", "startup1", "")
	b.Capture("WARN", "startup2", "")

	b.MarkStartupComplete()

	b.Capture("WARN", "rolling1", "")
	b.Capture("ERROR", "rolling2", "")
	b.Capture("WARN", "rolling3", "")

	if got := len(b.Startup()); got != 2 {
		t.Fatalf("Startup() len = %d, want 2", got)
	}
	if got := len(b.Recent()); got != 3 {
		t.Fatalf("Recent() len = %d, want 3", got)
	}
}

// TestBuffer_RollingRingWraps verifies that when the rolling buffer exceeds
// its cap it retains only the most recent rollingCap entries.
func TestBuffer_RollingRingWraps(t *testing.T) {
	b := New(10, 3)
	b.MarkStartupComplete()

	for i := 0; i < 5; i++ {
		b.Capture("WARN", fmt.Sprintf("msg%d", i), "")
	}

	entries := b.Recent()
	if len(entries) != 3 {
		t.Fatalf("Recent() len = %d, want 3", len(entries))
	}
	// Should be the last 3 in arrival order: msg2, msg3, msg4
	want := []string{"msg2", "msg3", "msg4"}
	for i, e := range entries {
		if e.Message != want[i] {
			t.Errorf("entries[%d].Message = %q, want %q", i, e.Message, want[i])
		}
	}
}

// TestBuffer_RecentReturnsChronologicalOrder verifies Recent() returns
// entries oldest-first.
func TestBuffer_RecentReturnsChronologicalOrder(t *testing.T) {
	b := New(10, 10)
	b.MarkStartupComplete()

	b.Capture("WARN", "A", "")
	b.Capture("ERROR", "B", "")
	b.Capture("WARN", "C", "")

	entries := b.Recent()
	if len(entries) != 3 {
		t.Fatalf("Recent() len = %d, want 3", len(entries))
	}
	order := []string{"A", "B", "C"}
	for i, e := range entries {
		if e.Message != order[i] {
			t.Errorf("entries[%d].Message = %q, want %q", i, e.Message, order[i])
		}
	}
	// Also verify times are non-decreasing
	for i := 1; i < len(entries); i++ {
		if entries[i].Time.Before(entries[i-1].Time) {
			t.Errorf("entry[%d].Time < entry[%d].Time — not chronological", i, i-1)
		}
	}
}

// TestBuffer_ClearRecent_EmptiesRollingOnly verifies that ClearRecent
// zeroes the rolling buffer while leaving the startup buffer intact.
func TestBuffer_ClearRecent_EmptiesRollingOnly(t *testing.T) {
	b := New(10, 10)
	b.Capture("WARN", "s1", "")
	b.Capture("WARN", "s2", "")

	b.MarkStartupComplete()

	b.Capture("WARN", "r1", "")
	b.Capture("WARN", "r2", "")
	b.Capture("WARN", "r3", "")

	b.ClearRecent()

	if got := len(b.Startup()); got != 2 {
		t.Errorf("Startup() after ClearRecent = %d, want 2", got)
	}
	if got := len(b.Recent()); got != 0 {
		t.Errorf("Recent() after ClearRecent = %d, want 0", got)
	}
}

// TestBuffer_ConcurrentCapture exercises Capture from many goroutines
// simultaneously, including across the MarkStartupComplete boundary.
// The test asserts: no data race (enforced by -race), total entries
// within expected caps, and no panic.
func TestBuffer_ConcurrentCapture(t *testing.T) {
	const goroutines = 100
	const perGoroutine = 100
	startupCap := 200
	rollingCap := 50

	b := New(startupCap, rollingCap)

	var wg sync.WaitGroup
	// Half the goroutines write before MarkStartupComplete, half after.
	// We serialise the boundary itself so the test is deterministic about
	// which side each goroutine lands on.
	startupDone := make(chan struct{})

	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				b.Capture("WARN", "startup", "")
			}
		}()
	}
	// Wait for half the goroutines to finish their initial burst, then flip.
	go func() {
		wg.Wait()
		b.MarkStartupComplete()
		close(startupDone)
	}()

	<-startupDone

	var wg2 sync.WaitGroup
	for i := 0; i < goroutines/2; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for j := 0; j < perGoroutine; j++ {
				b.Capture("ERROR", "rolling", "")
			}
		}()
	}
	wg2.Wait()

	startupEntries := b.Startup()
	rollingEntries := b.Recent()

	if len(startupEntries) > startupCap {
		t.Errorf("startup len %d > cap %d", len(startupEntries), startupCap)
	}
	if len(rollingEntries) > rollingCap {
		t.Errorf("rolling len %d > cap %d", len(rollingEntries), rollingCap)
	}
}

// TestBuffer_SnapshotIsolation verifies that a snapshot returned by Startup()
// is not affected by subsequent Capture calls.
func TestBuffer_SnapshotIsolation(t *testing.T) {
	b := New(10, 10)
	b.Capture("WARN", "first", "")
	b.Capture("ERROR", "second", "")

	snapshot := b.Startup()
	if len(snapshot) != 2 {
		t.Fatalf("initial snapshot len = %d, want 2", len(snapshot))
	}

	// Add more entries; the snapshot must be unaffected.
	b.Capture("WARN", "third", "")
	b.Capture("WARN", "fourth", "")

	if len(snapshot) != 2 {
		t.Errorf("snapshot mutated: len = %d, want 2", len(snapshot))
	}
	if snapshot[0].Message != "first" || snapshot[1].Message != "second" {
		t.Errorf("snapshot content changed: %v", snapshot)
	}
}
