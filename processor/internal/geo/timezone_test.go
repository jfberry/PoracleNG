package geo

import (
	"testing"
)

// TestGetTimezone_KnownCoordinates is a sanity check that the tzf
// finder backing GetTimezone resolves a representative spread of
// coordinates correctly. Worth keeping across tzf version bumps and
// finder changes — GetTimezone runs NewDefaultFinder, whose
// topology-simplified polygons trade ~111 m of boundary precision for
// roughly a fifth of the resident memory, so this is the fast signal
// if a boundary ever shifts far enough to matter.
func TestGetTimezone_KnownCoordinates(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"London", 51.5074, -0.1278, "Europe/London"},
		{"New York", 40.7128, -74.0060, "America/New_York"},
		{"Tokyo", 35.6762, 139.6503, "Asia/Tokyo"},
		{"Sydney", -33.8688, 151.2093, "Australia/Sydney"},
		{"Los Angeles", 34.0522, -118.2437, "America/Los_Angeles"},
		{"Paris", 48.8566, 2.3522, "Europe/Paris"},
		{"São Paulo", -23.5505, -46.6333, "America/Sao_Paulo"},
		{"Auckland", -36.8485, 174.7633, "Pacific/Auckland"},
		// Open ocean resolves to a nautical Etc zone rather than ""
		// (tzf v1.0.x returned empty here and our UTC fallback kicked
		// in). Pokemon spawns are on land so this doesn't matter in
		// practice — kept to document the behaviour.
		{"middle of Atlantic", 0, -30, "Etc/GMT+2"},

		// Zones that sit close to a neighbour or carry an unusual offset.
		// These are the cases most exposed to boundary simplification, so
		// they are the ones worth pinning across a finder change.
		{"Phoenix", 33.4484, -112.0740, "America/Phoenix"},
		{"Indianapolis", 39.7684, -86.1581, "America/Indiana/Indianapolis"},
		{"Kathmandu", 27.7172, 85.3240, "Asia/Kathmandu"},
		{"Adelaide", -34.9285, 138.6007, "Australia/Adelaide"},
		{"Lisbon", 38.7223, -9.1393, "Europe/Lisbon"},
		{"Madrid", 40.4168, -3.7038, "Europe/Madrid"},
		{"Berlin", 52.5200, 13.4050, "Europe/Berlin"},
		{"Moscow", 55.7558, 37.6173, "Europe/Moscow"},
		{"Anchorage", 61.2181, -149.9003, "America/Anchorage"},
		{"Honolulu", 21.3069, -157.8583, "Pacific/Honolulu"},
		{"Singapore", 1.3521, 103.8198, "Asia/Singapore"},
		{"Mumbai", 19.0760, 72.8777, "Asia/Kolkata"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetTimezone(c.lat, c.lon); got != c.want {
				t.Errorf("GetTimezone(%v, %v) = %q, want %q", c.lat, c.lon, got, c.want)
			}
		})
	}
}

// TestResolveTimezone_FallbackChain pins the three-step resolution
// order used by the profile + summary schedulers:
//  1. lat/lon non-zero → tzf lookup
//  2. defaultTZ non-empty → time.LoadLocation
//  3. otherwise → time.Local
func TestResolveTimezone_FallbackChain(t *testing.T) {
	cases := []struct {
		name          string
		lat, lon      float64
		defaultTZ     string
		wantName      string
		wantSource    TimezoneSource
		matchByPrefix bool // true → wantName is a prefix (tzf names like "Europe/London" are stable across Go versions but full text comparison brittle)
	}{
		{
			name: "lat/lon resolves via tzf (London)",
			lat:  51.5074, lon: -0.1278,
			defaultTZ:  "America/Los_Angeles",
			wantName:   "Europe/London",
			wantSource: TimezoneFromLocation,
		},
		{
			name: "zero lat/lon + valid defaultTZ uses default",
			lat:  0, lon: 0,
			defaultTZ:  "America/Los_Angeles",
			wantName:   "America/Los_Angeles",
			wantSource: TimezoneFromDefault,
		},
		{
			name: "zero lat/lon + empty defaultTZ falls to server local",
			lat:  0, lon: 0,
			defaultTZ:  "",
			wantSource: TimezoneFromServerLocal,
			// name varies by host — skip the equality check.
			matchByPrefix: true,
		},
		{
			name: "zero lat/lon + malformed defaultTZ falls to server local",
			lat:  0, lon: 0,
			defaultTZ:     "Not/A/Real/Zone",
			wantSource:    TimezoneFromServerLocal,
			matchByPrefix: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loc, name, source := ResolveTimezone(c.lat, c.lon, c.defaultTZ)
			if loc == nil {
				t.Fatal("loc must be non-nil")
			}
			if source != c.wantSource {
				t.Errorf("source = %v, want %v", source, c.wantSource)
			}
			if !c.matchByPrefix && name != c.wantName {
				t.Errorf("name = %q, want %q", name, c.wantName)
			}
		})
	}
}

// TestGetTimezone_SimplifiedBoundaryOffsets documents the accepted cost of
// NewDefaultFinder's topology-simplified polygons.
//
// Within roughly 111 m of a border the simplified boundary can place a point
// in the neighbouring zone, and that neighbour does not always share the same
// UTC offset. This point sits inside the India/Nepal band, where the two zones
// differ by 15 minutes: NewFullFinder resolves it to Asia/Kathmandu, the
// finder we ship resolves it to Asia/Kolkata.
//
// Pinned so the behaviour is a documented trade rather than a surprise, and so
// a tzf data update that moves the boundary shows up as a test change.
func TestGetTimezone_SimplifiedBoundaryOffsets(t *testing.T) {
	const lat, lon = 26.5000, 88.1000

	got := GetTimezone(lat, lon)
	if got != "Asia/Kolkata" {
		t.Errorf("GetTimezone(%v, %v) = %q, want %q (simplified-boundary behaviour changed)", lat, lon, got, "Asia/Kolkata")
	}
}
