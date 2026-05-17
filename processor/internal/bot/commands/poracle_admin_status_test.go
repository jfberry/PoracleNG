package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/ratelimit"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// statusCtx returns a CommandContext with the standard fixture from
// testCtx, plus all the optional Phase-2 introspection closures stubbed
// out as either nil or "happy" defaults. Individual tests then override
// just the fields they care about.
func statusCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ProcessStart = time.Now().Add(-2 * time.Hour)
	// Healthy defaults — overridden per test.
	ctx.WebhookRate = func() webhook.RateSnapshot {
		return webhook.RateSnapshot{
			Per5Min:  500,
			Per15Min: 1500,
			Per60Min: 6000,
			PerType: map[string]int{
				"pokemon": 4000,
				"raid":    1500,
				"quest":   500,
			},
		}
	}
	ctx.DiscordRate = func() delivery.DiscordRateSnapshot {
		return delivery.DiscordRateSnapshot{
			GlobalTokens:   50,
			GlobalCapacity: 50,
			Recent429Count: 0,
		}
	}
	ctx.TelegramRate = func() delivery.TelegramRateSnapshot {
		return delivery.TelegramRateSnapshot{
			Recent429Count:      0,
			CurrentBackoffUntil: time.Time{},
		}
	}
	// AlertLimiter with default config + no traffic → no blocked targets.
	// Stash the constructed limiter in a local so the cleanup closure
	// always closes the original, even if a test later replaces
	// ctx.AlertLimiter on the context.
	defaultLim := ratelimit.New(ratelimit.Config{
		TimingPeriod:        240,
		DMLimit:             20,
		ChannelLimit:        40,
		DMSummaryLimit:      10,
		ChannelSummaryLimit: 40,
		MaxLimitsBeforeStop: 10,
	})
	ctx.AlertLimiter = defaultLim
	t.Cleanup(defaultLim.Close)
	return ctx
}

// sectionTail returns the substring of out starting at startIdx and
// ending just before the next bold section header (the **NextSection**
// marker that follows). Used by tests to scope substring assertions
// to a specific section of the rendered report.
func sectionTail(out string, startIdx int) string {
	tail := out[startIdx:]
	// Skip a few bytes so the **Header** itself doesn't match.
	const skipPast = 16
	cursor := skipPast
	if cursor >= len(tail) {
		return tail
	}
	if cut := strings.Index(tail[cursor:], "**"); cut >= 0 {
		return tail[:cursor+cut]
	}
	return tail
}

// firstReplyText returns the first reply's text or fails the test.
func firstReplyText(t *testing.T, replies []bot.Reply) string {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	return replies[0].Text
}

func TestStatusReport_HappyPath(t *testing.T) {
	ctx := statusCtx(t)

	out := firstReplyText(t, statusReport(ctx, false))

	// Header always present.
	if !strings.Contains(out, "Poracle Admin") {
		t.Errorf("missing header line in output:\n%s", out)
	}
	// Healthy webhooks → 🟢 indicator somewhere in webhooks section.
	if !strings.Contains(out, indicatorGreen) {
		t.Errorf("expected at least one 🟢 indicator in healthy snapshot:\n%s", out)
	}
	if strings.Contains(out, indicatorRed) {
		t.Errorf("did not expect any 🔴 in healthy snapshot:\n%s", out)
	}
	// Pause banner must NOT appear when nothing is paused.
	if strings.Contains(out, "PAUSED") {
		t.Errorf("paused banner unexpectedly present:\n%s", out)
	}
}

func TestStatusReport_PausedBanner(t *testing.T) {
	ctx := statusCtx(t)
	// Construct a Dispatcher with no senders, then pause it. We only
	// need PauseState() to return paused=true.
	disp := delivery.NewDispatcherWithSenders(nil, nil, 0, delivery.QueueConfig{})
	disp.Pause("test maintenance window")
	ctx.Dispatcher = disp
	t.Cleanup(disp.Resume)

	out := firstReplyText(t, statusReport(ctx, false))

	if !strings.Contains(out, "PAUSED") {
		t.Errorf("expected PAUSED banner in output:\n%s", out)
	}
	if !strings.Contains(out, "test maintenance window") {
		t.Errorf("expected paused reason in banner:\n%s", out)
	}
	if !strings.Contains(out, indicatorRed) {
		t.Errorf("expected 🔴 in paused banner:\n%s", out)
	}
}

func TestStatusReport_WebhookFloorTriggers(t *testing.T) {
	ctx := statusCtx(t)
	ctx.WebhookRate = func() webhook.RateSnapshot {
		// 0 in last 5 min but 100 in last hour → install just went silent.
		return webhook.RateSnapshot{
			Per5Min:  0,
			Per15Min: 50,
			Per60Min: 100,
			PerType:  map[string]int{"pokemon": 100},
		}
	}

	out := firstReplyText(t, statusReport(ctx, false))

	// Webhooks header must be present + a 🔴 indicator must appear
	// after it.
	webhooksIdx := strings.Index(out, "Webhooks")
	if webhooksIdx < 0 {
		t.Fatalf("missing webhooks section:\n%s", out)
	}
	// Find first 🔴 anywhere after the section header.
	tail := out[webhooksIdx:]
	if !strings.Contains(tail, indicatorRed) {
		t.Errorf("expected 🔴 after Webhooks section for floor trigger:\n%s", out)
	}
}

func TestStatusReport_DiscordRateLimited(t *testing.T) {
	ctx := statusCtx(t)
	ctx.DiscordRate = func() delivery.DiscordRateSnapshot {
		return delivery.DiscordRateSnapshot{
			GlobalTokens:   10,
			GlobalCapacity: 50,
			Recent429Count: 7,
		}
	}

	out := firstReplyText(t, statusReport(ctx, false))

	// Locate the Discord section header and check 🔴 follows in that
	// section (before the next section starts).
	discordIdx := strings.Index(out, "Discord")
	if discordIdx < 0 {
		t.Fatalf("missing Discord rate section:\n%s", out)
	}
	tail := sectionTail(out, discordIdx)
	if !strings.Contains(tail, indicatorRed) {
		t.Errorf("expected 🔴 in Discord section after 429s>0:\n%s", out)
	}
	if !strings.Contains(out, "429s in last 5 min: 7") {
		t.Errorf("expected '429s in last 5 min: 7' in output:\n%s", out)
	}
}

func TestStatusReport_AlertLimitsBanned(t *testing.T) {
	ctx := statusCtx(t)

	// Use a tight DM limit and MaxLimitsBeforeStop=1 so a single
	// breach immediately tips the target into the banned state.
	// statusCtx's cleanup still closes the original limiter; this one
	// gets its own Cleanup.
	lim := ratelimit.New(ratelimit.Config{
		TimingPeriod:        240,
		DMLimit:             1,
		ChannelLimit:        40,
		DMSummaryLimit:      10,
		ChannelSummaryLimit: 40,
		MaxLimitsBeforeStop: 1,
	})
	t.Cleanup(lim.Close)
	ctx.AlertLimiter = lim

	// First Check fills the limit; second triggers JustBreached and
	// records the violation, tipping over MaxLimitsBeforeStop=1.
	lim.Check("u-banned", bot.TypeDiscordUser)
	lim.Check("u-banned", bot.TypeDiscordUser)

	out := firstReplyText(t, statusReport(ctx, false))

	limitsIdx := strings.Index(out, "Alert limits")
	if limitsIdx < 0 {
		t.Fatalf("missing Alert limits section:\n%s", out)
	}
	tail := sectionTail(out, limitsIdx)
	if !strings.Contains(tail, indicatorRed) {
		t.Errorf("expected 🔴 in Alert limits section when target is banned:\n%s", out)
	}
	// Sanity-check that the ban count made it into the rendered text.
	if !strings.Contains(tail, "Banned: 1") {
		t.Errorf("expected 'Banned: 1' in alert limits section:\n%s", out)
	}
}

// TestStatusReport_RenderQueueWarning — the render queue depth is not
// exposed through ctx yet (Phase 2 closed). The current implementation
// always renders "n/a" and does NOT trigger the 🟡 indicator until an
// accessor is added. This test pins that behaviour so a future change
// must consciously revisit the threshold wiring.
func TestStatusReport_RenderQueueWarning(t *testing.T) {
	ctx := statusCtx(t)

	out := firstReplyText(t, statusReport(ctx, false))

	rqIdx := strings.Index(out, "Render queue")
	if rqIdx < 0 {
		t.Fatalf("missing Render queue section:\n%s", out)
	}
	tail := sectionTail(out, rqIdx)
	// Render queue currently reports n/a until BotDeps wiring lands.
	if !strings.Contains(tail, "n/a") {
		t.Errorf("expected 'n/a' for render queue until accessor is wired:\n%s", out)
	}
	// Confirm the threshold constant stays in the file — the future
	// implementer must explicitly choose to lower it.
	if renderQueueWarnPercent != 80 {
		t.Errorf("renderQueueWarnPercent changed unexpectedly: got %d", renderQueueWarnPercent)
	}
}

func TestStatusReport_VerboseAddsRouteDetail(t *testing.T) {
	ctx := statusCtx(t)
	ctx.DiscordRate = func() delivery.DiscordRateSnapshot {
		return delivery.DiscordRateSnapshot{
			GlobalTokens:   30,
			GlobalCapacity: 50,
			Recent429Count: 0,
			Routes: []delivery.RouteState{
				{
					Route:     "/channels/:id/messages",
					Remaining: 1,
					Limit:     5,
					ResetAt:   time.Now().Add(2 * time.Second),
				},
			},
		}
	}

	plain := firstReplyText(t, statusReport(ctx, false))
	verbose := firstReplyText(t, statusReport(ctx, true))

	// Verbose must contain the route key; plain must not.
	if strings.Contains(plain, "/channels/:id/messages") {
		t.Errorf("plain mode unexpectedly contained route detail:\n%s", plain)
	}
	if !strings.Contains(verbose, "/channels/:id/messages") {
		t.Errorf("verbose mode missing per-route detail:\n%s", verbose)
	}
}

// TestStatusReport_NilFieldsTolerated confirms the helper handles a
// CommandContext with all introspection closures left nil — the spec
// requires "render n/a rather than panicking".
func TestStatusReport_NilFieldsTolerated(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	// Deliberately leave WebhookRate / DiscordRate / TelegramRate /
	// AlertLimiter / StateMgr / Dispatcher / DB all nil.

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("statusReport panicked on nil ctx fields: %v", r)
		}
	}()

	out := firstReplyText(t, statusReport(ctx, false))
	// Every section should fall back to "n/a" somewhere.
	if !strings.Contains(out, "n/a") {
		t.Errorf("expected 'n/a' fallback in output:\n%s", out)
	}
}

func TestStatusReport_TelegramRateLimited(t *testing.T) {
	ctx := statusCtx(t)
	ctx.TelegramRate = func() delivery.TelegramRateSnapshot {
		return delivery.TelegramRateSnapshot{
			Recent429Count:      0,
			CurrentBackoffUntil: time.Now().Add(30 * time.Second),
		}
	}

	out := firstReplyText(t, statusReport(ctx, false))

	telegramIdx := strings.Index(out, "Telegram")
	if telegramIdx < 0 {
		t.Fatalf("missing Telegram rate section:\n%s", out)
	}
	tail := sectionTail(out, telegramIdx)
	if !strings.Contains(tail, indicatorRed) {
		t.Errorf("expected 🔴 in Telegram section when backoff is active:\n%s", out)
	}
}

func TestStatusReport_WebhookAllZerosNeutral(t *testing.T) {
	ctx := statusCtx(t)
	ctx.WebhookRate = func() webhook.RateSnapshot {
		return webhook.RateSnapshot{
			Per5Min:  0,
			Per15Min: 0,
			Per60Min: 0,
			PerType:  map[string]int{},
		}
	}

	out := firstReplyText(t, statusReport(ctx, false))

	// Webhooks section must be present.
	webhooksIdx := strings.Index(out, "Webhooks")
	if webhooksIdx < 0 {
		t.Fatalf("missing Webhooks section:\n%s", out)
	}
	tail := sectionTail(out, webhooksIdx)

	// All-zeros is neutral (🟡), NOT 🔴 — the stopped-receiving
	// trigger requires Per60Min > 0 alongside Per5Min == 0.
	if strings.Contains(tail, indicatorRed) {
		t.Errorf("all-zeros webhook snapshot should not trigger 🔴:\n%s", out)
	}
	if !strings.Contains(tail, indicatorYellow) {
		t.Errorf("all-zeros webhook snapshot should show 🟡 (neutral):\n%s", out)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Hour + 30*time.Minute + 5*time.Second, "2h 30m 5s"},
		{50*time.Hour + 12*time.Minute, "2d 2h 12m"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestStatusReport_SummaryBufferSection verifies that the summary buffer
// section reads from ctx.SummaryBuffer and reports counts correctly.
func TestStatusReport_SummaryBufferSection(t *testing.T) {
	t.Run("nil_buffer_renders_na", func(t *testing.T) {
		ctx := statusCtx(t)
		ctx.SummaryBuffer = nil

		out := firstReplyText(t, statusReport(ctx, false))

		idx := strings.Index(out, "Summary buffer")
		if idx < 0 {
			t.Fatalf("expected 'Summary buffer' section in output:\n%s", out)
		}
		section := sectionTail(out, idx)
		if !strings.Contains(section, "n/a") {
			t.Errorf("expected n/a when SummaryBuffer is nil, got section:\n%s", section)
		}
	})

	t.Run("empty_buffer_green", func(t *testing.T) {
		ctx := statusCtx(t)
		ctx.SummaryBuffer = tracker.NewSummaryBuffer("")

		out := firstReplyText(t, statusReport(ctx, false))

		idx := strings.Index(out, "Summary buffer")
		if idx < 0 {
			t.Fatalf("expected 'Summary buffer' section in output:\n%s", out)
		}
		section := sectionTail(out, idx)
		// Zero entries → 🟢 indicator.
		if !strings.Contains(section, indicatorGreen) {
			t.Errorf("expected 🟢 for empty buffer, got section:\n%s", section)
		}
		if strings.Contains(section, indicatorYellow) {
			t.Errorf("did not expect 🟡 for empty buffer:\n%s", section)
		}
	})

	t.Run("over_threshold_yellow", func(t *testing.T) {
		ctx := statusCtx(t)
		buf := tracker.NewSummaryBuffer("")
		// Append enough entries to exceed summaryBufferWarn (100).
		for i := 0; i <= summaryBufferWarn; i++ {
			buf.Append("user-1", "quest", tracker.BufferedQuest{
				RewardType: 1,
				Reward:     i + 1, // distinct reward per iteration to avoid dedup
				PokestopID: "stop-id",
			})
		}
		ctx.SummaryBuffer = buf

		out := firstReplyText(t, statusReport(ctx, false))

		idx := strings.Index(out, "Summary buffer")
		if idx < 0 {
			t.Fatalf("expected 'Summary buffer' section in output:\n%s", out)
		}
		section := sectionTail(out, idx)
		if !strings.Contains(section, indicatorYellow) {
			t.Errorf("expected 🟡 when buffer exceeds threshold, got section:\n%s", section)
		}
		// Should also show top-user line.
		if !strings.Contains(section, "user-1") {
			t.Errorf("expected top-user 'user-1' in section:\n%s", section)
		}
	})
}
