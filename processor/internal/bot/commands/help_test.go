package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// helpTestDTS loads the real shipped fallbacks so help tests render the
// actual files we ship, not a toy template.
func helpTestDTS(t *testing.T) *dts.TemplateStore {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// processor/internal/bot/commands/help_test.go → repo root is 4 up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	ts, err := dts.LoadTemplates(t.TempDir(), filepath.Join(repoRoot, "fallbacks"))
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return ts
}

// TestHelpAdminTopicsGated — a non-admin asking for an admin-only help
// topic gets the "unknown topic" reply, not the admin command syntax.
// Admin users see the real help.
func TestHelpAdminTopicsGated(t *testing.T) {
	ts := helpTestDTS(t)

	cases := []string{"enable", "disable", "broadcast", "userlist", "community"}

	for _, topic := range cases {
		t.Run(topic+"/non-admin", func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = false
			ctx.DTS = ts
			cmd := &HelpCommand{}
			replies := cmd.Run(ctx, []string{topic})
			if len(replies) == 0 {
				t.Fatal("expected at least one reply")
			}
			if replies[0].React != "🙅" {
				t.Errorf("non-admin asking for !help %s should get the 🙅 unknown-topic reply, got react=%q text=%q", topic, replies[0].React, replies[0].Text)
			}
			if replies[0].Embed != nil {
				t.Error("non-admin should never see the admin help embed")
			}
		})

		t.Run(topic+"/admin", func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.IsAdmin = true
			ctx.DTS = ts
			cmd := &HelpCommand{}
			replies := cmd.Run(ctx, []string{topic})
			if len(replies) == 0 {
				t.Fatalf("admin asking for !help %s should get the admin help, got no reply", topic)
			}
			// Discord path returns Embed; Telegram would return Text. Either
			// way the non-empty payload proves the lookup went through.
			if replies[0].Embed == nil && replies[0].Text == "" {
				t.Errorf("admin asking for !help %s got an empty reply", topic)
			}
		})
	}
}

// TestHelpIndexHidesAdminSectionForNonAdmins — the user-facing index
// shouldn't leak the admin-commands field to non-admin viewers. We
// render the index for both isAdmin=true and isAdmin=false and assert
// the admin field is present only in the former.
func TestHelpIndexHidesAdminSectionForNonAdmins(t *testing.T) {
	ts := helpTestDTS(t)

	render := func(isAdmin bool) string {
		ctx, _ := testCtx(t)
		ctx.IsAdmin = isAdmin
		ctx.DTS = ts
		cmd := &HelpCommand{}
		replies := cmd.Run(ctx, nil) // no args → index
		if len(replies) == 0 {
			t.Fatalf("expected a reply (isAdmin=%v)", isAdmin)
		}
		// Discord returns Embed JSON; flatten to string for substring search.
		if replies[0].Embed != nil {
			return string(replies[0].Embed)
		}
		var sb strings.Builder
		for _, r := range replies {
			sb.WriteString(r.Text)
		}
		return sb.String()
	}

	adminView := render(true)
	userView := render(false)

	if !strings.Contains(adminView, "Admin commands") {
		t.Errorf("admin view should contain \"Admin commands\" section, got:\n%s", adminView)
	}
	if strings.Contains(userView, "Admin commands") {
		t.Errorf("non-admin view should NOT contain \"Admin commands\" section, got:\n%s", userView)
	}
	// Basic sanity — both views should carry the tracking commands section.
	for _, v := range []string{adminView, userView} {
		if !strings.Contains(v, "Tracking commands") {
			t.Errorf("index view missing tracking commands section:\n%s", v)
		}
	}
}

// TestHelp_TrackTopicResolves — !help track should return the
// help/track DTS template (or cmd.track.usage fallback), not the
// "unknown topic" error, and must not contain any mention of ping/
// @-mention (that's an admin-configured channel concept, not a user
// filter).
func TestHelp_TrackTopicResolves(t *testing.T) {
	ts := helpTestDTS(t)
	ctx, _ := testCtx(t)
	ctx.DTS = ts
	cmd := &HelpCommand{}
	replies := cmd.Run(ctx, []string{"track"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply")
	}
	// Flatten both Embed and Text into a single string for substring
	// checks — the Discord path returns an Embed, Telegram returns Text.
	var body strings.Builder
	if replies[0].Embed != nil {
		body.Write(replies[0].Embed)
	}
	for _, r := range replies {
		body.WriteString(r.Text)
	}
	got := body.String()
	if !strings.Contains(got, "track") || strings.Contains(got, "unknown topic") {
		t.Fatalf("!help track should render the track help content, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "ping") {
		t.Fatalf("!help track must not mention ping (admin concern, not user-facing): %q", got)
	}
}

// TestHelpPicksUserCustomWhenCallerHasEmptyLanguage simulates the
// reported end-user scenario: operator has a custom help entry shaped
// {"type":"help","id":"1","default":true,"language":"en","platform":"discord"},
// and a caller whose ctx.Language is "" (unregistered user, or user
// who never ran !language). Before the language fallback, the lookup
// passed language="" which fails to match entry-language="en" at
// priority levels 3 and 4, so pass-1 (user) returned nil and pass-2
// (readonly) picked the shipped help/index. After the fix the effective
// language resolves to the server default locale, so the user's entry
// matches at level 3 via the default flag.
func TestHelpPicksUserCustomWhenCallerHasEmptyLanguage(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	fallbackDir := filepath.Join(repoRoot, "fallbacks")

	configDir := t.TempDir()
	userHelp := `[{
		"type": "help",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {
			"embed": {
				"title": "Custom Operator Help",
				"fields": [{"name": "Area tracking", "value": "area list"}]
			}
		}
	}]`
	if err := os.WriteFile(filepath.Join(configDir, "dts.json"), []byte(userHelp), 0644); err != nil {
		t.Fatal(err)
	}

	ts, err := dts.LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	ctx, _ := testCtx(t)
	ctx.DTS = ts
	ctx.Language = "" // unregistered / never-ran-!language user
	ctx.Config = &config.Config{}
	ctx.Config.General.Locale = "en"

	cmd := &HelpCommand{}
	replies := cmd.Run(ctx, nil)
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}

	// Flatten the reply to a string to search for the distinctive title.
	var out strings.Builder
	if replies[0].Embed != nil {
		out.Write(replies[0].Embed)
	}
	for _, r := range replies {
		out.WriteString(r.Text)
	}

	// The help renderer blanks the title, so we search for the custom
	// FIELD name — unique enough to distinguish from the shipped help/index.
	if !strings.Contains(out.String(), "Area tracking") {
		t.Errorf("expected user's custom help fields to be rendered, got:\n%s", out.String())
	}
	// Sanity: make sure we didn't accidentally render the shipped index,
	// which has distinctive field name "Tracking commands".
	if strings.Contains(out.String(), "Tracking commands") {
		t.Errorf("user's custom help was shadowed by shipped help/index:\n%s", out.String())
	}
}

// helpReplyText flattens a help reply (Discord embed JSON or Telegram text)
// into one string for substring checks.
func helpReplyText(replies []bot.Reply) string {
	var sb strings.Builder
	for _, r := range replies {
		if r.Embed != nil {
			sb.Write(r.Embed)
		}
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// TestHelpTopicShowsDescription — help templates put the topic overview
// in the embed description. Discord keeps it in the embed; Telegram prints
// it ahead of the fields. The title is still dropped on both.
func TestHelpTopicShowsDescription(t *testing.T) {
	configDir := t.TempDir()
	topic := `[{
		"type": "help",
		"id": "widget",
		"platform": "",
		"language": "",
		"template": {
			"embed": {
				"title": "!widget",
				"description": "Widget overview text",
				"fields": [{"name": "Status", "value": "widget status"}]
			}
		}
	}]`
	if err := os.WriteFile(filepath.Join(configDir, "dts.json"), []byte(topic), 0644); err != nil {
		t.Fatal(err)
	}
	ts, err := dts.LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	const overview = "Widget overview text"

	t.Run("discord", func(t *testing.T) {
		ctx, _ := testCtx(t)
		ctx.DTS = ts
		replies := (&HelpCommand{}).Run(ctx, []string{"widget"})
		if len(replies) != 1 || replies[0].Embed == nil {
			t.Fatalf("expected one embed reply, got %+v", replies)
		}
		var msg struct {
			Embed struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"embed"`
		}
		if err := json.Unmarshal(replies[0].Embed, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Embed.Title != "" {
			t.Errorf("title should be blanked, got %q", msg.Embed.Title)
		}
		if msg.Embed.Description != overview {
			t.Errorf("description should be kept, got %q", msg.Embed.Description)
		}
	})

	t.Run("telegram", func(t *testing.T) {
		ctx, _ := testCtx(t)
		ctx.DTS = ts
		ctx.TargetType = bot.TypeTelegramUser
		replies := (&HelpCommand{}).Run(ctx, []string{"widget"})
		if len(replies) == 0 || replies[0].Text == "" {
			t.Fatalf("expected text replies, got %+v", replies)
		}
		first := replies[0].Text
		if !strings.Contains(first, overview) {
			t.Fatalf("first reply should open with the description, got:\n%s", first)
		}
		if i := strings.Index(first, "Status"); i >= 0 && i < strings.Index(first, overview) {
			t.Errorf("description should come before the fields, got:\n%s", first)
		}
	})
}

// TestHelpGreetingFallbackDropsDescription — with no help/index installed,
// !help falls back to the greeting template, whose description is the
// "thank you for registering" text. That stays hidden; the fields show.
func TestHelpGreetingFallbackDropsDescription(t *testing.T) {
	configDir := t.TempDir()
	greeting := `[{
		"type": "greeting",
		"id": "1",
		"platform": "",
		"language": "",
		"default": true,
		"template": {
			"embed": {
				"title": "Welcome",
				"description": "Thank you for registering",
				"fields": [{"name": "General commands", "value": "help"}]
			}
		}
	}]`
	if err := os.WriteFile(filepath.Join(configDir, "dts.json"), []byte(greeting), 0644); err != nil {
		t.Fatal(err)
	}
	ts, err := dts.LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	for _, targetType := range []string{bot.TypeDiscordUser, bot.TypeTelegramUser} {
		t.Run(targetType, func(t *testing.T) {
			ctx, _ := testCtx(t)
			ctx.DTS = ts
			ctx.TargetType = targetType
			got := helpReplyText((&HelpCommand{}).Run(ctx, nil))
			if !strings.Contains(got, "General commands") {
				t.Fatalf("expected the greeting fields, got:\n%s", got)
			}
			if strings.Contains(got, "Thank you for registering") {
				t.Errorf("greeting description should not appear in !help, got:\n%s", got)
			}
		})
	}
}

// TestHelpRoleIsDiscordOnly — !role only exists on Discord, so its help
// topic is a Discord-only entry and the index lists it only on Discord
// when role_subscriptions are configured.
func TestHelpRoleIsDiscordOnly(t *testing.T) {
	ts := helpTestDTS(t)
	withRoles := &config.Config{}
	withRoles.Discord.RoleSubscriptions = []config.RoleSubscriptionEntry{{}}

	t.Run("discord topic", func(t *testing.T) {
		ctx, _ := testCtx(t)
		ctx.DTS = ts
		replies := (&HelpCommand{}).Run(ctx, []string{"role"})
		if len(replies) == 0 || replies[0].Embed == nil {
			t.Fatalf("expected the role help embed, got %+v", replies)
		}
	})

	t.Run("telegram topic", func(t *testing.T) {
		ctx, _ := testCtx(t)
		ctx.DTS = ts
		ctx.TargetType = bot.TypeTelegramUser
		replies := (&HelpCommand{}).Run(ctx, []string{"role"})
		if len(replies) == 0 || replies[0].React != "🙅" {
			t.Fatalf("telegram !help role should be an unknown topic, got %+v", replies)
		}
	})

	index := func(targetType string, cfg *config.Config) string {
		ctx, _ := testCtx(t)
		ctx.DTS = ts
		ctx.TargetType = targetType
		if cfg != nil {
			ctx.Config = cfg
		}
		return helpReplyText((&HelpCommand{}).Run(ctx, nil))
	}
	if got := index(bot.TypeDiscordUser, withRoles); !strings.Contains(got, "help role") {
		t.Errorf("discord index with role_subscriptions should list role:\n%s", got)
	}
	if got := index(bot.TypeDiscordUser, &config.Config{}); strings.Contains(got, "help role") {
		t.Errorf("discord index without role_subscriptions should not list role:\n%s", got)
	}
	if got := index(bot.TypeTelegramUser, withRoles); strings.Contains(got, "help role") {
		t.Errorf("telegram index should never list role:\n%s", got)
	}
}
