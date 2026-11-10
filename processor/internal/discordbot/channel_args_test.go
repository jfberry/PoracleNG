package discordbot

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// TestParseChannelArgs_UnderscoreValues — the command parser lowercases and
// turns unquoted underscores into spaces, so `area:gent_centrum` arrives as
// "area:gent centrum" and `name:my_hook` as "name:my hook". Both values must
// survive (#282); the area resolves to the configured fence name.
func TestParseChannelArgs_UnderscoreValues(t *testing.T) {
	fences := []geofence.Fence{{Name: "Gent_Centrum"}, {Name: "Brugge"}}
	cases := []struct {
		args             []string
		name, area, lang string
	}{
		{[]string{"add", "area:gent centrum"}, "", "gent_centrum", ""},
		{[]string{"add", "area<gent centrum>"}, "", "gent_centrum", ""},
		{[]string{"add", "area:brugge", "language:de"}, "", "brugge", "de"},
		{[]string{"add", "name:my hook", "area:unknown place"}, "my hook", "unknown place", ""},
		{[]string{"add", "namefoo"}, "foo", "", ""},
	}
	for _, tc := range cases {
		name, area, lang := parseChannelArgs(tc.args, fences)
		if name != tc.name || area != tc.area || lang != tc.lang {
			t.Errorf("parseChannelArgs(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.args, name, area, lang, tc.name, tc.area, tc.lang)
		}
	}
}
