package commands

import (
	"sort"
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

	// Match against available_languages keys and translated language names
	available := ctx.Config.General.AvailableLanguages
	var matched string
	var displayEntries []string
	if len(available) > 0 {
		for code := range available {
			// Build display string: "en (English)" using i18n language names
			name := tr.T("language." + code)
			if name == "" || name == "language."+code {
				name = code
			}
			displayEntries = append(displayEntries, code+" ("+name+")")

			// Match by code
			if strings.ToLower(code) == input {
				matched = code
			}
			// Match by translated name (in user's language)
			if matched == "" && strings.EqualFold(name, input) {
				matched = code
			}
			// Also try English name for non-English users
			if matched == "" {
				enName := ctx.Translations.For("en").T("language." + code)
				if enName != "" && strings.EqualFold(enName, input) {
					matched = code
				}
			}
		}
		sort.Strings(displayEntries)
	} else {
		// No available_languages configured — accept any code
		matched = input
	}

	if matched == "" {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.language.unknown", strings.Join(displayEntries, ", "))}}
	}

	if err := ctx.Humans.SetLanguage(ctx.TargetID, matched); err != nil {
		return []bot.Reply{{React: "🙅"}}
	}

	// Show the language name in the confirmation
	name := tr.T("language." + matched)
	if name == "" || name == "language."+matched {
		name = matched
	}

	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.language.changed", name+" ("+matched+")")}}
}
