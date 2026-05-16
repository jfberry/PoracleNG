package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Untrack maps /untrack <subtype> tracking:<uid> sub-command invocations into
// the text-command grammar that UntrackCommand recognises.
//
// The slash surface uses one sub-command per tracking type ("pokemon", "raid",
// ...). Each sub-command exposes a single autocomplete-backed "tracking"
// option whose value is the database UID of the rule to remove. The text
// equivalent is `!untrack id:<uid>` — UntrackCommand's ParamRemoveUID matcher
// reads the `id:` prefix and routes to removeByUIDs(), bypassing
// pokemon/gen/type matching.
//
// The slash sub-command name itself ("pokemon", "raid", ...) is purely a
// routing hint: the autocomplete dispatcher uses it to filter the choice list
// by tracking type via findUntrackSubtype. The mapper does not propagate it
// to the text command — UntrackCommand has no per-type entry point; the UID
// alone is unambiguous.
func Untrack(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.untrack.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.untrack.no_subcommand"}
	}
	var uid string
	for _, o := range sub.Options {
		if o != nil && o.Name == "tracking" {
			uid = o.StringValue()
		}
	}
	if uid == "" {
		return nil, &MapperError{Key: "error.slash.untrack.no_tracking"}
	}
	return []string{"id:" + uid}, nil
}

func init() { registry["untrack"] = Untrack }
