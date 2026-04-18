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
