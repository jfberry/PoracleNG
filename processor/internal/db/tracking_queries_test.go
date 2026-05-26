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
