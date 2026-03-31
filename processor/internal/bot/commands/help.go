package commands

import (
	"github.com/pokemon/poracleng/processor/internal/bot"
)

// HelpCommand implements !help — show help text.
// Uses translatable i18n key so the help text can be localized.
type HelpCommand struct{}

func (c *HelpCommand) Name() string      { return "cmd.help" }
func (c *HelpCommand) Aliases() []string { return nil }

func (c *HelpCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	prefix := commandPrefix(ctx)

	// If args specify a command name, show that command's usage help
	if len(args) > 0 {
		usageKey := "cmd." + args[0] + ".usage"
		text := tr.Tf(usageKey, prefix)
		// If the key returned itself (no translation found), fall back to general help
		if text != usageKey {
			return []bot.Reply{{Text: text}}
		}
	}

	// General help text — translatable via i18n
	text := tr.Tf("cmd.help.text", prefix)

	return []bot.Reply{{Text: text}}
}
