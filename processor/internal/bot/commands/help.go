package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// HelpCommand implements !help — show help text.
// Full DTS help template rendering will be added when the command system matures.
// For now, shows a list of available commands.
type HelpCommand struct{}

func (c *HelpCommand) Name() string      { return "cmd.help" }
func (c *HelpCommand) Aliases() []string { return nil }

func (c *HelpCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	prefix := commandPrefix(ctx)

	text := "**Available commands:**\n"
	text += prefix + "track <pokemon> [iv:N] [cp:N] [level:N] [d:N] — Track pokemon\n"
	text += prefix + "untrack <pokemon> — Remove pokemon tracking\n"
	text += prefix + "raid <pokemon|level:N|legendary> [d:N] — Track raids\n"
	text += prefix + "egg <level:N|legendary|mega> [d:N] — Track eggs\n"
	text += prefix + "quest <pokemon|stardust|energy:pokemon|candy:pokemon> — Track quests\n"
	text += prefix + "invasion [everything|<type>] — Track invasions\n"
	text += prefix + "lure <type|everything> — Track lures\n"
	text += prefix + "gym <team|everything> [slot_changes] — Track gyms\n"
	text += prefix + "nest <pokemon|everything> — Track nests\n"
	text += prefix + "maxbattle <pokemon|level:N> — Track max battles\n"
	text += prefix + "fort <pokestop|gym|everything> — Track fort updates\n"
	text += prefix + "tracked — Show all tracking\n"
	text += prefix + "script — Export tracking as commands\n"
	text += prefix + "area [list|add|remove] — Manage areas\n"
	text += prefix + "location <lat,lon> — Set location\n"
	text += prefix + "profile [list|add|remove|switch] — Manage profiles\n"
	text += prefix + "language <code> — Set language\n"
	text += prefix + "start / " + prefix + "stop — Enable/disable alerts\n"
	text += prefix + "version — Show version\n"

	return []bot.Reply{{Text: text}}
}
