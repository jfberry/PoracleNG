package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
)

// fakeReverseGeocoder records what it was asked for and returns a fixed address.
type fakeReverseGeocoder struct {
	addr     *geocoding.Address
	gotLat   float64
	gotLon   float64
	gotLang  string
	forwards int
}

func (f *fakeReverseGeocoder) Forward(string) ([]geocoding.ForwardResult, error) {
	f.forwards++
	return nil, nil
}

func (f *fakeReverseGeocoder) GetAddressForLanguage(lat, lon float64, language string) *geocoding.Address {
	f.gotLat, f.gotLon, f.gotLang = lat, lon, language
	return f.addr
}

func reverseAPI(t *testing.T, g ReverseGeocoder) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterGeocodeReverse(NewHumaAPI(r, r.Group("/api"), "test"), g)
	return r
}

// Reverse existed internally but only forward was exposed, so a client that
// moved to the endpoint for search still had to call the provider directly for
// reverse — the same coupling, on the other half of the feature.
func TestGeocodeReverse_ReturnsAddress(t *testing.T) {
	fg := &fakeReverseGeocoder{addr: &geocoding.Address{
		City: "Washington", Country: "United States", StreetName: "Pennsylvania Avenue",
	}}
	r := reverseAPI(t, fg)

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/reverse?lat=38.8977&lon=-77.0365", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/geocode/reverse = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var got geocoding.Address
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
	}
	if got.City != "Washington" || got.StreetName != "Pennsylvania Avenue" {
		t.Errorf("body = %+v, want the full Address", got)
	}
	if fg.gotLat != 38.8977 || fg.gotLon != -77.0365 {
		t.Errorf("geocoder called with (%v,%v), want (38.8977,-77.0365)", fg.gotLat, fg.gotLon)
	}
}

// Reverse results are per-language and the cache keys on it, so the parameter
// has to reach the geocoder.
func TestGeocodeReverse_PassesLanguage(t *testing.T) {
	fg := &fakeReverseGeocoder{addr: &geocoding.Address{City: "Köln"}}
	r := reverseAPI(t, fg)

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/reverse?lat=50.9&lon=6.9&language=de", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if fg.gotLang != "de" {
		t.Errorf("language reached the geocoder as %q, want %q", fg.gotLang, "de")
	}
}

// forward_only (and any other reason the geocoder declines) yields no address.
// That is a 404 for the coordinate, not a 500 — nothing failed.
func TestGeocodeReverse_NoAddressIs404(t *testing.T) {
	r := reverseAPI(t, &fakeReverseGeocoder{addr: nil})

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/reverse?lat=1&lon=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no address resolves, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGeocodeReverse_NilGeocoderIs503(t *testing.T) {
	r := reverseAPI(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/reverse?lat=1&lon=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with no geocoder, got %d: %s", w.Code, w.Body.String())
	}
}

// Out-of-range coordinates are a client error, not a lookup that returns
// nothing.
func TestGeocodeReverse_RejectsOutOfRangeCoordinates(t *testing.T) {
	r := reverseAPI(t, &fakeReverseGeocoder{addr: &geocoding.Address{}})

	for _, q := range []string{"lat=91&lon=0", "lat=0&lon=181", "lat=-91&lon=0"} {
		req := httptest.NewRequest(http.MethodGet, "/api/geocode/reverse?"+q, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422", q, w.Code)
		}
	}
}
