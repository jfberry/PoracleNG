package commands

import (
	"errors"
	"testing"

	reconcilesentinel "github.com/pokemon/poracleng/processor/internal/discordbot/reconcile"
)

// TestReconcile_HelpNoArgs verifies that the reconcile subgroup with no args
// returns help text mentioning both subcommands.
func TestReconcile_HelpNoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"run", "user"} {
		if !containsStr(text, want) {
			t.Errorf("reconcile help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

// TestReconcile_RunSuccess verifies that a successful full reconciliation
// returns a success reply.
func TestReconcile_RunSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.RunReconcile = func() error { return nil }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "run"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if !containsStr(replies[0].Text, "✅") {
		t.Errorf("expected success reply (✅), got: %q", replies[0].Text)
	}
}

// TestReconcile_RunDisabled verifies that ErrReconciliationDisabled produces
// a friendly "not configured" reply.
func TestReconcile_RunDisabled(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.RunReconcile = func() error { return reconcilesentinel.ErrDisabled }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "run"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "not configured") {
		t.Errorf("expected not-configured message, got: %q", text)
	}
	if containsStr(text, "✅") {
		t.Errorf("not-configured reply should not contain success indicator: %q", text)
	}
}

// TestReconcile_RunDisabledWrapped verifies that errors.Is detects the sentinel
// even when it is wrapped (e.g. via fmt.Errorf with %w).
func TestReconcile_RunDisabledWrapped(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.RunReconcile = func() error {
		return errors.Join(reconcilesentinel.ErrDisabled, errors.New("extra"))
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "run"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if !containsStr(replies[0].Text, "not configured") {
		t.Errorf("expected not-configured message for wrapped sentinel, got: %q", replies[0].Text)
	}
}

// TestReconcile_RunGenericError verifies that a non-sentinel error surfaces
// the error message in the reply.
func TestReconcile_RunGenericError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.RunReconcile = func() error { return errors.New("discord gateway timeout") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "run"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if !containsStr(replies[0].Text, "discord gateway timeout") {
		t.Errorf("expected error message in reply, got: %q", replies[0].Text)
	}
	if containsStr(replies[0].Text, "✅") {
		t.Errorf("error reply should not contain success indicator: %q", replies[0].Text)
	}
}

// TestReconcile_UserSuccess verifies that a successful per-user reconciliation
// returns a success reply containing the user ID.
func TestReconcile_UserSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	var gotID string
	ctx.Reconciler = func(userID string) error { gotID = userID; return nil }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "user", "123456789"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if gotID != "123456789" {
		t.Errorf("expected Reconciler called with '123456789', got %q", gotID)
	}
	text := replies[0].Text
	if !containsStr(text, "123456789") {
		t.Errorf("expected user ID '123456789' in reply, got: %q", text)
	}
	if !containsStr(text, "✅") {
		t.Errorf("expected success reply (✅), got: %q", text)
	}
}

// TestReconcile_UserDisabled verifies that ErrReconciliationDisabled produces
// a friendly "not configured" reply for the per-user path.
func TestReconcile_UserDisabled(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Reconciler = func(userID string) error { return reconcilesentinel.ErrDisabled }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "user", "987654321"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "not configured") {
		t.Errorf("expected not-configured message, got: %q", text)
	}
	if containsStr(text, "✅") {
		t.Errorf("not-configured reply should not contain success indicator: %q", text)
	}
}

// TestReconcile_UserError verifies that a non-sentinel error surfaces the
// error message and user ID in the reply.
func TestReconcile_UserError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Reconciler = func(userID string) error { return errors.New("member fetch failed") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "user", "111222333"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "111222333") {
		t.Errorf("expected user ID '111222333' in error reply, got: %q", text)
	}
	if !containsStr(text, "member fetch failed") {
		t.Errorf("expected error message in reply, got: %q", text)
	}
	if containsStr(text, "✅") {
		t.Errorf("error reply should not contain success indicator: %q", text)
	}
}

// TestReconcile_UserMissingArg verifies that `user` with no ID returns
// the usage hint.
func TestReconcile_UserMissingArg(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Reconciler = func(userID string) error { return nil }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "user"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "user") {
		t.Errorf("expected 'user' in usage hint, got: %q", text)
	}
	if containsStr(text, "✅") {
		t.Errorf("usage hint must not contain success indicator: %q", text)
	}
}

// TestReconcile_UnknownSub verifies that an unrecognised subcommand returns
// the unknown-sub message naming the reconcile group.
func TestReconcile_UnknownSub(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reconcile", "bogus"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	if !containsStr(text, "reconcile") {
		t.Errorf("expected 'reconcile' in unknown-sub reply, got: %q", text)
	}
	if containsStr(text, "✅") {
		t.Errorf("unknown-sub reply must not contain success indicator: %q", text)
	}
}
