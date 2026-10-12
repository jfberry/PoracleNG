package enrichment

import "github.com/pokemon/poracleng/processor/internal/webhook"

// PersonalShinyIcons swaps imgUrl, imgUrlAlt and stickerUrl for the shiny
// variant for every user the pokemon is personally shiny for, so their alert
// shows the shiny sprite while everyone else keeps the normal one. Per-user
// fields take precedence over the base enrichment in the DTS view. Runs after
// AnnotatePersonal; the shiny URLs are resolved once per webhook.
func (e *Enricher) PersonalShinyIcons(perUser map[string]map[string]any, pokemon *webhook.PokemonWebhook) {
	var icons map[string]string
	for _, m := range perUser {
		if shiny, _ := m["personalShiny"].(bool); !shiny {
			continue
		}
		if icons == nil {
			icons = e.shinyPokemonIcons(pokemon)
		}
		for k, v := range icons {
			m[k] = v
		}
	}
}

func (e *Enricher) shinyPokemonIcons(p *webhook.PokemonWebhook) map[string]string {
	icons := make(map[string]string, 3)
	if e.ImgUicons != nil {
		icons["imgUrl"] = e.ImgUicons.PokemonIcon(p.PokemonID, p.Form, 0, p.Gender, p.Costume, 0, true, 0)
	}
	if e.ImgUiconsAlt != nil {
		icons["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(p.PokemonID, p.Form, 0, p.Gender, p.Costume, 0, true, 0)
	}
	if e.StickerUicons != nil {
		icons["stickerUrl"] = e.StickerUicons.PokemonIcon(p.PokemonID, p.Form, 0, p.Gender, p.Costume, 0, true, 0)
	}
	return icons
}
