package autocomplete

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// ivSuggestions are the canned IV-range hints offered by IVRange when the
// user hasn't typed anything specific. Order is meaningful: the most
// common request ("100" IV only) sits first.
var ivSuggestions = []string{"100", "95", "0-0"}

// IVRange returns choices for an IV / IV-range option. When focused is
// non-empty, the first choice echoes what the user has typed so they can
// commit to it directly; the canned suggestions follow, filtered to
// prefix matches. When focused is empty, all canned suggestions are
// returned in their natural order.
func IVRange(focused string) []*discordgo.ApplicationCommandOptionChoice {
	focused = strings.TrimSpace(focused)
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	seen := map[string]bool{}
	if focused != "" {
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: focused, Value: focused})
		seen[focused] = true
	}
	for _, s := range ivSuggestions {
		if seen[s] {
			continue
		}
		if focused != "" && !strings.HasPrefix(s, focused) {
			continue
		}
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
		if len(out) == 25 {
			break
		}
	}
	return out
}
