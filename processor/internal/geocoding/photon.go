package geocoding

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/geocoding/ocfmt"
)

// Photon implements Provider using the Photon geocoder API (komoot/photon).
// Photon returns GeoJSON responses based on OpenStreetMap data.
type Photon struct {
	baseURL        string
	client         *http.Client
	formatter      *ocfmt.Formatter
	includeCountry bool // render country into FormattedAddress via ocfmt
}

// NewPhoton creates a Photon provider. includeCountry controls whether the
// OpenCage-formatted FormattedAddress trails with the country name.
// FormattedAddress is always populated via OpenCage country-specific
// templates (ocfmt). Callers who want a different shape can drive it via
// the existing address_format template with the per-field helpers
// ({{{streetName}}}, {{{suburb}}}, {{{city}}}, etc.).
func NewPhoton(baseURL string, timeout time.Duration, includeCountry bool) *Photon {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Photon{
		baseURL:        strings.TrimRight(baseURL, "/"),
		client:         &http.Client{Timeout: timeout},
		formatter:      ocfmt.Global(),
		includeCountry: includeCountry,
	}
}

// photonResponse models the GeoJSON response from Photon.
type photonResponse struct {
	Features []photonFeature `json:"features"`
}

type photonFeature struct {
	Geometry   photonGeometry   `json:"geometry"`
	Properties photonProperties `json:"properties"`
}

type photonGeometry struct {
	Coordinates []float64 `json:"coordinates"` // [lon, lat]
}

// photonProperties models the documented Photon API properties.
// https://github.com/komoot/photon/blob/master/docs/api-v1.md
type photonProperties struct {
	Type        string `json:"type"`
	CountryCode string `json:"countrycode"`
	Name        string `json:"name"`
	HouseNumber string `json:"housenumber"`
	Street      string `json:"street"`
	District    string `json:"district"`
	City        string `json:"city"`
	County      string `json:"county"`
	State       string `json:"state"`
	Country     string `json:"country"`
	Postcode    string `json:"postcode"`
}

// Reverse performs a reverse geocode lookup using Photon.
func (p *Photon) Reverse(lat, lon float64) (*Address, error) {
	u, err := url.Parse(p.baseURL + "/reverse")
	if err != nil {
		return nil, fmt.Errorf("photon: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("lat", fmt.Sprintf("%f", lat))
	q.Set("lon", fmt.Sprintf("%f", lon))
	q.Set("radius", "10")
	u.RawQuery = q.Encode()

	resp, err := p.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("photon: request failed: %w", err)
	}
	defer func() {
		// Drain remaining body before close so HTTP/1.1 keep-alive can
		// reuse the connection. Matches the pattern in nominatim/google.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("photon: server error %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photon: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("photon: read body: %w", err)
	}

	var result photonResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("photon: unmarshal: %w", err)
	}
	if len(result.Features) == 0 {
		return nil, fmt.Errorf("photon: no results")
	}

	feature := result.Features[0]
	props := feature.Properties

	// Extract coordinates from GeoJSON geometry (lon, lat order)
	if len(feature.Geometry.Coordinates) < 2 {
		return nil, fmt.Errorf("photon: feature missing geometry coordinates")
	}
	resultLon := feature.Geometry.Coordinates[0]
	resultLat := feature.Geometry.Coordinates[1]

	components := photonComponents(props)

	city := firstNonEmpty(strings.TrimSpace(props.City), components["city"])
	countryCode := strings.ToUpper(strings.TrimSpace(props.CountryCode))
	suburb := firstNonEmpty(components["suburb"], strings.TrimSpace(props.District))

	streetName := photonStreetName(props, components)

	addr := &Address{
		Latitude:      resultLat,
		Longitude:     resultLon,
		Country:       firstNonEmpty(strings.TrimSpace(props.Country), components["country"]),
		CountryCode:   countryCode,
		State:         firstNonEmpty(strings.TrimSpace(props.State), components["state"]),
		City:          city,
		Zipcode:       firstNonEmpty(strings.TrimSpace(props.Postcode), components["postcode"]),
		StreetName:    streetName,
		StreetNumber:  firstNonEmpty(strings.TrimSpace(props.HouseNumber), components["housenumber"], components["house_number"]),
		Neighbourhood: strings.TrimSpace(props.District),
		County:        firstNonEmpty(strings.TrimSpace(props.County), components["county"]),
		Suburb:        suburb,
	}

	// Build FormattedAddress via OpenCage country-specific templates.
	addr.FormattedAddress = p.formatOpenCage(components, countryCode)

	return addr, nil
}

// formatOpenCage builds a FormattedAddress using OpenCage country templates.
func (p *Photon) formatOpenCage(components map[string]string, countryCode string) string {
	// Ensure country_code is set (uppercase)
	components["country_code"] = countryCode

	if !p.includeCountry {
		delete(components, "country")
	}

	result := p.formatter.Format(components)
	if result == "" {
		// Fallback: build a simple comma-separated string
		parts := filterNonEmpty(
			strings.TrimSpace(components["road"]+" "+components["house_number"]),
			components["postcode"]+" "+components["city"],
			components["country"],
		)
		return strings.Join(parts, ", ")
	}
	return result
}

func photonComponents(props photonProperties) map[string]string {
	components := map[string]string{
		"type":        strings.TrimSpace(props.Type),
		"countrycode": strings.TrimSpace(props.CountryCode),
		"name":        strings.TrimSpace(props.Name),
		"housenumber": strings.TrimSpace(props.HouseNumber),
		"street":      strings.TrimSpace(props.Street),
		"district":    strings.TrimSpace(props.District),
		"city":        strings.TrimSpace(props.City),
		"county":      strings.TrimSpace(props.County),
		"state":       strings.TrimSpace(props.State),
		"country":     strings.TrimSpace(props.Country),
		"postcode":    strings.TrimSpace(props.Postcode),
	}

	for k, v := range components {
		components[photonToOCKey(k)] = v
	}

	if propsType, name := components["type"], components["name"]; propsType != "" && name != "" && propsType != "house" {
		components[propsType] = name
		components[photonToOCKey(propsType)] = name
	}

	return components
}

func photonStreetName(props photonProperties, components map[string]string) string {
	streetName := firstNonEmpty(strings.TrimSpace(props.Street), components["street"], components["road"])
	if streetName != "" {
		return streetName
	}

	// Photon returns "type":"other" for POIs and named features that aren't
	// on a street. Surface Name as a sensible street-line fallback so
	// templates like {{{streetName}}} don't render empty.
	if strings.EqualFold(strings.TrimSpace(props.Type), "other") {
		return strings.TrimSpace(props.Name)
	}

	return streetName
}

// photonToOCKey maps Photon property names to OpenCage component names.
func photonToOCKey(key string) string {
	switch key {
	case "housenumber":
		return "house_number"
	case "street":
		return "road"
	case "postcode":
		return "postcode"
	case "district":
		return "city_district"
	case "countrycode":
		return "country_code"
	default:
		return key
	}
}

// filterNonEmpty returns non-empty, non-whitespace strings from the arguments.
func filterNonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Forward performs a forward geocode (address search) using Photon.
func (p *Photon) Forward(query string) ([]ForwardResult, error) {
	u, err := url.Parse(p.baseURL + "/api")
	if err != nil {
		return nil, fmt.Errorf("photon: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", "5")
	u.RawQuery = q.Encode()

	resp, err := p.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("photon: request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("photon: server error %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photon: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("photon: read body: %w", err)
	}

	var result photonResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("photon: unmarshal: %w", err)
	}

	out := make([]ForwardResult, 0, len(result.Features))
	for _, f := range result.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		components := photonComponents(f.Properties)
		city := components["city"]
		out = append(out, ForwardResult{
			Latitude:  f.Geometry.Coordinates[1],
			Longitude: f.Geometry.Coordinates[0],
			City:      city,
			Country:   components["country"],
		})
	}
	return out, nil
}
