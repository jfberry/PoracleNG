package slash

import (
	"fmt"
	"sort"

	"github.com/bwmarrin/discordgo"
)

// CommandMention is one *invocable* slash command path plus the id needed to
// build a Discord command mention.
type CommandMention struct {
	// Path is the space-separated invocation path: "help",
	// "untrack raid", "summary quest settime".
	Path string
	// ID is always the TOP-LEVEL command's id. Discord addresses a
	// subcommand by its full path but with the parent's id — there is no
	// separate id per subcommand.
	ID string
}

// Mention renders the Discord command-mention syntax, e.g.
// "</untrack raid:1234567890>", which clients render as a clickable command.
func (c CommandMention) Mention() string {
	return "</" + c.Path + ":" + c.ID + ">"
}

// FlattenCommands walks registered application commands into the flat list of
// paths a user can actually invoke, sorted by path for stable output.
//
// Two rules from Discord's command tree drive this:
//   - A command holding subcommands is not itself invocable, so it is not
//     emitted — only its leaves are.
//   - Depth is at most Command → Group → Subcommand, and every level shares
//     the top-level command's id.
//
// Non-subcommand options (strings, ints, autocomplete pickers) are arguments,
// not separate commands, so they never split a command.
func FlattenCommands(cmds []*discordgo.ApplicationCommand) []CommandMention {
	var out []CommandMention
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		leaves := subPaths(cmd.Options)
		if len(leaves) == 0 {
			out = append(out, CommandMention{Path: cmd.Name, ID: cmd.ID})
			continue
		}
		for _, leaf := range leaves {
			out = append(out, CommandMention{Path: cmd.Name + " " + leaf, ID: cmd.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// subPaths returns the invocable sub-paths below a command's options, or nil
// when the options are plain arguments rather than subcommands.
func subPaths(opts []*discordgo.ApplicationCommandOption) []string {
	var paths []string
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		switch opt.Type {
		case discordgo.ApplicationCommandOptionSubCommand:
			paths = append(paths, opt.Name)
		case discordgo.ApplicationCommandOptionSubCommandGroup:
			for _, sub := range opt.Options {
				if sub != nil && sub.Type == discordgo.ApplicationCommandOptionSubCommand {
					paths = append(paths, opt.Name+" "+sub.Name)
				}
			}
		}
	}
	return paths
}

// ListRegistered fetches the commands Discord currently has registered for a
// scope ("" = global) and flattens them into mention paths.
//
// This reads from Discord rather than from our intended command set on
// purpose: SyncCommands discards the ids Discord returns, and its fingerprint
// cache means a sync is usually skipped entirely, so the ids exist nowhere
// locally. Fetching also makes the output describe what is genuinely
// registered, which is what an operator needs when a mention isn't resolving.
func (d *Dispatcher) ListRegistered(guildID string) ([]CommandMention, error) {
	if d == nil || d.appID == "" {
		return nil, ErrSlashNotConfigured
	}
	api := d.commandsAPI
	if api == nil {
		if d.session == nil {
			return nil, ErrSlashNotConfigured
		}
		api = d.session
	}
	cmds, err := api.ApplicationCommands(d.appID, guildID)
	if err != nil {
		scope := guildID
		if scope == "" {
			scope = "global"
		}
		return nil, fmt.Errorf("list %s slash commands: %w", scope, err)
	}
	return FlattenCommands(cmds), nil
}

// ConfiguredGuilds returns the guild ids this dispatcher registers to, so
// callers can list every scope without knowing the config shape.
func (d *Dispatcher) ConfiguredGuilds() []string {
	if d == nil {
		return nil
	}
	return append([]string(nil), d.cfg.Guilds...)
}

// IsGlobal reports whether commands are registered globally rather than
// per-guild.
func (d *Dispatcher) IsGlobal() bool {
	return d != nil && d.cfg.Global
}
