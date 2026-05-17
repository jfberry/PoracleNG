package commands

import (
	"errors"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash"
)

// stubSlashDeps wires all 5 slash closures on ctx using the provided stubs.
// Pass nil for any closure to leave it nil (simulates unconfigured).
func stubSlashDeps(ctx *bot.CommandContext,
	syncFn func() error,
	forceResyncFn func() error,
	clearGlobalFn func() error,
	clearGuildFn func(string) error,
	statusFn func() (bot.SlashScope, []bot.SlashScope, error),
) {
	ctx.SlashSync = syncFn
	ctx.SlashForceResync = forceResyncFn
	ctx.SlashClearGlobal = clearGlobalFn
	ctx.SlashClearGuild = clearGuildFn
	ctx.SlashStatus = statusFn
}

// TestSlash_HelpNoArgs verifies that calling the slash subgroup with no args
// returns help text containing all five subcommand names.
func TestSlash_HelpNoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "slash" with no further args → group help
	replies := cmd.Run(ctx, []string{"slash"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, sub := range []string{"sync", "force-resync", "clear-global", "clear-guild", "status"} {
		if !containsStr(text, sub) {
			t.Errorf("slash help missing subcommand %q, got:\n%s", sub, text)
		}
	}
}

// TestSlash_SyncSuccess verifies that a successful sync returns a success reply.
func TestSlash_SyncSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	stubSlashDeps(ctx,
		func() error { return nil },     // sync OK
		nil, nil, nil, nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "sync"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if !containsStr(replies[0].Text, "✅") {
		t.Errorf("expected success reply (✅), got: %q", replies[0].Text)
	}
}

// TestSlash_SyncNotConfigured verifies that ErrSlashNotConfigured produces a
// friendly "not configured" reply (not a stack trace).
func TestSlash_SyncNotConfigured(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	stubSlashDeps(ctx,
		func() error { return slash.ErrSlashNotConfigured },
		nil, nil, nil, nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "sync"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	// The i18n "not configured" message must be present in the reply.
	if !containsStr(replies[0].Text, "not configured") {
		t.Errorf("expected not-configured message, got: %q", replies[0].Text)
	}
	// Must not be a success reply.
	if containsStr(replies[0].Text, "✅") {
		t.Errorf("not-configured reply should not contain success indicator: %q", replies[0].Text)
	}
}

// TestSlash_SyncError verifies that a generic error is surfaced in the reply text.
func TestSlash_SyncError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	stubSlashDeps(ctx,
		func() error { return errors.New("network timeout") },
		nil, nil, nil, nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "sync"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if !containsStr(replies[0].Text, "network timeout") {
		t.Errorf("expected error message in reply, got: %q", replies[0].Text)
	}
}

// TestSlash_ClearGuildNoArg verifies that clear-guild without a guild ID
// returns the needs-arg error.
func TestSlash_ClearGuildNoArg(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	stubSlashDeps(ctx, nil, nil, nil,
		func(gid string) error { return nil },
		nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "clear-guild"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	// Must mention the usage pattern.
	if !containsStr(replies[0].Text, "clear-guild") {
		t.Errorf("needs-arg reply should mention 'clear-guild', got: %q", replies[0].Text)
	}
	if containsStr(replies[0].Text, "✅") {
		t.Errorf("must not be success reply when arg missing, got: %q", replies[0].Text)
	}
}

// TestSlash_ClearGuildWithArg verifies that clear-guild with a guild ID
// succeeds and mentions the guild ID in the reply.
func TestSlash_ClearGuildWithArg(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	var got string
	stubSlashDeps(ctx, nil, nil, nil,
		func(gid string) error { got = gid; return nil },
		nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "clear-guild", "12345"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	if got != "12345" {
		t.Errorf("expected SlashClearGuild called with '12345', got %q", got)
	}
	if !containsStr(replies[0].Text, "12345") {
		t.Errorf("expected guild ID '12345' in reply, got: %q", replies[0].Text)
	}
	if !containsStr(replies[0].Text, "✅") {
		t.Errorf("expected success reply, got: %q", replies[0].Text)
	}
}

// TestSlash_StatusEmpty verifies that a status call returning zero scopes
// renders without crashing.
func TestSlash_StatusEmpty(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	stubSlashDeps(ctx, nil, nil, nil, nil,
		func() (bot.SlashScope, []bot.SlashScope, error) {
			return bot.SlashScope{Name: "global"}, nil, nil
		},
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "status"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	// Must contain the header and at least the global scope.
	text := replies[0].Text
	if !containsStr(text, "global") {
		t.Errorf("expected 'global' in status output, got: %q", text)
	}
}

// TestSlash_StatusPopulated verifies that a status call returning a global
// scope + one guild scope renders rows for both.
func TestSlash_StatusPopulated(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	stubSlashDeps(ctx, nil, nil, nil, nil,
		func() (bot.SlashScope, []bot.SlashScope, error) {
			global := bot.SlashScope{
				Name:         "global",
				LastSyncedAt: ts,
				Fingerprint:  "abcdef1234567890",
			}
			guild := bot.SlashScope{
				Name:         "98765",
				LastSyncedAt: ts,
				Fingerprint:  "fedcba9876543210",
			}
			return global, []bot.SlashScope{guild}, nil
		},
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "status"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "global") {
		t.Errorf("expected 'global' in status output, got: %q", text)
	}
	if !containsStr(text, "98765") {
		t.Errorf("expected guild '98765' in status output, got: %q", text)
	}
	// Fingerprint should be truncated to 8 chars.
	if !containsStr(text, "abcdef12") {
		t.Errorf("expected truncated global fingerprint 'abcdef12' in status output, got: %q", text)
	}
}

// TestSlash_TelegramRefusal verifies that a Telegram caller gets a
// discord_only refusal, not an error or a crash.
func TestSlash_TelegramRefusal(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Platform = "telegram"
	// Wire sync so the test doesn't accidentally pass due to nil closure.
	stubSlashDeps(ctx,
		func() error { return nil },
		nil, nil, nil, nil,
	)

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "sync"})

	if len(replies) == 0 {
		t.Fatal("expected reply, got none")
	}
	// Should mention Discord-only restriction.
	if !containsStr(replies[0].Text, "Discord") {
		t.Errorf("expected Discord-only message for Telegram caller, got: %q", replies[0].Text)
	}
	if containsStr(replies[0].Text, "✅") {
		t.Errorf("Telegram should not get success reply, got: %q", replies[0].Text)
	}
}
