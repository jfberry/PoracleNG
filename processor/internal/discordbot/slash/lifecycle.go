package slash

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

// ForceResync forces a full push to Discord by temporarily enabling
// ForceSync, then calling SyncCommands. No cache wipe is necessary:
// ForceSync=true bypasses the fingerprint guard inside SyncCommands
// regardless of what the cache file contains, and SyncCommands writes
// the new fingerprint back after a successful push.
//
// This is the live-command equivalent of -sync-slash-commands, useful
// when something outside Go (e.g. a manual Discord portal change) has
// made the cached fingerprint stale.
func (d *Dispatcher) ForceResync() error {
	// Temporarily enable ForceSync for this call only.
	origForce := d.cfg.ForceSync
	d.cfg.ForceSync = true
	err := d.SyncCommands()
	d.cfg.ForceSync = origForce
	return err
}

// ClearSingleGuild removes all slash commands from one specific guild
// (regardless of d.cfg.Guilds) and removes that guild's fingerprint cache
// entry. Used by !poracle-admin slash clear-guild <id> to target a single
// guild without touching others.
func (d *Dispatcher) ClearSingleGuild(guildID string) error {
	api := d.commandsAPI
	if api == nil {
		api = d.session
	}
	if _, err := api.ApplicationCommandBulkOverwrite(d.appID, guildID, nil); err != nil {
		return fmt.Errorf("clear guild %s slash commands: %w", guildID, err)
	}
	if d.cfg.CachePath != "" {
		cache := &Cache{Path: d.cfg.CachePath, Guilds: map[string]CacheEntry{}}
		_ = cache.Load()
		delete(cache.Guilds, guildID)
		if err := cache.Save(); err != nil {
			log.WithError(err).Warn("slash: failed to update fingerprint cache after single-guild clear")
		}
	}
	return nil
}

// CacheStatus reads the fingerprint cache and returns one entry per
// configured scope (global, or each guild in cfg.Guilds). A missing
// cache file is not an error — callers receive zero-value CacheEntry
// structs with an empty Fingerprint and zero SyncedAt.
//
// The returned slices use CacheEntry directly so callers don't need to
// import the slash package for the timestamp type; wiring code in
// main.go converts CacheEntry → bot.SlashScope.
func (d *Dispatcher) CacheStatus() (global CacheEntry, guilds map[string]CacheEntry, err error) {
	cache := &Cache{Path: d.cfg.CachePath, Guilds: map[string]CacheEntry{}}
	if d.cfg.CachePath != "" {
		if loadErr := cache.Load(); loadErr != nil {
			err = loadErr
			return
		}
	}
	global = cache.Global
	guilds = make(map[string]CacheEntry, len(d.cfg.Guilds))
	for _, gid := range d.cfg.Guilds {
		guilds[gid] = cache.Guilds[gid]
	}
	return
}
