package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Invasion maps /invasion options to text-command tokens.
//
// Options:
//
//	grunt_type  (string, required, autocomplete) — grunt template/type or event name
//	distance    (int)                            — alert radius in metres
//	clean       (bool)                            — auto-delete on expiry
//	template    (string, autocomplete)           — DTS template name
//
// The grunt_type is sent as a bare lowercased token; the bot's invasion
// command matches it against grunt template-derived names, translated type
// names, and pokestop event names (kecleon, showcase, etc.) — see
// processor/internal/bot/commands/invasion.go.
//
// Autocomplete for grunt_type is declared Autocomplete=true so Discord
// rounds-trips a request, but the dispatcher's routeAutocomplete tuple
// for (invasion, grunt_type) returns nil today — the field still accepts
// any free-text value the user types.
func Invasion(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	grunt := strings.ToLower(getString(o["grunt_type"]))
	if grunt == "" {
		return nil, &MapperError{Key: "error.slash.invasion.no_grunt"}
	}

	tokens := []string{grunt}
	appendDistance(&tokens, o["distance"])
	if tok := emitFlag(o["clean"], "clean"); tok != "" {
		tokens = append(tokens, tok)
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}
	return tokens, nil
}

func init() { registry["invasion"] = Invasion }
