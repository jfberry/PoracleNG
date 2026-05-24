package delivery

import "testing"

// TestExtractMessageIDForSnapshot pins the helper that strips Discord's
// composite SentMessage.ID format down to the bare message id used in
// the snapshot store key. Regression for the "alert has expired" click
// bug — the write path uses this, the click handler / eviction hook
// reads against the same shape, and they must agree.
func TestExtractMessageIDForSnapshot(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"discord bot DM composite", "bot/1186554123456789012:1234567890123456789", "1234567890123456789"},
		{"discord webhook composite", "https://discord.com/api/webhooks/123/abc:9876543210", "9876543210"},
		{"bare id (telegram or test stub)", "9876543210", "9876543210"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractMessageIDForSnapshot(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
