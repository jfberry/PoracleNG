package autocomplete

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// helpTopicDeps builds BotDeps over a DTS store holding help entries with the
// platform-agnostic ("") platform the shipped fallbacks actually use, plus one
// discord-specific entry to prove both are picked up.
func helpTopicDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dts.json"), []byte(`[
		{"type":"help","id":"track","platform":"","language":"","template":{"x":1}},
		{"type":"help","id":"index","platform":"","language":"","template":{"x":1}},
		{"type":"help","id":"enable","platform":"","language":"","template":{"x":1}},
		{"type":"help","id":"raid","platform":"discord","language":"","template":{"x":1}},
		{"type":"monster","id":"1","platform":"discord","language":"","template":{"x":1}}
	]`), 0644); err != nil {
		t.Fatal(err)
	}
	fb := filepath.Join(dir, "fb")
	if err := os.MkdirAll(fb, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fb, "dts.json"), []byte(`[]`), 0644); err != nil {
		t.Fatal(err)
	}
	ts, err := dts.LoadTemplates(dir, fb)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return &bot.BotDeps{DTS: ts}
}

func values(choices []*discordgo.ApplicationCommandOptionChoice) []string {
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.Value.(string))
	}
	return out
}

func contains(vals []string, want string) bool {
	return slices.Contains(vals, want)
}

// The shipped help entries carry platform "" — a platform-scoped lookup alone
// returns nothing, which is exactly why /help's topic autocomplete was empty.
func TestHelpTopicIncludesPlatformAgnosticEntries(t *testing.T) {
	got := values(HelpTopic(helpTopicDeps(t), "", "discord", true))
	for _, want := range []string{"track", "index"} {
		if !contains(got, want) {
			t.Errorf("expected platform-agnostic topic %q in %v", want, got)
		}
	}
	if !contains(got, "raid") {
		t.Errorf("expected platform-specific topic %q in %v", "raid", got)
	}
}

// Only help entries — a monster template is not a help topic.
func TestHelpTopicExcludesOtherTypes(t *testing.T) {
	if got := values(HelpTopic(helpTopicDeps(t), "", "discord", true)); contains(got, "1") {
		t.Errorf("non-help entry leaked into topics: %v", got)
	}
}

// Suggesting a topic the command would then refuse with 🙅 is worse than
// suggesting nothing, so admin-only topics are hidden from non-admins.
func TestHelpTopicHidesAdminOnlyFromNonAdmins(t *testing.T) {
	deps := helpTopicDeps(t)

	asAdmin := values(HelpTopic(deps, "", "discord", true))
	if !contains(asAdmin, "enable") {
		t.Errorf("admin should see admin-only topic: %v", asAdmin)
	}

	asUser := values(HelpTopic(deps, "", "discord", false))
	if contains(asUser, "enable") {
		t.Errorf("non-admin must not be offered %q: %v", "enable", asUser)
	}
	if !contains(asUser, "track") {
		t.Errorf("non-admin should still see ordinary topics: %v", asUser)
	}
}

func TestHelpTopicFiltersByFocused(t *testing.T) {
	got := values(HelpTopic(helpTopicDeps(t), "tra", "discord", true))
	if !contains(got, "track") {
		t.Errorf("expected track for focused %q: %v", "tra", got)
	}
	if contains(got, "index") {
		t.Errorf("index should be filtered out by focused %q: %v", "tra", got)
	}
}

func TestHelpTopicNilDeps(t *testing.T) {
	if got := HelpTopic(nil, "", "discord", true); got != nil {
		t.Errorf("expected nil for nil deps, got %v", got)
	}
	if got := HelpTopic(&bot.BotDeps{}, "", "discord", true); got != nil {
		t.Errorf("expected nil when DTS is unset, got %v", got)
	}
}
