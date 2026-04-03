package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Weather builds enrichment fields for a weather change event.
func (e *Enricher) Weather(lat, lon float64, gameplayCondition int, coords [][2]float64, showAlteredPokemonStaticMap bool) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	// Store lat/lon and coords for use by per-language enrichment
	m["latitude"] = lat
	m["longitude"] = lon
	if len(coords) > 0 {
		m["coords"] = coords
	}

	nextHour := geo.NextHourBoundary()
	m["weatherTth"] = geo.ComputeTTH(nextHour)

	if lat != 0 || lon != 0 {
		tz := geo.GetTimezone(lat, lon)
		addSunTimes(m, lat, lon, tz)
	}

	// Icon URLs (based on new weather condition)
	if gameplayCondition > 0 {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.WeatherIcon(gameplayCondition)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.WeatherIcon(gameplayCondition)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.WeatherIcon(gameplayCondition)
		}
	}

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Generate base weather tile (used when showAlteredPokemonStaticMap is false)
	var pending *staticmap.TilePending
	if !showAlteredPokemonStaticMap {
		webhookFields := map[string]any{
			"gameplay_condition": gameplayCondition,
		}
		if len(coords) > 0 {
			webhookFields["coords"] = coords
		}
		pending = e.addStaticMap(m, "weather", lat, lon, webhookFields)
	}

	return m, pending
}

// WeatherTranslate adds per-language translated fields for a weather change.
func (e *Enricher) WeatherTranslate(base map[string]any, oldWeatherID, newWeatherID int, activePokemons []webhook.ActivePokemonEntry, lang string, showAlteredPokemonStaticMap bool) (map[string]any, *staticmap.TilePending) {
	if e.GameData == nil || e.Translations == nil {
		return nil, nil
	}

	m := make(map[string]any, 10) // only translated fields; caller merges base + perLang

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
	var pending *staticmap.TilePending
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

		// Generate per-user tile with active pokemon data when configured.
		// Pass base enrichment so addStaticMap has access to imgUrl and
		// all other base fields for the tileserver payload.
		if showAlteredPokemonStaticMap {
			m["activePokemons"] = enrichedPokemon
			lat, _ := base["latitude"].(float64)
			lon, _ := base["longitude"].(float64)
			pending = e.addStaticMap(m, "weather", lat, lon, base)
		}
	}

	return m, pending
}
