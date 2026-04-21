package dts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// newTestStore builds a TemplateStore with a real temp config dir so the
// on-disk save path exercises actual file I/O. Entries are seeded directly
// into ts.entries; callers can write fixtures into configDir themselves.
func newTestStore(t *testing.T, entries []DTSEntry) (*TemplateStore, string) {
	t.Helper()
	tmp := t.TempDir()
	ts := &TemplateStore{
		entries:     entries,
		cache:       make(map[string]*raymond.Template),
		sourceCache: make(map[string]string),
		tileUsage:   make(map[string]bool),
		configDir:   tmp,
		fallbackDir: filepath.Join(tmp, "fallback"),
	}
	return ts, tmp
}

func TestFilteredEntriesDedupesOverride(t *testing.T) {
	entries := []DTSEntry{
		{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "from-dts.json", sourceFile: "config/dts.json"},
		{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "from-dts-folder", sourceFile: "config/dts/monster-1-discord.json"},
		// Unrelated entries — should pass through.
		{Type: "raid", ID: "1", Platform: "discord", Language: "", Template: "raid-t"},
	}
	ts, _ := newTestStore(t, entries)

	got := ts.FilteredEntries("monster", "discord", "", "1")
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped monster entry, got %d (%+v)", len(got), got)
	}
	if got[0].Template != "from-dts-folder" {
		t.Errorf("expected the dts/ folder override to win, got template=%v", got[0].Template)
	}

	// Full list still contains the unrelated raid entry.
	allEntries := ts.FilteredEntries("", "", "", "")
	if len(allEntries) != 2 {
		t.Errorf("expected 2 entries (deduped monster + raid), got %d", len(allEntries))
	}
}

func TestGetEntryPrefersLast(t *testing.T) {
	entries := []DTSEntry{
		{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "old"},
		{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "new"},
	}
	ts, _ := newTestStore(t, entries)

	got := ts.GetEntry("monster", "discord", "", "1")
	if got == nil {
		t.Fatal("expected non-nil entry")
	}
	if got.Template != "new" {
		t.Errorf("expected the last-loaded (override) entry, got template=%v", got.Template)
	}
}

func TestTemplateMetadataDedupesOverride(t *testing.T) {
	entries := []DTSEntry{
		{Type: "monster", ID: "1", Platform: "discord", Language: "en"},
		{Type: "monster", ID: "1", Platform: "discord", Language: "en"}, // duplicate
		{Type: "monster", ID: "2", Platform: "discord", Language: "en"},
	}
	ts, _ := newTestStore(t, entries)

	meta := ts.TemplateMetadata(false)
	discord, _ := meta["discord"].(map[string]any)
	monster, _ := discord["monster"].(map[string]any)
	ids, _ := monster["en"].([]string)

	if len(ids) != 2 {
		t.Fatalf("expected 2 unique ids in dropdown, got %d (%v)", len(ids), ids)
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("id %q appears more than once in dropdown", id)
		}
		seen[id] = true
	}
}

// TestLoadTemplatesDedupesIntVsStringID reproduces the user's reported
// scenario: config/dts.json has "id": 1 (integer) and
// config/dts/monster-1-discord.json has "id": "1" (string). After loading,
// the editor endpoints (TemplateMetadata, FilteredEntries) must treat these
// as the same key and show only the override.
func TestLoadTemplatesDedupesIntVsStringID(t *testing.T) {
	tmp := t.TempDir()

	// config/dts.json with integer id.
	dtsJSONPath := filepath.Join(tmp, "dts.json")
	if err := os.WriteFile(dtsJSONPath, []byte(`[
		{"type": "monster", "id": 1, "platform": "discord", "language": "", "template": {"content": "int-id"}}
	]`), 0644); err != nil {
		t.Fatal(err)
	}

	// config/dts/monster-1-discord.json with string id.
	dtsDir := filepath.Join(tmp, "dts")
	if err := os.MkdirAll(dtsDir, 0755); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(dtsDir, "monster-1-discord.json")
	if err := os.WriteFile(overridePath, []byte(`[
		{"type": "monster", "id": "1", "platform": "discord", "language": "", "template": {"content": "string-id override"}}
	]`), 0644); err != nil {
		t.Fatal(err)
	}

	fallbackDir := filepath.Join(tmp, "fallback")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Minimal fallback dts.json so LoadTemplates doesn't error.
	if err := os.WriteFile(filepath.Join(fallbackDir, "dts.json"), []byte(`[]`), 0644); err != nil {
		t.Fatal(err)
	}

	ts, err := LoadTemplates(tmp, fallbackDir)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	// Both files are loaded — raw ts.entries should have 2 entries with the
	// same entryKey.
	if len(ts.entries) != 2 {
		t.Fatalf("expected 2 raw entries loaded, got %d", len(ts.entries))
	}
	if entryKey(&ts.entries[0]) != entryKey(&ts.entries[1]) {
		t.Fatalf("entryKey mismatch for int (%q) vs string (%q) id — dedup will not collapse them",
			entryKey(&ts.entries[0]), entryKey(&ts.entries[1]))
	}

	// The editor endpoints should only show one entry, and it should be the
	// override (last-loaded).
	list := ts.FilteredEntries("monster", "discord", "", "1")
	if len(list) != 1 {
		t.Fatalf("FilteredEntries returned %d entries, want 1", len(list))
	}
	tmplMap, _ := list[0].Template.(map[string]any)
	if tmplMap["content"] != "string-id override" {
		t.Errorf("expected override template content, got %v", list[0].Template)
	}

	meta := ts.TemplateMetadata(false)
	discord, _ := meta["discord"].(map[string]any)
	monster, _ := discord["monster"].(map[string]any)
	// Blank language is keyed as "%" in the metadata map.
	ids, _ := monster["%"].([]string)
	if len(ids) != 1 || ids[0] != "1" {
		t.Errorf("TemplateMetadata dropdown ids = %v, want [\"1\"]", ids)
	}
}

// TestSaveEntryOmitsEmptyTemplateFile proves the on-disk JSON for a saved
// entry with no templateFile does not include `"templateFile": ""`.
func TestSaveEntryOmitsEmptyTemplateFile(t *testing.T) {
	ts, tmp := newTestStore(t, nil)

	entry := DTSEntry{
		Type:     "monster",
		ID:       "1",
		Platform: "discord",
		Language: "",
		Template: map[string]any{"content": "hello"},
	}
	if err := ts.SaveEntry(entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	// Read the written file and assert templateFile is absent from the JSON.
	savedPath := filepath.Join(tmp, "dts", "monster-1-discord.json")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if strings.Contains(string(data), `"templateFile"`) {
		t.Errorf("expected no templateFile key in saved JSON, got:\n%s", string(data))
	}
}

// TestSaveEntryCleansAllStaleDuplicates proves that when the store has
// the same key in BOTH config/dts.json and config/dts/<old-name>.json,
// a save moves the entry to the canonical filename and removes it from
// both old locations (not just whichever one SaveEntry happened to touch
// first). Matches the user-reported bug where "Old entry was left in
// dts.json" plus a differently-named dts/ folder file.
func TestSaveEntryCleansAllStaleDuplicates(t *testing.T) {
	tmp := t.TempDir()

	// Seed both stale files on disk.
	dtsJSON := filepath.Join(tmp, "dts.json")
	staleEntry := []DTSEntry{{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "stale-main"}}
	data, _ := json.Marshal(staleEntry)
	if err := os.WriteFile(dtsJSON, data, 0644); err != nil {
		t.Fatal(err)
	}
	dtsDir := filepath.Join(tmp, "dts")
	if err := os.MkdirAll(dtsDir, 0755); err != nil {
		t.Fatal(err)
	}
	oddPath := filepath.Join(dtsDir, "fabio old.json")
	data, _ = json.Marshal([]DTSEntry{{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "stale-fabio"}})
	if err := os.WriteFile(oddPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	ts := &TemplateStore{
		cache:       make(map[string]*raymond.Template),
		sourceCache: make(map[string]string),
		tileUsage:   make(map[string]bool),
		configDir:   tmp,
		fallbackDir: filepath.Join(tmp, "fallback"),
		entries: []DTSEntry{
			{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "stale-main", sourceFile: dtsJSON},
			{Type: "monster", ID: "1", Platform: "discord", Language: "", Template: "stale-fabio", sourceFile: oddPath},
		},
	}

	fresh := DTSEntry{
		Type: "monster", ID: "1", Platform: "discord", Language: "",
		Template: map[string]any{"content": "fresh"},
	}
	if err := ts.SaveEntry(fresh); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	// dts.json should now be empty (or an empty array).
	mainRaw, err := os.ReadFile(dtsJSON)
	if err != nil {
		t.Fatalf("read dts.json: %v", err)
	}
	var mainEntries []DTSEntry
	if err := json.Unmarshal(mainRaw, &mainEntries); err != nil {
		t.Fatalf("parse dts.json: %v", err)
	}
	for _, e := range mainEntries {
		if entryKey(&e) == entryKey(&fresh) {
			t.Errorf("dts.json still contains the stale monster/1/discord entry")
		}
	}

	// Odd-named file should either be gone or contain no matching entry.
	if oddRaw, err := os.ReadFile(oddPath); err == nil {
		var oddEntries []DTSEntry
		if err := json.Unmarshal(oddRaw, &oddEntries); err == nil {
			for _, e := range oddEntries {
				if entryKey(&e) == entryKey(&fresh) {
					t.Errorf("%q still contains the stale monster/1/discord entry", oddPath)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected error reading odd-named file: %v", err)
	}

	// In-memory state has exactly one entry for this key, pointing at the
	// canonical path.
	got := ts.FilteredEntries("monster", "discord", "", "1")
	if len(got) != 1 {
		t.Fatalf("expected 1 entry in memory after save, got %d", len(got))
	}
	wantPath := filepath.Join(dtsDir, "monster-1-discord.json")
	if got[0].sourceFile != wantPath {
		t.Errorf("sourceFile = %q, want %q", got[0].sourceFile, wantPath)
	}
}

// TestLogSummaryHelpShadowingAdvisory verifies the startup advisory fires
// when (and only when) a user-provided help entry has default:true. The
// default-flag semantic makes such an entry match every !help <topic> call,
// shadowing shipped per-topic help — operators should see a log line
// pointing to the DTS.md section so they can self-correct.
func TestLogSummaryHelpShadowingAdvisory(t *testing.T) {
	cases := []struct {
		name        string
		entries     []DTSEntry
		wantAdvice  bool
		wantPhrase  string
	}{
		{
			name: "user help with default:true fires advisory",
			entries: []DTSEntry{
				{Type: "help", ID: "1", Platform: "discord", Language: "en", Default: true},
			},
			wantAdvice: true,
			wantPhrase: "shadowing the shipped help/<topic>",
		},
		{
			name: "user help without default:true stays silent",
			entries: []DTSEntry{
				{Type: "help", ID: "index", Platform: "discord", Language: "en", Default: false},
			},
			wantAdvice: false,
		},
		{
			name: "readonly (shipped) help with default:true stays silent",
			entries: []DTSEntry{
				{Type: "help", ID: "1", Platform: "discord", Language: "en", Default: true, Readonly: true},
			},
			wantAdvice: false,
		},
		{
			name: "non-help default-flagged entry stays silent",
			entries: []DTSEntry{
				{Type: "monster", ID: "1", Platform: "discord", Language: "en", Default: true},
			},
			wantAdvice: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := test.NewGlobal()
			t.Cleanup(hook.Reset)

			ts, _ := newTestStore(t, tc.entries)
			ts.LogSummary()

			sawAdvisory := false
			for _, e := range hook.AllEntries() {
				if e.Level == logrus.InfoLevel && strings.Contains(e.Message, "default-flagged") {
					sawAdvisory = true
					if tc.wantPhrase != "" && !strings.Contains(e.Message, tc.wantPhrase) {
						t.Errorf("advisory message missing expected phrase %q, got %q", tc.wantPhrase, e.Message)
					}
				}
			}
			if sawAdvisory != tc.wantAdvice {
				t.Errorf("advisory fired=%v, want %v. messages: %v", sawAdvisory, tc.wantAdvice, hook.AllEntries())
			}
		})
	}
}
