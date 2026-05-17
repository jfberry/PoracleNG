package telegrambot

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

// telegramMockPauser implements bot.PauseChecker for Telegram bot tests.
type telegramMockPauser struct{ paused bool }

func (m *telegramMockPauser) PauseState() (bool, string, time.Time) {
	return m.paused, "test-reason", time.Time{}
}

const telegramTestSuffix = "🔧 Maintenance mode is active — alerts are not being delivered."

// TestTelegram_MaintenanceSuffix_NotPaused verifies that no suffix is added when
// the dispatcher is not paused.
func TestTelegram_MaintenanceSuffix_NotPaused(t *testing.T) {
	replies := []bot.Reply{{Text: "tracked pikachu ✅"}}
	got := bot.ApplyMaintenanceSuffix(replies, &telegramMockPauser{paused: false}, telegramTestSuffix)
	if len(got) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got))
	}
	if got[0].Text != "tracked pikachu ✅" {
		t.Errorf("unexpected text change: %q", got[0].Text)
	}
}

// TestTelegram_MaintenanceSuffix_PausedTextReply verifies that the suffix is
// appended on a new line to the last text reply when paused.
func TestTelegram_MaintenanceSuffix_PausedTextReply(t *testing.T) {
	replies := []bot.Reply{{Text: "tracked pikachu ✅"}}
	got := bot.ApplyMaintenanceSuffix(replies, &telegramMockPauser{paused: true}, telegramTestSuffix)
	if len(got) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got))
	}
	want := "tracked pikachu ✅\n" + telegramTestSuffix
	if got[0].Text != want {
		t.Errorf("got %q, want %q", got[0].Text, want)
	}
}

// TestTelegram_MaintenanceSuffix_PausedEmbedReply verifies that a new plain-text
// reply carrying the suffix is appended when the last reply is embed-only.
func TestTelegram_MaintenanceSuffix_PausedEmbedReply(t *testing.T) {
	replies := []bot.Reply{{Embed: []byte(`{"embed": {}}`)}}
	got := bot.ApplyMaintenanceSuffix(replies, &telegramMockPauser{paused: true}, telegramTestSuffix)
	if len(got) != 2 {
		t.Fatalf("expected 2 replies (embed + suffix), got %d", len(got))
	}
	if got[1].Text != telegramTestSuffix {
		t.Errorf("suffix reply text = %q, want %q", got[1].Text, telegramTestSuffix)
	}
}

// TestTelegram_MaintenanceSuffix_OnlyOnLastReply verifies the suffix appears
// exactly once (on the last reply) in a multi-reply command output.
func TestTelegram_MaintenanceSuffix_OnlyOnLastReply(t *testing.T) {
	replies := []bot.Reply{
		{Text: "chunk 1"},
		{Text: "chunk 2"},
		{Text: "chunk 3"},
	}
	got := bot.ApplyMaintenanceSuffix(replies, &telegramMockPauser{paused: true}, telegramTestSuffix)
	if len(got) != 3 {
		t.Fatalf("expected 3 replies, got %d", len(got))
	}
	if got[0].Text != "chunk 1" {
		t.Errorf("first reply modified: %q", got[0].Text)
	}
	if got[1].Text != "chunk 2" {
		t.Errorf("second reply modified: %q", got[1].Text)
	}
	wantLast := "chunk 3\n" + telegramTestSuffix
	if got[2].Text != wantLast {
		t.Errorf("last reply = %q, want %q", got[2].Text, wantLast)
	}
}

// TestTelegram_MaintenanceSuffix_NilDispatcher_NoCrash verifies no panic when
// the PauseChecker is nil (e.g. test contexts with no dispatcher wired).
func TestTelegram_MaintenanceSuffix_NilDispatcher_NoCrash(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic with nil checker: %v", r)
		}
	}()
	replies := []bot.Reply{{Text: "hello"}}
	got := bot.ApplyMaintenanceSuffix(replies, nil, telegramTestSuffix)
	if len(got) != 1 || got[0].Text != "hello" {
		t.Errorf("unexpected change with nil checker: %+v", got)
	}
}
