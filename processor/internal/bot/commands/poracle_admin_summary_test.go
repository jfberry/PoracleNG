package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// summaryCtx returns a CommandContext wired for summary subgroup tests.
// It always has IsAdmin=true. The SummaryBuffer is set to a fresh empty buffer
// unless the caller overrides it after.
func summaryCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.SummaryBuffer = tracker.NewSummaryBuffer("")
	return ctx
}

// runSummary is a convenience wrapper that calls the paSummary subgroup.
func runSummary(t *testing.T, ctx *bot.CommandContext, args ...string) []bot.Reply {
	t.Helper()
	cmd := &PoracleAdminCommand{}
	fullArgs := append([]string{"summary"}, args...)
	return cmd.Run(ctx, fullArgs)
}

// seedBuffer adds quest entries to the buffer for a given user.
func seedBuffer(sb *tracker.SummaryBuffer, humanID string, quests ...tracker.BufferedQuest) {
	for _, q := range quests {
		sb.Append(humanID, "quest", q)
	}
}

// ----------------------------------------------------------------------------
// 1. Help / no args
// ----------------------------------------------------------------------------

func TestSummary_HelpNoArgs(t *testing.T) {
	ctx := summaryCtx(t)

	replies := runSummary(t, ctx) // no args after "summary"
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"list", "show", "fire"} {
		if !containsStr(text, want) {
			t.Errorf("summary help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

// ----------------------------------------------------------------------------
// 2. list — empty
// ----------------------------------------------------------------------------

func TestSummary_ListEmpty(t *testing.T) {
	ctx := summaryCtx(t)
	// Buffer is empty.

	replies := runSummary(t, ctx, "list")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "🟢") {
		t.Errorf("empty list should show 🟢 message, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 3. list — populated
// ----------------------------------------------------------------------------

func TestSummary_ListPopulated(t *testing.T) {
	ctx := summaryCtx(t)

	seedBuffer(ctx.SummaryBuffer, "discord:user/111",
		tracker.BufferedQuest{RewardType: 2, Reward: 1300, PokestopID: "s1", ExpiresAt: 9999},
		tracker.BufferedQuest{RewardType: 2, Reward: 1301, PokestopID: "s2", ExpiresAt: 9999},
	)
	seedBuffer(ctx.SummaryBuffer, "discord:user/222",
		tracker.BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "s3", ExpiresAt: 9999},
	)

	replies := runSummary(t, ctx, "list")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := ""
	for _, r := range replies {
		text += r.Text
	}
	if !containsStr(text, "discord:user/111") {
		t.Errorf("list output missing first user, got:\n%s", text)
	}
	if !containsStr(text, "discord:user/222") {
		t.Errorf("list output missing second user, got:\n%s", text)
	}
	// Should show total entry count.
	if !containsStr(text, "3") {
		t.Errorf("list output should show total entry count, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 4. show — known user
// ----------------------------------------------------------------------------

func TestSummary_ShowKnownUser(t *testing.T) {
	ctx := summaryCtx(t)

	seedBuffer(ctx.SummaryBuffer, "discord:user/123",
		tracker.BufferedQuest{RewardType: 2, Reward: 1300, PokestopID: "s1", ExpiresAt: 9999},
		tracker.BufferedQuest{RewardType: 7, Reward: 25, PokestopID: "s2", ExpiresAt: 9999},
	)

	replies := runSummary(t, ctx, "show", "discord:user/123")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := ""
	for _, r := range replies {
		text += r.Text
	}
	if !containsStr(text, "discord:user/123") {
		t.Errorf("show output missing user ID, got:\n%s", text)
	}
	// Should list the alertType.
	if !containsStr(text, "quest") {
		t.Errorf("show output missing 'quest' alertType, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 5. show — empty user (nothing buffered for that user)
// ----------------------------------------------------------------------------

func TestSummary_ShowEmptyUser(t *testing.T) {
	ctx := summaryCtx(t)
	// No entries seeded for this user.

	replies := runSummary(t, ctx, "show", "discord:user/nobody")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "🟢") {
		t.Errorf("empty user show should contain 🟢, got:\n%s", text)
	}
	if !containsStr(text, "discord:user/nobody") {
		t.Errorf("empty user show should mention the user ID, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 6. show — missing arg
// ----------------------------------------------------------------------------

func TestSummary_ShowMissingArg(t *testing.T) {
	ctx := summaryCtx(t)

	replies := runSummary(t, ctx, "show")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Usage") {
		t.Errorf("missing-arg reply should show usage hint, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 7. fire — success
// ----------------------------------------------------------------------------

func TestSummary_FireSuccess(t *testing.T) {
	ctx := summaryCtx(t)

	// Seed some entries so count > 0.
	seedBuffer(ctx.SummaryBuffer, "discord:user/fire1",
		tracker.BufferedQuest{RewardType: 2, Reward: 1300, PokestopID: "s1", ExpiresAt: 9999},
		tracker.BufferedQuest{RewardType: 2, Reward: 1301, PokestopID: "s2", ExpiresAt: 9999},
	)

	var dispatched []string
	ctx.SummaryDispatch = func(humanID, alertType string) {
		dispatched = append(dispatched, humanID+"/"+alertType)
	}

	replies := runSummary(t, ctx, "fire", "discord:user/fire1")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "✅") {
		t.Errorf("fire success should contain ✅, got:\n%s", text)
	}
	if !containsStr(text, "discord:user/fire1") {
		t.Errorf("fire success should mention user, got:\n%s", text)
	}
	if !containsStr(text, "quest") {
		t.Errorf("fire success should mention alertType, got:\n%s", text)
	}

	// Dispatch should have been called.
	if len(dispatched) == 0 {
		t.Error("SummaryDispatch was not called")
	} else if dispatched[0] != "discord:user/fire1/quest" {
		t.Errorf("SummaryDispatch called with %q, want %q", dispatched[0], "discord:user/fire1/quest")
	}
}

// ----------------------------------------------------------------------------
// 8. fire — explicit alertType
// ----------------------------------------------------------------------------

func TestSummary_FireExplicitAlertType(t *testing.T) {
	ctx := summaryCtx(t)
	ctx.SummaryBuffer.Append("discord:user/fire2", "raid",
		tracker.BufferedQuest{PokestopID: "s1", ExpiresAt: 9999})

	var gotType string
	ctx.SummaryDispatch = func(humanID, alertType string) {
		gotType = alertType
	}

	replies := runSummary(t, ctx, "fire", "discord:user/fire2", "raid")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if gotType != "raid" {
		t.Errorf("explicit alertType 'raid' not passed to dispatch, got %q", gotType)
	}
}

// ----------------------------------------------------------------------------
// 9. fire — missing arg
// ----------------------------------------------------------------------------

func TestSummary_FireMissingArg(t *testing.T) {
	ctx := summaryCtx(t)

	replies := runSummary(t, ctx, "fire")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Usage") {
		t.Errorf("missing-arg reply should show usage hint, got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 10. unknown subcommand
// ----------------------------------------------------------------------------

func TestSummary_UnknownSub(t *testing.T) {
	ctx := summaryCtx(t)

	replies := runSummary(t, ctx, "bogus")
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	if !containsStr(text, "summary") {
		t.Errorf("unknown-sub reply should mention 'summary', got:\n%s", text)
	}
}

// ----------------------------------------------------------------------------
// 11. not configured (nil SummaryBuffer)
// ----------------------------------------------------------------------------

func TestSummary_NotConfigured(t *testing.T) {
	for _, sub := range [][]string{
		{"list"},
		{"show", "someUser"},
		{"fire", "someUser"},
	} {
		t.Run(sub[0], func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = true
			ctx.SummaryBuffer = nil // explicitly nil

			replies := runSummary(t, ctx, sub...)
			if len(replies) == 0 {
				t.Fatal("expected at least one reply, got none")
			}
			text := replies[0].Text
			if text == "" {
				t.Error("nil SummaryBuffer reply must be non-empty")
			}
			if !containsStr(text, "not configured") {
				t.Errorf("nil SummaryBuffer should say 'not configured', got:\n%s", text)
			}
		})
	}
}
