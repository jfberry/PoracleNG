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

	// Match against available_languages keys
	available := ctx.Config.General.AvailableLanguages
	var matched string
	var langCodes []string
	if len(available) > 0 {
		for code := range available {
			langCodes = append(langCodes, code)
			if strings.ToLower(code) == input {
				matched = code
			}
		}
	} else {
		// No available_languages configured — accept any code
		matched = input
	}

	if matched == "" {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.language.unknown", strings.Join(langCodes, ", "))}}
	}

	if err := ctx.Humans.SetLanguage(ctx.TargetID, matched); err != nil {
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.language.changed", matched)}}
}
