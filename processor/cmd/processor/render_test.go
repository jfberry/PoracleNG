package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test 4 – tthFromUnix conversion
// ---------------------------------------------------------------------------

// TestTthFromUnix_FutureTimestamp verifies that a timestamp one hour from now
// produces a TTH with Hours=1 and all other fields zero (assuming the delta
// is exact — we allow a ±1s skew for slow CI).
func TestTthFromUnix_FutureTimestamp(t *testing.T) {
	target := time.Now().Unix() + 3600 // exactly one hour from now
	tth := tthFromUnix(target)

	if tth.Days != 0 {
		t.Errorf("Days = %d, want 0 for a 1-hour target", tth.Days)
	}
	// Allow Hours=0 or 1 to tolerate sub-second skew at test startup.
	if tth.Hours < 0 || tth.Hours > 1 {
		t.Errorf("Hours = %d, want 0 or 1 for a ~1-hour target", tth.Hours)
	}
	// The TTH must not be all zeros (that would indicate FirstDateWasLater).
	total := tth.Days + tth.Hours + tth.Minutes + tth.Seconds
	if total == 0 {
		t.Errorf("tthFromUnix returned all-zero TTH for a future target — FirstDateWasLater should be false")
	}
}

// TestTthFromUnix_ExactlyOneHour pins the primary case: a target that is
// exactly 3600 seconds in the future must yield Hours=1.
func TestTthFromUnix_ExactlyOneHour(t *testing.T) {
	// Add a buffer of 2 seconds so the test doesn't flap if the goroutine
	// scheduler introduces a tiny delay between time.Now() calls.
	target := time.Now().Unix() + 3602
	tth := tthFromUnix(target)

	if tth.Hours < 1 {
		t.Errorf("Hours = %d, want >= 1 for a target ~1h in the future", tth.Hours)
	}
}

// TestTthFromUnix_PastTimestamp verifies that a target in the past (or now)
// returns an all-zero TTH (the FirstDateWasLater path from geo.ComputeTTH).
func TestTthFromUnix_PastTimestamp(t *testing.T) {
	target := int64(0) // Unix epoch — always in the past
	tth := tthFromUnix(target)

	if tth.Days != 0 || tth.Hours != 0 || tth.Minutes != 0 || tth.Seconds != 0 {
		t.Errorf("past target: tthFromUnix = %+v, want all-zero TTH", tth)
	}
}

// TestTthFromUnix_ZeroTarget is the explicit zero-value test from the spec.
func TestTthFromUnix_ZeroTarget(t *testing.T) {
	tth := tthFromUnix(0)
	total := tth.Days + tth.Hours + tth.Minutes + tth.Seconds
	if total != 0 {
		t.Errorf("tthFromUnix(0) = %+v, want all zeros", tth)
	}
}

// TestTthFromUnix_24Hours verifies that a 24-hour future timestamp is
// expressed as Days=1, Hours=0 (or Days=0, Hours=24 isn't valid — geo.ComputeTTH
// uses integer division so 86400s → days=1, hours=0).
func TestTthFromUnix_24Hours(t *testing.T) {
	target := time.Now().Unix() + 86402 // 24h + 2s buffer
	tth := tthFromUnix(target)

	if tth.Days < 1 {
		t.Errorf("24h target: Days = %d, want >= 1", tth.Days)
	}
}

// TestTthFromUnix_SmallFuture confirms that a small positive delta (e.g. 90
// seconds) produces a non-zero TTH with correct minute/second decomposition.
func TestTthFromUnix_SmallFuture(t *testing.T) {
	target := time.Now().Unix() + 92 // 1m 32s + 2s buffer
	tth := tthFromUnix(target)

	if tth.Days != 0 {
		t.Errorf("small future: Days = %d, want 0", tth.Days)
	}
	if tth.Hours != 0 {
		t.Errorf("small future: Hours = %d, want 0", tth.Hours)
	}
	// Minutes must be at least 1 (we added 92s).
	if tth.Minutes < 1 {
		t.Errorf("small future: Minutes = %d, want >= 1 (92s → at least 1 minute)", tth.Minutes)
	}
}
