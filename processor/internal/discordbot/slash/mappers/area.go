package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Area maps /area sub-command invocations to the text-command tokens read by
// AreaCommand in processor/internal/bot/commands/area.go.
//
// The text grammar accepts `arg.add <name>`, `arg.remove <name>`, `arg.list`,
// `arg.show [name]`, `arg.overview [name]`. The slash surface trims the
// option set down to the three operations a typical user wants from a
// drop-down command: add, remove, and (no-arg) show-current. Power users can
// still reach overview/list/show-by-name via the text command.
//
// /area show with no further arguments emits an empty token slice; the text
// command's bare `!area` invocation displays the user's current areas + a
// usage hint, which is exactly what we want for the "show" sub-command.
func Area(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.area.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.area.no_subcommand"}
	}
	switch sub.Name {
	case "add":
		if len(sub.Options) == 0 || sub.Options[0] == nil || sub.Options[0].StringValue() == "" {
			return nil, &MapperError{Key: "error.slash.area.no_area"}
		}
		return []string{"add", sub.Options[0].StringValue()}, nil
	case "remove":
		if len(sub.Options) == 0 || sub.Options[0] == nil || sub.Options[0].StringValue() == "" {
			return nil, &MapperError{Key: "error.slash.area.no_area"}
		}
		return []string{"remove", sub.Options[0].StringValue()}, nil
	case "show":
		// Bare `!area` text invocation shows current areas + usage hint;
		// returning no tokens is exactly that path.
		return nil, nil
	}
	return nil, &MapperError{Key: "error.slash.area.unknown_subcommand"}
}

func init() { registry["area"] = Area }
