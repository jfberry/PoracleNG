package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestPersonalShinyTileClonesBase(t *testing.T) {
	e := &Enricher{ImgUicons: uicons.NewWithIndex("https://icons.test", "png",
		&uicons.Index{Pokemon: map[string]bool{"25.png": true, "25_s.png": true}})}
	base := map[string]any{"imgUrl": "https://icons.test/pokemon/25.png", "staticMap": "base-tile", "staticmap": "base-tile", "name": "Pikachu"}

	m, pending := e.PersonalShinyTile(base, &webhook.PokemonWebhook{PokemonID: 25}, TileModeSkip)

	if m["imgUrl"] != "https://icons.test/pokemon/25_s.png" || m["name"] != "Pikachu" {
		t.Fatalf("shiny enrichment = %v", m)
	}
	if _, ok := m["staticMap"]; ok {
		t.Fatal("shiny enrichment must not reuse the base tile")
	}
	if pending != nil {
		t.Fatal("TileModeSkip must not submit a tile")
	}
	if base["imgUrl"] != "https://icons.test/pokemon/25.png" || base["staticMap"] != "base-tile" {
		t.Fatalf("base enrichment was modified: %v", base)
	}
}
