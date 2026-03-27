package staticmap

import (
	"math"
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestLimits(t *testing.T) {
	// Test the Web Mercator bounds calculation at a known location
	// Canterbury, UK: 51.28, 1.08
	bounds := limits(51.28, 1.08, 500, 250, 15)

	// Should produce a reasonable bounding box around the center
	minLat, minLon, maxLat, maxLon := bounds[0], bounds[1], bounds[2], bounds[3]

	// The top-left should be north-west of center (higher lat, lower lon)
	// The bottom-right should be south-east (lower lat, higher lon)
	// In Web Mercator, y increases downward, so [0,0] is NW, [w,h] is SE
	if minLat < maxLat {
		t.Errorf("expected minLat(%f) >= maxLat(%f) (NW corner has higher lat)", minLat, maxLat)
	}
	if minLon > maxLon {
		t.Errorf("expected minLon(%f) <= maxLon(%f)", minLon, maxLon)
	}

	// Bounds should be close to center (within ~0.02 degrees at zoom 15)
	if math.Abs(minLat-51.28) > 0.02 {
		t.Errorf("NW lat %f too far from center 51.28", minLat)
	}
	if math.Abs(maxLat-51.28) > 0.02 {
		t.Errorf("SE lat %f too far from center 51.28", maxLat)
	}

	// Symmetry: center should be roughly in the middle
	midLat := (minLat + maxLat) / 2
	midLon := (minLon + maxLon) / 2
	if math.Abs(midLat-51.28) > 0.001 {
		t.Errorf("mid lat %f not close to center 51.28", midLat)
	}
	if math.Abs(midLon-1.08) > 0.001 {
		t.Errorf("mid lon %f not close to center 1.08", midLon)
	}
}

func TestLimitsZoom(t *testing.T) {
	// Higher zoom = smaller area
	boundsLow := limits(51.28, 1.08, 500, 250, 12)
	boundsHigh := limits(51.28, 1.08, 500, 250, 18)

	spanLow := boundsLow[0] - boundsLow[2] // lat span
	spanHigh := boundsHigh[0] - boundsHigh[2]

	if spanHigh >= spanLow {
		t.Errorf("higher zoom should produce smaller span: zoom12=%f zoom18=%f", spanLow, spanHigh)
	}
}

func TestGoogleURL(t *testing.T) {
	r := New(Config{
		Provider:   "google",
		StaticKeys: []string{"testkey123"},
		Width:      320,
		Height:     200,
		Zoom:       15,
		MapType:    "roadmap",
	})

	data := map[string]any{"latitude": 51.28, "longitude": 1.08}
	url := r.GetStaticMapURL("monster", data, nil, nil)

	if !strings.Contains(url, "maps.googleapis.com") {
		t.Errorf("expected google URL, got %q", url)
	}
	if !strings.Contains(url, "key=testkey123") {
		t.Errorf("expected API key in URL, got %q", url)
	}
	if !strings.Contains(url, "320x200") {
		t.Errorf("expected size in URL, got %q", url)
	}
}

func TestOsmURL(t *testing.T) {
	r := New(Config{
		Provider:   "osm",
		StaticKeys: []string{"osmkey"},
		Width:      400,
		Height:     300,
		Zoom:       14,
	})

	data := map[string]any{"latitude": 51.28, "longitude": 1.08}
	url := r.GetStaticMapURL("raid", data, nil, nil)

	if !strings.Contains(url, "mapquestapi.com") {
		t.Errorf("expected mapquest URL, got %q", url)
	}
	if !strings.Contains(url, "key=osmkey") {
		t.Errorf("expected API key in URL, got %q", url)
	}
}

func TestMapboxURL(t *testing.T) {
	r := New(Config{
		Provider:   "mapbox",
		StaticKeys: []string{"mbtoken"},
		Width:      640,
		Height:     480,
		Zoom:       16,
	})

	data := map[string]any{"latitude": 51.28, "longitude": 1.08}
	url := r.GetStaticMapURL("gym", data, nil, nil)

	if !strings.Contains(url, "api.mapbox.com") {
		t.Errorf("expected mapbox URL, got %q", url)
	}
	if !strings.Contains(url, "access_token=mbtoken") {
		t.Errorf("expected access token, got %q", url)
	}
}

func TestNoneProvider(t *testing.T) {
	r := New(Config{Provider: "none"})

	data := map[string]any{"latitude": 51.28, "longitude": 1.08}
	url := r.GetStaticMapURL("monster", data, nil, nil)

	if url != "" {
		t.Errorf("expected empty URL for none provider, got %q", url)
	}
}

func TestFallbackURL(t *testing.T) {
	r := New(Config{
		Provider:    "none",
		FallbackURL: "https://example.com/fallback.png",
	})

	data := map[string]any{"latitude": 51.28, "longitude": 1.08}
	url := r.GetStaticMapURL("monster", data, nil, nil)

	if url != "https://example.com/fallback.png" {
		t.Errorf("expected fallback URL, got %q", url)
	}
}

func TestGetConfigForTileType(t *testing.T) {
	r := New(Config{
		Provider: "tileservercache",
		TileserverSettings: map[string]TileTypeConfig{
			"default": {Type: "staticMap", Width: 600, Height: 300, Zoom: 14, Pregenerate: boolPtr(true)},
			"raid":    {Width: 800, Height: 400},
		},
	})

	// Default config
	opts := r.getConfigForTileType("monster")
	if opts.Width != 600 {
		t.Errorf("default width = %d, want 600", opts.Width)
	}
	if opts.Zoom != 14 {
		t.Errorf("default zoom = %d, want 14", opts.Zoom)
	}

	// Raid override
	opts = r.getConfigForTileType("raid")
	if opts.Width != 800 {
		t.Errorf("raid width = %d, want 800", opts.Width)
	}
	if opts.Height != 400 {
		t.Errorf("raid height = %d, want 400", opts.Height)
	}
	if opts.Zoom != 14 {
		t.Errorf("raid zoom = %d, want 14 (inherited from default)", opts.Zoom)
	}
}

func TestGetConfigForTileTypeStaticMapType(t *testing.T) {
	r := New(Config{
		Provider: "tileservercache",
		StaticMapType: map[string]string{
			"pokemon": "myCustomTemplate",
			"raid":    "*nonPregenTemplate",
		},
	})

	// Pokemon: custom template, pregenerate=true
	opts := r.getConfigForTileType("monster") // monster maps to pokemon config
	if opts.Type != "myCustomTemplate" {
		t.Errorf("type = %q, want myCustomTemplate", opts.Type)
	}
	if !boolVal(opts.Pregenerate) {
		t.Error("expected pregenerate=true")
	}

	// Raid: * prefix = non-pregenerate
	opts = r.getConfigForTileType("raid")
	if opts.Type != "nonPregenTemplate" {
		t.Errorf("type = %q, want nonPregenTemplate", opts.Type)
	}
	if boolVal(opts.Pregenerate) {
		t.Error("expected pregenerate=false (had * prefix)")
	}
}

func TestGetTileURL(t *testing.T) {
	r := New(Config{
		Provider:    "tileservercache",
		ProviderURL: "https://tiles.example.com",
	})

	data := map[string]any{
		"latitude":   51.28,
		"longitude":  1.08,
		"pokemon_id": 25,
	}

	url := r.GetTileURL("monster", data, "staticMap")
	if !strings.Contains(url, "tiles.example.com/staticmap/poracle-monster") {
		t.Errorf("expected tileserver URL, got %q", url)
	}
	if !strings.Contains(url, "pokemon_id=25") {
		t.Errorf("expected query params, got %q", url)
	}
}

func TestGetTileURLMulti(t *testing.T) {
	r := New(Config{
		Provider:    "tileservercache",
		ProviderURL: "https://tiles.example.com",
	})

	url := r.GetTileURL("monster", map[string]any{}, "multiStaticMap")
	if !strings.Contains(url, "multistaticmap/poracle-multi-monster") {
		t.Errorf("expected multi URL, got %q", url)
	}
}

func TestCircuitBreaker(t *testing.T) {
	r := New(Config{
		Provider:                   "tileservercache",
		ProviderURL:                "https://tiles.example.com",
		TileserverFailureThreshold: 3,
		TileserverCooldownMs:       100, // short for testing
	})

	// Record errors to open circuit
	r.recordError()
	r.recordError()
	r.recordError()

	r.mu.Lock()
	if r.consecutiveErrors < 3 {
		t.Errorf("expected >= 3 consecutive errors, got %d", r.consecutiveErrors)
	}
	if r.circuitOpenSince.IsZero() {
		t.Error("expected circuit to be open")
	}
	r.mu.Unlock()

	// Pregenerate should return empty during cooldown
	result := r.GetPregeneratedTileURL("monster", map[string]any{}, "staticMap")
	if result != "" {
		t.Errorf("expected empty during circuit break, got %q", result)
	}
}

func TestRandomKey(t *testing.T) {
	r := New(Config{StaticKeys: []string{"a", "b", "c"}})
	key := r.randomKey()
	if key != "a" && key != "b" && key != "c" {
		t.Errorf("unexpected key %q", key)
	}

	r2 := New(Config{})
	if r2.randomKey() != "" {
		t.Error("expected empty key with no keys configured")
	}
}

func TestFilterFields(t *testing.T) {
	data := map[string]any{
		"pokemon_id": 25,
		"latitude":   51.28,
		"longitude":  1.08,
		"form":       0,
		"costume":    0,
		"imgUrl":     "https://example.com/pokemon/25.png",
		"extra_field": "should not appear",
		"another":     42,
	}

	allowed := []string{"pokemon_id", "latitude", "longitude", "form", "imgUrl"}
	filtered := filterFields(data, allowed)

	if len(filtered) != 5 {
		t.Errorf("expected 5 fields, got %d", len(filtered))
	}
	if _, ok := filtered["extra_field"]; ok {
		t.Error("extra_field should not be in filtered output")
	}
	if filtered["pokemon_id"] != 25 {
		t.Errorf("pokemon_id = %v, want 25", filtered["pokemon_id"])
	}
}

func TestTileserverFieldFiltering(t *testing.T) {
	r := New(Config{
		Provider:    "tileservercache",
		ProviderURL: "https://tiles.example.com",
	})

	data := map[string]any{
		"latitude":   51.28,
		"longitude":  1.08,
		"pokemon_id": 25,
		"secret":     "should-not-appear",
	}

	keys := []string{"pokemon_id", "latitude", "longitude"}
	url := r.GetTileURL("monster", filterFields(data, keys), "staticMap")

	if strings.Contains(url, "secret") {
		t.Errorf("filtered URL should not contain 'secret': %q", url)
	}
	if !strings.Contains(url, "pokemon_id=25") {
		t.Errorf("filtered URL should contain pokemon_id: %q", url)
	}
}

func TestAutopositionSingleMarker(t *testing.T) {
	result := Autoposition(AutopositionShape{
		Markers: []LatLon{{Latitude: 51.28, Longitude: 1.08}},
	}, 500, 250, 1.25, 17.5)

	if result == nil {
		t.Fatal("expected non-nil result for single marker")
	}
	if result.Zoom != 17.5 {
		t.Errorf("single marker zoom = %f, want default 17.5", result.Zoom)
	}
	if result.Latitude != 51.28 {
		t.Errorf("latitude = %f, want 51.28", result.Latitude)
	}
	if result.Longitude != 1.08 {
		t.Errorf("longitude = %f, want 1.08", result.Longitude)
	}
}

func TestAutopositionTwoMarkers(t *testing.T) {
	result := Autoposition(AutopositionShape{
		Markers: []LatLon{
			{Latitude: 51.28, Longitude: 1.08},
			{Latitude: 51.29, Longitude: 1.09},
		},
	}, 500, 250, 1.25, 17.5)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Center should be midpoint
	if math.Abs(result.Latitude-51.285) > 0.001 {
		t.Errorf("center lat = %f, want ~51.285", result.Latitude)
	}
	if math.Abs(result.Longitude-1.085) > 0.001 {
		t.Errorf("center lon = %f, want ~1.085", result.Longitude)
	}
	// Zoom should be less than default (need to fit two points)
	if result.Zoom >= 17.5 {
		t.Errorf("zoom = %f, should be < 17.5 for spread markers", result.Zoom)
	}
	if result.Zoom <= 0 {
		t.Errorf("zoom = %f, should be positive", result.Zoom)
	}
}

func TestAutopositionPolygon(t *testing.T) {
	result := Autoposition(AutopositionShape{
		Polygons: [][]LatLon{
			{
				{Latitude: 51.27, Longitude: 1.07},
				{Latitude: 51.27, Longitude: 1.09},
				{Latitude: 51.29, Longitude: 1.09},
				{Latitude: 51.29, Longitude: 1.07},
			},
		},
	}, 500, 250, 1.25, 17.5)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if math.Abs(result.Latitude-51.28) > 0.001 {
		t.Errorf("center lat = %f, want ~51.28", result.Latitude)
	}
	if result.Zoom >= 17.5 {
		t.Errorf("zoom = %f, should be < 17.5 for polygon", result.Zoom)
	}
}

func TestAutopositionCircle(t *testing.T) {
	result := Autoposition(AutopositionShape{
		Circles: []Circle{
			{Latitude: 51.28, Longitude: 1.08, RadiusM: 500},
		},
	}, 500, 250, 1.25, 17.5)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if math.Abs(result.Latitude-51.28) > 0.01 {
		t.Errorf("center lat = %f, want ~51.28", result.Latitude)
	}
	// 500m radius needs lower zoom than default
	if result.Zoom >= 17.5 {
		t.Errorf("zoom = %f, should be < 17.5 for 500m circle", result.Zoom)
	}
}

func TestAutopositionEmpty(t *testing.T) {
	result := Autoposition(AutopositionShape{}, 500, 250, 1.25, 17.5)
	if result != nil {
		t.Error("expected nil for empty shapes")
	}
}

func TestAutopositionWiderSpreadLowerZoom(t *testing.T) {
	close := Autoposition(AutopositionShape{
		Markers: []LatLon{
			{Latitude: 51.28, Longitude: 1.08},
			{Latitude: 51.281, Longitude: 1.081},
		},
	}, 500, 250, 1.25, 17.5)

	far := Autoposition(AutopositionShape{
		Markers: []LatLon{
			{Latitude: 51.0, Longitude: 1.0},
			{Latitude: 51.5, Longitude: 1.5},
		},
	}, 500, 250, 1.25, 17.5)

	if close == nil || far == nil {
		t.Fatal("expected non-nil results")
	}
	if far.Zoom >= close.Zoom {
		t.Errorf("wider spread (zoom=%f) should have lower zoom than close (zoom=%f)", far.Zoom, close.Zoom)
	}
}

func TestAdjustLatitude(t *testing.T) {
	lat := adjustLatitude(51.28, 1000) // 1km north
	if lat <= 51.28 {
		t.Errorf("1km north should increase latitude: got %f", lat)
	}
	if math.Abs(lat-51.28) > 0.02 { // ~1km is ~0.009 degrees
		t.Errorf("1km adjustment too large: %f", lat-51.28)
	}

	lat = adjustLatitude(51.28, -1000) // 1km south
	if lat >= 51.28 {
		t.Errorf("1km south should decrease latitude: got %f", lat)
	}
}

func TestAdjustLongitude(t *testing.T) {
	lon := adjustLongitude(51.28, 1.08, 1000) // 1km east
	if lon <= 1.08 {
		t.Errorf("1km east should increase longitude: got %f", lon)
	}

	lon = adjustLongitude(51.28, 1.08, -1000) // 1km west
	if lon >= 1.08 {
		t.Errorf("1km west should decrease longitude: got %f", lon)
	}
}
