package mappers

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// flattenOptions converts a slice of ApplicationCommandInteractionDataOption
// into a map keyed by option Name for O(1) lookup by mappers.
//
// Does not recurse into sub-commands; sub-command-aware mappers (e.g. /area,
// /profile, /untrack) iterate opts directly.
func flattenOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	out := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(opts))
	for _, o := range opts {
		out[o.Name] = o
	}
	return out
}

// getString safely reads a StringValue from an option, returning "" when nil.
func getString(opt *discordgo.ApplicationCommandInteractionDataOption) string {
	if opt == nil {
		return ""
	}
	return opt.StringValue()
}

// appendDistance emits a "d<N>" token for positive distance values. Silently
// skips zero/negative or nil options so the default ("no distance specified")
// behavior matches the text-command grammar.
func appendDistance(tokens *[]string, opt *discordgo.ApplicationCommandInteractionDataOption) {
	if opt == nil || opt.IntValue() <= 0 {
		return
	}
	*tokens = append(*tokens, fmt.Sprintf("d%d", opt.IntValue()))
}

// teamNameForValue maps the Discord choice integer for the "team" option to
// the canonical English team keyword consumed by the text-command parser.
//
// Team IDs come from processor/internal/bot/argmatch.go and match the in-game
// numeric IDs: 0=harmony, 1=mystic, 2=valor, 3=instinct. We expose all four as
// positive choices on slash commands (no "any" sentinel — omitting the option
// is how a user expresses "any team").
func teamNameForValue(v int) string {
	switch v {
	case 0:
		return "harmony"
	case 1:
		return "mystic"
	case 2:
		return "valor"
	case 3:
		return "instinct"
	}
	return ""
}

// hasNonZeroValue returns true when an option is present and its value is not
// the zero value for its type. Used by /quest to enforce mutual exclusion
// between reward types.
func hasNonZeroValue(opt *discordgo.ApplicationCommandInteractionDataOption) bool {
	if opt == nil {
		return false
	}
	switch opt.Type {
	case discordgo.ApplicationCommandOptionString:
		return opt.StringValue() != ""
	case discordgo.ApplicationCommandOptionInteger:
		return opt.IntValue() > 0
	case discordgo.ApplicationCommandOptionBoolean:
		return opt.BoolValue()
	}
	return false
}

// emitFlag returns keyword when opt represents an "on" value, otherwise "".
//
// Supports two option shapes so a mapper can switch the underlying option
// type (Boolean ↔ String-with-single-Choice) without changing call sites:
//
//   - ApplicationCommandOptionString: non-empty StringValue → on. This is
//     the single-click "Yes" pattern (`{Choices: [{Name: "Yes", Value: "yes"}]}`),
//     where picking the option auto-fills the only choice.
//   - ApplicationCommandOptionBoolean: BoolValue() == true → on. Legacy
//     two-click pattern, retained so a future revert is one definition flip.
//
// Other types (Integer, etc.) return "" — emitFlag is intentionally narrow.
func emitFlag(opt *discordgo.ApplicationCommandInteractionDataOption, keyword string) string {
	if opt == nil {
		return ""
	}
	switch opt.Type {
	case discordgo.ApplicationCommandOptionString:
		if opt.StringValue() != "" {
			return keyword
		}
	case discordgo.ApplicationCommandOptionBoolean:
		if opt.BoolValue() {
			return keyword
		}
	}
	return ""
}

// appendCommonTail emits the distance / clean / template / location / areas
// tokens that every tracking-style mapper accepts as its tail. Equivalent
// to the inline block
//
//	appendDistance(&tokens, o["distance"])
//	if tok := emitFlag(o["clean"], "clean"); tok != "" {
//	    tokens = append(tokens, tok)
//	}
//	if v, ok := o["template"]; ok && v.StringValue() != "" {
//	    tokens = append(tokens, "template:"+v.StringValue())
//	}
//	appendLocationArea(&tokens, o)
//
// Mappers with extra interleaved options (e.g. /quest's `summary` between
// `clean` and `template`) emit those inline rather than using this helper
// but must still call appendLocationArea themselves.
func appendCommonTail(tokens *[]string, o map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	appendDistance(tokens, o["distance"])
	if tok := emitFlag(o["clean"], "clean"); tok != "" {
		*tokens = append(*tokens, tok)
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		*tokens = append(*tokens, "template:"+v.StringValue())
	}
	appendLocationArea(tokens, o)
}

// fortTypeName maps the /fort fort_type choice integer to the canonical
// English fort-type keyword expected by the text bot. Values are aligned
// with the strings the bot's arg matcher accepts (`pokestop` / `gym`).
func fortTypeName(v int) string {
	switch v {
	case 0:
		return "pokestop"
	case 1:
		return "gym"
	}
	return ""
}

// appendLocationArea emits `location:<name>` and `area:<value>` tokens for
// the shared tracker options. Non-empty location values pass through
// unchanged to the text-command parser's `location:` keyword. The `areas`
// option accepts a comma-separated string that the text parser already
// handles as a single `area:X,Y` token.
func appendLocationArea(tokens *[]string, o map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	if v, ok := o["location"]; ok && v.StringValue() != "" {
		*tokens = append(*tokens, "location:"+v.StringValue())
	}
	if v, ok := o["areas"]; ok && v.StringValue() != "" {
		*tokens = append(*tokens, "area:"+v.StringValue())
	}
}
