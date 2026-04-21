package dts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	raymond "github.com/mailgun/raymond/v2"
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
