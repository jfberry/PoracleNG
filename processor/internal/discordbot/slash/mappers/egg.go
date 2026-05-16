package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Egg maps /egg options to text-command tokens.
//
// Options:
//
//	level     (string, required, choices) — raid level keyword
//	team      (int,    choices)           — 0..3 (harmony/mystic/valor/instinct)
//	distance  (int)                       — alert radius in metres
//	clean     (bool)                       — auto-delete on expiry
//	template  (string, autocomplete)      — DTS template name
//
// Validation: level is required. Discord enforces the Required flag itself,
// but we double-check defensively because mappers run on whatever Discord
// sends — including malformed proxies / replayed interactions.
func Egg(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	level := strings.ToLower(getString(o["level"]))
	if level == "" {
		return nil, &MapperError{Key: "error.slash.egg.no_level"}
	}

	tokens := []string{level}

	if v, ok := o["team"]; ok {
		if name := teamNameForValue(int(v.IntValue())); name != "" {
			tokens = append(tokens, name)
		}
	}

	appendDistance(&tokens, o["distance"])

	if tok := emitFlag(o["clean"], "clean"); tok != "" {
		tokens = append(tokens, tok)
	}

	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}

	return tokens, nil
}

func init() { registry["egg"] = Egg }
