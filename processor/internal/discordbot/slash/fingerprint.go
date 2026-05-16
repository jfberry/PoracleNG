package slash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Fingerprint returns a 16-char hex hash of the command set. Order-insensitive
// (commands are sorted by name before hashing). Includes localizations and
// every other JSON-serialized field on ApplicationCommand.
func Fingerprint(cmds []*discordgo.ApplicationCommand) string {
	sorted := make([]*discordgo.ApplicationCommand, len(cmds))
	copy(sorted, cmds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	h := sha256.New()
	enc := json.NewEncoder(h)
	enc.SetEscapeHTML(false)
	for _, c := range sorted {
		_ = enc.Encode(c)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Cache persists the fingerprint of the most-recently-synced command set so we
// can skip the Discord API push when nothing has changed across restarts.
//
// Path is set by the caller; Load/Save use it directly. Guilds is keyed by
// guild ID (matching Config.Guilds entries).
type Cache struct {
	Path   string                `json:"-"`
	Global CacheEntry            `json:"global"`
	Guilds map[string]CacheEntry `json:"guilds"`
}

type CacheEntry struct {
	Fingerprint string    `json:"fingerprint"`
	SyncedAt    time.Time `json:"synced_at"`
}

// Load reads the cache file at c.Path. A missing file is not an error (returns
// nil with the cache initialized to an empty state). Guilds is always non-nil
// after Load.
func (c *Cache) Load() error {
	if c.Guilds == nil {
		c.Guilds = map[string]CacheEntry{}
	}
	data, err := os.ReadFile(c.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, c)
}

// Save writes the cache as indented JSON to c.Path.
func (c *Cache) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, data, 0o644)
}
