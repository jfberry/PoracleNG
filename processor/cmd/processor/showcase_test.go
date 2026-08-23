package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestShowcaseActive — the core gate. showcase_expiry is the contest end time;
// only a future expiry means a live contest (Golbat never clears the fields, so
// stale showcases linger on unrelated pokéstop fires).
func TestShowcaseActive(t *testing.T) {
	const now = int64(1_000_000)
	if !showcaseActive(now+1, now) {
		t.Error("future expiry must be active")
	}
	if showcaseActive(now, now) {
		t.Error("expiry == now must be inactive")
	}
	if showcaseActive(now-1, now) {
		t.Error("past expiry must be inactive")
	}
	if showcaseActive(0, now) {
		t.Error("absent expiry (0) must be inactive")
	}
}

// TestShowcaseFingerprint — dedup key summarises rank-1 state. Golbat re-fires
// only on rank-1 movement, so the fingerprint must change when rank-1 changes
// but stay stable across last_update churn and rank 2/3 shuffles.
func TestShowcaseFingerprint(t *testing.T) {
	if got := showcaseFingerprint(nil); got != "0:none" {
		t.Errorf("nil rankings = %q, want 0:none", got)
	}
	if got := showcaseFingerprint(json.RawMessage(`{"total_entries":0,"contest_entries":[]}`)); got != "0:none" {
		t.Errorf("empty entries = %q, want 0:none", got)
	}

	withRank1 := json.RawMessage(`{"total_entries":3,"contest_entries":[{"rank":1,"pokemon_id":679,"score":1032.5},{"rank":2,"pokemon_id":51,"score":1032.2}]}`)
	if got := showcaseFingerprint(withRank1); got != "3:679@1032.5" {
		t.Errorf("fingerprint = %q, want 3:679@1032.5", got)
	}

	// last_update churn + a rank-2 change must NOT move the fingerprint.
	sameRank1 := json.RawMessage(`{"total_entries":3,"last_update":9999999,"contest_entries":[{"rank":1,"pokemon_id":679,"score":1032.5},{"rank":2,"pokemon_id":99,"score":1.0}]}`)
	if got := showcaseFingerprint(sameRank1); got != "3:679@1032.5" {
		t.Errorf("last_update / rank-2 change must not affect fingerprint, got %q", got)
	}

	// A rank-1 change must move the fingerprint (so the edit path sees it).
	newRank1 := json.RawMessage(`{"total_entries":4,"contest_entries":[{"rank":1,"pokemon_id":25,"score":50}]}`)
	if got := showcaseFingerprint(newRank1); got == "3:679@1032.5" {
		t.Errorf("rank-1 change must produce a new fingerprint, got %q", got)
	}
}

// TestProcessShowcase_Wiring verifies the handler wires the gate, dedup, edit
// key, clean-TTH override, and incident template — the pieces that make it a
// self-updating showcase alert. Source-grep because ProcessShowcase needs a
// fully-wired ProcessorService (same convention as lure/invasion tests).
func TestProcessShowcase_Wiring(t *testing.T) {
	src, err := os.ReadFile("showcase.go")
	if err != nil {
		t.Fatalf("read showcase.go: %v", err)
	}
	n := strings.Join(strings.Fields(string(src)), " ")
	for _, want := range []string{
		"showcaseActive(sc.ShowcaseExpiry",               // expiry gate
		"CheckShowcase(sc.PokestopID, sc.ShowcaseExpiry", // dedup with fingerprint
		`EditKey: fmt.Sprintf("showcase:%s:%d"`,          // edit-mode key
		"OverrideCleanTTH: sc.ShowcaseExpiry",            // clean-deletion TTH = contest end
		`TemplateType: "showcase"`,                       // dedicated showcase display model
	} {
		if !strings.Contains(n, want) {
			t.Errorf("showcase.go missing wiring: %q", want)
		}
	}
}
