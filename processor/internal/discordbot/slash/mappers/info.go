package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Info maps /info <sub> [args] to the text tokens read by InfoCommand.Run.
//
// Sub-commands:
//
//   - pokemon <name>  → ["<name>"]   (falls through to InfoCommand.pokemonInfo)
//   - rarity          → ["rarity"]   (matches msg.info.sub.rarity)
//   - shiny           → ["shiny"]    (matches msg.info.sub.shiny)
//   - weather [coords]→ ["weather"] or ["weather", "<coords>"]
//
// Keywords ("rarity", "shiny", "weather") are emitted as their canonical
// English value — InfoCommand.matchSub checks both the user's translator
// and the English fallback, so any locale works.
//
// Admin-only sub-commands from the text surface (translate, dts,
// templates) are not exposed via slash — admins still have them on the
// text command. Bare /info with no sub-command is impossible at the
// Discord layer because all four sub-commands are required choices.
func Info(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.info.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.info.no_subcommand"}
	}
	switch sub.Name {
	case "pokemon":
		if len(sub.Options) == 0 || sub.Options[0] == nil || sub.Options[0].StringValue() == "" {
			return nil, &MapperError{Key: "error.slash.info.no_pokemon"}
		}
		return []string{sub.Options[0].StringValue()}, nil
	case "rarity":
		return []string{"rarity"}, nil
	case "shiny":
		return []string{"shiny"}, nil
	case "weather":
		if len(sub.Options) > 0 && sub.Options[0] != nil {
			if coords := sub.Options[0].StringValue(); coords != "" {
				return []string{"weather", coords}, nil
			}
		}
		return []string{"weather"}, nil
	}
	return nil, &MapperError{Key: "error.slash.info.unknown_subcommand"}
}

func init() { registry["info"] = Info }
