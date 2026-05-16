package mappers

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Nest maps /nest options to text-command tokens.
//
// Options:
//
//	pokemon        (string, required, autocomplete) — pokemon name/ID
//	min_spawn_avg  (int)                            — minimum spawn average per hour
//	distance       (int)                            — alert radius in metres
//	clean          (bool)                            — auto-delete on expiry
//	template       (string, autocomplete)           — DTS template name
//
// min_spawn_avg emits the `minspawn<N>` prefix token consumed by
// `arg.prefix.minspawn` in argmatch.go (not `t<N>` — the bot's `t` prefix
// is reserved for other ranges).
func Nest(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	pokemon := strings.ToLower(getString(o["pokemon"]))
	if pokemon == "" {
		return nil, &MapperError{Key: "error.slash.nest.no_pokemon"}
	}

	tokens := []string{pokemon}
	if v, ok := o["min_spawn_avg"]; ok && v.IntValue() > 0 {
		tokens = append(tokens, fmt.Sprintf("minspawn%d", v.IntValue()))
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

func init() { registry["nest"] = Nest }
