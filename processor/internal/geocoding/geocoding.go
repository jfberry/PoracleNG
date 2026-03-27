// Package geocoding provides reverse and forward geocoding with a two-layer
// cache (in-memory ttlcache + on-disk pogreb) and support for Nominatim and
// Google providers. Field names match the alerter's existing format so that
// DTS templates continue to work without changes.
package geocoding

// Address holds reverse geocode result fields matching the alerter's format.
type Address struct {
	FormattedAddress string  `json:"formattedAddress"`
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
type ForwardResult struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city,omitempty"`
	Country   string  `json:"country,omitempty"`
}

// Provider performs geocoding API calls.
type Provider interface {
	Reverse(lat, lon float64) (*Address, error)
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
