package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
)

func multiPrefixes() map[string][]int { return bot.DayPrefixMap(nil) }

// Input matching neither form must report an error. Returning (nil, nil)
// made every malformed spec a silent no-op: the caller saw zero entries and
// fell through to its usage text without saying anything was wrong.
func TestParseSettimeArgUnrecognisedIsAnError(t *testing.T) {
	for _, in := range []string{"garbage", "mon07:30,weekday09-17/2", "25:99:99:99"} {
		entries, err := ParseSettimeArg(in, multiPrefixes())
		if err == nil {
			t.Errorf("%q: expected an error, got %d entries and nil", in, len(entries))
		}
	}
}

// A comma-separated list is what the slash surface sends (and what the
// mapper's comment always claimed was supported).
func TestParseSettimeArgsSplitsCommas(t *testing.T) {
	entries, err := ParseSettimeArgs([]string{"mon07:30,weekday09-17/2"}, multiPrefixes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// mon07:30 → 1 entry; weekday09-17/2 → 5 entries (Mon–Fri).
	if len(entries) != 6 {
		t.Fatalf("expected 6 entries, got %d: %+v", len(entries), entries)
	}
}

// The text bot splits on spaces itself, but a quoted slash value can carry
// whitespace, so both separators must work.
func TestParseSettimeArgsSplitsWhitespace(t *testing.T) {
	entries, err := ParseSettimeArgs([]string{"mon07:30 weekend09:00"}, multiPrefixes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 { // 1 + 2 (Sat, Sun)
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
}

// Multiple args (the text-bot shape) still work, and mix with separators.
func TestParseSettimeArgsHandlesMultipleArgsAndBlanks(t *testing.T) {
	entries, err := ParseSettimeArgs([]string{"mon07:30,", "", "  ", "weekend09:00"}, multiPrefixes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
}

// A bad element must name itself so the user can see which one to fix,
// rather than the whole input being rejected anonymously.
func TestParseSettimeArgsReportsOffendingToken(t *testing.T) {
	_, err := ParseSettimeArgs([]string{"mon07:30,nonsense"}, multiPrefixes())
	if err == nil {
		t.Fatal("expected an error for the bad element")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("error should name the offending token, got %q", err.Error())
	}
}

func TestParseSettimeArgsEmptyInput(t *testing.T) {
	entries, err := ParseSettimeArgs([]string{"", "   "}, multiPrefixes())
	if err != nil {
		t.Fatalf("blank input should not error, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %+v", entries)
	}
}
