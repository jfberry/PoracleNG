package geo

import (
	"testing"
)

// TestGetTimezone_KnownCoordinates is a sanity check that the tzf
// finder backing GetTimezone resolves a representative spread of
// coordinates correctly. Worth keeping after tzf version bumps —
// e.g. the v1.0 → v1.2 jump switches NewDefaultFinder for
// NewFullFinder (FuzzyFinder + accurate Finder fallback) and we
// want a fast signal if a boundary shifts.
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
		// Open ocean now resolves to a nautical etc. zone with
		// NewFullFinder; NewDefaultFinder (v1.0.x) returned ""
		// and our UTC fallback kicked in. Pokemon spawns are on
		// land so this doesn't matter in practice — kept here to
		// document the version-bump behaviour change.
		{"middle of Atlantic", 0, -30, "Etc/GMT+2"},
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
//   1. lat/lon non-zero → tzf lookup
//   2. defaultTZ non-empty → time.LoadLocation
//   3. otherwise → time.Local
func TestResolveTimezone_FallbackChain(t *testing.T) {
	cases := []struct {
		name         string
		lat, lon     float64
		defaultTZ    string
		wantName     string
		wantSource   TimezoneSource
		matchByPrefix bool // true → wantName is a prefix (tzf names like "Europe/London" are stable across Go versions but full text comparison brittle)
	}{
		{
			name:       "lat/lon resolves via tzf (London)",
			lat:        51.5074, lon: -0.1278,
			defaultTZ:  "America/Los_Angeles",
			wantName:   "Europe/London",
			wantSource: TimezoneFromLocation,
		},
		{
			name:       "zero lat/lon + valid defaultTZ uses default",
			lat:        0, lon: 0,
			defaultTZ:  "America/Los_Angeles",
			wantName:   "America/Los_Angeles",
			wantSource: TimezoneFromDefault,
		},
		{
			name:       "zero lat/lon + empty defaultTZ falls to server local",
			lat:        0, lon: 0,
			defaultTZ:  "",
			wantSource: TimezoneFromServerLocal,
			// name varies by host — skip the equality check.
			matchByPrefix: true,
		},
		{
			name:       "zero lat/lon + malformed defaultTZ falls to server local",
			lat:        0, lon: 0,
			defaultTZ:  "Not/A/Real/Zone",
			wantSource: TimezoneFromServerLocal,
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
