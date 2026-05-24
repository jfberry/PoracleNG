package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// untrackRemoveAllSentinel mirrors listers.RemoveAllSentinel — duplicated
// as a string literal here to avoid importing the listers package
// (which would drag the whole user-state listing surface into the
// mappers package). Both files name it the same way and any change
// must keep them in lockstep.
const untrackRemoveAllSentinel = "everything"

// Untrack maps /untrack <subtype> tracking:<uid|everything> sub-command
// invocations into text tokens for the underlying per-type command.
//
// Two value shapes for the tracking option:
//
//   - a decimal UID (e.g. "42") — picked from the autocomplete list
//   - the literal "everything" sentinel — emitted by the lister's
//     "Remove ALL" affordance at the top of the picker
//
// Only cmd.untrack itself (the pokemon untracker) treats a bare "id:<uid>"
// or "everything" as "remove these rules". Every other per-type command
// (cmd.raid, cmd.egg, ...) requires the explicit "remove" keyword
// alongside the rest of the args; emit it for the non-pokemon sub-commands
// so the rerouted command actually deletes the rows.
//
// The dispatcher special-cases /untrack and rewrites cmdKey to cmd.<subtype>
// for the non-pokemon sub-commands — see HandleCommand. The mapper does not
// know which command will ultimately run; it just emits a grammar that works
// regardless ("remove id:N" is harmless on cmd.untrack, but cmd.untrack is
// only routed to for "pokemon").
func Untrack(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	if len(opts) == 0 {
		return nil, &MapperError{Key: "error.slash.untrack.no_subcommand"}
	}
	sub := opts[0]
	if sub == nil || sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		return nil, &MapperError{Key: "error.slash.untrack.no_subcommand"}
	}
	var value string
	for _, o := range sub.Options {
		if o != nil && o.Name == "tracking" {
			value = o.StringValue()
		}
	}
	if value == "" {
		return nil, &MapperError{Key: "error.slash.untrack.no_tracking"}
	}

	removeAll := value == untrackRemoveAllSentinel
	switch {
	case sub.Name == "pokemon" && removeAll:
		// !untrack everything — removes all pokemon tracking.
		return []string{"everything"}, nil
	case sub.Name == "pokemon":
		return []string{"id:" + value}, nil
	case removeAll:
		// !raid remove everything (and per-type equivalents) — the
		// command's HasKeyword("arg.everything") branch deletes all
		// rules of that type.
		return []string{"remove", "everything"}, nil
	default:
		return []string{"remove", "id:" + value}, nil
	}
}

func init() { registry["untrack"] = Untrack }
