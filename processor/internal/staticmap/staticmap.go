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
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/scanner"
	"github.com/pokemon/poracleng/processor/internal/uicons"
)

// TileTypeConfig holds per-tile-type configuration for tileservercache.
// Boolean fields use *bool so that empty/absent config sections don't
// override defaults (nil = not set, non-nil = explicitly set).
type TileTypeConfig struct {
	Type         string
	IncludeStops *bool
	Width        int
	Height       int
	Zoom         int
	Pregenerate  *bool
}

// boolVal returns the value of a *bool, defaulting to false if nil.
func boolVal(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
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

// Stats holds tile generation statistics for periodic logging.
type Stats struct {
	Calls   int64
	TotalMs int64
	Errors  int64
}

// AvgMs returns the average duration in milliseconds, or 0 if no calls.
func (s Stats) AvgMs() int64 {
	if s.Calls == 0 {
		return 0
	}
	return s.TotalMs / s.Calls
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

	// Stats counters for periodic logging
	statCalls   atomic.Int64
	statTotalMs atomic.Int64
	statErrors  atomic.Int64
}

// GetStats returns the current tile generation statistics.
func (r *Resolver) GetStats() Stats {
	return Stats{
		Calls:   r.statCalls.Load(),
		TotalMs: r.statTotalMs.Load(),
		Errors:  r.statErrors.Load(),
	}
}

// ResetStats resets all tile generation statistics to zero.
func (r *Resolver) ResetStats() {
	r.statCalls.Store(0)
	r.statTotalMs.Store(0)
	r.statErrors.Store(0)
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
	if boolVal(tileOpts.IncludeStops) && boolVal(tileOpts.Pregenerate) && r.config.Scanner != nil {
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

	if !boolVal(tileOpts.Pregenerate) {
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
	t, f := true, false
	opts := TileTypeConfig{
		Type:        "staticMap",
		IncludeStops: &f,
		Width:        500,
		Height:       250,
		Zoom:         15,
		Pregenerate:  &t,
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
				opts.Pregenerate = &f
			} else {
				opts.Type = smType
				opts.Pregenerate = &t
			}
		}
	}

	// Apply per-tile-template overrides
	if ts, ok := r.config.TileserverSettings[maptype]; ok {
		mergeOpts(&opts, ts)
	}

	return opts
}

// mergeOpts applies non-zero/non-nil values from src to dst.
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
	if src.IncludeStops != nil {
		dst.IncludeStops = src.IncludeStops
	}
	if src.Pregenerate != nil {
		dst.Pregenerate = src.Pregenerate
	}
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
			metrics.TileTotal.WithLabelValues("circuit_break").Inc()
			log.Debugf("staticmap: circuit breaker open for %s, skipping tile", maptype)
			return ""
		}
		// Half-open: allow one probe
	}
	r.mu.Unlock()

	metrics.TileInFlight.Inc()
	defer metrics.TileInFlight.Dec()
	start := time.Now()

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
		metrics.TileTotal.WithLabelValues("error").Inc()
		return ""
	}

	log.Debugf("staticmap: POST %s type=%s%s fields=%v", reqURL, templateType, maptype, mapKeys(data))

	// Acquire concurrency slot
	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	resp, err := r.client.Post(reqURL, "application/json", bytes.NewReader(body))
	if err != nil {
		r.recordError()
		r.statErrors.Add(1)
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: pregenerate request failed: %s", err)
		return ""
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		r.recordError()
		r.statErrors.Add(1)
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: read pregenerate response: %s", err)
		return ""
	}

	if resp.StatusCode != http.StatusOK {
		r.recordError()
		r.statErrors.Add(1)
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: pregenerate %s got status %d: %s (sent fields: %v)", reqURL, resp.StatusCode, string(respBody), mapKeys(data))
		return ""
	}

	result := strings.TrimSpace(string(respBody))
	if result == "" || strings.Contains(result, "<") {
		r.recordError()
		r.statErrors.Add(1)
		metrics.TileTotal.WithLabelValues("error").Inc()
		metrics.TileDuration.Observe(time.Since(start).Seconds())
		log.Warnf("staticmap: pregenerate got invalid response: %s", result)
		return ""
	}

	duration := time.Since(start)
	metrics.TileDuration.Observe(duration.Seconds())
	metrics.TileTotal.WithLabelValues("success").Inc()
	r.statCalls.Add(1)
	r.statTotalMs.Add(duration.Milliseconds())

	// Reset circuit breaker on success
	r.mu.Lock()
	r.consecutiveErrors = 0
	r.mu.Unlock()

	// If the result is already a full URL, return it directly
	if strings.HasPrefix(result, "http") {
		log.Debugf("staticmap: tile generated %s (%dms)", result, duration.Milliseconds())
		return result
	}
	// Otherwise construct the URL from the tileserver base + pregenerated path
	tileURL := fmt.Sprintf("%s/%s/pregenerated/%s", r.config.ProviderURL, mapPath, result)
	log.Debugf("staticmap: tile generated %s (%dms)", tileURL, duration.Milliseconds())
	return tileURL
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

// LatLon represents a geographic coordinate.
type LatLon struct {
	Latitude  float64
	Longitude float64
}

// Circle represents a geographic circle with a radius in meters.
type Circle struct {
	Latitude  float64
	Longitude float64
	RadiusM   float64
}

// AutopositionShape holds collections of geographic shapes for autoposition calculation.
type AutopositionShape struct {
	Markers  []LatLon
	Polygons [][]LatLon // each polygon is a path of points
	Circles  []Circle
}

// AutopositionResult holds the computed zoom and center point.
type AutopositionResult struct {
	Zoom      float64
	Latitude  float64
	Longitude float64
}

// Autoposition calculates the optimal zoom level and center lat/lon for a map tile
// that fits all given shapes within the specified pixel dimensions.
// Ported from the JS tileserverPregen.autoposition() function.
func Autoposition(shapes AutopositionShape, width, height int, margin, defaultZoom float64) *AutopositionResult {
	w := float64(width) / margin
	h := float64(height) / margin

	var objs [][2]float64

	// Expand circles to their bounding points
	for _, c := range shapes.Circles {
		objs = append(objs,
			[2]float64{adjustLatitude(c.Latitude, -c.RadiusM), c.Longitude},
			[2]float64{adjustLatitude(c.Latitude, c.RadiusM), c.Longitude},
			[2]float64{c.Latitude, adjustLongitude(c.Latitude, c.Longitude, -c.RadiusM)},
			[2]float64{c.Latitude, adjustLongitude(c.Latitude, c.Longitude, c.RadiusM)},
		)
	}

	// Add markers
	for _, m := range shapes.Markers {
		objs = append(objs, [2]float64{m.Latitude, m.Longitude})
	}

	// Add polygon vertices
	for _, poly := range shapes.Polygons {
		for _, pt := range poly {
			objs = append(objs, [2]float64{pt.Latitude, pt.Longitude})
		}
	}

	if len(objs) == 0 {
		return nil
	}

	// Compute bounding box
	minLat, maxLat := objs[0][0], objs[0][0]
	minLon, maxLon := objs[0][1], objs[0][1]
	for _, o := range objs[1:] {
		if o[0] < minLat {
			minLat = o[0]
		}
		if o[0] > maxLat {
			maxLat = o[0]
		}
		if o[1] < minLon {
			minLon = o[1]
		}
		if o[1] > maxLon {
			maxLon = o[1]
		}
	}

	latitude := minLat + (maxLat-minLat)/2.0
	longitude := minLon + (maxLon-minLon)/2.0

	// If all points are the same, return default zoom
	if maxLat == minLat && maxLon == minLon {
		return &AutopositionResult{
			Zoom:      defaultZoom,
			Latitude:  latitude,
			Longitude: longitude,
		}
	}

	latFraction := (latRad(maxLat) - latRad(minLat)) / math.Pi
	angle := maxLon - minLon
	if angle < 0.0 {
		angle += 360.0
	}
	lonFraction := angle / 360.0

	latZoom := zoomCalc(h, latFraction)
	lonZoom := zoomCalc(w, lonFraction)
	z := latZoom
	if lonZoom < z {
		z = lonZoom
	}

	return &AutopositionResult{
		Zoom:      z,
		Latitude:  latitude,
		Longitude: longitude,
	}
}

// adjustLatitude shifts a latitude by the given distance in meters.
func adjustLatitude(lat, distanceM float64) float64 {
	const earth = 6378.137 // radius of the earth in km
	m := (1.0 / ((2.0 * math.Pi / 360.0) * earth)) / 1000.0 // 1 meter in degrees
	return lat + (distanceM * m)
}

// adjustLongitude shifts a longitude by the given distance in meters, accounting for latitude.
func adjustLongitude(lat, lon, distanceM float64) float64 {
	const earth = 6378.137
	m := (1.0 / ((2.0 * math.Pi / 360.0) * earth)) / 1000.0
	return lon + (distanceM*m)/math.Cos(lat*(math.Pi/180.0))
}

// latRad converts latitude to radians in the Mercator projection.
func latRad(lat float64) float64 {
	sinLat := math.Sin(lat * math.Pi / 180.0)
	rad := math.Log((1.0+sinLat)/(1.0-sinLat)) / 2.0
	return math.Max(math.Min(rad, math.Pi), -math.Pi) / 2.0
}

// zoomCalc computes the zoom level for a given pixel size and fraction.
func zoomCalc(px, fraction float64) float64 {
	z := math.Log2(px / 256.0 / fraction)
	// Round to two decimal places
	return math.Round(z*100) / 100
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
