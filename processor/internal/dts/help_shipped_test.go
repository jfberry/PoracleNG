package dts

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestShippedHelpTemplatesLoadAndRender — load the real fallbacks/dts/help/*
// files the repo ships and verify each one compiles and renders without
// error for both Discord and Telegram. Catches malformed JSON, typos in
// Handlebars blocks, and wrong platform/language metadata before deploy.
//
// Locate fallbacks by walking up from this test file's directory so the
// test works regardless of the cwd the runner uses.
func TestShippedHelpTemplatesLoadAndRender(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	// .../processor/internal/dts/help_shipped_test.go → repo root is 3 levels up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	fallbackDir := filepath.Join(repoRoot, "fallbacks")

	ts, err := LoadTemplates(t.TempDir(), fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	helpIDs := []string{
		"index",
		"track", "untrack", "raid", "egg", "quest",
		"invasion", "incident", "lure", "nest", "gym", "fort", "maxbattle",
		"tracked", "script",
		"area", "location", "profile", "language",
		"start", "stop", "poracle", "unregister",
		"enable", "disable", "broadcast", "userlist", "community",
	}

	view := map[string]any{"prefix": "!"}

	for _, id := range helpIDs {
		for _, platform := range []string{"discord", "telegram"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				tmpl := ts.Get("help", platform, id, "en")
				if tmpl == nil {
					t.Fatalf("no template found — expected fallbacks/dts/help/%s.json to provide a platform-wildcard entry", id)
				}
				result, err := tmpl.Exec(view)
				if err != nil {
					t.Fatalf("render failed: %v", err)
				}
				if result == "" {
					t.Fatalf("rendered empty string")
				}
			})
		}
	}
}

// TestShippedHelpFallsBackForForeignLanguages — a zh-cn / de / fr user
// who doesn't have translated help entries should still get the English
// content as a fallback, not "unknown topic". Shipped entries use
// language: "" (wildcard) so level 2 of the selection chain matches any
// requested language. When someone ships a localised entry with an
// explicit language value, their entry wins (level 1 exact match).
func TestShippedHelpFallsBackForForeignLanguages(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	ts, err := LoadTemplates(t.TempDir(), filepath.Join(repoRoot, "fallbacks"))
	if err != nil {
		t.Fatal(err)
	}

	for _, lang := range []string{"zh-cn", "ja", "de", "fr", "nb-no", "xx-unknown"} {
		for _, platform := range []string{"discord", "telegram"} {
			t.Run(lang+"/"+platform, func(t *testing.T) {
				tmpl := ts.Get("help", platform, "track", lang)
				if tmpl == nil {
					t.Fatalf("!help track for language=%q platform=%q should fall back to the English wildcard entry, got nil", lang, platform)
				}
			})
		}
	}
}
