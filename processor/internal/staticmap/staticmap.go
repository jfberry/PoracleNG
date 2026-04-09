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
	TileQueueSize              int // async tile request queue depth (default 100)
	TileDeadlineMs             int // max time a payload waits for its tile (default 5000)

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

// TilePending represents an in-flight tile generation request.
// The sender checks Result (non-blocking) to see if the tile is ready.
type TilePending struct {
	Result   chan string     // receives tile URL when done (buffered, size 1)
	Deadline time.Time      // if not resolved by this time, use Fallback
	Fallback string         // fallback URL if deadline expires or generation fails
	target   map[string]any // enrichment map to write staticMap/staticmap into
}

// Apply writes the resolved tile URL into the enrichment map.
func (tp *TilePending) Apply(url string) {
	if tp.target != nil {
		tp.target["staticMap"] = url
		tp.target["staticmap"] = url
	}
}

// tileRequest is an internal work item for the tile queue.
type tileRequest struct {
	pending       *TilePending
	maptype       string
	data          map[string]any
	staticMapType string
}

// Resolver generates static map URLs for different providers.
type Resolver struct {
	config    Config
	client    *http.Client
	tileQueue chan tileRequest // async tile generation queue
	done      chan struct{}    // signals tile workers to stop
	wg        sync.WaitGroup  // tracks tile worker goroutines

	// Circuit breaker state
	consecutiveErrors    int
	circuitOpenSince     time.Time
	halfOpenProbeActive  bool // true when a half-open probe request is in flight
	mu                   sync.Mutex

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
		config.TileserverConcurrency = 2
	}
	if config.TileQueueSize <= 0 {
		config.TileQueueSize = 100
	}
	if config.TileDeadlineMs <= 0 {
		config.TileDeadlineMs = 10000
	}

	r := &Resolver{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TileserverTimeout) * time.Millisecond,
		},
		tileQueue: make(chan tileRequest, config.TileQueueSize),
		done:      make(chan struct{}),
	}

	// Start tile worker goroutines
	for range config.TileserverConcurrency {
		r.wg.Add(1)
		go r.tileWorker()
	}

	return r
}

// Close stops tile workers and drains any remaining queued tile requests.
func (r *Resolver) Close() {
	close(r.done)
	r.wg.Wait() // wait for all tile workers to finish

	// Drain remaining tile requests with fallback URLs
	for {
		select {
		case req := <-r.tileQueue:
			req.pending.Result <- req.pending.Fallback
		default:
			return
		}
	}
}

// TileDeadline returns the configured deadline duration for async tile requests.
func (r *Resolver) TileDeadline() time.Duration {
	return time.Duration(r.config.TileDeadlineMs) * time.Millisecond
}

// SubmitTile queues an async tile generation request and returns a TilePending.
// The caller should NOT block on the result — the sender will resolve it.
// For non-pregenerate or non-tileservercache providers, returns nil (URL set synchronously).
func (r *Resolver) SubmitTile(maptype string, data map[string]any, staticMapType string, target map[string]any) *TilePending {
	pending := &TilePending{
		Result:   make(chan string, 1),
		Deadline: time.Now().Add(r.TileDeadline()),
		Fallback: r.config.FallbackURL,
		target:   target,
	}

	select {
	case r.tileQueue <- tileRequest{pending: pending, maptype: maptype, data: data, staticMapType: staticMapType}:
		metrics.TileQueueDepth.Set(float64(len(r.tileQueue)))
	default:
		// queue full, resolve immediately with fallback
		pending.Result <- r.config.FallbackURL
		metrics.TileTotal.WithLabelValues("queue_full").Inc()
		log.Warnf("staticmap: tile queue full, using fallback for %s", maptype)
	}

	return pending
}

// tileWorker drains the tile queue and generates tiles.
func (r *Resolver) tileWorker() {
	defer r.wg.Done()
	for {
		select {
		case req := <-r.tileQueue:
			metrics.TileQueueDepth.Set(float64(len(r.tileQueue)))
			if time.Now().After(req.pending.Deadline) {
				req.pending.Result <- req.pending.Fallback
				metrics.TileTotal.WithLabelValues("deadline").Inc()
				continue
			}
			url := r.generatePregenTile(req.maptype, req.data, req.staticMapType)
			if url == "" {
				url = req.pending.Fallback
			}
			req.pending.Result <- url
		case <-r.done:
			return
		}
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

// GetStaticMapURLAsync returns a static map URL for webhook enrichment.
// For instant providers (google, osm, mapbox, non-pregen tileservercache), returns (url, nil).
// For pregenerate tileservercache, returns ("", pending) — the sender resolves the pending.
// target is the enrichment map where staticMap/staticmap will be written by the pending.
func (r *Resolver) GetStaticMapURLAsync(maptype string, data map[string]any, keys, pregenKeys []string, target map[string]any) (string, *TilePending) {
	provider := strings.ToLower(r.config.Provider)

	if provider != "tileservercache" {
		// Non-tileservercache: instant URL, no async
		return r.GetStaticMapURL(maptype, data, keys, pregenKeys), nil
	}

	return r.tileserverCacheAsync(maptype, data, keys, pregenKeys, target)
}

// tileserverCacheAsync handles async tile generation for tileservercache pregenerate mode.
// Does NOT mutate the input data map — nearby stops are added to the filtered copy.
func (r *Resolver) tileserverCacheAsync(maptype string, data map[string]any, keys, pregenKeys []string, target map[string]any) (string, *TilePending) {
	tileOpts := r.getConfigForTileType(maptype)

	if tileOpts.Type == "" || tileOpts.Type == "none" {
		return "", nil
	}

	if !boolVal(tileOpts.Pregenerate) {
		// Non-pregenerate: instant URL construction
		return r.GetTileURL(maptype, filterFields(data, keys), tileOpts.Type), nil
	}

	// Pregenerate: filter to pregen keys
	filtered := filterFields(data, pregenKeys)

	// Fetch nearby stops into the filtered copy (not the shared data map)
	r.addNearbyStops(filtered, data, tileOpts)

	return "", r.SubmitTile(maptype, filtered, tileOpts.Type, target)
}

// tileserverCache handles the tileservercache provider (synchronous, for API endpoints).
// Does NOT mutate the input data map — nearby stops are added to the filtered copy.
func (r *Resolver) tileserverCache(maptype string, data map[string]any, lat, lon float64, keys, pregenKeys []string) string {
	tileOpts := r.getConfigForTileType(maptype)

	if tileOpts.Type == "" || tileOpts.Type == "none" {
		return ""
	}

	if !boolVal(tileOpts.Pregenerate) {
		return r.GetTileURL(maptype, filterFields(data, keys), tileOpts.Type)
	}

	// Pregenerate: filter to pregen keys
	filtered := filterFields(data, pregenKeys)

	// Fetch nearby stops into the filtered copy (not the shared data map)
	r.addNearbyStops(filtered, data, tileOpts)

	return r.GetPregeneratedTileURL(maptype, filtered, tileOpts.Type)
}

// addNearbyStops fetches nearby stops from the scanner DB and adds them to the target map.
func (r *Resolver) addNearbyStops(target, data map[string]any, tileOpts TileTypeConfig) {
	if !boolVal(tileOpts.IncludeStops) || r.config.Scanner == nil {
		return
	}

	lat, _ := getFloat(data, "latitude")
	lon, _ := getFloat(data, "longitude")
	bounds := limits(lat, lon, tileOpts.Width, tileOpts.Height, tileOpts.Zoom)
	stops, err := r.config.Scanner.GetStopData(bounds[0], bounds[1], bounds[2], bounds[3])
	if err != nil {
		log.Warnf("staticmap: failed to get stop data: %s", err)
		return
	}

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
	target["nearbyStops"] = stopData
	if r.config.ImgUicons != nil {
		target["uiconPokestopUrl"] = r.config.ImgUicons.PokestopIcon(0, false, 0, false)
	}
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

// GetTileURL builds a non-pregenerated tile URL with query parameters.
func (r *Resolver) GetTileURL(maptype string, data map[string]any, staticMapType string) string {
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

// GetPregeneratedTileURL synchronously POSTs data to the tileserver to pregenerate a tile.
// Used by tile API endpoints that need a blocking result.
// For webhook enrichment, use SubmitTile instead (async via tile worker pool).
func (r *Resolver) GetPregeneratedTileURL(maptype string, data map[string]any, staticMapType string) string {
	return r.generatePregenTile(maptype, data, staticMapType)
}

// generatePregenTile does the actual HTTP POST to the tileserver.
// Called by both the synchronous GetPregeneratedTileURL and the async tile workers.
func (r *Resolver) generatePregenTile(maptype string, data map[string]any, staticMapType string) string {
	// Circuit breaker check
	r.mu.Lock()
	if r.consecutiveErrors >= r.config.TileserverFailureThreshold {
		elapsed := time.Since(r.circuitOpenSince)
		cooldown := time.Duration(r.config.TileserverCooldownMs) * time.Millisecond
		if elapsed < cooldown {
			r.mu.Unlock()
			metrics.TileTotal.WithLabelValues("circuit_break").Inc()
			log.Debugf("staticmap: circuit breaker open for %s, skipping tile", maptype)
			return ""
		}
		// Half-open: allow exactly one probe request
		if r.halfOpenProbeActive {
			r.mu.Unlock()
			metrics.TileTotal.WithLabelValues("circuit_break").Inc()
			return ""
		}
		r.halfOpenProbeActive = true
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
	r.halfOpenProbeActive = false
	r.mu.Unlock()
	metrics.TileCircuitHealthy.Set(1)

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
	r.halfOpenProbeActive = false
	if r.consecutiveErrors >= r.config.TileserverFailureThreshold {
		r.circuitOpenSince = time.Now()
		metrics.TileCircuitHealthy.Set(0)
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
