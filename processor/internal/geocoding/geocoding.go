// Package geocoding provides reverse and forward geocoding with a two-layer
// cache (in-memory ttlcache + on-disk pogreb) and support for Nominatim and
// Google providers. Field names match the alerter's existing format so that
// DTS templates continue to work without changes.
package geocoding

// Address holds reverse geocode result fields matching the alerter's format.
//
// FormattedAddress is populated by every provider using OpenCage country-
// specific templates (via the ocfmt package), so it gives the same country-
// idiomatic layout regardless of which provider is configured. DisplayName
// carries the provider's own pre-formatted string when it has one — today
// that's Nominatim's display_name — so users who preferred the original
// Nominatim layout can set address_format = "{{{displayName}}}".
type Address struct {
	FormattedAddress string  `json:"formattedAddress"`
	DisplayName      string  `json:"displayName"`
	Country          string  `json:"country"`
	CountryCode      string  `json:"countryCode"`
	State            string  `json:"state"`
	City             string  `json:"city"`
	Zipcode          string  `json:"zipcode"`
	StreetName       string  `json:"streetName"`
	StreetNumber     string  `json:"streetNumber"`
	Neighbourhood    string  `json:"neighbourhood"`
	County           string  `json:"county"`
	Suburb           string  `json:"suburb"`
	Town             string  `json:"town"`
	Village          string  `json:"village"`
	Addr             string  `json:"addr"` // formatted address from template
	Flag             string  `json:"flag"` // country flag emoji
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
}

// ForwardResult holds forward geocode result.
// ForwardResult is one candidate from a forward geocode (address search).
//
// Everything past latitude/longitude is omitempty and best-effort: providers
// differ in what they return, and a field a provider does not supply is
// omitted rather than sent blank, so a client can tell "unknown" from "empty".
//
// The detail matters because the caller is almost always drawing a picker.
// city + country alone collapses five hits on one street into five identical
// rows, distinguishable only by coordinates the user cannot see; every
// provider already parses the rest before narrowing it away.
type ForwardResult struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`

	// DisplayName is the provider's own one-line rendering of the result, and
	// the single most useful field for a picker row.
	DisplayName string `json:"displayName,omitempty"`
	// Name is the place's own name when it has one distinct from its address
	// ("White House"), otherwise empty.
	Name string `json:"name,omitempty"`

	StreetNumber string `json:"streetNumber,omitempty"`
	StreetName   string `json:"streetName,omitempty"`
	City         string `json:"city,omitempty"`
	State        string `json:"state,omitempty"`
	Zipcode      string `json:"zipcode,omitempty"`
	Country      string `json:"country,omitempty"`
}

// Provider performs geocoding API calls.
type Provider interface {
	Reverse(lat, lon float64, language string) (*Address, error)
	Forward(query string) ([]ForwardResult, error)
}

// Stats holds geocoder statistics for periodic logging.
type Stats struct {
	Calls         int64
	TotalMs       int64
	Errors        int64
	Hits          int64
	CircuitBreaks int64
}

// AvgMs returns the average duration in milliseconds, or 0 if no calls.
func (s Stats) AvgMs() int64 {
	if s.Calls == 0 {
		return 0
	}
	return s.TotalMs / s.Calls
}
