package db

import "testing"

// normaliseInvasionGruntType maps legacy/third-party 'metal' to the
// canonical 'steel' that the webhook classifier produces. Anything
// else passes through unchanged.
func TestNormaliseInvasionGruntType(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"metal", "steel"},
		{"Metal", "steel"}, // case-insensitive — third-party tools vary
		{"METAL", "steel"},
		{"steel", "steel"},  // canonical → unchanged
		{"fire", "fire"},    // typed grunt unchanged
		{"giovanni", "giovanni"},
		{"everything", "everything"},
		{"boss", "boss"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normaliseInvasionGruntType(c.in); got != c.want {
			t.Errorf("normaliseInvasionGruntType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
