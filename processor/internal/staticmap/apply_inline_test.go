package staticmap

import "testing"

// ApplyInline must leave a non-empty {{staticMap}} on success, including when
// no fallback is configured: an empty one makes the Discord sender skip the
// upload, so the fetched tile was silently dropped (empty [fallbacks]
// static_map).
func TestApplyInline(t *testing.T) {
	cases := []struct {
		name, fallback, want string
	}{
		{"no fallback", "", InlineImageRef},
		{"fallback set", "https://example.com/fallback.png", "https://example.com/fallback.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := map[string]any{}
			tp := &TilePending{Inline: true, Fallback: tc.fallback, target: target}
			tp.ApplyInline()
			if target["staticMap"] != tc.want || target["staticmap"] != tc.want {
				t.Errorf("staticMap=%v staticmap=%v, want %q", target["staticMap"], target["staticmap"], tc.want)
			}
		})
	}
}
