package mappers

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Maxbattle maps /maxbattle options to text-command tokens.
//
// Options:
//
//	pokemon   (string, required, autocomplete) — pokemon name/ID
//	level     (int, choices)                   — battle level 1..6
//	gmax      (bool)                            — gigantamax only
//	distance  (int)                            — alert radius in metres
//	clean     (bool)                            — auto-delete on expiry
//	template  (string, autocomplete)           — DTS template name
//
// The bot's !maxbattle accepts either a pokemon name OR a level (via
// `level<N>`); the slash form lets users combine both — the bot will
// then treat the pokemon as primary and the level as a redundant filter,
// which matches the more lenient text behavior.
func Maxbattle(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	pokemon := strings.ToLower(getString(o["pokemon"]))
	if pokemon == "" {
		return nil, &MapperError{Key: "error.slash.maxbattle.no_pokemon"}
	}

	tokens := []string{pokemon}
	if v, ok := o["level"]; ok && v.IntValue() > 0 {
		tokens = append(tokens, fmt.Sprintf("level%d", v.IntValue()))
	}
	if v, ok := o["gmax"]; ok && v.BoolValue() {
		tokens = append(tokens, "gmax")
	}
	appendDistance(&tokens, o["distance"])
	if v, ok := o["clean"]; ok && v.BoolValue() {
		tokens = append(tokens, "clean")
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}
	return tokens, nil
}

func init() { registry["maxbattle"] = Maxbattle }
