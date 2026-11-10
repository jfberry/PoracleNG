package gamedata

import "testing"

// The embedded util.json had no showcaseFocus block, so showcaseFocusEmoji
// was always empty (#272). Every focus class must map to an emoji key with
// a default.
func TestEmbeddedUtilShowcaseFocusEmoji(t *testing.T) {
	u, err := ParseUtilData(embeddedUtilJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pokemon", "type", "alignment", "class", "family", "buddy", "generation", "hatched", "mega", "shiny"} {
		info, ok := u.ShowcaseFocus[f]
		if !ok || info.Emoji == "" {
			t.Errorf("showcaseFocus %q: missing or no emoji key", f)
			continue
		}
		if u.Emojis[info.Emoji] == "" {
			t.Errorf("emoji key %q has no default", info.Emoji)
		}
	}
}
