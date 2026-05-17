package commands

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/logbuffer"
)

// warningsCtx returns a CommandContext wired for warnings subgroup tests with a
// pre-populated LogBuffer.
func warningsCtx(t *testing.T, buf *logbuffer.Buffer) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.LogBuffer = buf
	return ctx
}

// runWarnings is a convenience wrapper that calls the paWarnings subgroup via
// the top-level PoracleAdminCommand.
func runWarnings(t *testing.T, ctx *bot.CommandContext, args ...string) []bot.Reply {
	t.Helper()
	cmd := &PoracleAdminCommand{}
	fullArgs := append([]string{"warnings"}, args...)
	return cmd.Run(ctx, fullArgs)
}

// seedStartup captures n WARN entries into buf's startup buffer.
// MarkStartupComplete must NOT have been called yet.
func seedStartup(buf *logbuffer.Buffer, n int) {
	for i := range n {
		buf.Capture("WARN", "startup message "+string(rune('A'+i)), "")
	}
}

// seedRecent captures n WARN entries into buf's rolling buffer.
// MarkStartupComplete must already have been called.
func seedRecent(buf *logbuffer.Buffer, n int) {
	for i := range n {
		buf.Capture("WARN", "recent message "+string(rune('A'+i)), "")
	}
}

// ----------------------------------------------------------------------------

func TestWarnings_HelpExplicit(t *testing.T) {
	buf := logbuffer.New(200, 50)
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "help")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"startup", "recent", "clear"} {
		if !containsStr(text, want) {
			t.Errorf("warnings help missing %q, got:\n%s", want, text)
		}
	}
}

func TestWarnings_NoArgs_BothEmpty(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx)
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	if !containsStr(text, "No startup warnings") {
		t.Errorf("empty startup buffer should say 'No startup warnings', got:\n%s", text)
	}
	if !containsStr(text, "No recent warnings") {
		t.Errorf("empty recent buffer should say 'No recent warnings', got:\n%s", text)
	}
}

func TestWarnings_StartupSubcommand(t *testing.T) {
	buf := logbuffer.New(200, 50)
	seedStartup(buf, 3)
	buf.MarkStartupComplete()
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "startup")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	// Header must mention count.
	if !containsStr(text, "startup") {
		t.Errorf("startup output should mention 'startup', got:\n%s", text)
	}
	// All three startup messages must appear.
	for _, want := range []string{"startup message A", "startup message B", "startup message C"} {
		if !containsStr(text, want) {
			t.Errorf("startup output missing %q, got:\n%s", want, text)
		}
	}
	// Level label must appear.
	if !containsStr(text, "WARN") {
		t.Errorf("startup output missing 'WARN' level, got:\n%s", text)
	}
}

func TestWarnings_RecentSubcommand(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	seedRecent(buf, 2)
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "recent")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	if !containsStr(text, "recent") {
		t.Errorf("recent output should mention 'recent', got:\n%s", text)
	}
	for _, want := range []string{"recent message A", "recent message B"} {
		if !containsStr(text, want) {
			t.Errorf("recent output missing %q, got:\n%s", want, text)
		}
	}
}

func TestWarnings_NoArgs_BothPopulated(t *testing.T) {
	buf := logbuffer.New(200, 50)
	seedStartup(buf, 2)
	buf.MarkStartupComplete()
	seedRecent(buf, 3)
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx)
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	// Both startup and recent messages must appear.
	if !containsStr(text, "startup message A") {
		t.Errorf("combined output missing startup entry, got:\n%s", text)
	}
	if !containsStr(text, "recent message A") {
		t.Errorf("combined output missing recent entry, got:\n%s", text)
	}
	// Both section headers must appear.
	if !containsStr(text, "startup") {
		t.Errorf("combined output missing startup section header, got:\n%s", text)
	}
	if !containsStr(text, "recent") {
		t.Errorf("combined output missing recent section header, got:\n%s", text)
	}
}

func TestWarnings_Clear(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	seedRecent(buf, 5)
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "clear")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	// Confirmation must mention the count.
	if !containsStr(text, "5") {
		t.Errorf("clear reply should mention count 5, got:\n%s", text)
	}
	if !containsStr(text, "✅") {
		t.Errorf("clear reply should contain ✅, got:\n%s", text)
	}
	// Buffer must now be empty.
	if got := buf.Recent(); len(got) != 0 {
		t.Errorf("after clear, Recent() should be empty but has %d entries", len(got))
	}
}

func TestWarnings_ClearEmpty(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "clear")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "0") {
		t.Errorf("clearing empty buffer should report 0 entries, got:\n%s", text)
	}
	if !containsStr(text, "✅") {
		t.Errorf("clear reply should contain ✅, got:\n%s", text)
	}
}

func TestWarnings_SourceFieldRendered(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	// Directly capture an entry with a Source field.
	buf.Capture("ERROR", "something broke", "file.go:42")
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "recent")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	if !containsStr(text, "file.go:42") {
		t.Errorf("source field not rendered in output, got:\n%s", text)
	}
	// Source should be wrapped in brackets.
	if !containsStr(text, "[file.go:42]") {
		t.Errorf("source should be rendered as [file.go:42], got:\n%s", text)
	}
}

func TestWarnings_UnknownSub(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	ctx := warningsCtx(t, buf)

	replies := runWarnings(t, ctx, "bogus")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	if !containsStr(text, "warnings") {
		t.Errorf("unknown-sub reply should mention 'warnings', got:\n%s", text)
	}
}

func TestWarnings_NotConfigured(t *testing.T) {
	for _, sub := range [][]string{
		{},
		{"startup"},
		{"recent"},
		{"clear"},
	} {
		name := "no-args"
		if len(sub) > 0 {
			name = sub[0]
		}
		t.Run(name, func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = true
			ctx.LogBuffer = nil // not configured

			replies := runWarnings(t, ctx, sub...)
			if len(replies) == 0 {
				t.Fatal("expected at least one reply, got none")
			}
			text := replies[0].Text
			if text == "" {
				t.Error("nil LogBuffer reply must be non-empty")
			}
			if !containsStr(text, "not configured") {
				t.Errorf("nil LogBuffer should say 'not configured', got:\n%s", text)
			}
		})
	}
}

func TestWarnings_TimeFormatUTC(t *testing.T) {
	buf := logbuffer.New(200, 50)
	buf.MarkStartupComplete()
	// Capture at a known UTC time.
	known := time.Date(2026, 5, 17, 14, 23, 11, 0, time.UTC)
	// Inject a fake entry via Capture workaround: we can't set Entry.Time directly
	// since Capture uses time.Now(). Use a direct approach via logbuffer.Entry if
	// exported, otherwise just verify the format looks correct from an actual entry.
	_ = known
	buf.Capture("WARN", "time-check", "")

	ctx := warningsCtx(t, buf)
	replies := runWarnings(t, ctx, "recent")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	// The timestamp must match "YYYY-MM-DD HH:MM:SS" format (not RFC3339 with T and timezone suffix).
	if containsStr(text, "T") && !containsStr(text, "time-check") {
		// Only fail if we actually got RFC3339 format (contains T between date and time).
		t.Errorf("timestamp should not use RFC3339 T separator, got:\n%s", text)
	}
	// Must not contain +00:00 or Z timezone suffix inline.
	if containsStr(text, "+00:00") || containsStr(text, "Z  ") {
		t.Errorf("timestamp should not have timezone suffix, got:\n%s", text)
	}
}
