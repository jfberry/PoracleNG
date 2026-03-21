package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Weather builds enrichment fields for a weather change event.
func (e *Enricher) Weather(lat, lon float64) map[string]any {
	m := make(map[string]any)

	nextHour := geo.NextHourBoundary()
	m["weatherTth"] = geo.ComputeTTH(nextHour)

	if lat != 0 || lon != 0 {
		tz := geo.GetTimezone(lat, lon)
		addSunTimes(m, lat, lon, tz)
	}

	return m
}

// WeatherTranslate adds per-language translated fields for a weather change.
func (e *Enricher) WeatherTranslate(base map[string]any, oldWeatherID, newWeatherID int, activePokemons []webhook.ActivePokemonEntry, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+10)
	for k, v := range base {
		m[k] = v
	}

	gd := e.GameData
	tr := e.Translations.For(lang)

	// Weather names and emoji keys
	m["oldWeatherName"] = TranslateWeatherName(tr, oldWeatherID)
	m["weatherName"] = TranslateWeatherName(tr, newWeatherID)
	if oldWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[oldWeatherID]; ok {
			m["oldWeatherEmojiKey"] = wInfo.Emoji
		}
	}
	if newWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[newWeatherID]; ok {
			m["weatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Active pokemon names
	if len(activePokemons) > 0 {
		enrichedPokemon := make([]map[string]any, len(activePokemons))
		for i, pok := range activePokemons {
			entry := map[string]any{
				"pokemon_id":    pok.PokemonID,
				"form":          pok.Form,
				"iv":            pok.IV,
				"cp":            pok.CP,
				"latitude":      pok.Latitude,
				"longitude":     pok.Longitude,
				"disappearTime": pok.DisappearTime,
			}
			nameInfo := make(map[string]any)
			TranslateMonsterNamesEng(nameInfo, gd, tr, e.Translations, pok.PokemonID, pok.Form, 0)
			entry["name"] = nameInfo["name"]
			entry["nameEng"] = nameInfo["nameEng"]
			entry["formName"] = nameInfo["formName"]
			entry["formNormalised"] = nameInfo["formNormalised"]
			entry["fullName"] = nameInfo["fullName"]
			entry["fullNameEng"] = nameInfo["fullNameEng"]
			enrichedPokemon[i] = entry
		}
		m["enrichedActivePokemons"] = enrichedPokemon
	}

	return m
}
