package slash

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// commandsAPI is the subset of *discordgo.Session SyncCommands uses,
// extracted into an interface for testability.
type commandsAPI interface {
	ApplicationCommandBulkOverwrite(appID, guildID string, cmds []*discordgo.ApplicationCommand, opts ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error)
}

// SyncCommands pushes the current command set to Discord.
// Phase 1 omits the fingerprint cache; Task 12 adds it.
func (d *Dispatcher) SyncCommands(ctx context.Context) error {
	_ = ctx
	intent := AllDefinitions(d.bundle, d.cfg.Enable)
	api := d.commandsAPI
	if api == nil {
		api = d.session
	}

	if d.cfg.Global {
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, "", intent); err != nil {
			return fmt.Errorf("global slash sync: %w", err)
		}
		return nil
	}

	var lastErr error
	for _, gid := range d.cfg.Guilds {
		if _, err := api.ApplicationCommandBulkOverwrite(d.appID, gid, intent); err != nil {
			lastErr = fmt.Errorf("guild %s slash sync: %w", gid, err)
			continue
		}
	}
	return lastErr
}
