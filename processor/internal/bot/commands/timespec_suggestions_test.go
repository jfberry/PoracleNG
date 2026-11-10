package commands

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// Every spec the slash autocomplete offers must parse. This is the contract
// that matters: a suggestion the parser rejects is worse than no suggestion,
// because the user trusts the picker.
//
// Lives in this package because ParseSettimeArg is here and the autocomplete
// package cannot import it (commands → discordbot/slash → autocomplete), so
// the round-trip can only be asserted from this side.
func TestTimeSpecSuggestionsAllParse(t *testing.T) {
	dayPrefixes := bot.DayPrefixMap(nil)

	// Cover the empty box plus every entry point a user can type into.
	focusedInputs := []string{
		"", "m", "mo", "mon", "t", "w", "we", "wee", "weekday", "weekend",
		"e", "every", "s", "sat", "sun", "f", "0", "07", "08",
	}
	for _, focused := range focusedInputs {
		for _, choice := range autocomplete.TimeSpec(focused, nil) {
			spec, _ := choice.Value.(string)
			if spec == focused {
				continue // the echo of raw user input is not ours to validate
			}
			entries, err := ParseSettimeArg(spec, dayPrefixes)
			if err != nil {
				t.Errorf("focused %q suggested %q which fails to parse: %v", focused, spec, err)
				continue
			}
			// A spec that parses to nothing is silently useless — the
			// command treats zero entries as "no schedule given".
			if len(entries) == 0 {
				t.Errorf("focused %q suggested %q which parses to zero entries", focused, spec)
			}
		}
	}
}
