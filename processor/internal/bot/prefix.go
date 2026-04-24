package bot

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// CommandPrefix returns the user-visible command prefix for the sender's
// platform. Discord's prefix is configurable (cfg.Discord.Prefix, defaults
// to "!"); Telegram always uses "/" because the Bot API hard-codes it.
//
// All user-facing text that references a command name ("use !location to
// set one", "I have made a lot of changes. See !tracked for details") MUST
// route through this helper so Telegram users see "/location" and "/tracked"
// instead of a bogus Discord prefix.
func CommandPrefix(ctx *CommandContext) string {
	if ctx == nil {
		return "!"
	}
	if ctx.Platform == "telegram" {
		return "/"
	}
	return discordPrefix(ctx.Config)
}

// CommandPrefixForType returns the prefix to use when the caller only has
// the target's human-store type string (e.g. "discord:user",
// "telegram:channel"). Used on code paths that don't build a
// CommandContext — rate-limit notifications, tracking API confirmation
// messages, etc.
func CommandPrefixForType(cfg *config.Config, targetType string) string {
	if strings.HasPrefix(targetType, "telegram:") {
		return "/"
	}
	return discordPrefix(cfg)
}

func discordPrefix(cfg *config.Config) string {
	if cfg != nil && cfg.Discord.Prefix != "" {
		return cfg.Discord.Prefix
	}
	return "!"
}
