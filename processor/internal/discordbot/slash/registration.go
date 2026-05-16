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
//
// All decision branches log at Info so the operator can verify from the
// startup log what actually happened (pushed, skipped, or partial-failure).
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
			log.Infof("slash: global sync skipped — fingerprint %s matches cache (%d commands)", want, len(intent))
			return nil
		}
		log.Infof("slash: pushing global command set — %d commands, fingerprint %s", len(intent), want)
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, "", intent); err != nil {
			return fmt.Errorf("global slash sync: %w", err)
		}
		log.Infof("slash: global sync OK")
		if !useCache {
			return nil
		}
		cache.Global = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
		return cache.Save()
	}

	if len(d.cfg.Guilds) == 0 {
		log.Warn("slash: per-guild sync requested but no guilds configured — set [discord.slash_commands] guilds or register_globally=true")
		return nil
	}

	var lastErr error
	pushed := 0
	skipped := 0
	for _, gid := range d.cfg.Guilds {
		if useCache && cache.Guilds[gid].Fingerprint == want && !d.cfg.ForceSync {
			skipped++
			continue
		}
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, gid, intent); err != nil {
			lastErr = fmt.Errorf("guild %s slash sync: %w", gid, err)
			log.WithError(err).Warnf("slash: guild %s sync failed; continuing", gid)
			continue
		}
		log.Infof("slash: guild %s sync OK — %d commands, fingerprint %s", gid, len(intent), want)
		pushed++
		if useCache {
			cache.Guilds[gid] = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
		}
	}
	if skipped > 0 && pushed == 0 {
		log.Infof("slash: all %d guild(s) up-to-date, fingerprint %s — nothing pushed", skipped, want)
	} else if skipped > 0 {
		log.Infof("slash: %d guild(s) up-to-date, %d pushed", skipped, pushed)
	}
	if useCache && pushed > 0 {
		if err := cache.Save(); err != nil {
			log.WithError(err).Warn("slash: failed to save fingerprint cache")
		}
	}
	return lastErr
}
