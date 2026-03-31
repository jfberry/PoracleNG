package commands

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

type LanguageCommand struct{}

func (c *LanguageCommand) Name() string      { return "cmd.language" }
func (c *LanguageCommand) Aliases() []string { return nil }

func (c *LanguageCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: "Please specify a language"}}
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
		return []bot.Reply{{React: "🙅", Text: "Unknown language. Available: " + strings.Join(available, ", ")}}
	}

	_, err := ctx.DB.Exec("UPDATE humans SET language = ? WHERE id = ?", matched, ctx.TargetID)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}

	return []bot.Reply{{React: "✅"}}
}
