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

	// Static map framing. The nest templates place the map centre at
	// map_latitude/map_longitude with zoom, draw poly_path as the nest
	// outline, and put the pokemon marker at latitude/longitude. The map
	// fields are always set (nest position, zoom 16 when there is no usable
	// polygon) because the templates emit them unquoted: missing values
	// would make the tileserver reject the template as invalid JSON.
	m["map_latitude"] = nest.Lat
	m["map_longitude"] = nest.Lon
	m["zoom"] = float64(16)
	var rawPolygons [][][2]float64
	if nest.PolyPath != "" {
		if err := json.Unmarshal([]byte(nest.PolyPath), &rawPolygons); err != nil {
			log.Debugf("[%d] nest: failed to parse poly_path: %s", nest.NestID, err)
			rawPolygons = nil
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
	e.addLocationFields(m, nest.Lat, nest.Lon)

	// Static map tile. latitude/longitude stay the nest's own position (the
	// marker); map_latitude/map_longitude/zoom reach the payload from m.
	webhookFields := map[string]any{
		"pokemon_id":      nest.PokemonID,
		"form":            nest.Form,
		"pokemonSpawnAvg": nest.PokemonAvg,
	}
	if len(rawPolygons) > 0 {
		webhookFields["poly_path"] = rawPolygons
	}
	pending := e.addStaticMap(m, "nest", nest.Lat, nest.Lon, webhookFields, tileMode, strconv.FormatInt(nest.NestID, 10))

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
func (e *Enricher) NestTranslate(base map[string]any, nest *webhook.NestWebhook, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil || nest == nil {
		return nil
	}

	m := make(map[string]any, 15) // only translated fields; caller merges base + perLang
	defer e.addLocalizedGeoResult(m, nest.Lat, nest.Lon, lang)
	pokemonID := nest.PokemonID
	form := nest.Form

	tr := e.Translations.For(lang)
	TranslateMonsterNamesEng(m, e.GameData, tr, e.Translations, pokemonID, form, 0, 0)

	monster := e.GameData.GetMonster(pokemonID, form)
	if monster != nil {
		TranslateTypeNames(m, tr, e.Translations.For("en"), monster.Types)
	}

	return m
}
