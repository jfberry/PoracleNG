package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Lure maps /lure options to text-command tokens.
//
// Options:
//
//	lure_type  (string, required, choices) — normal/glacial/mossy/magnetic/rainy/sparkly
//	distance   (int)                       — alert radius in metres
//	clean      (bool)                       — auto-delete on expiry
//	template   (string, autocomplete)      — DTS template name
//
// The lure_type choice values map to the lure-name keywords accepted by
// processor/internal/bot/argmatch.go (`arg.normal`, `arg.glacial`, etc.).
// We lowercase defensively even though all choice values are already
// lowercase — protects against an operator-renamed choice slipping in.
func Lure(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	lure := strings.ToLower(getString(o["lure_type"]))
	if lure == "" {
		return nil, &MapperError{Key: "error.slash.lure.no_type"}
	}

	tokens := []string{lure}
	appendDistance(&tokens, o["distance"])
	if tok := emitFlag(o["clean"], "clean"); tok != "" {
		tokens = append(tokens, tok)
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}
	return tokens, nil
}

func init() { registry["lure"] = Lure }
