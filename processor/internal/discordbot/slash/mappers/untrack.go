package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Untrack maps /untrack <subtype> tracking:<uid> sub-command invocations into
// text tokens for the underlying per-type command.
//
// Only cmd.untrack itself (the pokemon untracker) treats a bare "id:<uid>" as
// "remove this rule". Every other per-type command (cmd.raid, cmd.egg, ...)
// requires the explicit "remove" keyword alongside the UID; bare "id:<uid>"
// without "remove" is just a filter token and would silently no-op. Emit the
// keyword for the non-pokemon sub-commands so the rerouted command actually
// deletes the row.
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
	var uid string
	for _, o := range sub.Options {
		if o != nil && o.Name == "tracking" {
			uid = o.StringValue()
		}
	}
	if uid == "" {
		return nil, &MapperError{Key: "error.slash.untrack.no_tracking"}
	}
	if sub.Name == "pokemon" {
		return []string{"id:" + uid}, nil
	}
	return []string{"remove", "id:" + uid}, nil
}

func init() { registry["untrack"] = Untrack }
