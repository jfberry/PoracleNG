package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type LanguageCommand struct{}

func (c *LanguageCommand) Name() string      { return "cmd.language" }
func (c *LanguageCommand) Aliases() []string { return nil }

func (c *LanguageCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.language.specify")}}
	}

	input := strings.ToLower(args[0])

	// Match by code or by display name
	available := ctx.Config.General.AvailableLanguages
	if len(available) == 0 {
		available = []string{"en"}
	}

	var matched string
	for _, lang := range available {
		if strings.ToLower(lang) == input {
			matched = lang
			break
		}
	}

	if matched == "" {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.language.unknown", strings.Join(available, ", "))}}
	}

	if err := ctx.Humans.SetLanguage(ctx.TargetID, matched); err != nil {
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.language.changed", matched)}}
}
