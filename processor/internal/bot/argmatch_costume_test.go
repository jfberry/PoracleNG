package bot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// newCostumeTestMatcher builds an ArgMatcher seeded with one multi-word
// costume name. Mirrors newMultiWordTestMatcher in argmatch_test.go, scoped
// to costume: so this file doesn't need to touch the shared harness.
func newCostumeTestMatcher() *ArgMatcher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"arg.prefix.costume": "costume",
		"costume_1":          "Holiday 2016",
	}))
	gd := &gamedata.GameData{
		Util:     &gamedata.UtilData{},
		Costumes: map[int]gamedata.CostumeInfo{1: {ID: 1, Name: "Holiday 2016"}},
	}
	return NewArgMatcher(bundle, gd, nil, []string{"en"})
}

// TestCostumeArgCaptured verifies costume:<name> is captured into
// parsed.Strings["costume"] and resolves via ResolveCostume. Underscore→space
// substitution happens earlier in the pipeline (bot/parser.go tokenize step,
// applied per raw token before ArgMatcher ever sees it) — so "holiday_2016"
// typed by a user arrives here as the single token "costume:holiday 2016",
// same convention as TestArgMatchMultiWordKeyword's "no rsvp".
func TestCostumeArgCaptured(t *testing.T) {
	am := newCostumeTestMatcher()
	params := []ParamDef{{Type: ParamPrefixString, Key: "arg.prefix.costume"}}
	result := am.Match([]string{"costume:holiday 2016"}, params, "en")
	got, ok := result.Strings["costume"]
	if !ok {
		t.Fatal("expected parsed.Strings[\"costume\"] to be set")
	}
	id, resolved := am.ResolveCostume(got, "en")
	if !resolved || id != 1 {
		t.Errorf("ResolveCostume(%q) = (%d, %v), want (1, true)", got, id, resolved)
	}
}

// TestCostumeArgEagerJoinsMultiWord verifies "costume:holiday" "2016" is
// collapsed into a single "costume:holiday 2016" token by the multi-word
// pre-pass (mirroring move:/form: eager-join), and resolves the same way.
func TestCostumeArgEagerJoinsMultiWord(t *testing.T) {
	am := newCostumeTestMatcher()

	collapsed := am.collapseMultiWord([]string{"costume:holiday", "2016"})
	want := []string{"costume:holiday 2016"}
	if len(collapsed) != len(want) || collapsed[0] != want[0] {
		t.Fatalf("collapseMultiWord = %v, want %v", collapsed, want)
	}

	params := []ParamDef{{Type: ParamPrefixString, Key: "arg.prefix.costume"}}
	result := am.Match([]string{"costume:holiday", "2016"}, params, "en")
	got, ok := result.Strings["costume"]
	if !ok {
		t.Fatal("expected parsed.Strings[\"costume\"] to be set")
	}
	id, resolved := am.ResolveCostume(got, "en")
	if !resolved || id != 1 {
		t.Errorf("ResolveCostume(%q) = (%d, %v), want (1, true)", got, id, resolved)
	}
}

// TestResolveCostumeNumeric verifies numeric costume values (including the
// 0 "no costume" sentinel) pass straight through.
func TestResolveCostumeNumeric(t *testing.T) {
	am := newCostumeTestMatcher()
	cases := []struct {
		in   string
		want int
	}{
		{"0", 0},
		{"5", 5},
	}
	for _, c := range cases {
		id, ok := am.ResolveCostume(c.in, "en")
		if !ok || id != c.want {
			t.Errorf("ResolveCostume(%q) = (%d, %v), want (%d, true)", c.in, id, ok, c.want)
		}
	}
}

// TestResolveCostumeUnknownName verifies an unrecognized costume name
// resolves to ok=false rather than silently falling back to some ID.
func TestResolveCostumeUnknownName(t *testing.T) {
	am := newCostumeTestMatcher()
	if id, ok := am.ResolveCostume("nonexistent costume", "en"); ok {
		t.Errorf("ResolveCostume(unknown) = (%d, true), want ok=false", id)
	}
}
