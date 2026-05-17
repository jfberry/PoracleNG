package commands

import (
	"errors"
	"testing"
)

func TestReload_HelpNoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "reload" with no further args → group help
	replies := cmd.Run(ctx, []string{"reload"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"dts", "geofence", "state"} {
		if !containsStr(text, want) {
			t.Errorf("reload help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

func TestReload_HelpExplicit(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "reload help" → also group help
	replies := cmd.Run(ctx, []string{"reload", "help"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "dts") {
		t.Errorf("reload help missing 'dts', got:\n%s", text)
	}
}

func TestReload_DtsSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadDTS = func() (int, error) { return 42, nil }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "dts"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "DTS reloaded") {
		t.Errorf("expected DTS success message, got: %q", text)
	}
	// Template count must appear
	if !containsStr(text, "42") {
		t.Errorf("expected template count '42' in reply, got: %q", text)
	}
}

func TestReload_DtsError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadDTS = func() (int, error) { return 0, errors.New("disk read failed") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "dts"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "disk read failed") {
		t.Errorf("expected error message in reply, got: %q", text)
	}
}

func TestReload_GeofenceSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadGeofence = func() error { return nil }
	// StateMgr is nil in test ctx, so counts will be 0 — that's fine.

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "geofence"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Geofence reloaded") {
		t.Errorf("expected geofence success message, got: %q", text)
	}
}

func TestReload_GeofenceError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadGeofence = func() error { return errors.New("no geofence file") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "geofence"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "no geofence file") {
		t.Errorf("expected error message in reply, got: %q", text)
	}
}

func TestReload_StateSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadState = func() error { return nil }
	// StateMgr is nil in test ctx, so counts will be 0 — acceptable.

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "state"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "State reloaded") {
		t.Errorf("expected state success message, got: %q", text)
	}
}

func TestReload_StateError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadState = func() error { return errors.New("db connection refused") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "state"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "db connection refused") {
		t.Errorf("expected error message in reply, got: %q", text)
	}
}

func TestReload_UnknownSub(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "bogus"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	// The reply should name the subgroup so the user knows where they went wrong.
	if !containsStr(text, "reload") {
		t.Errorf("expected 'reload' in unknown-sub reply, got: %q", text)
	}
}

func TestReload_DtsRendererNotConfigured(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadDTS = func() (int, error) { return 0, errors.New("DTS renderer not configured") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "dts"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "DTS renderer not configured") {
		t.Errorf("expected sentinel error message in reply, got: %q", text)
	}
	if containsStr(text, "DTS reloaded") {
		t.Errorf("reply must not contain success message when renderer is nil, got: %q", text)
	}
}

func TestReload_DtsNilFunc(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.ReloadDTS = nil // not configured

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"reload", "dts"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	// Should return an error message, not panic
	if replies[0].Text == "" {
		t.Error("nil ReloadDTS should return non-empty error reply")
	}
}
