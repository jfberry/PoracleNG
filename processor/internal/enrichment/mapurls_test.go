package enrichment

import "testing"

// TestAddMapURLsDiadem verifies that diademUrl is built with the singular
// path segment Diadem expects, that trailing slashes on the configured base
// are tolerated, and that the field is suppressed when the entity type
// isn't in the supported map (e.g. a webhook variant we don't yet wire).
func TestAddMapURLsDiadem(t *testing.T) {
	cases := []struct {
		name       string
		base       string
		entityType string
		entityID   string
		want       string // empty = field should not be set
	}{
		{"gym", "https://diadem.example.com/", "gyms", "abc.16", "https://diadem.example.com/gym/abc.16"},
		{"pokestop", "https://diadem.example.com", "pokestops", "stop.16", "https://diadem.example.com/pokestop/stop.16"},
		{"nest", "https://diadem.example.com", "nests", "12345", "https://diadem.example.com/nest/12345"},
		{"station", "https://diadem.example.com", "stations", "stn.23", "https://diadem.example.com/station/stn.23"},
		{"pokemon", "https://diadem.example.com", "pokemon", "123456789", "https://diadem.example.com/pokemon/123456789"},
		{"unknown entity type suppresses field", "https://diadem.example.com", "weather", "5169892116045758464", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Enricher{MapConfig: &MapConfig{DiademURL: tc.base}}
			m := map[string]any{}
			e.addMapURLs(m, 0, 0, tc.entityType, tc.entityID)
			got, ok := m["diademUrl"]
			if tc.want == "" {
				if ok {
					t.Errorf("expected diademUrl unset, got %q", got)
				}
				return
			}
			if !ok {
				t.Fatalf("diademUrl not set, want %q", tc.want)
			}
			if got != tc.want {
				t.Errorf("diademUrl = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAddMapURLsDiademUnconfigured confirms diademUrl is omitted entirely
// when DiademURL is empty (default), matching how reactMapUrl/rdmUrl behave.
func TestAddMapURLsDiademUnconfigured(t *testing.T) {
	e := &Enricher{MapConfig: &MapConfig{}}
	m := map[string]any{}
	e.addMapURLs(m, 0, 0, "gyms", "abc.16")
	if _, ok := m["diademUrl"]; ok {
		t.Errorf("diademUrl should not be set when DiademURL is empty")
	}
}
