package delivery

import "testing"

func TestTargetClass(t *testing.T) {
	cases := map[string]string{
		"discord:user":     "dm",
		"telegram:user":    "dm",
		"api:user":         "dm",
		"discord:channel":  "channel",
		"discord:thread":   "channel",
		"telegram:group":   "channel",
		"telegram:channel": "channel",
		"telegram:topic":   "channel",
		"api:channel":      "channel",
		"webhook":          "webhook",
		"bogus":            "",
	}
	for in, want := range cases {
		if got := TargetClass(in); got != want {
			t.Errorf("TargetClass(%q) = %q, want %q", in, got, want)
		}
	}
}
