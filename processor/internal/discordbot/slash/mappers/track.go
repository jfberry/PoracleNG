package mappers

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Track maps /track options to text-command tokens for the text bot parser.
//
// Options:
//
//	pokemon       (string, required, autocomplete) — pokemon ID or "everything"
//	iv            (string, autocomplete)           — "100", "95", "0-0"
//	distance      (int)                            — alert radius in metres
//	great_rank    (int)                            — top PVP rank Great League
//	ultra_rank    (int)                            — top PVP rank Ultra League
//	little_rank   (int)                            — top PVP rank Little League
//	clean         (bool)                           — auto-delete on expiry
//	template      (string, autocomplete)           — DTS template name
//	form          (string, autocomplete)           — pokemon form (cascades)
//	size          (string, choices)                — xxs/xs/m/xl/xxl ("all" omits)
//
// Output tokens are lowercase and follow the text bot's argument grammar.
// Returns a MapperError when the required "pokemon" option is absent.
func Track(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)
	tokens := []string{}

	val := strings.ToLower(getString(o["pokemon"]))
	if val == "" {
		return nil, &MapperError{Key: "error.slash.track.no_pokemon"}
	}
	tokens = append(tokens, val)

	if v, ok := o["iv"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "iv"+v.StringValue())
	}

	for _, league := range []string{"great", "ultra", "little"} {
		if opt, ok := o[league+"_rank"]; ok && opt.IntValue() > 0 {
			tokens = append(tokens, fmt.Sprintf("%s%d", league, opt.IntValue()))
		}
	}

	appendCommonTail(&tokens, o)

	if v, ok := o["form"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "form:"+v.StringValue())
	}

	if v, ok := o["size"]; ok {
		size := strings.ToLower(v.StringValue())
		if size != "" && size != "all" {
			tokens = append(tokens, size)
		}
	}

	return tokens, nil
}

func init() { registry["track"] = Track }
