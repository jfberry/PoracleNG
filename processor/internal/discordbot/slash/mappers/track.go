package mappers

import (
	"fmt"
	"strconv"
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
//	costume       (string, autocomplete)           — pokemon costume ID
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

	if v, ok := o["iv"]; ok {
		// autocomplete.IVRange offers the user's raw input back as a
		// committable choice, so free text arrives here. Slash tokens bypass
		// the text parser, so an unparseable value would reach ArgMatcher
		// verbatim, land in Unrecognized, and abort the WHOLE command with
		// "Unrecognized: iv100%". Report it instead.
		if iv := strings.TrimSpace(v.StringValue()); iv != "" {
			if !validIVRange(iv) {
				return nil, &MapperError{Key: "error.slash.track.bad_iv", Args: []any{iv}}
			}
			tokens = append(tokens, "iv"+iv)
		}
	}

	for _, league := range []string{"great", "ultra", "little"} {
		if opt, ok := o[league+"_rank"]; ok && opt.IntValue() > 0 {
			tokens = append(tokens, fmt.Sprintf("%s%d", league, opt.IntValue()))
		}
	}

	appendCommonTail(&tokens, o)

	// form and costume are compared case-insensitively downstream
	// (filterByForm lowercases the translation it compares against), but slash
	// tokens never pass through the text parser's lowercasing — so normalise
	// here or a capitalised autocomplete value fails to resolve and aborts the
	// command.
	if v, ok := o["form"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "form:"+strings.ToLower(v.StringValue()))
	}

	if v, ok := o["costume"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "costume:"+strings.ToLower(v.StringValue()))
	}

	if v, ok := o["size"]; ok {
		size := strings.ToLower(v.StringValue())
		if size != "" && size != "all" {
			tokens = append(tokens, "size:"+size)
		}
	}

	return tokens, nil
}

func init() { registry["track"] = Track }

// validIVRange reports whether s is a token ArgMatcher's range parser accepts
// after the "iv" prefix: a bare integer, or two separated by "-".
//
// It deliberately checks the GRAMMAR, not the 0-100 bounds — the command layer
// owns bounds and reports them with its own message. This exists only so an
// unparseable value produces a clear error instead of aborting the command.
func validIVRange(s string) bool {
	parts := strings.SplitN(s, "-", 2)
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
