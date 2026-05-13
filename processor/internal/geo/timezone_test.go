package geo

import "testing"

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
