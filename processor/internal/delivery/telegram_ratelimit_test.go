package delivery

import (
	"testing"
	"time"
)

// newTestTelegramSender returns a minimal TelegramSender wired with an
// injectable clock — no HTTP server needed for rate-limit-only tests.
func newTestTelegramSender(nowFn func() time.Time) *TelegramSender {
	return &TelegramSender{
		nowFunc: nowFn,
	}
}

func TestTelegramSender_Snapshot_Empty(t *testing.T) {
	ts := newTestTelegramSender(nil)
	snap := ts.Snapshot()

	if snap.Recent429Count != 0 {
		t.Errorf("Recent429Count = %d, want 0 on fresh sender", snap.Recent429Count)
	}
	if !snap.CurrentBackoffUntil.IsZero() {
		t.Errorf("CurrentBackoffUntil = %v, want zero on fresh sender", snap.CurrentBackoffUntil)
	}
}

func TestTelegramSender_Record429_Counts(t *testing.T) {
	fixedTime := time.Date(2026, 1, 1, 12, 0, 30, 0, time.UTC)
	ts := newTestTelegramSender(func() time.Time { return fixedTime })

	ts.Record429()
	ts.Record429()
	ts.Record429()

	snap := ts.Snapshot()
	if snap.Recent429Count != 3 {
		t.Errorf("Recent429Count = %d, want 3", snap.Recent429Count)
	}
}

func TestTelegramSender_Record429_DecaysAfter5Min(t *testing.T) {
	pastTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ts := newTestTelegramSender(func() time.Time { return pastTime })

	ts.Record429()
	ts.Record429()
	ts.Record429()

	// Advance clock by 6 minutes — past the 5-minute window.
	futureTime := pastTime.Add(6 * time.Minute)
	ts.nowFunc = func() time.Time { return futureTime }

	snap := ts.Snapshot()
	if snap.Recent429Count != 0 {
		t.Errorf("Recent429Count = %d, want 0 — 429s from 6 min ago are outside the 5-min window", snap.Recent429Count)
	}
}

func TestTelegramSender_BackoffWindowReflectsActiveDeadline(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ts := newTestTelegramSender(func() time.Time { return now })

	deadline := now.Add(10 * time.Second)
	ts.setBackoffUntil(deadline)

	snap := ts.Snapshot()
	if snap.CurrentBackoffUntil.IsZero() {
		t.Fatal("CurrentBackoffUntil is zero, want active deadline")
	}
	if !snap.CurrentBackoffUntil.Equal(deadline) {
		t.Errorf("CurrentBackoffUntil = %v, want %v", snap.CurrentBackoffUntil, deadline)
	}
}

func TestTelegramSender_BackoffWindowClearsAfterExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC)
	ts := newTestTelegramSender(func() time.Time { return now })

	// Backoff deadline is 1 second in the past.
	expiredDeadline := now.Add(-1 * time.Second)
	ts.setBackoffUntil(expiredDeadline)

	snap := ts.Snapshot()
	if !snap.CurrentBackoffUntil.IsZero() {
		t.Errorf("CurrentBackoffUntil = %v, want zero for expired backoff", snap.CurrentBackoffUntil)
	}
}
