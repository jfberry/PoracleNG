package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Incident maps /incident options to text-command tokens. cmd.incident is
// an alias of cmd.invasion in the text-bot registry, so /incident
// dispatches through the same InvasionCommand.Run — the only thing
// distinguishing /incident from /invasion is the smaller autocomplete
// scope (pokestop events only: Kecleon, Gold Pokestop, Showcase,
// Pokestop Spawn) and the absence of a gender filter (incidents have
// no gender variants).
//
// The `type` value is sent as a bare lowercased token; the bot's
// matchInvasionType resolver compares it against canonical event names
// in util.PokestopEvent.
func Incident(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	t := strings.ToLower(getString(o["type"]))
	if t == "" {
		return nil, &MapperError{Key: "error.slash.incident.no_type"}
	}

	tokens := []string{t}
	appendCommonTail(&tokens, o)
	return tokens, nil
}

func init() { registry["incident"] = Incident }
