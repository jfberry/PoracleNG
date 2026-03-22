package geocoding

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Google implements Provider using the Google Geocoding API.
type Google struct {
	keys   []string
	client *http.Client
}

// NewGoogle creates a Google geocoding provider.
func NewGoogle(keys []string, timeout time.Duration) *Google {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Google{
		keys: keys,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (g *Google) randomKey() string {
	if len(g.keys) == 0 {
		return ""
	}
	return g.keys[rand.IntN(len(g.keys))]
}

// googleResponse models the Google Geocoding API response.
type googleResponse struct {
	Status  string         `json:"status"`
	Results []googleResult `json:"results"`
}

type googleResult struct {
	FormattedAddress  string             `json:"formatted_address"`
	AddressComponents []googleComponent  `json:"address_components"`
	Geometry          googleGeometry     `json:"geometry"`
}

type googleComponent struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}

type googleGeometry struct {
	Location struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
}

// Reverse performs a reverse geocode lookup via Google.
func (g *Google) Reverse(lat, lon float64) (*Address, error) {
	u := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?latlng=%f,%f&key=%s",
		lat, lon, g.randomKey())

	resp, err := g.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("google geocode: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google geocode: read body: %w", err)
	}

	var gResp googleResponse
	if err := json.Unmarshal(body, &gResp); err != nil {
		return nil, fmt.Errorf("google geocode: unmarshal: %w", err)
	}

	if gResp.Status != "OK" || len(gResp.Results) == 0 {
		return nil, fmt.Errorf("google geocode: status=%s results=%d", gResp.Status, len(gResp.Results))
	}

	r := gResp.Results[0]
	addr := &Address{
		FormattedAddress: r.FormattedAddress,
		Latitude:         r.Geometry.Location.Lat,
		Longitude:        r.Geometry.Location.Lng,
	}

	for _, c := range r.AddressComponents {
		for _, t := range c.Types {
			switch t {
			case "country":
				addr.Country = c.LongName
				addr.CountryCode = strings.ToUpper(c.ShortName)
			case "administrative_area_level_1":
				if addr.State == "" {
					addr.State = c.LongName
				}
			case "locality":
				addr.City = c.LongName
			case "sublocality", "sublocality_level_1":
				if addr.Neighbourhood == "" {
					addr.Neighbourhood = c.LongName
				}
			case "postal_code":
				addr.Zipcode = c.LongName
			case "route":
				addr.StreetName = c.LongName
			case "street_number":
				addr.StreetNumber = c.LongName
			case "neighborhood":
				addr.Suburb = c.LongName
			}
		}
	}

	return addr, nil
}

// Forward performs a forward geocode (address search) via Google.
func (g *Google) Forward(query string) ([]ForwardResult, error) {
	u := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s",
		url.QueryEscape(query), g.randomKey())

	resp, err := g.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("google geocode: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google geocode: read body: %w", err)
	}

	var gResp googleResponse
	if err := json.Unmarshal(body, &gResp); err != nil {
		return nil, fmt.Errorf("google geocode: unmarshal: %w", err)
	}

	if gResp.Status != "OK" {
		return nil, fmt.Errorf("google geocode: status=%s", gResp.Status)
	}

	out := make([]ForwardResult, 0, len(gResp.Results))
	for _, r := range gResp.Results {
		fr := ForwardResult{
			Latitude:  r.Geometry.Location.Lat,
			Longitude: r.Geometry.Location.Lng,
		}
		for _, c := range r.AddressComponents {
			for _, t := range c.Types {
				switch t {
				case "locality":
					fr.City = c.LongName
				case "country":
					fr.Country = c.LongName
				}
			}
		}
		out = append(out, fr)
	}
	return out, nil
}
