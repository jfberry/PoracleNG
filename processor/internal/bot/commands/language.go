package commands

import (
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

type LanguageCommand struct{}

func (c *LanguageCommand) Name() string      { return "cmd.language" }
func (c *LanguageCommand) Aliases() []string { return nil }

func (c *LanguageCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Build available languages display list
	available := ctx.Config.General.AvailableLanguages
	displayEntries := buildLanguageList(ctx, tr)

	// !language with no args — show current language and available options
	if len(args) == 0 {
		current := ctx.Language
		currentName := tr.T("language." + current)
		if currentName == "" || currentName == "language."+current {
			currentName = current
		}

		msg := tr.Tf("msg.language.current", currentName+" ("+current+")")
		if len(displayEntries) > 0 {
			msg += "\n" + tr.Tf("msg.language.available", strings.Join(displayEntries, ", "))
		}
		return []bot.Reply{{Text: msg}}
	}

	input := strings.ToLower(args[0])

	// Match against available_languages keys and translated language names
	var matched string
	if len(available) > 0 {
		for code := range available {
			// Match by code
			if strings.ToLower(code) == input {
				matched = code
			}
			// Match by translated name (in user's current language)
			if matched == "" {
				name := tr.T("language." + code)
				if name != "" && name != "language."+code && strings.EqualFold(name, input) {
					matched = code
				}
			}
			// Also try English name for non-English users
			if matched == "" {
				enName := ctx.Translations.For("en").T("language." + code)
				if enName != "" && strings.EqualFold(enName, input) {
					matched = code
				}
			}
		}
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

	ctx.TriggerReload()

	// Confirm in the DESTINATION language
	destTr := ctx.Translations.For(matched)
	name := destTr.T("language." + matched)
	if name == "" || name == "language."+matched {
		name = matched
	}
	return []bot.Reply{{React: "✅", Text: destTr.Tf("msg.language.changed", name+" ("+matched+")")}}
}

// buildLanguageList returns sorted "code (Name)" entries for available languages.
func buildLanguageList(ctx *bot.CommandContext, tr *i18n.Translator) []string {
	available := ctx.Config.General.AvailableLanguages
	if len(available) == 0 {
		return nil
	}
	entries := make([]string, 0, len(available))
	for code := range available {
		name := tr.T("language." + code)
		if name == "" || name == "language."+code {
			name = code
		}
		entries = append(entries, code+" ("+name+")")
	}
	sort.Strings(entries)
	return entries
}
