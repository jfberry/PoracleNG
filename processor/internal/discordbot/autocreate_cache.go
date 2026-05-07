package discordbot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// autocreateCache is the on-disk state for bulk-autocreate runs, keyed by
// rule name. Loaded at startup, saved at the end of each sync.
type autocreateCache map[string]*autocreateRuleState

// autocreateRuleState records what one rule's last sync produced. The
// runner uses this for diff (which fences are already created), reconcile
// (which IDs are still valid Discord-side), and cleanup (which categories
// might be empty after orphan removal).
type autocreateRuleState struct {
	GuildID    string                           `json:"guild_id"`
	Categories []autocreateCategory             `json:"categories"`
	Fences     map[string]*autocreateFenceState `json:"fences"`
	LastSync   time.Time                        `json:"last_sync,omitempty"`
}

// autocreateCategory tracks a category created (or reused) by this rule.
// Indexed by name so the sort and removal-when-empty steps can locate it.
type autocreateCategory struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// autocreateFenceState is one geofence's mapped Discord state. Keyed in
// the rule's Fences map by the original-case fence name (so the case-
// preserving RawArgs work end-to-end).
type autocreateFenceState struct {
	CategoryID string            `json:"category_id"`
	ChannelID  string            `json:"channel_id"`
	ThreadIDs  map[string]string `json:"thread_ids,omitempty"`
}

// loadAutocreateCache reads the JSON file at the given path. A missing
// file returns an empty cache rather than an error — the first sync run
// against a clean install populates the file.
func loadAutocreateCache(path string) (autocreateCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return autocreateCache{}, nil
		}
		return nil, fmt.Errorf("read autocreate cache %s: %w", path, err)
	}
	cache := autocreateCache{}
	if len(data) == 0 {
		return cache, nil
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse autocreate cache %s: %w", path, err)
	}
	return cache, nil
}

// saveAutocreateCache writes the cache atomically (temp file + rename)
// so a crash mid-write doesn't leave a truncated JSON file behind.
// Creates the parent directory if it doesn't exist.
func saveAutocreateCache(path string, cache autocreateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create autocreate cache dir: %w", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal autocreate cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write autocreate cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename autocreate cache: %w", err)
	}
	return nil
}
