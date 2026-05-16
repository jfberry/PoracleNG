package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Invasion maps /invasion options to text-command tokens.
//
// Options:
//
//	grunt_type  (string, required, autocomplete) — grunt type / boss / incident
//	gender      (string, choices)                — male / female / genderless
//	distance    (int)                            — alert radius in metres
//	clean       (string, single-choice)          — auto-delete on expiry
//	template    (string, autocomplete)           — DTS template name
//
// grunt_type is sent as a bare lowercased token; the bot's InvasionCommand
// matches it against grunt template-derived names, translated type names,
// boss leader names, and pokestop event names — see
// processor/internal/bot/commands/invasion.go::validTypes. gender goes
// through unchanged for ParamGender to parse.
func Invasion(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	grunt := strings.ToLower(getString(o["grunt_type"]))
	if grunt == "" {
		return nil, &MapperError{Key: "error.slash.invasion.no_grunt"}
	}

	tokens := []string{grunt}
	if v, ok := o["gender"]; ok && v.StringValue() != "" {
		tokens = append(tokens, strings.ToLower(v.StringValue()))
	}
	appendCommonTail(&tokens, o)
	return tokens, nil
}

func init() { registry["invasion"] = Invasion }
