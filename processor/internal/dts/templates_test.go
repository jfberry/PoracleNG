package dts

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDTS(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "dts.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateLoadFromConfig(t *testing.T) {
	configDir := t.TempDir()
	fallbackDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"embed": {"title": "Config {{name}}"}}
	}]`)

	writeDTS(t, fallbackDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"embed": {"title": "Fallback {{name}}"}}
	}]`)

	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}
}

func TestTemplateLoadFallback(t *testing.T) {
	configDir := t.TempDir() // no dts.json here
	fallbackDir := t.TempDir()

	writeDTS(t, fallbackDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"embed": {"title": "Fallback {{name}}"}}
	}]`)

	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil template from fallback")
	}
}

func TestTemplateSelectionChain(t *testing.T) {
	configDir := t.TempDir()

	// Setup entries covering all selection chain levels:
	// - id=1 type=monster platform=discord language=en (exact)
	// - id=1 type=monster platform=discord language="" (no-lang match)
	// - default type=monster platform=discord language=fr
	// - default type=monster platform=discord language="" (default no-lang)
	// - default type=monster platform=discord language=de (default any-lang fallback)
	writeDTS(t, configDir, `[
		{"type":"monster","id":"1","platform":"discord","language":"en","default":false,"template":{"content":"exact-en"}},
		{"type":"monster","id":"1","platform":"discord","language":"","default":false,"template":{"content":"exact-nolang"}},
		{"type":"monster","id":"99","platform":"discord","language":"fr","default":true,"template":{"content":"default-fr"}},
		{"type":"monster","id":"99","platform":"discord","language":"","default":true,"template":{"content":"default-nolang"}},
		{"type":"monster","id":"99","platform":"discord","language":"de","default":true,"template":{"content":"default-de"}}
	]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Level 1: exact type+id+platform+language
	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("level 1: expected non-nil")
	}

	// Level 2: type+id+platform, no language match → falls to entry with empty language
	tmpl = ts.Get("monster", "discord", "1", "ja")
	if tmpl == nil {
		t.Fatal("level 2: expected non-nil")
	}

	// Level 3: default+type+platform+language
	tmpl = ts.Get("monster", "discord", "5", "fr")
	if tmpl == nil {
		t.Fatal("level 3: expected non-nil")
	}

	// Level 4: default+type+platform (no language on entry)
	// Request language "xx" — no exact match with id=5, no empty-lang id=5,
	// no default with language="xx", but there's a default with empty language
	tmpl = ts.Get("monster", "discord", "5", "xx")
	if tmpl == nil {
		t.Fatal("level 4: expected non-nil")
	}

	// Level 5: default+type+platform (any language — last resort)
	// Remove the empty-language default to test this level
	configDir2 := t.TempDir()
	writeDTS(t, configDir2, `[
		{"type":"raid","id":"1","platform":"telegram","language":"de","default":true,"template":{"content":"only-de"}}
	]`)
	ts2, err := LoadTemplates(configDir2, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Request language "en" — no exact match, no empty-lang, falls to any-lang default
	tmpl = ts2.Get("raid", "telegram", "99", "en")
	if tmpl == nil {
		t.Fatal("level 5: expected non-nil")
	}

	// No match at all
	tmpl = ts2.Get("raid", "discord", "1", "en")
	if tmpl == nil {
		// This is expected to be nil since there's no discord entry
	}
}

func TestTemplateNumericID(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": 1,
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "numeric id {{name}}"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil with numeric id")
	}
}

func TestTemplateIDCaseInsensitive(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "ABC",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "test"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "abc", "en")
	if tmpl == nil {
		t.Fatal("expected case-insensitive ID match")
	}
}

func TestTemplateArrayJoining(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {
			"embed": {
				"description": ["Line 1\n", "Line 2\n", "Line 3"],
				"title": ["Part A", "Part B"]
			},
			"content": ["Hello ", "World"]
		}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil")
	}
	// The template compiled — arrays were joined to strings before JSON marshal
}

func TestTemplateIncludeDirective(t *testing.T) {
	configDir := t.TempDir()
	dtsDir := filepath.Join(configDir, "dts")
	if err := os.MkdirAll(dtsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the include file
	if err := os.WriteFile(filepath.Join(dtsDir, "pvp.hbs"), []byte("PVP: {{rank}}"), 0644); err != nil {
		t.Fatal(err)
	}

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "Pokemon @include pvp.hbs"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil with @include")
	}
}

func TestTemplateExternalFile(t *testing.T) {
	configDir := t.TempDir()
	dtsDir := filepath.Join(configDir, "dts")
	if err := os.MkdirAll(dtsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the external template file
	extTemplate := `{"content": "External {{name}}"}`
	if err := os.WriteFile(filepath.Join(dtsDir, "custom.json"), []byte(extTemplate), 0644); err != nil {
		t.Fatal(err)
	}

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"templateFile": "dts/custom.json"
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl == nil {
		t.Fatal("expected non-nil from templateFile")
	}
}

func TestTemplateCacheHit(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "cached {{name}}"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// First call compiles
	tmpl1 := ts.Get("monster", "discord", "1", "en")
	if tmpl1 == nil {
		t.Fatal("expected non-nil")
	}

	// Second call should return cached pointer
	tmpl2 := ts.Get("monster", "discord", "1", "en")
	if tmpl2 == nil {
		t.Fatal("expected non-nil cached")
	}
	if tmpl1 != tmpl2 {
		t.Fatal("expected same pointer from cache")
	}
}

func TestTemplateCompilationError(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "{{#if}}malformed{{/each}}"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Should return nil, not panic
	tmpl := ts.Get("monster", "discord", "1", "en")
	if tmpl != nil {
		t.Fatal("expected nil for malformed template")
	}
}

func TestTemplateNoMatch(t *testing.T) {
	configDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "test"}
	}]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tmpl := ts.Get("raid", "telegram", "1", "en")
	if tmpl != nil {
		t.Fatal("expected nil for non-matching type/platform")
	}
}

func TestTemplateReloadClearsCache(t *testing.T) {
	configDir := t.TempDir()
	fallbackDir := t.TempDir()

	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "version1"}
	}]`)

	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}

	// Populate cache
	tmpl1 := ts.Get("monster", "discord", "1", "en")
	if tmpl1 == nil {
		t.Fatal("expected non-nil")
	}

	// Update the file with different content
	writeDTS(t, configDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"content": "version2"}
	}]`)

	// Reload
	if err := ts.Reload(configDir, fallbackDir); err != nil {
		t.Fatal(err)
	}

	// Cache should be cleared — new compilation
	tmpl2 := ts.Get("monster", "discord", "1", "en")
	if tmpl2 == nil {
		t.Fatal("expected non-nil after reload")
	}

	// Should be different pointer since cache was cleared
	if tmpl1 == tmpl2 {
		t.Fatal("expected different pointer after reload")
	}
}

// TestTemplatePlatformWildcard — entries with platform: "" match any
// platform once the platform-specific selection chain returns nothing,
// matching the pattern empty Language already uses as a wildcard.
// Platform-specific entries always win so the fallback only kicks in
// when nothing more specific is available.
func TestTemplatePlatformWildcard(t *testing.T) {
	configDir := t.TempDir()
	writeDTS(t, configDir, `[
		{
			"type": "help",
			"id": "track",
			"platform": "",
			"language": "en",
			"template": {"embed": {"title": "Shared {{prefix}}track help"}}
		},
		{
			"type": "help",
			"id": "raid",
			"platform": "discord",
			"language": "en",
			"template": {"embed": {"title": "Discord-specific raid help"}}
		}
	]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Discord requester hits the platform: "" entry for track (no
	// discord-specific track entry exists).
	if tmpl := ts.Get("help", "discord", "track", "en"); tmpl == nil {
		t.Error("discord requesting help/track should match the platform-wildcard entry")
	}
	// Telegram requester hits the same wildcard entry for track.
	if tmpl := ts.Get("help", "telegram", "track", "en"); tmpl == nil {
		t.Error("telegram requesting help/track should also match the platform-wildcard entry")
	}
	// Discord requester for raid gets the platform-specific entry — the
	// wildcard tier only runs if the platform-specific tier returns nil.
	tmpl := ts.Get("help", "discord", "raid", "en")
	if tmpl == nil {
		t.Fatal("discord requesting help/raid should match the discord entry")
	}
	// Telegram requester for raid — no telegram or wildcard entry —
	// returns nil (absence proof, not a fallthrough).
	if ts.Get("help", "telegram", "raid", "en") != nil {
		t.Error("telegram requesting help/raid should return nil (no match)")
	}
}

// TestTemplatePlatformSpecificWinsOverWildcard — when both a
// platform-specific and a platform-wildcard entry exist, the specific
// one wins regardless of file order.
func TestTemplatePlatformSpecificWinsOverWildcard(t *testing.T) {
	configDir := t.TempDir()
	writeDTS(t, configDir, `[
		{
			"type": "help",
			"id": "track",
			"platform": "",
			"language": "en",
			"template": {"embed": {"title": "Shared"}}
		},
		{
			"type": "help",
			"id": "track",
			"platform": "discord",
			"language": "en",
			"template": {"embed": {"title": "Discord"}}
		}
	]`)

	ts, err := LoadTemplates(configDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	result, err := ts.Get("help", "discord", "track", "en").Exec(nil)
	if err != nil {
		t.Fatal(err)
	}
	// The discord-specific entry should win; if the wildcard were winning
	// we'd see "Shared" in the rendered JSON.
	if !contains(result, "Discord") {
		t.Errorf("expected discord-specific template to win, got %q", result)
	}
}

// TestFallbackDtsSubdir — bundled fallback entries can ship in
// fallbacks/dts/**/*.json alongside fallbacks/dts.json, so each help
// topic lives in its own file rather than bloating the monolithic
// fallback. Loaded entries are stamped Readonly like the main fallback.
func TestFallbackDtsSubdir(t *testing.T) {
	configDir := t.TempDir()
	fallbackDir := t.TempDir()

	// Main fallback file — one entry for monster.
	writeDTS(t, fallbackDir, `[{
		"type": "monster",
		"id": "1",
		"platform": "discord",
		"language": "en",
		"default": true,
		"template": {"embed": {"title": "Fallback monster"}}
	}]`)

	// Subdirectory file — separate fallback entry for help.
	subDir := filepath.Join(fallbackDir, "dts", "help")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subFile := filepath.Join(subDir, "track.json")
	if err := os.WriteFile(subFile, []byte(`[{
		"type": "help",
		"id": "track",
		"platform": "",
		"language": "en",
		"template": {"embed": {"title": "Bundled track help"}}
	}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	ts, err := LoadTemplates(configDir, fallbackDir)
	if err != nil {
		t.Fatal(err)
	}

	if tmpl := ts.Get("monster", "discord", "1", "en"); tmpl == nil {
		t.Error("main fallbacks/dts.json entry should still load")
	}
	if tmpl := ts.Get("help", "discord", "track", "en"); tmpl == nil {
		t.Error("fallbacks/dts/help/track.json entry should load for discord (platform wildcard)")
	}
	if tmpl := ts.Get("help", "telegram", "track", "en"); tmpl == nil {
		t.Error("fallbacks/dts/help/track.json entry should load for telegram (platform wildcard)")
	}
}

