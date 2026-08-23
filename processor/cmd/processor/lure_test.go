package main

import (
	"os"
	"strings"
	"testing"
)

// TestHasActiveLure covers the genuine-active-lure gate. Golbat's unified
// pokestop webhook can carry a leftover lure_id and a past lure_expiration on
// incident/showcase stops; without this gate `!lure everything` (lure_id 0 =
// any) fires on them. Valid lure IDs are 501+; an active lure expires in the
// future.
func TestHasActiveLure(t *testing.T) {
	const now = int64(1_000_000)
	cases := []struct {
		name       string
		lureID     int
		expiration int64
		want       bool
	}{
		{"active normal lure", 501, now + 600, true},
		{"active golden lure", 506, now + 1, true},
		{"no lure id", 0, now + 600, false},
		{"boundary id 500 invalid", 500, now + 600, false},
		{"valid id but expired", 501, now - 1, false},
		{"valid id expiring exactly now", 501, now, false},
		{"stale showcase leftover", 502, now - 3600, false},
	}
	for _, c := range cases {
		if got := hasActiveLure(c.lureID, c.expiration, now); got != c.want {
			t.Errorf("%s: hasActiveLure(%d, %d, %d) = %v, want %v",
				c.name, c.lureID, c.expiration, now, got, c.want)
		}
	}
}

// TestHasActiveLure_RealShowcaseStopWithStaleLure locks the exact production
// webhook that surfaced the bug: a pokestop actively showcasing (future
// showcase_expiry, populated showcase_rankings) that still carries a
// lure_expiration from ~2 days earlier with a valid lure_id 501. Pre-gate,
// `!lure everything` fired on it. The id > 500 half doesn't catch it (501 is
// valid) — the expiry half must.
func TestHasActiveLure_RealShowcaseStopWithStaleLure(t *testing.T) {
	const now = int64(1784062072)     // webhook `updated`
	const lureExpiration = 1783895246 // ~1.93 days before `now`
	if hasActiveLure(501, lureExpiration, now) {
		t.Fatalf("stale lure (expired %d s ago) on an active-showcase stop must not count as active", now-lureExpiration)
	}
}

// TestProcessLure_GatesOnActiveLure verifies the gate is actually wired into
// the handler (predicate-defined-but-not-called would otherwise pass). Source-
// grep for the same reason as TestProcessLure_SetsEditKey.
func TestProcessLure_GatesOnActiveLure(t *testing.T) {
	src, err := os.ReadFile("lure.go")
	if err != nil {
		t.Fatalf("read lure.go: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(src)), " ")
	if !strings.Contains(normalized, "hasActiveLure(lure.LureID, lure.LureExpiration") {
		t.Fatal("lure.go must call hasActiveLure(lure.LureID, lure.LureExpiration, ...) and return early — otherwise expired/no-lure pokestops (e.g. showcases) still alert `!lure everything` users")
	}
}

// TestProcessLure_SetsEditKey: lure RenderJobs must carry
// EditKey = "lure:<pokestop>:<lure_id>" so users with the edit
// flag get revised-expiration updates edited in place. Source-grep
// on purpose: ProcessLure needs a fully-wired ProcessorService to
// drive end-to-end. Per-user gating is covered by renderer tests.
func TestProcessLure_SetsEditKey(t *testing.T) {
	src, err := os.ReadFile("lure.go")
	if err != nil {
		t.Fatalf("read lure.go: %v", err)
	}
	normalized := strings.Join(strings.Fields(string(src)), " ")
	want := `EditKey: fmt.Sprintf("lure:%s:%d", lure.PokestopID, lure.LureID)`
	if !strings.Contains(normalized, want) {
		t.Fatalf("lure.go must set RenderJob.EditKey to %q — without it, the edit flag on a user's lure rule has no effect and revised-expiration lures arrive as duplicates", want)
	}
}
