package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
)

// newTestLimiter constructs a Limiter with small limits to make it easy to
// exceed them in tests. The caller is responsible for calling Close() via
// t.Cleanup.
func newTestLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	l := ratelimit.New(ratelimit.Config{
		TimingPeriod:        300, // 5 min — won't expire during any test
		DMLimit:             20,
		ChannelLimit:        40,
		DMSummaryLimit:      10,
		ChannelSummaryLimit: 40,
		MaxLimitsBeforeStop: 5,
	})
	t.Cleanup(l.Close)
	return l
}

// seedBreach exceeds the alert bucket DM limit for id, creating a breached state.
func seedBreach(l *ratelimit.Limiter, id, dtype string, n int) {
	for range n {
		l.Check(id, dtype)
	}
}

// ratelimitCtx returns a CommandContext wired for ratelimit subgroup tests.
func ratelimitCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.AlertLimiter = newTestLimiter(t)
	ctx.Registry = bot.NewRegistry()
	return ctx
}

// runRatelimit is a convenience wrapper that calls the paRatelimit subgroup.
func runRatelimit(t *testing.T, ctx *bot.CommandContext, args ...string) []bot.Reply {
	t.Helper()
	cmd := &PoracleAdminCommand{}
	fullArgs := append([]string{"ratelimit"}, args...)
	return cmd.Run(ctx, fullArgs)
}

// ----------------------------------------------------------------------------

func TestRatelimit_HelpNoArgs(t *testing.T) {
	ctx := ratelimitCtx(t)

	replies := runRatelimit(t, ctx) // no args after "ratelimit"
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"list", "show", "reset", "userlist"} {
		if !containsStr(text, want) {
			t.Errorf("ratelimit help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

func TestRatelimit_ListEmpty(t *testing.T) {
	ctx := ratelimitCtx(t)
	// Fresh limiter — nothing blocked.

	replies := runRatelimit(t, ctx, "list")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "🟢") {
		t.Errorf("empty list should show 🟢 message, got:\n%s", text)
	}
	if !containsStr(text, "No targets") {
		t.Errorf("empty list should say 'No targets', got:\n%s", text)
	}
}

func TestRatelimit_ListPopulated(t *testing.T) {
	ctx := ratelimitCtx(t)

	// Breach the alert bucket for two users.
	// DM limit is 20, so we send 21 to exceed it.
	seedBreach(ctx.AlertLimiter, "user111", "discord:user", 21)
	// For the banned target: MaxLimitsBeforeStop=5 and TimingPeriod=300s.
	// We need at least 5 violations. To trigger violations in a single window
	// we cannot wait for window resets, so instead create a fresh limiter
	// with limit=1 and MaxLimitsBeforeStop=1 for the banned scenario.
	bannedLimiter := ratelimit.New(ratelimit.Config{
		TimingPeriod:        300,
		DMLimit:             1,
		ChannelLimit:        5,
		MaxLimitsBeforeStop: 1,
	})
	t.Cleanup(bannedLimiter.Close)
	// Two sends: first is at limit, second is JustBreached + Banned.
	bannedLimiter.Check("bannedUser", "discord:user")
	bannedLimiter.Check("bannedUser", "discord:user")

	ctx.AlertLimiter = bannedLimiter
	// Also add a breach (user111 won't show up here since we replaced the limiter).
	seedBreach(ctx.AlertLimiter, "user222", "discord:user", 3)

	replies := runRatelimit(t, ctx, "list")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	// Header must mention the bucket name.
	if !containsStr(text, "alert") {
		t.Errorf("list output should mention 'alert' bucket, got:\n%s", text)
	}
	// Both targets must appear.
	if !containsStr(text, "bannedUser") {
		t.Errorf("list output missing bannedUser, got:\n%s", text)
	}
	if !containsStr(text, "user222") {
		t.Errorf("list output missing user222, got:\n%s", text)
	}
	// Ban status for bannedUser.
	if !containsStr(text, "banned") {
		t.Errorf("list output should mention 'banned' status for bannedUser, got:\n%s", text)
	}
}

func TestRatelimit_ShowKnownTarget(t *testing.T) {
	ctx := ratelimitCtx(t)

	// Seed both buckets.
	ctx.AlertLimiter.Check("target1", "discord:user")
	ctx.AlertLimiter.CheckSummary("target1", "discord:user")

	// Use dtype/id form.
	replies := runRatelimit(t, ctx, "show", "discord:user/target1")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "discord:user/target1") {
		t.Errorf("show output missing target ID, got:\n%s", text)
	}
	// Both bucket headers must appear.
	if !containsStr(text, "alert") {
		t.Errorf("show output missing 'alert' bucket, got:\n%s", text)
	}
	if !containsStr(text, "summary") {
		t.Errorf("show output missing 'summary' bucket, got:\n%s", text)
	}
	// Count must appear.
	if !containsStr(text, "Count:") {
		t.Errorf("show output missing Count field, got:\n%s", text)
	}
}

func TestRatelimit_ShowUnknownTarget(t *testing.T) {
	ctx := ratelimitCtx(t)
	// StateFor returns empty for ghost.

	replies := runRatelimit(t, ctx, "show", "ghost")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "🟢") {
		t.Errorf("unknown target should show 🟢 message, got:\n%s", text)
	}
	if !containsStr(text, "ghost") {
		t.Errorf("unknown target reply should mention target name, got:\n%s", text)
	}
}

func TestRatelimit_ShowMissingArg(t *testing.T) {
	ctx := ratelimitCtx(t)

	replies := runRatelimit(t, ctx, "show")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Usage") {
		t.Errorf("missing-arg reply should show usage hint, got:\n%s", text)
	}
}

func TestRatelimit_ResetSuccess(t *testing.T) {
	ctx := ratelimitCtx(t)
	// Seed a breached state.
	seedBreach(ctx.AlertLimiter, "resetMe", "discord:user", 21)

	replies := runRatelimit(t, ctx, "reset", "resetMe")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "✅") {
		t.Errorf("reset success should contain ✅, got:\n%s", text)
	}
	if !containsStr(text, "resetMe") {
		t.Errorf("reset success should mention target, got:\n%s", text)
	}
	// admin_disable note must appear.
	if !containsStr(text, "admin_disable") {
		t.Errorf("reset success should include admin_disable note, got:\n%s", text)
	}
}

func TestRatelimit_ResetNoState(t *testing.T) {
	ctx := ratelimitCtx(t)
	// Nothing seeded for ghost.

	replies := runRatelimit(t, ctx, "reset", "ghost")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "ℹ️") {
		t.Errorf("no-state reset should contain ℹ️, got:\n%s", text)
	}
	if !containsStr(text, "ghost") {
		t.Errorf("no-state reset reply should mention target, got:\n%s", text)
	}
}

func TestRatelimit_ResetMissingArg(t *testing.T) {
	ctx := ratelimitCtx(t)

	replies := runRatelimit(t, ctx, "reset")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Usage") {
		t.Errorf("missing-arg reply should show usage hint, got:\n%s", text)
	}
}

func TestRatelimit_UserlistReroutes(t *testing.T) {
	ctx := ratelimitCtx(t)

	// Register a spy for cmd.userlist.
	spy := &spyCommand{
		name:    "cmd.userlist",
		replies: []bot.Reply{{Text: "userlist output"}},
	}
	ctx.Registry.Register(spy)

	replies := runRatelimit(t, ctx, "userlist")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}

	// Spy must have been called with ["disabled"].
	if len(spy.gotArgs) == 0 {
		t.Fatal("cmd.userlist spy was not called")
	}
	if spy.gotArgs[0] != "disabled" {
		t.Errorf("cmd.userlist should be called with [\"disabled\"], got %v", spy.gotArgs)
	}

	// Reply should be the spy's reply, not a ratelimit-generated one.
	if replies[0].Text != "userlist output" {
		t.Errorf("expected userlist spy output, got %q", replies[0].Text)
	}
}

func TestRatelimit_UnknownSub(t *testing.T) {
	ctx := ratelimitCtx(t)

	replies := runRatelimit(t, ctx, "bogus")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	if !containsStr(text, "ratelimit") {
		t.Errorf("unknown-sub reply should mention 'ratelimit', got:\n%s", text)
	}
}

func TestRatelimit_NotConfigured(t *testing.T) {
	// For each relevant subcommand, nil AlertLimiter must return a graceful message.
	for _, sub := range [][]string{
		{"list"},
		{"show", "someTarget"},
		{"reset", "someTarget"},
	} {
		t.Run(sub[0], func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = true
			ctx.AlertLimiter = nil
			ctx.Registry = bot.NewRegistry()

			replies := runRatelimit(t, ctx, sub...)
			if len(replies) == 0 {
				t.Fatal("expected at least one reply, got none")
			}
			text := replies[0].Text
			if text == "" {
				t.Error("nil AlertLimiter reply must be non-empty")
			}
			if !containsStr(text, "not configured") {
				t.Errorf("nil AlertLimiter should say 'not configured', got:\n%s", text)
			}
		})
	}
}
