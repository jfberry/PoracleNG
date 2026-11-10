package db

import "testing"

func TestParseOverrideAreas_Normalizes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`["Berlin"]`, "berlin"},
		{`["london_central"]`, "london central"},
		{`["BERLIN_MITTE"]`, "berlin mitte"},
	}
	for _, c := range cases {
		got := parseOverrideAreas(c.in)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("parseOverrideAreas(%q) = %v, want %q", c.in, got, c.want)
		}
	}
}

func TestParseOverrideAreas_Empty(t *testing.T) {
	if got := parseOverrideAreas(""); got != nil {
		t.Errorf("parseOverrideAreas(\"\") = %v, want nil", got)
	}
}

func TestParseOverrideAreas_InvalidJSON(t *testing.T) {
	if got := parseOverrideAreas("not-json"); got != nil {
		t.Errorf("parseOverrideAreas(invalid) = %v, want nil", got)
	}
}

// TestParseOverrideAreas_V1StaysSpaced: parseOverrideAreas feeds the v1 GET
// endpoints, !tracked and rowtext. They are frozen on the spaced key form, so a
// row v2 stored canonically ("gent_centrum") must still read "gent centrum"
// here; v2 shows the fence name via its own read-side canonicalisation.
func TestParseOverrideAreas_V1StaysSpaced(t *testing.T) {
	got := parseOverrideAreas(`["gent_centrum","Gent Centrum"]`)
	if len(got) != 2 || got[0] != "gent centrum" || got[1] != "gent centrum" {
		t.Fatalf("parseOverrideAreas = %v, want [gent centrum gent centrum]", got)
	}
}
