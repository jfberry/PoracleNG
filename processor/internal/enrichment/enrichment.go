package enrichment

import (
	"maps"
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
	TimeLayout         string
	DateLayout         string
	WeatherProvider    WeatherProvider
	ForecastProvider   ForecastProvider  // optional; triggers AccuWeather fetch
	ShinyProvider      ShinyRateProvider // optional; provides shiny rates
	EventChecker       *PogoEventChecker
	GameData           *gamedata.GameData  // game master data for enrichment
	Translations       *i18n.Bundle        // translations for per-language enrichment
	MapConfig          *MapConfig          // map URL configuration
	IvColors           []string            // Discord IV color hex strings (6 thresholds)
	PVPDisplay         *PVPDisplayConfig   // PVP display filtering config
	ImgUicons          *uicons.Uicons      // Primary icon resolver
	ImgUiconsAlt       *uicons.Uicons      // Alternative icon resolver
	StickerUicons      *uicons.Uicons      // Sticker icon resolver (webp)
	DefaultLocale      string              // Fallback locale when user has no language set
	RequestShinyImages bool                // Whether to request shiny icon variants
	StaticMap          *staticmap.Resolver // Static map tile resolver (nil = disabled)
	Geocoder           *geocoding.Geocoder // Reverse geocoder (nil = disabled)

	// Fallback icon URLs when uicons are not configured or fail
	FallbackImgURL      string
	FallbackImgWeather  string
	FallbackImgEgg      string
	FallbackImgGym      string
	FallbackImgPokestop string
	FallbackPokestopURL string
}

// setFallbackImg sets imgUrl to the fallback if it wasn't set by uicons.
func (e *Enricher) setFallbackImg(m map[string]any, fallback string) {
	if _, ok := m["imgUrl"]; !ok && fallback != "" {
		m["imgUrl"] = fallback
	}
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
	m["county"] = addr.County
	m["zipcode"] = addr.Zipcode
	m["country"] = addr.Country
	m["countryCode"] = addr.CountryCode
	m["neighbourhood"] = addr.Neighbourhood
	m["suburb"] = addr.Suburb
	m["formattedAddress"] = addr.FormattedAddress
}

// Tile mode constants. Defined here to avoid import cycles with cmd/processor.
const (
	TileModeSkip         = 0 // no template uses staticMap → don't generate tile
	TileModeInline       = 1 // all users can accept bytes → POST without pregenerate
	TileModeURL          = 2 // at least one user needs a fetchable URL → pregenerate
	TileModeURLWithBytes = 3 // mixed: pregenerate (public URL embedded in message) + fetch bytes once via internal URL for Discord-upload destinations in the batch
)

// addStaticMap generates a static map tile URL and adds it to the enrichment map.
// For async-capable providers (tileservercache pregenerate), returns a TilePending
// that the sender resolves before flushing. For instant providers, sets the URL
// directly and returns nil.
// tileMode controls whether to skip (0), generate inline bytes (1), or generate
// a fetchable URL (2).
func (e *Enricher) addStaticMap(m map[string]any, maptype string, lat, lon float64, webhookFields map[string]any, tileMode int) *staticmap.TilePending {
	if e.StaticMap == nil || tileMode == TileModeSkip {
		return nil
	}
	// Build tileserver payload from enrichment + webhook fields
	merged := make(map[string]any, len(m)+len(webhookFields)+2)
	maps.Copy(merged, m)
	maps.Copy(merged, webhookFields)
	merged["latitude"] = lat
	merged["longitude"] = lon
	keys, pregenKeys := staticMapFieldsForType(maptype)

	if tileMode == TileModeInline {
		// Inline mode POSTs without pregenerate=true — the tileserver returns
		// image bytes directly. Assumes tileservercache with POST-body support.
		filtered := filterFields(merged, pregenKeys)
		e.StaticMap.AddNearbyStops(filtered, merged, maptype)
		return e.StaticMap.SubmitTileInline(maptype, filtered, e.StaticMap.GetStaticMapType(maptype), m)
	}

	if tileMode == TileModeURLWithBytes {
		// Mixed batch: pregenerate to produce a public URL (embedded in the
		// rendered message for Telegram / upload-off Discord) AND download
		// the bytes once via internal_url so Discord-upload destinations in
		// the same batch don't each re-fetch the public URL.
		filtered := filterFields(merged, pregenKeys)
		e.StaticMap.AddNearbyStops(filtered, merged, maptype)
		return e.StaticMap.SubmitTileBoth(maptype, filtered, e.StaticMap.GetStaticMapType(maptype), m)
	}

	// TileModeURL — current flow
	url, pending := e.StaticMap.GetStaticMapURLAsync(maptype, merged, keys, pregenKeys, m)
	if pending != nil {
		// Tile will be resolved async by the sender
		return pending
	}
	// Instant URL (non-pregen or non-tileservercache)
	m["staticMap"] = url
	m["staticmap"] = url // deprecated alias
	return nil
}

// filterFields returns a new map containing only the keys in the allowed list.
func filterFields(data map[string]any, allowed []string) map[string]any {
	result := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if v, ok := data[key]; ok {
			result[key] = v
		}
	}
	return result
}

// staticMapFieldsForType returns the field lists for non-pregenerate (keys) and
// pregenerate (pregenKeys) modes.
//
// Both use the same base set of fields. Pregenerate adds nightTime/duskTime/dawnTime
// (not useful in a URL) and large array fields like nearbyStops and activePokemons.
func staticMapFieldsForType(maptype string) (keys []string, pregenKeys []string) {
	// Common fields included in both modes
	common := []string{"latitude", "longitude", "imgUrl", "imgUrlAlt",
		"nightTime", "duskTime", "dawnTime", "style"}

	switch maptype {
	case "monster":
		typeFields := []string{"pokemon_id", "display_pokemon_id", "form", "costume", "pokemonId",
			"generation", "weather", "confirmedTime", "shinyPossible", "seenType", "seen_type", "verified",
			"cell_coords"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "raid":
		typeFields := []string{"pokemon_id", "form", "level", "teamId", "evolution", "costume"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "pokestop":
		typeFields := []string{"gruntTypeId", "displayTypeId", "lureTypeId"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "quest":
		keys = common
		pregenKeys = keys
	case "gym":
		typeFields := []string{"teamId", "slotsAvailable", "inBattle", "ex"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "nest":
		typeFields := []string{"pokemon_id", "form", "pokemonSpawnAvg"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "weather":
		typeFields := []string{"gameplay_condition", "coords"}
		keys = append(common, typeFields...)
		// Pregen adds activePokemons (large array, not suitable for URL)
		pregenKeys = append(append([]string{}, keys...), "activePokemons")
	case "maxbattle":
		typeFields := []string{"battle_level", "battle_pokemon_id"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	case "fort-update":
		typeFields := []string{"isEditLocation", "fortType", "map_latitude", "map_longitude",
			"oldLatitude", "oldLongitude", "zoom"}
		keys = append(common, typeFields...)
		pregenKeys = keys
	default:
		keys = []string{"latitude", "longitude"}
		pregenKeys = []string{"latitude", "longitude"}
	}
	return
}
