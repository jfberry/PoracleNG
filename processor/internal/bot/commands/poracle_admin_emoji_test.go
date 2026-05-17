package commands

import (
	"errors"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/dts"
)

// TestEmoji_HelpNoArgs checks that the help subcommand mentions list/reload/test.
func TestEmoji_HelpNoArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	// "emoji" with no further args → group help
	replies := cmd.Run(ctx, []string{"emoji"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	for _, want := range []string{"list", "reload", "test"} {
		if !containsStr(text, want) {
			t.Errorf("emoji help missing subcommand %q, got:\n%s", want, text)
		}
	}
}

// TestEmoji_HelpExplicit checks "emoji help" returns the same help.
func TestEmoji_HelpExplicit(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "help"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if !containsStr(replies[0].Text, "list") {
		t.Errorf("emoji help missing 'list', got:\n%s", replies[0].Text)
	}
}

// TestEmoji_ListPopulated checks that list output includes all keys and their platforms.
func TestEmoji_ListPopulated(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Emoji = dts.LoadEmoji(t.TempDir(), map[string]string{
		"emojiWeather": "☀️",
		"emojiTeam":    "🛡️",
		"emojiItem":    "🎒",
	})

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}

	// Combine all reply text (output may be chunked for large configs).
	combined := ""
	for _, r := range replies {
		combined += r.Text
	}

	for _, want := range []string{"emojiWeather", "emojiTeam", "emojiItem"} {
		if !containsStr(combined, want) {
			t.Errorf("emoji list missing key %q, got:\n%s", want, combined)
		}
	}
	// Key count must appear.
	if !containsStr(combined, "3") {
		t.Errorf("expected key count '3' in list header, got:\n%s", combined)
	}
}

// TestEmoji_ListEmpty checks that an empty emoji config reports 0 keys.
func TestEmoji_ListEmpty(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Emoji = dts.LoadEmoji(t.TempDir(), map[string]string{})

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "list"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "0") {
		t.Errorf("expected key count '0' in list output, got:\n%s", text)
	}
}

// TestEmoji_ReloadSuccess checks that a successful reload reports the key count.
func TestEmoji_ReloadSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.EmojiReload = func() (int, error) { return 42, nil }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "reload"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "42") {
		t.Errorf("expected key count '42' in reload success, got: %q", text)
	}
	if containsStr(text, "❌") {
		t.Errorf("reload success must not contain error symbol, got: %q", text)
	}
}

// TestEmoji_ReloadError checks that an error is surfaced correctly.
func TestEmoji_ReloadError(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.EmojiReload = func() (int, error) { return 0, errors.New("disk read failed") }

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "reload"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "disk read failed") {
		t.Errorf("expected error message in reload reply, got: %q", text)
	}
}

// TestEmoji_TestSuccess checks that a known key is resolved for the platform.
func TestEmoji_TestSuccess(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Platform = "discord"
	ctx.Emoji = dts.LoadEmoji(t.TempDir(), map[string]string{
		"emojiWeather": "<:weather:123>",
	})

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "test", "emojiWeather"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "emojiWeather") {
		t.Errorf("expected key name in reply, got: %q", text)
	}
	if !containsStr(text, "discord") {
		t.Errorf("expected platform in reply, got: %q", text)
	}
	if !containsStr(text, "<:weather:123>") {
		t.Errorf("expected resolved emoji in reply, got: %q", text)
	}
}

// TestEmoji_TestNotFound checks that an unknown key returns a not-found message.
func TestEmoji_TestNotFound(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Platform = "discord"
	ctx.Emoji = dts.LoadEmoji(t.TempDir(), map[string]string{})

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "test", "emojiBogus"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "emojiBogus") {
		t.Errorf("expected missing key name in reply, got: %q", text)
	}
	// Must not claim success.
	if containsStr(text, "✅") {
		t.Errorf("not-found reply must not contain success symbol, got: %q", text)
	}
}

// TestEmoji_TestMissingArg checks that missing key argument returns usage hint.
func TestEmoji_TestMissingArg(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Emoji = dts.LoadEmoji(t.TempDir(), map[string]string{})

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "test"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !containsStr(text, "Usage") {
		t.Errorf("expected usage hint in reply, got: %q", text)
	}
}

// TestEmoji_UnknownSub checks that an unknown subcommand returns an error.
func TestEmoji_UnknownSub(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true

	cmd := &PoracleAdminCommand{}
	replies := cmd.Run(ctx, []string{"emoji", "bogus"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if text == "" {
		t.Error("unknown-sub reply must be non-empty")
	}
	// Should name the subgroup.
	if !containsStr(text, "emoji") {
		t.Errorf("expected 'emoji' in unknown-sub reply, got: %q", text)
	}
}

// TestEmoji_NotLoaded checks that nil Emoji returns a not-loaded message.
func TestEmoji_NotLoaded(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Emoji = nil // not loaded

	cmd := &PoracleAdminCommand{}

	t.Run("list", func(t *testing.T) {
		replies := cmd.Run(ctx, []string{"emoji", "list"})
		if len(replies) == 0 {
			t.Fatal("expected at least one reply")
		}
		if replies[0].Text == "" {
			t.Error("not-loaded reply must be non-empty")
		}
	})

	t.Run("test", func(t *testing.T) {
		replies := cmd.Run(ctx, []string{"emoji", "test", "emojiWeather"})
		if len(replies) == 0 {
			t.Fatal("expected at least one reply")
		}
		if replies[0].Text == "" {
			t.Error("not-loaded reply must be non-empty")
		}
	})

	t.Run("reload_nil_func", func(t *testing.T) {
		ctx2, _ := testCtx(t)
		ctx2.IsAdmin = true
		ctx2.Emoji = nil
		ctx2.EmojiReload = nil
		replies := cmd.Run(ctx2, []string{"emoji", "reload"})
		if len(replies) == 0 {
			t.Fatal("expected at least one reply")
		}
		if replies[0].Text == "" {
			t.Error("nil EmojiReload must return non-empty reply")
		}
	})
}
