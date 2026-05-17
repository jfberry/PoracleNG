package main

import (
	"os"
	"strings"
	"testing"
)

// TestProcessLure_SetsEditKey is a source-level guard: lure
// RenderJobs must carry EditKey = "lure:<pokestop>:<lure_id>" so
// users with the edit flag (Clean bit 2) get the revised-expiration
// update edited in place instead of receiving a duplicate alert.
// Without this, Golbat's mid-lure expiration revisions show up as
// back-to-back "Lure at X" messages — exactly the screenshot the
// user reported. Source-grep on purpose: ProcessLure needs a fully
// constructed ProcessorService (matcher, enricher, render channel)
// which is impractical to assemble here. The per-user gating
// (only-set-for-edit-flag) is exercised at the renderer layer.
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
