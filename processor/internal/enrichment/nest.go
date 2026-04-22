package enrichment

import (
	"encoding/json"
	"math"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Nest builds enrichment fields for a nest webhook.
func (e *Enricher) Nest(nest *webhook.NestWebhook, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	expiration := nest.ResetTime + 7*24*60*60
	tz := geo.GetTimezone(nest.Lat, nest.Lon)

	m["tth"] = geo.ComputeTTH(expiration)
	m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
	m["disappearDate"] = geo.FormatTime(expiration, tz, e.DateLayout)
	m["resetTime"] = geo.FormatTime(nest.ResetTime, tz, e.TimeLayout)
	m["resetDate"] = geo.FormatTime(nest.ResetTime, tz, e.DateLayout)

	// Nest identity
	m["nest_id"] = nest.NestID
	m["nest_name"] = nest.Name
	m["pokemonCount"] = nest.PokemonCount
	m["pokemonSpawnAvg"] = nest.PokemonAvg

	// Normalise lat/lon to latitude/longitude for common field consumers
	m["latitude"] = nest.Lat
	m["longitude"] = nest.Lon

	// Icon URLs
	if e.ImgUicons != nil {
		m["imgUrl"] = e.ImgUicons.PokemonIcon(nest.PokemonID, nest.Form, 0, 0, 0, 0, false, 0)
	}
	if e.ImgUiconsAlt != nil {
		m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(nest.PokemonID, nest.Form, 0, 0, 0, 0, false, 0)
	}
	if e.StickerUicons != nil {
		m["stickerUrl"] = e.StickerUicons.PokemonIcon(nest.PokemonID, nest.Form, 0, 0, 0, 0, false, 0)
	}

	// Autoposition from polygon paths
	if nest.PolyPath != "" {
		var rawPolygons [][][2]float64
		if err := json.Unmarshal([]byte(nest.PolyPath), &rawPolygons); err != nil {
			log.Debugf("nest: failed to parse poly_path: %s", err)
		} else {
			var polygons [][]staticmap.LatLon
			for _, rawPoly := range rawPolygons {
				var poly []staticmap.LatLon
				for _, pt := range rawPoly {
					poly = append(poly, staticmap.LatLon{
						Latitude:  pt[0],
						Longitude: pt[1],
					})
				}
				polygons = append(polygons, poly)
			}

			position := staticmap.Autoposition(staticmap.AutopositionShape{
				Polygons: polygons,
			}, 500, 250, 1.25, 17.5)

			if position != nil {
				m["zoom"] = math.Min(position.Zoom, 16)
				m["map_latitude"] = position.Latitude
				m["map_longitude"] = position.Longitude
			}
		}
	}

	// Map URLs — ReactMap deep-links by nest id (`/id/nests/{nest_id}`)
	e.addMapURLs(m, nest.Lat, nest.Lon, "nests", strconv.FormatInt(nest.NestID, 10))

	// Reverse geocoding
	e.addGeoResult(m, nest.Lat, nest.Lon)

	// Static map tile — use autopositioned center if available, else original coords
	mapLat, mapLon := nest.Lat, nest.Lon
	if autoLat, ok := m["map_latitude"].(float64); ok {
		mapLat = autoLat
		mapLon = m["map_longitude"].(float64)
	}
	// Static map tile
	pending := e.addStaticMap(m, "nest", mapLat, mapLon, map[string]any{
		"pokemon_id":      nest.PokemonID,
		"form":            nest.Form,
		"pokemonSpawnAvg": nest.PokemonAvg,
	}, tileMode)

	// Pokemon identity
	m["pokemonId"] = nest.PokemonID

	// Game data enrichment
	if e.GameData != nil {
		monster := e.GameData.GetMonster(nest.PokemonID, nest.Form)
		if monster != nil {
			m["types"] = monster.Types
			m["color"] = e.GameData.GetTypeColor(monster.Types)
			m["typeEmojiKeys"] = e.GameData.GetTypeEmojiKeys(monster.Types)
		}
	}

	// Shiny possible
	if e.ShinyProvider != nil {
		rate := e.ShinyProvider.GetShinyRate(nest.PokemonID)
		if rate > 0 {
			m["shinyPossible"] = true
			m["shinyPossibleEmojiKey"] = "shiny"
		} else {
			m["shinyPossible"] = false
		}
	}

	e.setFallbackImg(m, e.FallbackImgURL)

	return m, pending
}

// NestTranslate adds per-language translated fields.
func (e *Enricher) NestTranslate(base map[string]any, pokemonID, form int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 5) // only translated fields; caller merges base + perLang

	tr := e.Translations.For(lang)
	TranslateMonsterNamesEng(m, e.GameData, tr, e.Translations, pokemonID, form, 0)

	monster := e.GameData.GetMonster(pokemonID, form)
	if monster != nil {
		TranslateTypeNames(m, tr, e.Translations.For("en"), monster.Types)
	}

	return m
}
