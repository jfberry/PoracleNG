package slash

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
)

// commandsAPI is the subset of *discordgo.Session SyncCommands uses,
// extracted into an interface for testability.
type commandsAPI interface {
	ApplicationCommandBulkOverwrite(appID, guildID string, cmds []*discordgo.ApplicationCommand, opts ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error)
}

// SyncCommands pushes the current command set to Discord. Idempotent via a
// fingerprint cache: when the hash of the intended command set matches what we
// last successfully pushed, the API call is skipped. ForceSync=true bypasses
// the cache.
//
// When d.cfg.CachePath is empty, the cache is disabled entirely (no read, no
// write) and every call pushes — this matches the test-default behavior where
// a Dispatcher is constructed without a CachePath. The wiring that defaults
// the path to config/.cache/slash-fingerprint.json lives in main.go (Task 45).
func (d *Dispatcher) SyncCommands(ctx context.Context) error {
	_ = ctx
	intent := AllDefinitions(d.bundle, d.cfg.Enable)
	want := Fingerprint(intent)
	api := d.commandsAPI
	if api == nil {
		api = d.session
	}

	useCache := d.cfg.CachePath != ""
	cache := &Cache{Path: d.cfg.CachePath, Guilds: map[string]CacheEntry{}}
	if useCache {
		if err := cache.Load(); err != nil {
			log.WithError(err).Warn("slash: failed to load fingerprint cache; pushing anyway")
		}
	}

	if d.cfg.Global {
		if useCache && cache.Global.Fingerprint == want && !d.cfg.ForceSync {
			return nil
		}
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, "", intent); err != nil {
			return fmt.Errorf("global slash sync: %w", err)
		}
		if !useCache {
			return nil
		}
		cache.Global = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
		return cache.Save()
	}

	var lastErr error
	pushed := false
	for _, gid := range d.cfg.Guilds {
		if useCache && cache.Guilds[gid].Fingerprint == want && !d.cfg.ForceSync {
			continue
		}
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, gid, intent); err != nil {
			lastErr = fmt.Errorf("guild %s slash sync: %w", gid, err)
			log.WithError(err).Warnf("slash: guild %s sync failed; continuing", gid)
			continue
		}
		if useCache {
			cache.Guilds[gid] = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
			pushed = true
		}
	}
	if useCache && pushed {
		if err := cache.Save(); err != nil {
			log.WithError(err).Warn("slash: failed to save fingerprint cache")
		}
	}
	return lastErr
}
