package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

// WeatherProvider looks up weather conditions for an S2 cell.
type WeatherProvider interface {
	GetCurrentWeatherInCell(cellID string) int
	GetWeatherForecast(cellID string) tracker.WeatherForecast
}

// ForecastProvider fetches external weather forecasts on demand.
type ForecastProvider interface {
	EnsureForecast(cellID string)
}

// ShinyRateProvider returns shiny rate for a pokemon.
type ShinyRateProvider interface {
	GetShinyRate(pokemonID int) float64
}

// MapConfig holds the map URL configuration for enrichment.
type MapConfig struct {
	RdmURL       string
	ReactMapURL  string
	RocketMadURL string
}

// PVPDisplayConfig holds configuration for per-user PVP display filtering.
type PVPDisplayConfig struct {
	MaxRank       int
	GreatMinCP    int
	UltraMinCP    int
	LittleMinCP   int
	FilterByTrack bool
}

// Enricher computes additional fields to accompany webhook messages
// sent to the alerter. The enrichment map is sent alongside the original
// raw message so neither needs to be re-encoded.
type Enricher struct {
	TimeLayout       string
	DateLayout       string
	WeatherProvider  WeatherProvider
	ForecastProvider ForecastProvider  // optional; triggers AccuWeather fetch
	ShinyProvider    ShinyRateProvider // optional; provides shiny rates
	EventChecker     *PogoEventChecker
	GameData         *gamedata.GameData  // game master data for enrichment
	Translations     *i18n.Bundle        // translations for per-language enrichment
	MapConfig        *MapConfig          // map URL configuration
	IvColors           []string            // Discord IV color hex strings (6 thresholds)
	PVPDisplay         *PVPDisplayConfig   // PVP display filtering config
	ImgUicons          *uicons.Uicons        // Primary icon resolver
	ImgUiconsAlt       *uicons.Uicons        // Alternative icon resolver
	StickerUicons      *uicons.Uicons        // Sticker icon resolver (webp)
	RequestShinyImages bool                   // Whether to request shiny icon variants
	StaticMap          *staticmap.Resolver    // Static map tile resolver (nil = disabled)
}

// New creates a new Enricher.
func New(timeLayout, dateLayout string, weather WeatherProvider, eventChecker *PogoEventChecker) *Enricher {
	return &Enricher{
		TimeLayout:      timeLayout,
		DateLayout:      dateLayout,
		WeatherProvider: weather,
		EventChecker:    eventChecker,
	}
}

// GetForecast returns the weather forecast for a cell, triggering an external
// forecast fetch if a ForecastProvider is configured.
func (e *Enricher) GetForecast(cellID string) tracker.WeatherForecast {
	if e.ForecastProvider != nil {
		e.ForecastProvider.EnsureForecast(cellID)
	}
	return e.WeatherProvider.GetWeatherForecast(cellID)
}

// addSunTimes adds nightTime/dawnTime/duskTime booleans to the enrichment map.
func addSunTimes(m map[string]any, lat, lon float64, tz string) {
	nowUnix := time.Now().Unix()
	sunTimes := geo.ComputeSunTimes(nowUnix, lat, lon, tz)
	m["nightTime"] = sunTimes.NightTime
	m["dawnTime"] = sunTimes.DawnTime
	m["duskTime"] = sunTimes.DuskTime
}

// addMapURLs adds all map URL fields to the enrichment map.
func (e *Enricher) addMapURLs(m map[string]any, lat, lon float64, entityType, entityID string) {
	m["appleMapUrl"] = geo.AppleMapURL(lat, lon)
	m["googleMapUrl"] = geo.GoogleMapURL(lat, lon)
	m["wazeMapUrl"] = geo.WazeMapURL(lat, lon)

	if e.MapConfig == nil {
		return
	}

	if e.MapConfig.RdmURL != "" {
		m["rdmUrl"] = normalizeTrailingSlash(e.MapConfig.RdmURL) + "@" + entityType + "/" + entityID
	}
	if e.MapConfig.ReactMapURL != "" {
		m["reactMapUrl"] = normalizeTrailingSlash(e.MapConfig.ReactMapURL) + "id/" + entityType + "/" + entityID
	}
	if e.MapConfig.RocketMadURL != "" {
		m["rocketMadUrl"] = normalizeTrailingSlash(e.MapConfig.RocketMadURL) + "?lat=" + geo.FormatCoord(lat) + "&lon=" + geo.FormatCoord(lon) + "&zoom=18.0"
	}
}

func normalizeTrailingSlash(url string) string {
	if len(url) > 0 && url[len(url)-1] != '/' {
		return url + "/"
	}
	return url
}

// addStaticMap generates a static map tile URL and adds it to the enrichment map.
// Sets latitude/longitude on the map before generating, then generates the tile
// using only the fields required by the tileserver for this alert type.
func (e *Enricher) addStaticMap(m map[string]any, maptype string, lat, lon float64) {
	if e.StaticMap == nil {
		return
	}
	m["latitude"] = lat
	m["longitude"] = lon
	keys, pregenKeys := staticMapFieldsForType(maptype)
	url := e.StaticMap.GetStaticMapURL(maptype, m, keys, pregenKeys)
	m["staticMap"] = url
	m["staticmap"] = url // deprecated alias
}

// Common pregen fields shared by all tile types
var pregenCommon = []string{"latitude", "longitude", "imgUrl", "imgUrlAlt", "nightTime", "duskTime", "dawnTime", "style"}

// staticMapFieldsForType returns the field lists for non-pregenerate (keys) and
// pregenerate (pregenKeys) modes, matching the alerter's per-controller field lists.
func staticMapFieldsForType(maptype string) (keys []string, pregenKeys []string) {
	switch maptype {
	case "monster":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "costume", "imgUrl", "imgUrlAlt", "style"}
		pregenKeys = append(pregenCommon, "pokemon_id", "display_pokemon_id", "verified", "costume", "form", "pokemonId", "generation", "weather", "confirmedTime", "shinyPossible", "seenType", "seen_type", "cell_coords")
	case "raid":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "level", "imgUrl", "style"}
		pregenKeys = append(pregenCommon, "pokemon_id", "form", "level")
	case "pokestop":
		keys = []string{"latitude", "longitude", "imgUrl", "gruntTypeId", "displayTypeId", "style"}
		pregenKeys = append(pregenCommon, "gruntTypeId", "displayTypeId", "lureTypeId")
	case "quest":
		keys = []string{"latitude", "longitude", "imgUrl", "style"}
		pregenKeys = append(pregenCommon[:0:0], pregenCommon...) // copy base
	case "gym":
		keys = []string{"latitude", "longitude", "imgUrl", "team_id", "style"}
		pregenKeys = append(pregenCommon, "team_id", "slotsAvailable", "inBattle", "ex")
	case "nest":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "imgUrl", "style"}
		pregenKeys = append(pregenCommon, "pokemon_id", "form", "pokemonSpawnAvg")
	case "weather":
		keys = []string{"latitude", "longitude", "gameplay_condition", "imgUrl", "style"}
		pregenKeys = append(pregenCommon, "gameplay_condition")
	case "maxbattle":
		keys = []string{"latitude", "longitude", "imgUrl", "battle_level", "style"}
		pregenKeys = append(pregenCommon, "battle_level", "battle_pokemon_id")
	case "fort-update":
		keys = []string{"latitude", "longitude", "style"}
		pregenKeys = []string{"latitude", "longitude", "nightTime", "duskTime", "dawnTime", "style"}
	default:
		keys = []string{"latitude", "longitude"}
		pregenKeys = []string{"latitude", "longitude"}
	}
	return
}
