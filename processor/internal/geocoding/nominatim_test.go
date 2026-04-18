package geocoding

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNominatimReverseOCFMT walks a representative Nominatim reverse response
// through the whole provider path and verifies the OpenCage-formatted string
// lands in FormattedAddress, the raw display_name survives as DisplayName, and
// the new County field propagates.
func TestNominatimReverseOCFMT(t *testing.T) {
	const body = `{
        "lat": "52.517037",
        "lon": "13.388860",
        "display_name": "Brandenburger Tor, Platz des 18. März, Tiergarten, Mitte, Berlin, 10117, Germany",
        "address": {
            "country": "Germany",
            "country_code": "de",
            "state": "Berlin",
            "county": "Mitte",
            "city": "Berlin",
            "suburb": "Tiergarten",
            "neighbourhood": "Mitte",
            "postcode": "10117",
            "road": "Unter den Linden",
            "house_number": "1"
        }
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/reverse") {
			t.Errorf("expected /reverse, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	n := NewNominatim(srv.URL, 2*time.Second)
	addr, err := n.Reverse(52.517, 13.389)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	if addr.CountryCode != "DE" {
		t.Errorf("CountryCode = %q, want DE", addr.CountryCode)
	}
	if addr.County != "Mitte" {
		t.Errorf("County = %q, want Mitte", addr.County)
	}
	if addr.DisplayName == "" || !strings.Contains(addr.DisplayName, "Brandenburger Tor") {
		t.Errorf("DisplayName should carry the raw Nominatim string, got %q", addr.DisplayName)
	}
	// OpenCage-formatted output doesn't mention "Brandenburger Tor"
	// (Nominatim includes POI names in display_name; ocfmt doesn't).
	if strings.Contains(addr.FormattedAddress, "Brandenburger Tor") {
		t.Errorf("FormattedAddress should be OpenCage output, not display_name verbatim: %q", addr.FormattedAddress)
	}
	for _, want := range []string{"Unter den Linden", "10117", "Berlin", "Germany"} {
		if !strings.Contains(addr.FormattedAddress, want) {
			t.Errorf("FormattedAddress missing %q: %q", want, addr.FormattedAddress)
		}
	}
}

// TestNominatimFallsBackToDisplayName — if ocfmt can't produce anything from
// the sparse components, we still surface a sensible FormattedAddress from
// Nominatim's own display_name.
func TestNominatimFallsBackToDisplayName(t *testing.T) {
	const body = `{
        "lat": "0",
        "lon": "0",
        "display_name": "Some Point, Nowhere",
        "address": {}
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	n := NewNominatim(srv.URL, 2*time.Second)
	addr, err := n.Reverse(0, 0)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if addr.FormattedAddress == "" {
		t.Errorf("FormattedAddress should fall back to display_name when components are empty, got empty")
	}
}
