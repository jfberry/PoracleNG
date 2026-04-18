package geocoding

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPhotonComponentsPromotesNameByType(t *testing.T) {
	props := photonProperties{
		Type:        "city",
		Name:        "Berlin",
		CountryCode: "de",
		Country:     "Germany",
	}

	components := photonComponents(props)

	if got := components["city"]; got != "Berlin" {
		t.Fatalf("expected promoted city to be Berlin, got %q", got)
	}
	if got := components["country_code"]; got != "de" {
		t.Fatalf("expected country_code to mirror countrycode, got %q", got)
	}
}

func TestPhotonComponentsPromotesRoadFromNameType(t *testing.T) {
	props := photonProperties{
		Type: "street",
		Name: "Unter den Linden",
	}

	components := photonComponents(props)

	if got := components["street"]; got != "Unter den Linden" {
		t.Fatalf("expected street from name/type promotion, got %q", got)
	}
	if got := components["road"]; got != "Unter den Linden" {
		t.Fatalf("expected road alias from name/type promotion, got %q", got)
	}
}

func TestPhotonComponentsDoesNotPromoteHouseFromNameType(t *testing.T) {
	props := photonProperties{
		Type: "house",
		Name: "I-Punkt Skateland",
	}

	components := photonComponents(props)

	if got := components["house"]; got != "" {
		t.Fatalf("expected house promotion to be skipped, got %q", got)
	}
}

func TestPhotonStreetName(t *testing.T) {
	tests := []struct {
		name       string
		props      photonProperties
		components map[string]string
		want       string
	}{
		{
			name:       "uses street when present",
			props:      photonProperties{Street: "Main Street", Name: "POI Name", Type: "other"},
			components: map[string]string{},
			want:       "Main Street",
		},
		{
			name:       "falls back to name for type=other with no street",
			props:      photonProperties{Type: "other", Name: "I-Punkt Skateland"},
			components: map[string]string{},
			want:       "I-Punkt Skateland",
		},
		{
			name:       "does not surface name for non-other type",
			props:      photonProperties{Type: "city", Name: "Berlin"},
			components: map[string]string{},
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := photonStreetName(tt.props, tt.components)
			if got != tt.want {
				t.Fatalf("photonStreetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPhotonReverse exercises the HTTP → parse → Address path with a stub
// server returning a representative Photon GeoJSON response. Covers the
// component mapping, OpenCage templating, and county propagation in one
// shot so regressions in the Reverse pipeline surface here.
func TestPhotonReverse(t *testing.T) {
	const body = `{
        "features": [{
            "geometry": {"coordinates": [13.388860, 52.517037]},
            "properties": {
                "type": "house",
                "countrycode": "DE",
                "housenumber": "1",
                "street": "Unter den Linden",
                "city": "Berlin",
                "district": "Mitte",
                "state": "Berlin",
                "country": "Germany",
                "postcode": "10117",
                "county": "Mitte"
            }
        }]
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/reverse") {
			t.Errorf("expected /reverse, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewPhoton(srv.URL, 2*time.Second)
	addr, err := p.Reverse(52.517, 13.389)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	if addr.CountryCode != "DE" {
		t.Errorf("CountryCode = %q, want DE", addr.CountryCode)
	}
	if addr.City != "Berlin" {
		t.Errorf("City = %q, want Berlin", addr.City)
	}
	if addr.StreetName != "Unter den Linden" {
		t.Errorf("StreetName = %q, want Unter den Linden", addr.StreetName)
	}
	if addr.StreetNumber != "1" {
		t.Errorf("StreetNumber = %q, want 1", addr.StreetNumber)
	}
	if addr.County != "Mitte" {
		t.Errorf("County = %q, want Mitte", addr.County)
	}
	// OpenCage DE template yields something like "Unter den Linden 1, 10117 Berlin, Germany".
	if addr.FormattedAddress == "" {
		t.Error("FormattedAddress should not be empty")
	}
	for _, want := range []string{"Unter den Linden", "10117", "Berlin", "Germany"} {
		if !strings.Contains(addr.FormattedAddress, want) {
			t.Errorf("FormattedAddress %q missing %q", addr.FormattedAddress, want)
		}
	}
}

func TestPhotonReverseEmptyFeatures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"features":[]}`))
	}))
	defer srv.Close()

	p := NewPhoton(srv.URL, 2*time.Second)
	if _, err := p.Reverse(0, 0); err == nil {
		t.Fatal("expected error for empty features")
	}
}

func TestPhotonReverseServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewPhoton(srv.URL, 2*time.Second)
	if _, err := p.Reverse(0, 0); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPhotonForward(t *testing.T) {
	const body = `{
        "features": [
            {"geometry": {"coordinates": [-0.127758, 51.507351]},
             "properties": {"name": "London", "country": "United Kingdom", "countrycode": "GB"}},
            {"geometry": {"coordinates": [-0.13, 51.51]},
             "properties": {"name": "Westminster", "city": "London", "country": "United Kingdom", "countrycode": "GB"}}
        ]
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api") {
			t.Errorf("expected /api, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "london" {
			t.Errorf("query %q, want london", r.URL.Query().Get("q"))
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewPhoton(srv.URL, 2*time.Second)
	results, err := p.Forward("london")
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Latitude != 51.507351 || results[0].Longitude != -0.127758 {
		t.Errorf("coords swapped: %v, %v", results[0].Latitude, results[0].Longitude)
	}
}

func TestPhotonTypeAliasMapping(t *testing.T) {
	cases := map[string]string{
		"housenumber": "house_number",
		"street":      "road",
		"district":    "city_district",
		"countrycode": "country_code",
		"city":        "city",
	}

	for in, want := range cases {
		if got := photonToOCKey(in); got != want {
			t.Fatalf("photonToOCKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterNonEmpty(t *testing.T) {
	got := filterNonEmpty("", "  a  ", " ", "b")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterNonEmpty() = %#v, want %#v", got, want)
	}
}
