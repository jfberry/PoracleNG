package geofence

import "testing"

func TestCanonicalAreaName(t *testing.T) {
	fences := []Fence{{Name: "Gent_Centrum"}, {Name: "Other"}}
	for _, in := range []string{"gent centrum", "GENT_CENTRUM", "Gent Centrum", "gent_centrum"} {
		if got := CanonicalAreaName(fences, in); got != "gent_centrum" {
			t.Errorf("CanonicalAreaName(%q) = %q, want gent_centrum", in, got)
		}
	}
	if got := CanonicalAreaName(fences, "Foo Bar"); got != "foo bar" {
		t.Errorf("CanonicalAreaName(Foo Bar) = %q, want foo bar (no fence: lowercase only)", got)
	}
	if got := CanonicalAreaName(nil, "Gent_Centrum"); got != "gent_centrum" {
		t.Errorf("CanonicalAreaName(nil fences) = %q, want gent_centrum", got)
	}
	// A spaced fence name stays spaced.
	if got := CanonicalAreaName([]Fence{{Name: "Gent Centrum"}}, "gent_centrum"); got != "gent centrum" {
		t.Errorf("CanonicalAreaName(spaced fence) = %q, want gent centrum", got)
	}
}
