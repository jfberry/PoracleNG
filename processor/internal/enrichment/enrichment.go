package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
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
	Geocoder           *geocoding.Geocoder    // Reverse geocoder (nil = disabled)

	// LastTilePending is set by the most recent addStaticMap call.
	// Webhook handlers should read this after calling enrichment functions
	// and attach it to the OutboundPayload. Reset with ResetTilePending().
	// Safe for single-goroutine use (one handler per enricher call).
	LastTilePending *staticmap.TilePending
}

// ResetTilePending clears any pending tile from a previous enrichment call.
func (e *Enricher) ResetTilePending() {
	e.LastTilePending = nil
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

// addGeoResult performs a reverse geocode lookup and adds address fields to the
// enrichment map. This should be called BEFORE addStaticMap so that static map
// templates can reference address fields.
func (e *Enricher) addGeoResult(m map[string]any, lat, lon float64) {
	if e.Geocoder == nil {
		return
	}
	addr := e.Geocoder.GetAddress(lat, lon)
	if addr == nil {
		return
	}
	m["addr"] = addr.Addr
	m["flag"] = addr.Flag
	m["streetName"] = addr.StreetName
	m["streetNumber"] = addr.StreetNumber
	m["city"] = addr.City
	m["state"] = addr.State
	m["zipcode"] = addr.Zipcode
	m["country"] = addr.Country
	m["countryCode"] = addr.CountryCode
	m["neighbourhood"] = addr.Neighbourhood
	m["suburb"] = addr.Suburb
	m["formattedAddress"] = addr.FormattedAddress
}

// addStaticMap generates a static map tile URL and adds it to the enrichment map.
// For async-capable providers (tileservercache pregenerate), returns a TilePending
// that the sender resolves before flushing. For instant providers, sets the URL
// directly and returns nil.
func (e *Enricher) addStaticMap(m map[string]any, maptype string, lat, lon float64, webhookFields map[string]any) *staticmap.TilePending {
	if e.StaticMap == nil {
		return nil
	}
	// Build tileserver payload from enrichment + webhook fields
	merged := make(map[string]any, len(m)+len(webhookFields)+2)
	for k, v := range m {
		merged[k] = v
	}
	for k, v := range webhookFields {
		merged[k] = v
	}
	merged["latitude"] = lat
	merged["longitude"] = lon
	keys, pregenKeys := staticMapFieldsForType(maptype)

	url, pending := e.StaticMap.GetStaticMapURLAsync(maptype, merged, keys, pregenKeys, m)
	if pending != nil {
		// Tile will be resolved async by the sender
		e.LastTilePending = pending
		return pending
	}
	// Instant URL (non-pregen or non-tileservercache)
	m["staticMap"] = url
	m["staticmap"] = url // deprecated alias
	return nil
}

// pregenBase builds a new pregenKeys slice from the common base fields plus extras.
// Returns a fresh slice each time — safe for concurrent use.
func pregenBase(extras ...string) []string {
	base := []string{"latitude", "longitude", "imgUrl", "imgUrlAlt", "nightTime", "duskTime", "dawnTime", "style"}
	result := make([]string, len(base), len(base)+len(extras))
	copy(result, base)
	return append(result, extras...)
}

// staticMapFieldsForType returns the field lists for non-pregenerate (keys) and
// pregenerate (pregenKeys) modes, matching the alerter's per-controller field lists.
func staticMapFieldsForType(maptype string) (keys []string, pregenKeys []string) {
	switch maptype {
	case "monster":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "costume", "imgUrl", "imgUrlAlt", "style"}
		pregenKeys = pregenBase("pokemon_id", "display_pokemon_id", "verified", "costume", "form", "pokemonId", "generation", "weather", "confirmedTime", "shinyPossible", "seenType", "seen_type", "cell_coords")
	case "raid":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "level", "imgUrl", "style"}
		pregenKeys = pregenBase("pokemon_id", "form", "level")
	case "pokestop":
		keys = []string{"latitude", "longitude", "imgUrl", "gruntTypeId", "displayTypeId", "style"}
		pregenKeys = pregenBase("gruntTypeId", "displayTypeId", "lureTypeId")
	case "quest":
		keys = []string{"latitude", "longitude", "imgUrl", "style"}
		pregenKeys = pregenBase()
	case "gym":
		keys = []string{"latitude", "longitude", "imgUrl", "team_id", "style"}
		pregenKeys = pregenBase("team_id", "slotsAvailable", "inBattle", "ex")
	case "nest":
		keys = []string{"pokemon_id", "latitude", "longitude", "form", "imgUrl", "style"}
		pregenKeys = pregenBase("pokemon_id", "form", "pokemonSpawnAvg")
	case "weather":
		keys = []string{"latitude", "longitude", "gameplay_condition", "coords", "activePokemons", "imgUrl", "style"}
		pregenKeys = pregenBase("gameplay_condition", "coords", "activePokemons")
	case "maxbattle":
		keys = []string{"latitude", "longitude", "imgUrl", "battle_level", "style"}
		pregenKeys = pregenBase("battle_level", "battle_pokemon_id")
	case "fort-update":
		keys = []string{"latitude", "longitude", "isEditLocation", "fortType", "map_latitude", "map_longitude", "oldLatitude", "oldLongitude", "zoom", "style"}
		pregenKeys = []string{"latitude", "longitude", "nightTime", "duskTime", "dawnTime", "style",
			"isEditLocation", "fortType", "map_latitude", "map_longitude", "oldLatitude", "oldLongitude", "zoom"}
	default:
		keys = []string{"latitude", "longitude"}
		pregenKeys = []string{"latitude", "longitude"}
	}
	return
}
