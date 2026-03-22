// Package staticmap resolves static map tile URLs for enriched webhook payloads.
// Supports tileservercache (pregenerate and non-pregenerate modes), Google,
// OSM (MapQuest), and Mapbox providers. Includes a circuit breaker for
// tileservercache to avoid hammering a failing server.
package staticmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

// TileTypeConfig holds per-tile-type configuration for tileservercache.
type TileTypeConfig struct {
	Type         string `json:"type" toml:"type"`                   // staticMap or multiStaticMap
	IncludeStops bool   `json:"includeStops" toml:"include_stops"`
	Width        int    `json:"width" toml:"width"`
	Height       int    `json:"height" toml:"height"`
	Zoom         int    `json:"zoom" toml:"zoom"`
	Pregenerate  bool   `json:"pregenerate" toml:"pregenerate"`
}

// Config holds all static map configuration.
type Config struct {
	Provider    string // "none", "tileservercache", "google", "osm", "mapbox"
	ProviderURL string // tileserver URL

	StaticKeys []string // API keys (cycled randomly)
	Width      int
	Height     int
	Zoom       int
	MapType    string // e.g. "klokantech-basic"

	// Per-tile-type settings (key: "default", "monster", "raid", etc.)
	TileserverSettings map[string]TileTypeConfig
	// Map alert type to tile template name (key: "pokemon", "raid", etc.)
	StaticMapType map[string]string

	// Concurrency limit for tileserver requests (default 2)
	TileserverConcurrency int

	// Scanner for nearby stops
	Scanner scanner.Scanner
	// Uicons for stop/gym marker icons
	ImgUicons *uicons.Uicons

	// Styles per time of day
	DayStyle   string
	DawnStyle  string
	DuskStyle  string
	NightStyle string

	// Tuning
	TileserverTimeout          int // ms (default 10000)
	TileserverFailureThreshold int // consecutive errors before circuit opens (default 5)
	TileserverCooldownMs       int // ms to keep circuit open (default 30000)

	// Fallback URL if tile generation fails
	FallbackURL string
}

// Resolver generates static map URLs for different providers.
type Resolver struct {
	config Config
	client *http.Client
	sem    chan struct{} // concurrency limiter for tileserver requests

	// Circuit breaker state
	consecutiveErrors int
	circuitOpenSince  time.Time
	mu                sync.Mutex
}

// New creates a new static map Resolver.
func New(config Config) *Resolver {
	if config.TileserverTimeout == 0 {
		config.TileserverTimeout = 10000
	}
	if config.TileserverFailureThreshold == 0 {
		config.TileserverFailureThreshold = 5
	}
	if config.TileserverCooldownMs == 0 {
		config.TileserverCooldownMs = 30000
	}
	if config.Width == 0 {
		config.Width = 320
	}
	if config.Height == 0 {
		config.Height = 200
	}
	if config.Zoom == 0 {
		config.Zoom = 15
	}
	if config.TileserverConcurrency <= 0 {
		config.TileserverConcurrency = 10
	}

	return &Resolver{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TileserverTimeout) * time.Millisecond,
		},
		sem: make(chan struct{}, config.TileserverConcurrency),
	}
}

// GetStaticMapURL returns a static map URL for the given alert type.
// maptype is the alert type: "monster", "raid", "pokestop", "quest", "gym", "nest", "weather", "maxbattle", "fort-update".
// data contains the full enrichment fields.
// keys lists fields for non-pregenerate mode; pregenKeys lists fields for pregenerate mode.
// The resolver filters data to only include the relevant fields before sending to the tileserver.
func (r *Resolver) GetStaticMapURL(maptype string, data map[string]any, keys, pregenKeys []string) string {
	provider := strings.ToLower(r.config.Provider)

	lat, _ := getFloat(data, "latitude")
	lon, _ := getFloat(data, "longitude")

	var result string

	switch provider {
	case "tileservercache":
		result = r.tileserverCache(maptype, data, lat, lon, keys, pregenKeys)
	case "google":
		key := r.randomKey()
		result = fmt.Sprintf(
			"https://maps.googleapis.com/maps/api/staticmap?center=%f,%f&markers=color:red|%f,%f&maptype=%s&zoom=%d&size=%dx%d&key=%s",
			lat, lon, lat, lon, r.config.MapType, r.config.Zoom, r.config.Width, r.config.Height, key,
		)
	case "osm":
		key := r.randomKey()
		result = fmt.Sprintf(
			"https://www.mapquestapi.com/staticmap/v5/map?locations=%f,%f&size=%d,%d&defaultMarker=marker-md-3B5998-22407F&zoom=%d&key=%s",
			lat, lon, r.config.Width, r.config.Height, r.config.Zoom, key,
		)
	case "mapbox":
		key := r.randomKey()
		result = fmt.Sprintf(
			"https://api.mapbox.com/styles/v1/mapbox/streets-v10/static/url-https%%3A%%2F%%2Fi.imgur.com%%2FMK4NUzI.png(%f,%f)/%f,%f,%d,0,0/%dx%d?access_token=%s",
			lon, lat, lon, lat, r.config.Zoom, r.config.Width, r.config.Height, key,
		)
	default:
		// "none" or unknown
		result = ""
	}

	if result == "" && r.config.FallbackURL != "" {
		return r.config.FallbackURL
	}
	return result
}

// tileserverCache handles the tileservercache provider.
func (r *Resolver) tileserverCache(maptype string, data map[string]any, lat, lon float64, keys, pregenKeys []string) string {
	tileOpts := r.getConfigForTileType(maptype)

	// If includeStops and pregenerate, fetch nearby stops from scanner
	if tileOpts.IncludeStops && tileOpts.Pregenerate && r.config.Scanner != nil {
		bounds := limits(lat, lon, tileOpts.Width, tileOpts.Height, tileOpts.Zoom)
		stops, err := r.config.Scanner.GetStopData(bounds[0], bounds[1], bounds[2], bounds[3])
		if err != nil {
			log.Warnf("staticmap: failed to get stop data: %s", err)
		} else {
			// Add icon URLs for gyms
			stopData := make([]map[string]any, 0, len(stops))
			for _, s := range stops {
				entry := map[string]any{
					"latitude":  s.Latitude,
					"longitude": s.Longitude,
					"type":      s.Type,
				}
				if s.Type == "gym" {
					entry["teamId"] = s.TeamID
					entry["slots"] = s.Slots
					if r.config.ImgUicons != nil {
						trainerCount := 6 - s.Slots
						entry["imgUrl"] = r.config.ImgUicons.GymIcon(s.TeamID, trainerCount, false, false)
					}
				}
				stopData = append(stopData, entry)
			}
			data["nearbyStops"] = stopData
			if r.config.ImgUicons != nil {
				data["uiconPokestopUrl"] = r.config.ImgUicons.PokestopIcon(0, false, 0, false)
			}
		}
	}

	if tileOpts.Type == "" || tileOpts.Type == "none" {
		return ""
	}

	if !tileOpts.Pregenerate {
		return r.getTileURL(maptype, filterFields(data, keys), tileOpts.Type)
	}
	// For pregenerate, include nearbyStops/uiconPokestopUrl plus the pregenKeys
	filtered := filterFields(data, pregenKeys)
	if v, ok := data["nearbyStops"]; ok {
		filtered["nearbyStops"] = v
	}
	if v, ok := data["uiconPokestopUrl"]; ok {
		filtered["uiconPokestopUrl"] = v
	}
	return r.getPregeneratedTileURL(maptype, filtered, tileOpts.Type)
}

// getConfigForTileType merges default, per-staticMapType, and per-tileserverSettings
// config for the given alert type.
func (r *Resolver) getConfigForTileType(maptype string) TileTypeConfig {
	// Start with defaults
	opts := TileTypeConfig{
		Type:        "staticMap",
		IncludeStops: false,
		Width:        500,
		Height:       250,
		Zoom:         15,
		Pregenerate:  true,
	}

	// Apply global default overrides
	if def, ok := r.config.TileserverSettings["default"]; ok {
		mergeOpts(&opts, def)
	}

	// Apply per-config-template staticMapType
	configTemplate := maptype
	if configTemplate == "monster" {
		configTemplate = "pokemon"
	}
	if r.config.StaticMapType != nil {
		if smType, ok := r.config.StaticMapType[configTemplate]; ok {
			if strings.HasPrefix(smType, "*") {
				opts.Type = smType[1:]
				opts.Pregenerate = false
			} else {
				opts.Type = smType
				opts.Pregenerate = true
			}
		}
	}

	// Apply per-tile-template overrides
	if ts, ok := r.config.TileserverSettings[maptype]; ok {
		mergeOpts(&opts, ts)
	}

	return opts
}

// mergeOpts applies non-zero values from src to dst.
func mergeOpts(dst *TileTypeConfig, src TileTypeConfig) {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Width > 0 {
		dst.Width = src.Width
	}
	if src.Height > 0 {
		dst.Height = src.Height
	}
	if src.Zoom > 0 {
		dst.Zoom = src.Zoom
	}
	// Booleans are always copied (they default to false which is meaningful)
	dst.IncludeStops = src.IncludeStops
	dst.Pregenerate = src.Pregenerate
}

// getTileURL builds a non-pregenerated tile URL with query parameters.
func (r *Resolver) getTileURL(maptype string, data map[string]any, staticMapType string) string {
	mapPath := "staticmap"
	templateType := ""
	if strings.EqualFold(staticMapType, "multistaticmap") {
		mapPath = "multistaticmap"
		templateType = "multi-"
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/poracle-%s%s", r.config.ProviderURL, mapPath, templateType, maptype))
	if err != nil {
		return ""
	}

	q := u.Query()
	for k, v := range data {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// getPregeneratedTileURL POSTs data to the tileserver to pregenerate a tile.
func (r *Resolver) getPregeneratedTileURL(maptype string, data map[string]any, staticMapType string) string {
	// Circuit breaker check
	r.mu.Lock()
	if r.consecutiveErrors >= r.config.TileserverFailureThreshold {
		elapsed := time.Since(r.circuitOpenSince)
		if elapsed < time.Duration(r.config.TileserverCooldownMs)*time.Millisecond {
			r.mu.Unlock()
			return ""
		}
		// Half-open: allow one probe
	}
	r.mu.Unlock()

	mapPath := "staticmap"
	templateType := ""
	if strings.EqualFold(staticMapType, "multistaticmap") {
		mapPath = "multistaticmap"
		templateType = "multi-"
	}

	reqURL := fmt.Sprintf("%s/%s/poracle-%s%s?pregenerate=true&regeneratable=true",
		r.config.ProviderURL, mapPath, templateType, maptype)

	body, err := json.Marshal(data)
	if err != nil {
		log.Warnf("staticmap: marshal data: %s", err)
		return ""
	}

	log.Debugf("staticmap: POST %s type=%s%s fields=%v", reqURL, templateType, maptype, mapKeys(data))

	// Acquire concurrency slot
	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	resp, err := r.client.Post(reqURL, "application/json", bytes.NewReader(body))
	if err != nil {
		r.recordError()
		log.Warnf("staticmap: pregenerate request failed: %s", err)
		return ""
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		r.recordError()
		log.Warnf("staticmap: read pregenerate response: %s", err)
		return ""
	}

	if resp.StatusCode != http.StatusOK {
		r.recordError()
		log.Warnf("staticmap: pregenerate %s got status %d: %s (sent fields: %v)", reqURL, resp.StatusCode, string(respBody), mapKeys(data))
		return ""
	}

	result := strings.TrimSpace(string(respBody))
	if result == "" || strings.Contains(result, "<") {
		r.recordError()
		log.Warnf("staticmap: pregenerate got invalid response: %s", result)
		return ""
	}

	// Reset circuit breaker on success
	r.mu.Lock()
	r.consecutiveErrors = 0
	r.mu.Unlock()

	// If the result is already a full URL, return it directly
	if strings.HasPrefix(result, "http") {
		return result
	}
	// Otherwise construct the URL from the tileserver base + pregenerated path
	return fmt.Sprintf("%s/%s/pregenerated/%s", r.config.ProviderURL, mapPath, result)
}

// recordError increments the consecutive error counter and opens the circuit if threshold reached.
func (r *Resolver) recordError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutiveErrors++
	if r.consecutiveErrors >= r.config.TileserverFailureThreshold {
		r.circuitOpenSince = time.Now()
	}
}

// limits converts pixel coordinates to lat/lon using the Web Mercator projection.
// Returns [minLat, minLon, maxLat, maxLon].
// Ported from the JS tileserverPregen.limits() function.
func limits(latCenter, lonCenter float64, width, height, zoom int) [4]float64 {
	c := (256.0 / (2.0 * math.Pi)) * math.Pow(2, float64(zoom))

	xcenter := c * (lonCenter*math.Pi/180.0 + math.Pi)
	ycenter := c * (math.Pi - math.Log(math.Tan(math.Pi/4+latCenter*math.Pi/360.0)))

	w := float64(width)
	h := float64(height)

	// Point [0,0] — top-left corner
	xTopLeft := xcenter - w/2
	yTopLeft := ycenter - h/2

	mTopLeft := (xTopLeft / c) - math.Pi
	nTopLeft := -(yTopLeft / c) + math.Pi

	lonTL := mTopLeft * 180.0 / math.Pi
	latTL := (math.Atan(math.Exp(nTopLeft)) - math.Pi/4) * 2 * 180.0 / math.Pi

	// Point [width,height] — bottom-right corner
	xBotRight := xcenter + w/2
	yBotRight := ycenter + h/2

	mBotRight := (xBotRight / c) - math.Pi
	nBotRight := -(yBotRight / c) + math.Pi

	lonBR := mBotRight * 180.0 / math.Pi
	latBR := (math.Atan(math.Exp(nBotRight)) - math.Pi/4) * 2 * 180.0 / math.Pi

	return [4]float64{latTL, lonTL, latBR, lonBR}
}

// randomKey picks a random API key from the configured list.
func (r *Resolver) randomKey() string {
	keys := r.config.StaticKeys
	if len(keys) == 0 {
		return ""
	}
	return keys[rand.IntN(len(keys))]
}

// mapKeys returns the keys of a map for logging.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

// getFloat extracts a float64 from a map value.
func getFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch f := v.(type) {
	case float64:
		return f, true
	case float32:
		return float64(f), true
	case int:
		return float64(f), true
	case int64:
		return float64(f), true
	default:
		return 0, false
	}
}
