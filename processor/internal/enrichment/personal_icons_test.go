package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestPersonalShinyIcons(t *testing.T) {
	idx := func() *uicons.Index {
		return &uicons.Index{Pokemon: map[string]bool{"25.png": true, "25_s.png": true, "25.webp": true, "25_s.webp": true}}
	}
	e := &Enricher{
		ImgUicons:     uicons.NewWithIndex("https://icons.test", "png", idx()),
		ImgUiconsAlt:  uicons.NewWithIndex("https://alt.test", "png", idx()),
		StickerUicons: uicons.NewWithIndex("https://sticker.test", "webp", idx()),
	}
	perUser := map[string]map[string]any{
		"shiny":      {"hasLinkedAccounts": true, "personalShiny": true},
		"notShiny":   {"hasLinkedAccounts": true, "personalShiny": false},
		"linkedOnly": {"hasLinkedAccounts": true}, // spawn not evaluated
	}
	e.PersonalShinyIcons(perUser, &webhook.PokemonWebhook{PokemonID: 25})

	want := map[string]string{
		"imgUrl":     "https://icons.test/pokemon/25_s.png",
		"imgUrlAlt":  "https://alt.test/pokemon/25_s.png",
		"stickerUrl": "https://sticker.test/pokemon/25_s.webp",
	}
	for k, v := range want {
		if got := perUser["shiny"][k]; got != v {
			t.Errorf("shiny user %s = %v, want %v", k, got, v)
		}
	}
	for _, id := range []string{"notShiny", "linkedOnly"} {
		for k := range want {
			if _, ok := perUser[id][k]; ok {
				t.Errorf("%s got per-user %s; must keep the base icon", id, k)
			}
		}
	}

	// No icon resolvers configured, or no per-user map: nothing to do, no panic.
	(&Enricher{}).PersonalShinyIcons(perUser, &webhook.PokemonWebhook{PokemonID: 25})
	e.PersonalShinyIcons(nil, &webhook.PokemonWebhook{PokemonID: 25})
}
