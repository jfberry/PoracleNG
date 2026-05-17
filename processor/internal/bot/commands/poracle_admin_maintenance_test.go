package commands

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// newTestDispatcher creates a minimal *delivery.Dispatcher suitable for
// maintenance subgroup tests. It wires a single in-memory discord sender so
// pause/resume/status are exercised without hitting the network.
// The caller must call d.Stop() (typically via t.Cleanup).
func newTestDispatcher(t *testing.T) *delivery.Dispatcher {
	t.Helper()
	// delivery.NewDispatcherWithSenders is exported and accepts externally-
	// provided senders, allowing test-internal construction without needing
	// the delivery test-package helpers.
	senders := map[string]delivery.Sender{} // empty — no real senders needed
	tracker := delivery.NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.Stop() })
	d := delivery.NewDispatcherWithSenders(senders, tracker, 100, delivery.QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	})
	d.Start()
	t.Cleanup(d.Stop)
	return d
}

// maintenanceCtx builds a CommandContext pre-wired for maintenance tests.
func maintenanceCtx(t *testing.T, d *delivery.Dispatcher) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Dispatcher = d
	return ctx
}

// runMaintenance is a convenience wrapper that calls the paMaintenance subgroup
// via the top-level PoracleAdminCommand.Run, so the full dispatch path is exercised.
func runMaintenance(t *testing.T, ctx *bot.CommandContext, args ...string) []bot.Reply {
	t.Helper()
	cmd := &PoracleAdminCommand{}
	fullArgs := append([]string{"maintenance"}, args...)
	return cmd.Run(ctx, fullArgs)
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// TestMaintenance_HelpNoArgs_RendersStatus verifies that no-arg invocation
// shows status (not help), since status is the most common, idempotent op.
func TestMaintenance_HelpNoArgs_RendersStatus(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	// Call with no args after "maintenance" — paMaintenance.run is called with [].
	replies := runMaintenance(t, ctx)

	out := firstReplyText(t, replies)

	// Status when running → should contain 🟢 running indicator.
	if !containsStr(out, "🟢") {
		t.Errorf("no-arg (status) output should contain 🟢, got:\n%s", out)
	}
	// Should NOT be the help text (which would contain "pause" as a subcommand).
	if containsStr(out, "`pause") {
		t.Errorf("no-arg should render status not help, got:\n%s", out)
	}
}

// TestMaintenance_Help verifies that "help" returns help text mentioning the
// pause, resume, and status subcommands.
func TestMaintenance_Help(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	replies := runMaintenance(t, ctx, "help")

	out := firstReplyText(t, replies)

	for _, want := range []string{"pause", "resume", "status"} {
		if !containsStr(out, want) {
			t.Errorf("maintenance help missing subcommand %q, got:\n%s", want, out)
		}
	}
}

// TestMaintenance_PauseNew verifies that pausing a fresh dispatcher reports
// the reason in the reply.
func TestMaintenance_PauseNew(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	replies := runMaintenance(t, ctx, "pause", "db", "migration")

	out := firstReplyText(t, replies)

	if !containsStr(out, "db migration") {
		t.Errorf("pause reply should echo reason 'db migration', got:\n%s", out)
	}
	if !containsStr(out, "🔴") {
		t.Errorf("pause reply should contain 🔴, got:\n%s", out)
	}
	// The dispatcher should now be paused.
	paused, reason, _ := d.PauseState()
	if !paused {
		t.Error("dispatcher should be paused after pause command")
	}
	if reason != "db migration" {
		t.Errorf("dispatcher pause reason should be 'db migration', got %q", reason)
	}

	d.Resume() // clean up so Stop doesn't block
}

// TestMaintenance_PauseAlreadyPaused verifies that a second pause reports
// the already-paused state and the original reason is preserved.
func TestMaintenance_PauseAlreadyPaused(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	// First pause.
	d.Pause("first reason")
	_, _, firstSince := d.PauseState()

	// Small sleep so the timestamps differ measurably.
	time.Sleep(5 * time.Millisecond)

	// Second pause via command (different reason).
	replies := runMaintenance(t, ctx, "pause", "second", "reason")
	out := firstReplyText(t, replies)

	if !containsStr(out, "Already paused") && !containsStr(out, "already paused") {
		t.Errorf("second pause should report already-paused, got:\n%s", out)
	}
	if !containsStr(out, "first reason") {
		t.Errorf("second pause should show original reason 'first reason', got:\n%s", out)
	}

	// Original reason and since preserved (idempotent).
	paused, reason, since := d.PauseState()
	if !paused {
		t.Error("dispatcher should still be paused")
	}
	if reason != "first reason" {
		t.Errorf("dispatcher pause reason should be preserved as 'first reason', got %q", reason)
	}
	if !since.Equal(firstSince) {
		t.Errorf("pausedSince should not change on second Pause: first=%v second=%v", firstSince, since)
	}

	d.Resume()
}

// TestMaintenance_ResumeWhilePaused verifies that resume after a pause
// confirms the resume in the reply.
func TestMaintenance_ResumeWhilePaused(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	d.Pause("pre-paused")

	replies := runMaintenance(t, ctx, "resume")
	out := firstReplyText(t, replies)

	if !containsStr(out, "🟢") {
		t.Errorf("resume reply should contain 🟢, got:\n%s", out)
	}
	if !containsStr(out, "pre-paused") {
		t.Errorf("resume reply should mention original reason 'pre-paused', got:\n%s", out)
	}

	// Dispatcher should be running.
	paused, _, _ := d.PauseState()
	if paused {
		t.Error("dispatcher should be running after resume command")
	}
}

// TestMaintenance_ResumeNotPaused verifies that resume when not paused
// reports that delivery is not paused.
func TestMaintenance_ResumeNotPaused(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	replies := runMaintenance(t, ctx, "resume")
	out := firstReplyText(t, replies)

	if !containsStr(out, "not currently paused") && !containsStr(out, "Not currently paused") {
		t.Errorf("resume when not paused should say 'not currently paused', got:\n%s", out)
	}
}

// TestMaintenance_StatusRunning verifies that status when running shows 🟢.
func TestMaintenance_StatusRunning(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	replies := runMaintenance(t, ctx, "status")
	out := firstReplyText(t, replies)

	if !containsStr(out, "🟢") {
		t.Errorf("status (running) should contain 🟢, got:\n%s", out)
	}
}

// TestMaintenance_StatusPaused verifies that status when paused shows 🔴 and
// the reason + duration.
func TestMaintenance_StatusPaused(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	d.Pause("scheduled downtime")

	replies := runMaintenance(t, ctx, "status")
	out := firstReplyText(t, replies)

	if !containsStr(out, "🔴") {
		t.Errorf("status (paused) should contain 🔴, got:\n%s", out)
	}
	if !containsStr(out, "scheduled downtime") {
		t.Errorf("status (paused) should mention reason 'scheduled downtime', got:\n%s", out)
	}

	d.Resume()
}

// TestMaintenance_NoDispatcher verifies graceful handling when ctx.Dispatcher
// is nil — all subcommands should return a clear "no dispatcher" message
// rather than panicking.
func TestMaintenance_NoDispatcher(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Dispatcher = nil // explicitly nil

	for _, sub := range [][]string{
		{},
		{"pause"},
		{"resume"},
		{"status"},
	} {
		sub := sub
		t.Run(joinOrNone(sub), func(t *testing.T) {
			replies := runMaintenance(t, ctx, sub...)
			out := firstReplyText(t, replies)
			if !containsStr(out, "Dispatcher") && !containsStr(out, "dispatcher") {
				t.Errorf("nil-dispatcher reply should mention 'dispatcher', got:\n%s", out)
			}
		})
	}
}

// TestMaintenance_UnknownSub verifies that an unrecognised subcommand returns
// the unknown-sub error message.
func TestMaintenance_UnknownSub(t *testing.T) {
	d := newTestDispatcher(t)
	ctx := maintenanceCtx(t, d)

	replies := runMaintenance(t, ctx, "bogus")
	out := firstReplyText(t, replies)

	if !containsStr(out, "maintenance") {
		t.Errorf("unknown-sub error should mention 'maintenance', got:\n%s", out)
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// joinOrNone returns a display name for a subcommand slice.
func joinOrNone(args []string) string {
	if len(args) == 0 {
		return "(no-arg)"
	}
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}

// Ensure the delivery import is used (NewMessageTracker and QueueConfig are
// referenced in newTestDispatcher above, so no dummy var is needed).
var _ = delivery.QueueConfig{}
