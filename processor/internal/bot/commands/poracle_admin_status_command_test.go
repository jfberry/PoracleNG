package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// statusCtxWithRouteDetail returns a statusCtx that has a near-limit Discord
// route in its snapshot, allowing verbose-mode detection via the route key.
func statusCtxWithRouteDetail(t *testing.T) (*bot.CommandContext, string) {
	t.Helper()
	const routeKey = "/channels/:id/messages"
	ctx := statusCtx(t)
	ctx.Admin.DiscordRate = func() delivery.DiscordRateSnapshot {
		return delivery.DiscordRateSnapshot{
			GlobalTokens:   30,
			GlobalCapacity: 50,
			Recent429Count: 0,
			Routes: []delivery.RouteState{
				{
					Route:     routeKey,
					Remaining: 1,
					Limit:     5,
					ResetAt:   time.Now().Add(2 * time.Second),
				},
			},
		}
	}
	return ctx, routeKey
}

// TestStatus_NoArgs_RendersReport verifies that the no-arg entry point
// (paStatus.help — the hook called when no further args are given to the
// subgroup) renders the full health snapshot, not just help text.
// This exercises the actual user-hit path: "!poracle-admin status".
func TestStatus_NoArgs_RendersReport(t *testing.T) {
	ctx := statusCtx(t)

	// paStatus.help is the hook invoked when the user types just
	// "!poracle-admin status" with no further arguments.
	replies := paStatus.help(ctx)

	out := firstReplyText(t, replies)

	// The header line is the most reliable marker for "statusReport was called".
	if !strings.Contains(out, "Poracle Admin") {
		t.Errorf("expected status header in output, got:\n%s", out)
	}
	// Sanity: should have multiple sections.
	if !strings.Contains(out, "Webhooks") {
		t.Errorf("expected Webhooks section in output, got:\n%s", out)
	}
}

// TestStatus_VerboseFlag_PassesThrough verifies that -v, --verbose, and
// "verbose" all activate verbose mode. Verbose mode adds per-route Discord
// detail when a near-limit route exists.
func TestStatus_VerboseFlag_PassesThrough(t *testing.T) {
	for _, flag := range []string{"-v", "--verbose", "verbose"} {
		t.Run(flag, func(t *testing.T) {
			ctx, routeKey := statusCtxWithRouteDetail(t)

			// Plain (no args) should NOT include route detail.
			plainOut := firstReplyText(t, paStatusRun(ctx, []string{}))
			if strings.Contains(plainOut, routeKey) {
				t.Errorf("plain (no-arg) mode unexpectedly contained route detail %q:\n%s",
					routeKey, plainOut)
			}

			// Verbose flag should include route detail.
			verboseOut := firstReplyText(t, paStatusRun(ctx, []string{flag}))
			if !strings.Contains(verboseOut, routeKey) {
				t.Errorf("verbose flag %q missing per-route detail %q:\n%s",
					flag, routeKey, verboseOut)
			}
		})
	}
}

// TestStatus_HelpSubcommand verifies that "help" as the first argument returns
// the status help text, not the full snapshot.
func TestStatus_HelpSubcommand(t *testing.T) {
	ctx := statusCtx(t)

	replies := paStatusRun(ctx, []string{"help"})

	out := firstReplyText(t, replies)

	// The help text should mention the command name / -v flag.
	if !strings.Contains(out, "status") {
		t.Errorf("expected 'status' in help text, got:\n%s", out)
	}
	if !strings.Contains(out, "-v") {
		t.Errorf("expected '-v' mentioned in help text, got:\n%s", out)
	}
	// It must NOT be the full snapshot — no Webhooks section header.
	if strings.Contains(out, "Webhooks") {
		t.Errorf("help reply unexpectedly contained Webhooks section:\n%s", out)
	}
}

// TestStatus_UnknownSubcommand verifies that an unrecognised first argument
// returns the unknown-sub error message with "status" in it.
func TestStatus_UnknownSubcommand(t *testing.T) {
	ctx := statusCtx(t)

	replies := paStatusRun(ctx, []string{"bogus"})

	out := firstReplyText(t, replies)

	// Should mention "status" as the subgroup name.
	if !strings.Contains(out, "status") {
		t.Errorf("unknown-sub error should mention 'status', got:\n%s", out)
	}
	// Must NOT be the full snapshot.
	if strings.Contains(out, "Poracle Admin") {
		t.Errorf("unknown-sub reply unexpectedly contained snapshot header:\n%s", out)
	}
}
