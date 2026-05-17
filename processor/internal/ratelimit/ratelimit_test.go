package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnderLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5})
	defer l.Close()

	for i := range 3 {
		r := l.Check("user1", "discord:user")
		if !r.Allowed {
			t.Fatalf("message %d should be allowed", i+1)
		}
		if r.JustBreached || r.Banned {
			t.Fatalf("message %d should not be breached/banned", i+1)
		}
	}
}

func TestAtLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5})
	defer l.Close()

	// Send exactly 3 (the limit)
	for i := range 3 {
		r := l.Check("user1", "discord:user")
		if !r.Allowed {
			t.Fatalf("message %d should be allowed", i+1)
		}
	}
}

func TestOverLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Send 3 allowed
	for range 3 {
		l.Check("user1", "discord:user")
	}

	// 4th message = first over limit
	r := l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("4th message should not be allowed")
	}
	if !r.JustBreached {
		t.Fatal("4th message should have JustBreached=true")
	}
	if r.Banned {
		t.Fatal("should not be banned after one violation")
	}

	// 5th message = still over, but not JustBreached
	r = l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("5th message should not be allowed")
	}
	if r.JustBreached {
		t.Fatal("5th message should not have JustBreached=true")
	}
}

func TestChannelLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 3, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	for i := range 5 {
		r := l.Check("chan1", "discord:channel")
		if !r.Allowed {
			t.Fatalf("channel message %d should be allowed", i+1)
		}
	}

	r := l.Check("chan1", "discord:channel")
	if r.Allowed {
		t.Fatal("6th channel message should not be allowed")
	}
	if !r.JustBreached {
		t.Fatal("6th channel message should have JustBreached=true")
	}
}

func TestWindowExpiry(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Fill the window
	l.Check("user1", "discord:user")
	l.Check("user1", "discord:user")
	r := l.Check("user1", "discord:user")
	if r.Allowed {
		t.Fatal("should be over limit")
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)

	r = l.Check("user1", "discord:user")
	if !r.Allowed {
		t.Fatal("should be allowed after window expiry")
	}
}

func TestBannedAfterThreshold(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 3})
	defer l.Close()

	for violation := range 3 {
		// Fill window + breach
		l.Check("user1", "discord:user")
		r := l.Check("user1", "discord:user")
		if !r.JustBreached {
			t.Fatalf("violation %d: should be JustBreached", violation+1)
		}

		if violation < 2 {
			if r.Banned {
				t.Fatalf("violation %d: should not be banned yet", violation+1)
			}
		} else {
			if !r.Banned {
				t.Fatal("violation 3: should be banned")
			}
		}

		// Wait for rate limit window to reset
		time.Sleep(1100 * time.Millisecond)
	}
}

func TestOverrides(t *testing.T) {
	l := New(Config{
		TimingPeriod:        60,
		DMLimit:             2,
		ChannelLimit:        5,
		MaxLimitsBeforeStop: 10,
		Overrides:           map[string]int{"vip_user": 100},
	})
	defer l.Close()

	// VIP user should use override limit
	for i := range 100 {
		r := l.Check("vip_user", "discord:user")
		if !r.Allowed {
			t.Fatalf("VIP message %d should be allowed (limit 100)", i+1)
		}
	}

	r := l.Check("vip_user", "discord:user")
	if r.Allowed {
		t.Fatal("101st VIP message should not be allowed")
	}

	// Normal user is still limited to 2
	l.Check("normal_user", "discord:user")
	l.Check("normal_user", "discord:user")
	r = l.Check("normal_user", "discord:user")
	if r.Allowed {
		t.Fatal("3rd normal user message should not be allowed")
	}
}

func TestTelegramUserType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Telegram user gets DM limit
	l.Check("tguser", "telegram:user")
	l.Check("tguser", "telegram:user")
	r := l.Check("tguser", "telegram:user")
	if r.Allowed {
		t.Fatal("telegram user should hit DM limit of 2")
	}
}

func TestTelegramChannelType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Telegram channel gets channel limit
	for i := range 5 {
		r := l.Check("tgchan", "telegram:channel")
		if !r.Allowed {
			t.Fatalf("telegram channel message %d should be allowed", i+1)
		}
	}
	r := l.Check("tgchan", "telegram:channel")
	if r.Allowed {
		t.Fatal("6th telegram channel message should not be allowed")
	}
}

func TestWebhookType(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Webhook type gets channel limit
	for i := range 5 {
		r := l.Check("wh1", "webhook")
		if !r.Allowed {
			t.Fatalf("webhook message %d should be allowed", i+1)
		}
	}
	r := l.Check("wh1", "webhook")
	if r.Allowed {
		t.Fatal("6th webhook message should not be allowed")
	}
}

func TestResetSeconds(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	r := l.Check("user1", "discord:user")
	if r.ResetSeconds < 55 || r.ResetSeconds > 60 {
		t.Fatalf("ResetSeconds should be ~60, got %d", r.ResetSeconds)
	}
}

func TestResultLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10, MaxLimitsBeforeStop: 10})
	defer l.Close()

	r := l.Check("user1", "discord:user")
	if r.Limit != 5 {
		t.Fatalf("DM user limit should be 5, got %d", r.Limit)
	}

	r = l.Check("chan1", "discord:channel")
	if r.Limit != 10 {
		t.Fatalf("channel limit should be 10, got %d", r.Limit)
	}
}

func TestConcurrentAccess(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 100, ChannelLimit: 100})
	defer l.Close()

	var wg sync.WaitGroup
	var allowed atomic.Int64

	for range 200 {
		wg.Go(func() {
			r := l.Check("user1", "discord:user")
			if r.Allowed {
				allowed.Add(1)
			}
		})
	}

	wg.Wait()
	if allowed.Load() != 100 {
		t.Fatalf("expected exactly 100 allowed, got %d", allowed.Load())
	}
}

func TestIsBlocked(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5})
	defer l.Close()

	// Fresh destination is never blocked.
	if l.IsBlocked("user1", "discord:user") {
		t.Fatal("fresh destination should not be blocked")
	}

	// Under limit: still not blocked.
	l.Check("user1", "discord:user")
	if l.IsBlocked("user1", "discord:user") {
		t.Fatal("destination under limit should not be blocked")
	}

	// At limit (2 sends consumed quota): now blocked for new sends.
	l.Check("user1", "discord:user")
	if !l.IsBlocked("user1", "discord:user") {
		t.Fatal("destination at limit should be blocked")
	}

	// IsBlocked must not mutate state — successive calls return the same answer
	// without ever incrementing the counter or producing JustBreached.
	for range 5 {
		if !l.IsBlocked("user1", "discord:user") {
			t.Fatal("repeated IsBlocked should remain blocked")
		}
	}
	r := l.Check("user1", "discord:user")
	if !r.JustBreached {
		t.Fatal("first Check after IsBlocked spam should still be JustBreached — IsBlocked must not increment")
	}
}

func TestIsBlockedRespectsWindowExpiry(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 1, ChannelLimit: 5})
	defer l.Close()

	l.Check("user1", "discord:user")
	if !l.IsBlocked("user1", "discord:user") {
		t.Fatal("should be blocked at limit")
	}

	time.Sleep(1100 * time.Millisecond)

	if l.IsBlocked("user1", "discord:user") {
		t.Fatal("should be unblocked after window expiry")
	}
}

// TestSummaryBucket_SeparateFromAlertBucket pins the design intent:
// the summary bucket and the alert bucket count independently, so a
// destination near (or past) its alert cap can still receive
// summaries up to the summary limit, and vice versa.
func TestSummaryBucket_SeparateFromAlertBucket(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, DMSummaryLimit: 3, ChannelSummaryLimit: 6})
	defer l.Close()

	// Burn the alert bucket past its limit.
	for range 3 {
		_ = l.Check("user1", "discord:user")
	}
	if !l.IsBlocked("user1", "discord:user") {
		t.Fatal("alert bucket should be over limit after 3 Check calls (DM limit 2)")
	}

	// Summary bucket should still allow up to DMSummaryLimit (3) — the
	// alert bucket's saturation must not bleed into it.
	for i := range 3 {
		r := l.CheckSummary("user1", "discord:user")
		if !r.Allowed {
			t.Fatalf("summary dispatch %d should be allowed despite alert bucket being saturated", i+1)
		}
	}

	// 4th summary dispatch is the first over-limit one — JustBreached
	// fires exactly once per window so the dispatch path can notify.
	r := l.CheckSummary("user1", "discord:user")
	if r.Allowed {
		t.Fatal("4th summary dispatch should not be allowed (over DM summary limit 3)")
	}
	if !r.JustBreached {
		t.Fatal("4th summary dispatch should set JustBreached for the one-time notification")
	}
	if r.Banned {
		t.Fatal("summary bucket should never set Banned — no ban path for opt-in digests")
	}

	// 5th over-limit dispatch must NOT set JustBreached again (one
	// notification per window).
	r = l.CheckSummary("user1", "discord:user")
	if r.JustBreached {
		t.Fatal("subsequent over-limit calls in the same window should not re-trigger JustBreached")
	}
}

// TestSummaryBucket_ChannelLimit pins that channel destinations get
// their own (higher by default) summary limit, distinct from the DM
// limit, matching the alert-bucket DM/Channel split.
func TestSummaryBucket_ChannelLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMSummaryLimit: 2, ChannelSummaryLimit: 5})
	defer l.Close()

	// A discord channel destination should be allowed 5 dispatches,
	// not 2 — the DM limit must not apply.
	for i := range 5 {
		r := l.CheckSummary("chan1", "discord:channel")
		if !r.Allowed {
			t.Fatalf("channel summary dispatch %d should be allowed under channel limit 5", i+1)
		}
		if r.Limit != 5 {
			t.Errorf("result.Limit = %d, want 5 (channel)", r.Limit)
		}
	}
	if r := l.CheckSummary("chan1", "discord:channel"); r.Allowed {
		t.Fatal("6th channel summary dispatch should be over the channel limit")
	}
}

// TestSummaryBucket_DefaultLimits confirms the New() defaults when the
// operator leaves both fields at zero: DM=10, Channel=40.
func TestSummaryBucket_DefaultLimits(t *testing.T) {
	l := New(Config{TimingPeriod: 60}) // both summary limits unset
	defer l.Close()

	r := l.CheckSummary("user1", "discord:user")
	if r.Limit != 10 {
		t.Errorf("DM default: result.Limit = %d, want 10", r.Limit)
	}
	r = l.CheckSummary("chan1", "discord:channel")
	if r.Limit != 40 {
		t.Errorf("Channel default: result.Limit = %d, want 40", r.Limit)
	}
}

func TestTelegramGroupGetsChannelLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5})
	defer l.Close()

	// telegram:group should use channel limit (5), not DM limit (2)
	for i := range 5 {
		r := l.Check("group1", "telegram:group")
		if !r.Allowed {
			t.Fatalf("telegram group message %d should be allowed (channel limit 5)", i+1)
		}
	}
	r := l.Check("group1", "telegram:group")
	if r.Allowed {
		t.Fatal("6th telegram group message should not be allowed")
	}
}

// ---------------------------------------------------------------------------
// Introspection API: ListBlocked / StateFor / Reset
// ---------------------------------------------------------------------------

func TestLimiter_ListBlocked_Empty(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10})
	defer l.Close()

	got := l.ListBlocked()
	if len(got) != 0 {
		t.Fatalf("fresh limiter: expected empty slice, got %d entries", len(got))
	}
}

func TestLimiter_ListBlocked_OneOverLimit(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Exceed the DM limit — 3 calls, limit is 2.
	for range 3 {
		l.Check("user1", "discord:user")
	}

	blocked := l.ListBlocked()
	if len(blocked) != 1 {
		t.Fatalf("expected 1 blocked entry, got %d", len(blocked))
	}
	s := blocked[0]
	if s.ID != "user1" {
		t.Errorf("ID = %q, want %q", s.ID, "user1")
	}
	if s.Type != "discord:user" {
		t.Errorf("Type = %q, want %q", s.Type, "discord:user")
	}
	if s.Bucket != "alert" {
		t.Errorf("Bucket = %q, want %q", s.Bucket, "alert")
	}
	if s.Count != 3 {
		t.Errorf("Count = %d, want 3", s.Count)
	}
	if s.Limit != 2 {
		t.Errorf("Limit = %d, want 2", s.Limit)
	}
	if s.WindowStart.IsZero() {
		t.Error("WindowStart should be set")
	}
	if !s.WindowEnd.After(s.WindowStart) {
		t.Error("WindowEnd should be after WindowStart")
	}
}

func TestLimiter_ListBlocked_OneBanned(t *testing.T) {
	// MaxLimitsBeforeStop=1 so the first breach triggers a ban.
	l := New(Config{TimingPeriod: 1, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 1})
	defer l.Close()

	// Trigger one breach (and thus one violation → ban threshold reached).
	l.Check("user1", "discord:user") // at limit
	l.Check("user1", "discord:user") // JustBreached + Banned

	// Wait for the alert window to expire so the counter is stale, but
	// the 24h violation window is still live.
	time.Sleep(1100 * time.Millisecond)

	blocked := l.ListBlocked()

	// Should contain at least the ban entry even though the counter window expired.
	var found bool
	for _, s := range blocked {
		if s.ID == "user1" {
			found = true
			if s.BannedUntil.IsZero() {
				t.Error("BannedUntil should be set for a banned target")
			}
			if s.Violations24h < 1 {
				t.Errorf("Violations24h = %d, want >= 1", s.Violations24h)
			}
		}
	}
	if !found {
		t.Fatal("banned target should appear in ListBlocked")
	}
}

func TestLimiter_ListBlocked_StaleWindowExcluded(t *testing.T) {
	l := New(Config{TimingPeriod: 1, DMLimit: 1, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Breach the limit.
	l.Check("user1", "discord:user")
	l.Check("user1", "discord:user")

	// Window is stale after 1s.
	time.Sleep(1100 * time.Millisecond)

	blocked := l.ListBlocked()
	for _, s := range blocked {
		if s.ID == "user1" && s.Bucket == "alert" {
			t.Fatalf("stale window entry should not appear in ListBlocked; got %+v", s)
		}
	}
}

func TestLimiter_StateFor_BothBuckets(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10, DMSummaryLimit: 3, ChannelSummaryLimit: 8})
	defer l.Close()

	// Seed both buckets for the same destination.
	l.Check("user1", "discord:user")
	l.CheckSummary("user1", "discord:user")

	states := l.StateFor("user1", "discord:user")
	if len(states) != 2 {
		t.Fatalf("expected 2 states (alert + summary), got %d", len(states))
	}

	buckets := map[string]TargetState{}
	for _, s := range states {
		buckets[s.Bucket] = s
	}

	alert, ok := buckets["alert"]
	if !ok {
		t.Fatal("alert bucket missing from StateFor result")
	}
	if alert.Count != 1 {
		t.Errorf("alert Count = %d, want 1", alert.Count)
	}
	if alert.Limit != 5 {
		t.Errorf("alert Limit = %d, want 5 (DM limit)", alert.Limit)
	}

	summary, ok := buckets["summary"]
	if !ok {
		t.Fatal("summary bucket missing from StateFor result")
	}
	if summary.Count != 1 {
		t.Errorf("summary Count = %d, want 1", summary.Count)
	}
	if summary.Limit != 3 {
		t.Errorf("summary Limit = %d, want 3 (DM summary limit)", summary.Limit)
	}
}

func TestLimiter_StateFor_TypeFilter(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10})
	defer l.Close()

	// Seed "userA" with type "discord:user". There is no way to have the
	// same ID with a different type in the map simultaneously (the map is
	// keyed by ID), so we simulate the scenario by using different IDs
	// and confirming dtype filtering works correctly.
	l.Check("userA", "discord:user")
	l.Check("chanA", "discord:channel")

	// Fetch by ID+type — should return only the matching entry.
	states := l.StateFor("userA", "discord:user")
	if len(states) != 1 {
		t.Fatalf("StateFor(userA, discord:user): expected 1 entry, got %d", len(states))
	}
	if states[0].Type != "discord:user" {
		t.Errorf("Type = %q, want %q", states[0].Type, "discord:user")
	}

	// Fetch by ID+wrong type — should return empty (type mismatch).
	states = l.StateFor("userA", "discord:channel")
	if len(states) != 0 {
		t.Fatalf("StateFor(userA, discord:channel): expected 0 entries (type mismatch), got %d", len(states))
	}

	// Fetch by ID alone — should return the entry regardless of type.
	states = l.StateFor("userA", "")
	if len(states) != 1 {
		t.Fatalf("StateFor(userA, \"\"): expected 1 entry, got %d", len(states))
	}
}

func TestLimiter_StateFor_NotFound(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10})
	defer l.Close()

	states := l.StateFor("ghost", "discord:user")
	if states == nil {
		t.Fatal("StateFor should return empty non-nil slice for unknown target")
	}
	if len(states) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(states))
	}
}

func TestLimiter_Reset_ClearsBothBuckets(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 2, ChannelLimit: 5, MaxLimitsBeforeStop: 10})
	defer l.Close()

	// Seed both buckets and a violation.
	l.Check("user1", "discord:user")
	l.Check("user1", "discord:user")
	l.Check("user1", "discord:user") // breach → violation recorded
	l.CheckSummary("user1", "discord:user")

	// Verify state exists before reset.
	before := l.StateFor("user1", "discord:user")
	if len(before) == 0 {
		t.Fatal("expected state before reset")
	}

	changed := l.Reset("user1", "discord:user")
	if !changed {
		t.Fatal("Reset should return true when something was cleared")
	}

	// After reset: StateFor should return nothing.
	after := l.StateFor("user1", "discord:user")
	if len(after) != 0 {
		t.Fatalf("after Reset: expected 0 entries, got %d: %+v", len(after), after)
	}

	// And ListBlocked should not contain the target.
	for _, s := range l.ListBlocked() {
		if s.ID == "user1" {
			t.Fatalf("after Reset: user1 should not appear in ListBlocked: %+v", s)
		}
	}
}

func TestLimiter_Reset_NoChange(t *testing.T) {
	l := New(Config{TimingPeriod: 60, DMLimit: 5, ChannelLimit: 10})
	defer l.Close()

	changed := l.Reset("ghost", "discord:user")
	if changed {
		t.Fatal("Reset on unknown target should return false")
	}
}
