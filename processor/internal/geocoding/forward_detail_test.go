package geocoding

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every provider parses a full address for a forward result and then keeps
// only city and country. A location picker renders one row per candidate, so
// five hits on the same street collapse into five identical "Washington,
// United States" rows, distinguishable only by coordinates the user cannot
// see. The detail is already in hand at the point ForwardResult is built.
func TestNominatimForwardKeepsAddressDetail(t *testing.T) {
	const body = `[{"lat":"38.8977","lon":"-77.0365",
		"display_name":"White House, 1600, Pennsylvania Avenue Northwest, Washington, 20500, United States",
		"address":{"house_number":"1600","road":"Pennsylvania Avenue Northwest","city":"Washington",
		           "state":"District of Columbia","postcode":"20500","country":"United States"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	results, err := NewNominatim(srv.URL, 2*time.Second, true).Forward("1600 Pennsylvania Ave")
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	for _, c := range []struct{ field, got, want string }{
		{"DisplayName", r.DisplayName, "White House, 1600, Pennsylvania Avenue Northwest, Washington, 20500, United States"},
		{"StreetNumber", r.StreetNumber, "1600"},
		{"StreetName", r.StreetName, "Pennsylvania Avenue Northwest"},
		{"Zipcode", r.Zipcode, "20500"},
		{"State", r.State, "District of Columbia"},
		{"City", r.City, "Washington"},
		{"Country", r.Country, "United States"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestPhotonForwardKeepsAddressDetail(t *testing.T) {
	const body = `{"features":[{"geometry":{"coordinates":[-77.0365,38.8977]},
		"properties":{"name":"White House","housenumber":"1600","street":"Pennsylvania Avenue Northwest",
		              "city":"Washington","state":"District of Columbia","postcode":"20500","country":"United States"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	results, err := NewPhoton(srv.URL, 2*time.Second, true).Forward("1600 Pennsylvania Ave")
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	for _, c := range []struct{ field, got, want string }{
		{"StreetNumber", r.StreetNumber, "1600"},
		{"StreetName", r.StreetName, "Pennsylvania Avenue Northwest"},
		{"Zipcode", r.Zipcode, "20500"},
		{"State", r.State, "District of Columbia"},
		{"Name", r.Name, "White House"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

// Fields a provider does not supply must be omitted, not empty strings in the
// JSON — that is what lets a client tell "unknown" from "blank".
func TestForwardResultOmitsAbsentDetail(t *testing.T) {
	const body = `[{"lat":"51.5","lon":"-0.1","display_name":"London","address":{"city":"London","country":"UK"}}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	results, _ := NewNominatim(srv.URL, 2*time.Second, true).Forward("London")
	if results[0].StreetName != "" || results[0].Zipcode != "" {
		t.Errorf("absent fields should stay empty, got %+v", results[0])
	}
	if results[0].DisplayName != "London" {
		t.Errorf("DisplayName = %q, want London", results[0].DisplayName)
	}
}

func TestGoogleForwardKeepsAddressDetail(t *testing.T) {
	// Google's Forward hardcodes the API host, so the mapping is tested
	// directly rather than adding a base-URL seam to production for a test.
	r := googleResult{
		FormattedAddress: "1600 Pennsylvania Avenue NW, Washington, DC 20500, USA",
		AddressComponents: []googleComponent{
			{LongName: "1600", Types: []string{"street_number"}},
			{LongName: "Pennsylvania Avenue Northwest", Types: []string{"route"}},
			{LongName: "Washington", Types: []string{"locality"}},
			{LongName: "District of Columbia", Types: []string{"administrative_area_level_1"}},
			{LongName: "20500", Types: []string{"postal_code"}},
			{LongName: "United States", Types: []string{"country"}},
		},
	}
	r.Geometry.Location.Lat = 38.8977
	r.Geometry.Location.Lng = -77.0365

	got := googleForwardResult(r)
	for _, c := range []struct{ field, got, want string }{
		{"DisplayName", got.DisplayName, "1600 Pennsylvania Avenue NW, Washington, DC 20500, USA"},
		{"StreetNumber", got.StreetNumber, "1600"},
		{"StreetName", got.StreetName, "Pennsylvania Avenue Northwest"},
		{"State", got.State, "District of Columbia"},
		{"Zipcode", got.Zipcode, "20500"},
		{"City", got.City, "Washington"},
		{"Country", got.Country, "United States"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if got.Latitude != 38.8977 {
		t.Errorf("Latitude = %v, want 38.8977", got.Latitude)
	}
}

// displayName is what a picker actually renders — row label, the value a
// search box takes on selection, the address line. Nominatim copies
// display_name and Google copies formatted_address; Photon has no single
// formatted string, so it was the one provider leaving the field empty. That
// is the provider most likely to be configured by whoever adopts this, since
// Photon is what prompted the request.
func TestPhotonForwardComposesDisplayName(t *testing.T) {
	const body = `{"features":[{"geometry":{"coordinates":[-77.0365,38.8977]},
		"properties":{"name":"White House","housenumber":"1600","street":"Pennsylvania Avenue Northwest",
		              "city":"Washington","state":"District of Columbia","postcode":"20500",
		              "countrycode":"US","country":"United States"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	results, err := NewPhoton(srv.URL, 2*time.Second, true).Forward("1600 Pennsylvania Ave")
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	got := results[0].DisplayName
	if got == "" {
		t.Fatal("DisplayName is empty; Photon must compose one like it already does for reverse")
	}
	// Composed, not a bare field copy: it has to carry enough to tell two
	// hits on the same street apart.
	for _, want := range []string{"1600", "Pennsylvania Avenue Northwest", "Washington"} {
		if !strings.Contains(got, want) {
			t.Errorf("DisplayName = %q, want it to contain %q", got, want)
		}
	}
}

// formatOpenCage mutates the components map — it deletes "country" when the
// provider is configured with includeCountry=false. Composing the display name
// before reading Country would therefore blank the Country field.
func TestPhotonForwardKeepsCountryWhenNotIncludedInDisplayName(t *testing.T) {
	const body = `{"features":[{"geometry":{"coordinates":[-77.0365,38.8977]},
		"properties":{"street":"Pennsylvania Avenue Northwest","city":"Washington",
		              "countrycode":"US","country":"United States"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	// includeCountry=false is the case that deletes the key.
	results, err := NewPhoton(srv.URL, 2*time.Second, false).Forward("x")
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if results[0].Country != "United States" {
		t.Errorf("Country = %q, want it preserved even when omitted from displayName", results[0].Country)
	}
	if results[0].DisplayName == "" {
		t.Error("DisplayName should still be composed with includeCountry=false")
	}
}
