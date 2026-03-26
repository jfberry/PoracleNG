package geocoding

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Nominatim implements Provider using the Nominatim API (OpenStreetMap).
type Nominatim struct {
	baseURL string
	client  *http.Client
}

// NewNominatim creates a Nominatim provider.
func NewNominatim(baseURL string, timeout time.Duration) *Nominatim {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Nominatim{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// nominatimReverseResult models the Nominatim reverse geocoding JSON response.
type nominatimReverseResult struct {
	Lat         string                `json:"lat"`
	Lon         string                `json:"lon"`
	DisplayName string                `json:"display_name"`
	Error       string                `json:"error"`
	Address     nominatimAddress      `json:"address"`
}

type nominatimAddress struct {
	Country       string `json:"country"`
	CountryCode   string `json:"country_code"`
	State         string `json:"state"`
	City          string `json:"city"`
	Town          string `json:"town"`
	Village       string `json:"village"`
	Hamlet        string `json:"hamlet"`
	Postcode      string `json:"postcode"`
	Road          string `json:"road"`
	Quarter       string `json:"quarter"`
	Cycleway      string `json:"cycleway"`
	HouseNumber   string `json:"house_number"`
	Neighbourhood string `json:"neighbourhood"`
	Suburb        string `json:"suburb"`
	Shop          string `json:"shop"`
}

// Reverse performs a reverse geocode lookup.
func (n *Nominatim) Reverse(lat, lon float64) (*Address, error) {
	u, err := url.Parse(n.baseURL + "/reverse")
	if err != nil {
		return nil, fmt.Errorf("nominatim: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("format", "json")
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("addressdetails", "1")
	u.RawQuery = q.Encode()

	resp, err := n.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("nominatim: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("nominatim: server error %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nominatim: read body: %w", err)
	}

	var result nominatimReverseResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("nominatim: unmarshal: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("nominatim: %s", result.Error)
	}

	countryCode := strings.ToUpper(result.Address.CountryCode)

	resultLat, _ := strconv.ParseFloat(result.Lat, 64)
	resultLon, _ := strconv.ParseFloat(result.Lon, 64)

	city := firstNonEmpty(result.Address.City, result.Address.Town, result.Address.Village, result.Address.Hamlet)
	streetName := firstNonEmpty(result.Address.Road, result.Address.Quarter, result.Address.Cycleway)

	return &Address{
		Latitude:         resultLat,
		Longitude:        resultLon,
		FormattedAddress: result.DisplayName,
		Country:          result.Address.Country,
		CountryCode:      countryCode,
		State:            result.Address.State,
		City:             city,
		Zipcode:          result.Address.Postcode,
		StreetName:       streetName,
		StreetNumber:     result.Address.HouseNumber,
		Neighbourhood:    result.Address.Neighbourhood,
		Suburb:           result.Address.Suburb,
		Town:             result.Address.Town,
		Village:          result.Address.Village,
	}, nil
}

// nominatimForwardResult models a single entry from the Nominatim search response.
type nominatimForwardResult struct {
	Lat         string           `json:"lat"`
	Lon         string           `json:"lon"`
	DisplayName string           `json:"display_name"`
	Address     nominatimAddress `json:"address"`
}

// Forward performs a forward geocode (address search).
func (n *Nominatim) Forward(query string) ([]ForwardResult, error) {
	u, err := url.Parse(n.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("nominatim: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("format", "json")
	q.Set("q", query)
	q.Set("addressdetails", "1")
	u.RawQuery = q.Encode()

	resp, err := n.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("nominatim: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("nominatim: server error %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nominatim: read body: %w", err)
	}

	var results []nominatimForwardResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("nominatim: unmarshal: %w", err)
	}

	out := make([]ForwardResult, 0, len(results))
	for _, r := range results {
		lat, _ := strconv.ParseFloat(r.Lat, 64)
		lon, _ := strconv.ParseFloat(r.Lon, 64)
		city := firstNonEmpty(r.Address.City, r.Address.Town, r.Address.Village, r.Address.Hamlet)
		out = append(out, ForwardResult{
			Latitude:  lat,
			Longitude: lon,
			City:      city,
			Country:   r.Address.Country,
		})
	}
	return out, nil
}

// firstNonEmpty returns the first non-empty string argument.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
