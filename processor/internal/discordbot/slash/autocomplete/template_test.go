package autocomplete

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// templateTestDeps writes a tiny dts.json into a temp config dir and loads
// it via the real LoadTemplates path so the test exercises the same
// TemplateSummaryDetailed code Template depends on at runtime.
func templateTestDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	tmp := t.TempDir()
	fixture := `[
		{"type":"monster","id":"1","platform":"discord","language":"","default":true,"template":"x"},
		{"type":"monster","id":"verbose","platform":"discord","language":"","template":"x"},
		{"type":"monster","id":"compact","platform":"discord","language":"","template":"x"},
		{"type":"monster","id":"telegram-only","platform":"telegram","language":"","template":"x"},
		{"type":"raid","id":"1","platform":"discord","language":"","default":true,"template":"x"}
	]`
	if err := os.WriteFile(filepath.Join(tmp, "dts.json"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	store, err := dts.LoadTemplates(tmp, filepath.Join(tmp, "missing-fallback"))
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return &bot.BotDeps{DTS: store}
}

func TestTemplate_EmptyFocusedReturnsAll(t *testing.T) {
	deps := templateTestDeps(t)
	out := Template(context.Background(), deps, "", "monster", "discord", "en")
	if len(out) != 3 {
		t.Fatalf("got %d entries, want 3 (1, verbose, compact); got=%+v", len(out), out)
	}
}

func TestTemplate_SubstringMatch(t *testing.T) {
	deps := templateTestDeps(t)
	out := Template(context.Background(), deps, "verb", "monster", "discord", "en")
	if len(out) != 1 || out[0].Name != "verbose" {
		t.Errorf("got %+v, want single 'verbose'", out)
	}
}

func TestTemplate_PlatformIsolation(t *testing.T) {
	deps := templateTestDeps(t)
	out := Template(context.Background(), deps, "", "monster", "telegram", "en")
	if len(out) != 1 || out[0].Name != "telegram-only" {
		t.Errorf("expected only the telegram template, got %+v", out)
	}
}

func TestTemplate_UnknownTypeReturnsNil(t *testing.T) {
	deps := templateTestDeps(t)
	out := Template(context.Background(), deps, "", "no-such-type", "discord", "en")
	if out != nil {
		t.Errorf("expected nil for unknown type, got %+v", out)
	}
}

func TestFilterStringChoices(t *testing.T) {
	got := filterStringChoices([]string{"alpha", "beta", "gamma", "delta"}, "lp")
	// only "alpha" contains "lp"
	if len(got) != 1 {
		t.Fatalf("got %d, want 1 entry: %+v", len(got), got)
	}
	if got[0].Name != "alpha" {
		t.Errorf("got %q, want 'alpha'", got[0].Name)
	}
}
