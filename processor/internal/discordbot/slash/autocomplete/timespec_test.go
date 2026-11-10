package autocomplete

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func timeSpecValues(focused string, tr *i18n.Translator) []string {
	choices := TimeSpec(focused, tr)
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		v, _ := c.Value.(string)
		out = append(out, v)
	}
	return out
}

// With nothing typed the user needs to learn the grammar, so every shape it
// supports (single fire, range, stepped range) must be represented.
func TestTimeSpecEmptyShowsEachShape(t *testing.T) {
	got := timeSpecValues("", nil)
	if len(got) == 0 {
		t.Fatal("expected suggestions for empty input")
	}
	var hasSingle, hasRange, hasStep bool
	for _, v := range got {
		switch {
		case strings.Contains(v, "/"):
			hasStep = true
		case strings.Contains(v, "-"):
			hasRange = true
		default:
			hasSingle = true
		}
	}
	if !hasSingle || !hasRange || !hasStep {
		t.Errorf("expected single, range and stepped shapes; got %v", got)
	}
}

// Typing a partial day prefix should complete to that prefix with usable
// times — this is the case the syntax is hardest to remember for.
func TestTimeSpecCompletesDayPrefix(t *testing.T) {
	got := timeSpecValues("wee", nil)
	if len(got) == 0 {
		t.Fatal("expected suggestions for \"wee\"")
	}
	var weekday, weekend bool
	for _, v := range got {
		if v == "wee" {
			continue // the echo of what was typed
		}
		if strings.HasPrefix(v, "weekday") {
			weekday = true
		}
		if strings.HasPrefix(v, "weekend") {
			weekend = true
		}
		if !strings.HasPrefix(v, "wee") {
			t.Errorf("suggestion %q does not extend the typed prefix", v)
		}
	}
	if !weekday || !weekend {
		t.Errorf("expected both weekday and weekend completions, got %v", got)
	}
}

// Free text must stay committable: the option accepts any valid spec, and a
// user who knows the syntax should not be forced onto a canned value.
func TestTimeSpecEchoesTypedValueFirst(t *testing.T) {
	got := timeSpecValues("sat23:45", nil)
	if len(got) == 0 || got[0] != "sat23:45" {
		t.Fatalf("typed value must be the first choice, got %v", got)
	}
}

// A single day prefix narrows to that day only.
func TestTimeSpecSingleDayPrefix(t *testing.T) {
	for _, v := range timeSpecValues("mon", nil) {
		if v == "mon" {
			continue
		}
		if !strings.HasPrefix(v, "mon") {
			t.Errorf("suggestion %q should start with mon, got %v", v, timeSpecValues("mon", nil))
		}
	}
}

// Digits complete the time shapes rather than day prefixes.
func TestTimeSpecDigitsCompleteTimes(t *testing.T) {
	got := timeSpecValues("07", nil)
	if len(got) < 2 {
		t.Fatalf("expected the echo plus at least one time completion, got %v", got)
	}
	for _, v := range got[1:] {
		if !strings.HasPrefix(v, "07") {
			t.Errorf("suggestion %q should extend \"07\": %v", v, got)
		}
	}
}

// Discord rejects a payload with more than 25 choices.
func TestTimeSpecRespectsChoiceCap(t *testing.T) {
	for _, f := range []string{"", "m", "w", "s", "0", "1"} {
		if n := len(timeSpecValues(f, nil)); n > 25 {
			t.Errorf("focused %q produced %d choices, max is 25", f, n)
		}
	}
}

// Translated day prefixes are accepted by the parser, so they must be
// offered to users running in that language.
func TestTimeSpecOffersTranslatedPrefixes(t *testing.T) {
	tr := i18n.NewTranslator("de", map[string]string{
		"arg.prefix.weekday": "wochentag",
	})
	got := timeSpecValues("woch", tr)
	var found bool
	for _, v := range got {
		if strings.HasPrefix(v, "wochentag") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a wochentag completion, got %v", got)
	}
}
