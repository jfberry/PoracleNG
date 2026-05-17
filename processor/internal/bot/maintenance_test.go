package bot

import (
	"testing"
	"time"
)

const testSuffix = "🔧 Maintenance mode is active — alerts are not being delivered."

// mockPauser implements PauseChecker for tests.
type mockPauser struct{ paused bool }

func (m *mockPauser) IsPaused() bool                            { return m.paused }
func (m *mockPauser) PauseState() (bool, string, time.Time) {
	return m.paused, "test", time.Time{}
}

// pausedChecker always reports paused.
func pausedChecker() PauseChecker { return &mockPauser{paused: true} }

// runningChecker always reports not paused.
func runningChecker() PauseChecker { return &mockPauser{paused: false} }

// TestApplyMaintenanceSuffix_NotPaused confirms no suffix is added when delivery is running.
func TestApplyMaintenanceSuffix_NotPaused(t *testing.T) {
	replies := []Reply{{Text: "hello"}}
	got := ApplyMaintenanceSuffix(replies, runningChecker(), testSuffix)
	if len(got) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got))
	}
	if got[0].Text != "hello" {
		t.Errorf("text changed unexpectedly: %q", got[0].Text)
	}
}

// TestApplyMaintenanceSuffix_PausedTextReply confirms the suffix is appended on a new line.
func TestApplyMaintenanceSuffix_PausedTextReply(t *testing.T) {
	replies := []Reply{{Text: "tracked pikachu ✅"}}
	got := ApplyMaintenanceSuffix(replies, pausedChecker(), testSuffix)
	if len(got) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got))
	}
	want := "tracked pikachu ✅\n" + testSuffix
	if got[0].Text != want {
		t.Errorf("got text %q, want %q", got[0].Text, want)
	}
}

// TestApplyMaintenanceSuffix_PausedEmbedReply confirms a new trailing text reply is
// added when the last reply has no text (embed-only).
func TestApplyMaintenanceSuffix_PausedEmbedReply(t *testing.T) {
	replies := []Reply{{Embed: []byte(`{"embed": {}}`)}}
	got := ApplyMaintenanceSuffix(replies, pausedChecker(), testSuffix)
	if len(got) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(got))
	}
	if got[1].Text != testSuffix {
		t.Errorf("trailing reply text = %q, want %q", got[1].Text, testSuffix)
	}
	// Original embed reply is untouched.
	if got[0].Text != "" {
		t.Errorf("embed reply text modified unexpectedly: %q", got[0].Text)
	}
}

// TestApplyMaintenanceSuffix_OnlyOnLastReply confirms the suffix appears only once in
// a multi-reply batch (on the last entry).
func TestApplyMaintenanceSuffix_OnlyOnLastReply(t *testing.T) {
	replies := []Reply{
		{Text: "chunk 1"},
		{Text: "chunk 2"},
		{Text: "chunk 3"},
	}
	got := ApplyMaintenanceSuffix(replies, pausedChecker(), testSuffix)
	if len(got) != 3 {
		t.Fatalf("expected 3 replies, got %d", len(got))
	}
	// Only the last reply should have the suffix.
	if got[0].Text != "chunk 1" {
		t.Errorf("first reply modified: %q", got[0].Text)
	}
	if got[1].Text != "chunk 2" {
		t.Errorf("second reply modified: %q", got[1].Text)
	}
	wantLast := "chunk 3\n" + testSuffix
	if got[2].Text != wantLast {
		t.Errorf("last reply = %q, want %q", got[2].Text, wantLast)
	}
}

// TestApplyMaintenanceSuffix_NilChecker confirms no panic when checker is nil.
func TestApplyMaintenanceSuffix_NilChecker(t *testing.T) {
	replies := []Reply{{Text: "hello"}}
	got := ApplyMaintenanceSuffix(replies, nil, testSuffix)
	if len(got) != 1 || got[0].Text != "hello" {
		t.Errorf("unexpected change when checker is nil: %+v", got)
	}
}

// TestApplyMaintenanceSuffix_EmptyReplies confirms empty slices are handled safely.
func TestApplyMaintenanceSuffix_EmptyReplies(t *testing.T) {
	got := ApplyMaintenanceSuffix(nil, pausedChecker(), testSuffix)
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d replies", len(got))
	}
	got2 := ApplyMaintenanceSuffix([]Reply{}, pausedChecker(), testSuffix)
	if len(got2) != 0 {
		t.Errorf("expected empty result, got %d replies", len(got2))
	}
}

// TestApplyMaintenanceSuffix_ImageOnlyReply confirms a new text reply is appended
// when the last reply is image-only (no Text, no Embed).
func TestApplyMaintenanceSuffix_ImageOnlyReply(t *testing.T) {
	replies := []Reply{{ImageURL: "https://example.com/map.png"}}
	got := ApplyMaintenanceSuffix(replies, pausedChecker(), testSuffix)
	if len(got) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(got))
	}
	if got[1].Text != testSuffix {
		t.Errorf("trailing reply text = %q, want %q", got[1].Text, testSuffix)
	}
}
