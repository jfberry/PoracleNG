package discordbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutocreateCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autocreate.json")

	original := autocreateCache{
		"uk-areas": &autocreateRuleState{
			GuildID: "12345",
			Categories: []autocreateCategory{
				{Name: "Belgium", ID: "cat1"},
			},
			Fences: map[string]*autocreateFenceState{
				"Gent_centrum": {
					CategoryID: "cat1",
					ChannelID:  "ch1",
					ThreadIDs:  map[string]map[string]string{"alerts-gent": {"Hundos": "th1"}},
				},
			},
		},
	}

	if err := saveAutocreateCache(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadAutocreateCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded["uk-areas"].GuildID; got != "12345" {
		t.Errorf("GuildID = %q, want %q", got, "12345")
	}
	if got := loaded["uk-areas"].Fences["Gent_centrum"].ChannelID; got != "ch1" {
		t.Errorf("ChannelID = %q, want %q", got, "ch1")
	}
	if got := loaded["uk-areas"].Fences["Gent_centrum"].ThreadIDs["alerts-gent"]["Hundos"]; got != "th1" {
		t.Errorf("ThreadIDs[alerts-gent][Hundos] = %q, want %q", got, "th1")
	}
}

func TestAutocreateCacheLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	loaded, err := loadAutocreateCache(path)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if loaded == nil {
		t.Fatal("missing file should return empty map, not nil")
	}
	if len(loaded) != 0 {
		t.Errorf("missing file should return empty map, got %d entries", len(loaded))
	}
}

func TestAutocreateCacheSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "autocreate.json")

	if err := saveAutocreateCache(path, autocreateCache{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
