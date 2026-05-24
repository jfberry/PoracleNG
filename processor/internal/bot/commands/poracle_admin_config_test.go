package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// synthConfig builds a minimal *config.Config with known values for testing.
func synthConfig() *config.Config {
	return &config.Config{
		Discord: config.DiscordConfig{
			Token:        "super-secret-discord-token",
			CommandToken: "super-secret-command-token",
			Prefix:       "!",
			Admins:       []string{"123456789", "987654321"},
			Enabled:      true,
		},
		Telegram: config.TelegramConfig{
			Token:   "super-secret-telegram-token",
			Admins:  []string{"111222333"},
			Enabled: true,
		},
		Database: config.DatabaseConfig{
			Host:     "localhost",
			Port:     3306,
			User:     "poracle",
			Password: "db-secret-password",
			Database: "poracle",
		},
		Processor: config.ProcessorConfig{
			Host:      "127.0.0.1",
			Port:      3030,
			APISecret: "processor-api-secret",
		},
		General: config.GeneralConfig{
			Locale: "en",
		},
	}
}

// adminCtx builds a CommandContext with admin privileges and a synthetic config.
func adminCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Config = synthConfig()
	return ctx
}

// collectText joins all reply texts into one string for easier assertions.
func collectText(replies []bot.Reply) string {
	var parts []string
	for _, r := range replies {
		if r.Text != "" {
			parts = append(parts, r.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TestConfig_HelpNoSubcommand — "help" arg → help text mentions keys, section, full.
func TestConfig_HelpNoSubcommand(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"help"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	for _, want := range []string{"keys", "section", "full"} {
		if !containsStr(text, want) {
			t.Errorf("help text should mention %q; got:\n%s", want, text)
		}
	}
}

// TestConfig_FullDump — empty args via paConfigFull → multiple [section] headers.
func TestConfig_FullDump(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfigFull(ctx)

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	// Multiple section headers must appear.
	for _, sec := range []string{"[processor]", "[discord]", "[telegram]"} {
		if !containsStr(text, sec) {
			t.Errorf("full dump missing section %q; got (truncated):\n%.500s", sec, text)
		}
	}
}

// TestConfig_FullDump_ViaNoArgs — run with no args also triggers full dump.
func TestConfig_FullDump_ViaNoArgs(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	if !containsStr(text, "[discord]") {
		t.Errorf("no-arg run should trigger full dump; missing [discord]; got:\n%.500s", text)
	}
}

// TestConfig_KeysListing — "keys" → section names + counts.
func TestConfig_KeysListing(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"keys"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	// At minimum the sections we populated should appear.
	for _, sec := range []string{"discord", "telegram", "processor"} {
		if !containsStr(text, sec) {
			t.Errorf("keys listing should mention section %q; got:\n%s", sec, text)
		}
	}

	// Each line should contain a colon (section: N keys).
	if !containsStr(text, ":") {
		t.Errorf("keys listing should contain 'section: N keys' format; got:\n%s", text)
	}
}

// TestConfig_OneSection — "discord" → only the discord section.
func TestConfig_OneSection(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"discord"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	if !containsStr(text, "discord") {
		t.Errorf("section output should contain 'discord'; got:\n%s", text)
	}

	// The prefix field should appear (it's not redacted).
	if !containsStr(text, "prefix") {
		t.Errorf("section output should contain 'prefix'; got:\n%s", text)
	}
}

// TestConfig_UnknownSection — non-existent section → unknown-section reply.
func TestConfig_UnknownSection(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"nonexistent"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	if !containsStr(text, "nonexistent") {
		t.Errorf("unknown section reply should mention the bad section name; got:\n%s", text)
	}
}

// TestConfig_RedactionApplied — discord.token must be redacted.
func TestConfig_RedactionApplied(t *testing.T) {
	ctx := adminCtx(t)

	// Verify the synthetic config actually has the secret we expect.
	plainToken := "super-secret-discord-token"
	if ctx.Config.Discord.DiscordTokens()[0] != plainToken {
		t.Fatalf("synthetic config discord token unexpected: %q", ctx.Config.Discord.DiscordTokens()[0])
	}

	replies := paConfigFull(ctx)
	text := collectText(replies)

	if containsStr(text, plainToken) {
		t.Errorf("full dump MUST NOT contain plain discord token %q; it was leaked", plainToken)
	}
	if !containsStr(text, redactedLabel) {
		t.Errorf("full dump must contain redacted label %q; got:\n%.500s", redactedLabel, text)
	}

	// Same check for database password.
	if containsStr(text, "db-secret-password") {
		t.Errorf("full dump MUST NOT contain database password; it was leaked")
	}
	// And processor api_secret.
	if containsStr(text, "processor-api-secret") {
		t.Errorf("full dump MUST NOT contain processor api_secret; it was leaked")
	}
}

// TestConfig_AdminsListNotRedacted — discord.admins should be visible (not secret).
func TestConfig_AdminsListNotRedacted(t *testing.T) {
	ctx := adminCtx(t)
	// We seeded admins: ["123456789", "987654321"]

	replies := paConfigFull(ctx)
	text := collectText(replies)

	if !containsStr(text, "123456789") {
		t.Errorf("discord.admins should NOT be redacted; 123456789 missing from output:\n%.500s", text)
	}
	if !containsStr(text, "987654321") {
		t.Errorf("discord.admins should NOT be redacted; 987654321 missing from output:\n%.500s", text)
	}
}

// TestConfig_SectionRedactionApplied — per-section dump also redacts.
func TestConfig_SectionRedactionApplied(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"discord"})
	text := collectText(replies)

	if containsStr(text, "super-secret-discord-token") {
		t.Errorf("per-section dump MUST NOT contain plain discord token; it was leaked")
	}
	if containsStr(text, "super-secret-command-token") {
		t.Errorf("per-section dump MUST NOT contain plain command_token; it was leaked")
	}
	if !containsStr(text, redactedLabel) {
		t.Errorf("per-section dump must contain redacted label %q", redactedLabel)
	}
}

// TestConfig_SearchByPath — search "admin" should match discord.admins / telegram.admins.
func TestConfig_SearchByPath(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"search", "admin"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	for _, want := range []string{"discord.admins", "telegram.admins"} {
		if !containsStr(text, want) {
			t.Errorf("search 'admin' should match %q; got:\n%s", want, text)
		}
	}
}

// TestConfig_SearchByValue — search for the literal "127.0.0.1" should find
// processor.host.
func TestConfig_SearchByValue(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"search", "127.0.0.1"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	if !containsStr(text, "processor.host") {
		t.Errorf("search '127.0.0.1' should find processor.host; got:\n%s", text)
	}
}

// TestConfig_SearchNeverLeaksSecrets — a search whose term substring-matches
// a redacted value path must still render the value as "***", not the
// actual secret. Belt-and-braces — renderLeaf handles this but the test
// pins it so future refactors don't accidentally bypass redaction.
func TestConfig_SearchNeverLeaksSecrets(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"search", "token"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)

	for _, leak := range []string{"super-secret-discord-token", "super-secret-command-token", "super-secret-telegram-token"} {
		if containsStr(text, leak) {
			t.Errorf("search 'token' MUST NOT leak plain secret %q", leak)
		}
	}
	if !containsStr(text, redactedLabel) {
		t.Errorf("search 'token' should still contain the redacted label %q; got:\n%.500s", redactedLabel, text)
	}
}

// TestConfig_SearchNoMatches — a search term that hits nothing returns the
// no-matches text rather than an empty reply.
func TestConfig_SearchNoMatches(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"search", "zzzzzzzz"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	if !containsStr(text, "zzzzzzzz") {
		t.Errorf("no-matches reply should echo the search term; got:\n%s", text)
	}
}

// TestConfig_SearchEmptyTerm — `search` with no further args returns usage.
func TestConfig_SearchEmptyTerm(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"search"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := collectText(replies)
	if !containsStr(text, "search") {
		t.Errorf("usage should mention `search`; got:\n%s", text)
	}
}

// TestConfig_SearchFindAlias — `find` is an alias for `search`.
func TestConfig_SearchFindAlias(t *testing.T) {
	ctx := adminCtx(t)
	replies := paConfig.run(ctx, []string{"find", "admin"})

	if len(replies) == 0 {
		t.Fatal("expected at least one reply for `find admin`")
	}
	text := collectText(replies)
	if !containsStr(text, "discord.admins") {
		t.Errorf("`find admin` should behave like `search admin`; got:\n%s", text)
	}
}

// TestConfig_NilConfig — nil config is handled gracefully.
func TestConfig_NilConfig(t *testing.T) {
	ctx, _ := testCtx(t)
	ctx.IsAdmin = true
	ctx.Config = nil

	replies := paConfigFull(ctx)
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	// Should not panic; must return some text.
	if replies[0].Text == "" {
		t.Error("nil-config reply must have non-empty text")
	}
}

// TestConfig_SliceOfSecretsRedacted — weather.accuweather_api_keys is a
// []string; every element must be redacted and the raw values must not appear.
func TestConfig_SliceOfSecretsRedacted(t *testing.T) {
	ctx := adminCtx(t)
	ctx.Config.Weather.AccuWeatherAPIKeys = []string{"super-secret-1", "super-secret-2"}

	replies := paConfigFull(ctx)
	text := collectText(replies)

	if containsStr(text, "super-secret-1") || containsStr(text, "super-secret-2") {
		t.Errorf("slice secret leaked into output: %.500s", text)
	}
	if !containsStr(text, redactedLabel) {
		t.Errorf("expected redaction marker %q in output: %.500s", redactedLabel, text)
	}
}

// TestConfig_NestedStructSecretRedacted — database.scanner.password is a
// nested struct field; the plain value must not appear and redaction must fire.
func TestConfig_NestedStructSecretRedacted(t *testing.T) {
	ctx := adminCtx(t)
	ctx.Config.Database.Scanner.Password = "scanner-db-secret"

	replies := paConfig.run(ctx, []string{"database"})
	text := collectText(replies)

	if containsStr(text, "scanner-db-secret") {
		t.Errorf("nested struct secret leaked into output: %.500s", text)
	}
	if !containsStr(text, redactedLabel) {
		t.Errorf("expected redaction marker %q in output: %.500s", redactedLabel, text)
	}
}

// TestShouldRedact_Heuristic — confirm the defensive heuristic fires for
// unlisted but sensitive-sounding field names.
func TestShouldRedact_Heuristic(t *testing.T) {
	cases := []struct {
		path   string
		expect bool
	}{
		{"discord.token", true},
		{"discord.command_token", true},           // HasSuffix("command_token","token")
		{"telegram.token", true},
		{"database.password", true},
		{"processor.api_secret", true},
		{"geocoding.geocoding_key", true},         // explicit map + HasSuffix("geocoding_key","key")
		{"geocoding.static_key", true},            // explicit map + HasSuffix("static_key","key")
		{"weather.accuweather_api_keys", true},    // explicit map + HasSuffix("accuweather_api_keys","keys")
		{"geofence.koji.bearer_token", true},
		{"some.unknown.api_key", true},            // heuristic: exact "api_key"
		{"some.unknown.secret", true},             // heuristic: exact "secret"
		{"some.unknown.geocoding_key", true},      // heuristic: HasSuffix(*,"key")
		{"some.unknown.static_key", true},         // heuristic: HasSuffix(*,"key")
		{"some.unknown.api_keys", true},           // heuristic: HasSuffix(*,"keys")
		{"discord.admins", false},                 // explicit not-redacted
		{"discord.prefix", false},                 // not sensitive
		{"general.locale", false},
	}

	for _, c := range cases {
		got := shouldRedact(c.path)
		if got != c.expect {
			t.Errorf("shouldRedact(%q) = %v, want %v", c.path, got, c.expect)
		}
	}
}
