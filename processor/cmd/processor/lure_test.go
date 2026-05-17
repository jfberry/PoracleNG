package main

import (
	"os"
	"strings"
	"testing"
)

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
