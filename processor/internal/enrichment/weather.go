package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Weather builds enrichment fields for a weather change event.
func (e *Enricher) Weather(lat, lon float64, gameplayCondition int, showAlteredPokemonStaticMap bool) map[string]any {
	m := make(map[string]any)

	nextHour := geo.NextHourBoundary()
	m["weatherTth"] = geo.ComputeTTH(nextHour)

	if lat != 0 || lon != 0 {
		tz := geo.GetTimezone(lat, lon)
		addSunTimes(m, lat, lon, tz)
	}

	// Generate base weather tile (used when showAlteredPokemonStaticMap is false)
	if !showAlteredPokemonStaticMap {
		e.addStaticMap(m, "weather", lat, lon, map[string]any{
			"gameplay_condition": gameplayCondition,
		})
	}

	return m
}

// WeatherTranslate adds per-language translated fields for a weather change.
func (e *Enricher) WeatherTranslate(base map[string]any, oldWeatherID, newWeatherID int, activePokemons []webhook.ActivePokemonEntry, lang string, showAlteredPokemonStaticMap bool) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+10)
	for k, v := range base {
		m[k] = v
	}

	gd := e.GameData
	tr := e.Translations.For(lang)

	// Icon URLs
	if e.ImgUicons != nil {
		m["imgUrl"] = e.ImgUicons.WeatherIcon(newWeatherID)
	}
	if e.ImgUiconsAlt != nil {
		m["imgUrlAlt"] = e.ImgUiconsAlt.WeatherIcon(newWeatherID)
	}
	if e.StickerUicons != nil {
		m["stickerUrl"] = e.StickerUicons.WeatherIcon(newWeatherID)
	}

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
			// Active pokemon icon URLs
			if e.ImgUicons != nil {
				entry["imgUrl"] = e.ImgUicons.PokemonIcon(pok.PokemonID, pok.Form, 0, 0, 0, 0, false)
			}
			enrichedPokemon[i] = entry
		}
		m["enrichedActivePokemons"] = enrichedPokemon

		// Generate per-user tile with active pokemon data when configured
		if showAlteredPokemonStaticMap {
			m["activePokemons"] = enrichedPokemon
			lat, _ := base["latitude"].(float64)
			lon, _ := base["longitude"].(float64)
			e.addStaticMap(m, "weather", lat, lon, map[string]any{
				"gameplay_condition": newWeatherID,
			})
		}
	}

	return m
}
