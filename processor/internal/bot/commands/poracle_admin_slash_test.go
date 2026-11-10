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
	if ctx.Admin == nil {
		ctx.Admin = &bot.AdminDeps{}
	}
	ctx.Admin.SlashSync = syncFn
	ctx.Admin.SlashForceResync = forceResyncFn
	ctx.Admin.SlashClearGlobal = clearGlobalFn
	ctx.Admin.SlashClearGuild = clearGuildFn
	ctx.Admin.SlashStatus = statusFn
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
		func() error { return nil }, // sync OK
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

// --- list ---

// The whole point of the command: each row carries the rendered mention
// (proves the id resolves) AND the same syntax fenced, because Discord gives
// no way to copy text back out of a rendered mention.
func TestSlash_ListRendersMentionAndCopyableSyntax(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Admin = &bot.AdminDeps{
		SlashList: func(guildID string) ([]bot.SlashScopeCommands, error) {
			return []bot.SlashScopeCommands{{
				Name: "global",
				Commands: []bot.SlashCommandMention{
					{Path: "help", ID: "111"},
					{Path: "untrack raid", ID: "222"},
				},
			}}, nil
		},
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "list"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	text := replies[0].Text
	for _, want := range []string{
		"</help:111>", "`</help:111>`",
		"</untrack raid:222>", "`</untrack raid:222>`",
		"global",
	} {
		if !containsStr(text, want) {
			t.Errorf("reply missing %q:\n%s", want, text)
		}
	}
}

// Guild-scoped registrations get different ids per guild, so scopes are
// reported separately and the invoking guild is passed through.
func TestSlash_ListPassesInvokingGuildAndSeparatesScopes(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.GuildID = "guild-9"
	var gotGuild string
	ctx.Admin = &bot.AdminDeps{
		SlashList: func(guildID string) ([]bot.SlashScopeCommands, error) {
			gotGuild = guildID
			return []bot.SlashScopeCommands{
				{Name: "global", Commands: []bot.SlashCommandMention{{Path: "help", ID: "111"}}},
				{Name: "guild-9", Commands: []bot.SlashCommandMention{{Path: "help", ID: "999"}}},
			}, nil
		},
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "list"})
	if gotGuild != "guild-9" {
		t.Errorf("SlashList got guildID %q, want guild-9", gotGuild)
	}
	text := replies[0].Text
	if !containsStr(text, "</help:111>") || !containsStr(text, "</help:999>") {
		t.Errorf("both scopes' ids should appear:\n%s", text)
	}
}

func TestSlash_ListNotConfigured(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Admin = &bot.AdminDeps{}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "list"})
	if len(replies) == 0 || !containsStr(replies[0].Text, "not configured") {
		t.Fatalf("expected not-configured message, got %+v", replies)
	}
}

func TestSlash_ListErrorSurfaces(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Admin = &bot.AdminDeps{
		SlashList: func(string) ([]bot.SlashScopeCommands, error) {
			return nil, errors.New("discord says no")
		},
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "list"})
	if len(replies) == 0 || !containsStr(replies[0].Text, "discord says no") {
		t.Fatalf("error should surface to the operator, got %+v", replies)
	}
}

// A long command set exceeds Discord's message limit, so it must fall back to
// a file attachment rather than being split across many messages.
func TestSlash_ListAttachesWhenLong(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	many := make([]bot.SlashCommandMention, 0, 120)
	for i := range 120 {
		many = append(many, bot.SlashCommandMention{
			Path: "command-with-a-longish-name-" + itoaTest(i), ID: "1234567890123456789",
		})
	}
	ctx.Admin = &bot.AdminDeps{
		SlashList: func(string) ([]bot.SlashScopeCommands, error) {
			return []bot.SlashScopeCommands{{Name: "global", Commands: many}}, nil
		},
	}

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"slash", "list"})
	if len(replies) != 1 || replies[0].Attachment == nil {
		t.Fatalf("expected a single reply with an attachment, got %d replies", len(replies))
	}
	if !containsStr(string(replies[0].Attachment.Content), "</command-with-a-longish-name-0:1234567890123456789>") {
		t.Error("attachment should carry the full list")
	}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
