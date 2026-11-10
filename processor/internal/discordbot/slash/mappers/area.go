package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Area maps /area sub-command invocations to the text-command tokens read by
// AreaCommand in processor/internal/bot/commands/area.go.
//
// The text grammar accepts `arg.add <name...>`, `arg.remove <name...>`,
// `arg.list`, `arg.show [name]`, `arg.overview [name]` — add and remove take
// any number of names. The slash surface exposes add,
// remove, show (selected), list (available, marked), and overview (map).
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
	case "add", "remove":
		names := areaNames(sub)
		if len(names) == 0 {
			return nil, &MapperError{Key: "error.slash.area.no_area"}
		}
		return append([]string{sub.Name}, names...), nil
	case "show":
		// Bare `!area` text invocation shows current areas + usage hint;
		// returning no tokens is exactly that path.
		return nil, nil
	case "list":
		return []string{"list"}, nil
	case "overview":
		return []string{"overview"}, nil
	}
	return nil, &MapperError{Key: "error.slash.area.unknown_subcommand"}
}

// areaNames reads the sub-command's `area` value as a comma-separated
// list, one token per name. The picker composes multi-area values that way
// (see autocomplete.FilterAndCapMulti), and the text command has always
// taken several names: `!area add london paris`.
//
// Segments are trimmed and empties dropped, so a trailing comma left over
// from picking is harmless. Internal spaces survive — "Gent Centrum" is
// one name, and slash tokens reach the command without re-tokenisation.
func areaNames(sub *discordgo.ApplicationCommandInteractionDataOption) []string {
	if len(sub.Options) == 0 || sub.Options[0] == nil {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(sub.Options[0].StringValue(), ",") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func init() { registry["area"] = Area }
