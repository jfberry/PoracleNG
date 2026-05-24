package enrichment

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestLureTranslateAddsLocalizedGeocoding(t *testing.T) {
	const body = `{
		"lat": "52.517037",
		"lon": "13.388860",
		"display_name": "Lokalisierte Strasse 1, Berlin DE, Deutschland",
		"address": {
			"country": "Deutschland",
			"country_code": "de",
			"city": "Berlin DE",
			"postcode": "10117",
			"road": "Lokalisierte Strasse",
			"house_number": "1"
		}
	}`
	var gotLanguage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLanguage = r.URL.Query().Get("accept-language")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	geocoder, err := geocoding.New(geocoding.Config{
		Provider:       "nominatim",
		ProviderURL:    srv.URL,
		Timeout:        int((2 * time.Second) / time.Millisecond),
		CacheDetail:    3,
		IncludeCountry: true,
	})
	if err != nil {
		t.Fatalf("geocoding.New: %v", err)
	}

	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{"lure_501": "Lockmodul"}))
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"lure_501": "Lure Module"}))

	e := &Enricher{
		Geocoder:      geocoder,
		DefaultLocale: "en",
		Translations:  bundle,
		GameData: &gamedata.GameData{Util: &gamedata.UtilData{Lures: map[int]gamedata.LureInfo{
			501: {},
		}}},
	}
	lure := &webhook.LureWebhook{LureID: 501, Latitude: 52.517, Longitude: 13.389}

	got := e.LureTranslate(map[string]any{}, lure, "de")
	if gotLanguage != "de" {
		t.Fatalf("accept-language=%q, want de", gotLanguage)
	}
	if got["city"] != "Berlin DE" {
		t.Fatalf("city=%q, want Berlin DE", got["city"])
	}
	if got["formattedAddress"] == "" || got["addr"] == "" {
		t.Fatalf("expected localized address fields, got formatted=%q addr=%q", got["formattedAddress"], got["addr"])
	}
}

func TestLureTranslateSkipsLocalizedGeocodingForDefaultLocale(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	geocoder, err := geocoding.New(geocoding.Config{Provider: "nominatim", ProviderURL: srv.URL})
	if err != nil {
		t.Fatalf("geocoding.New: %v", err)
	}
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{"lure_501": "Lure Module"}))

	e := &Enricher{
		Geocoder:      geocoder,
		DefaultLocale: "en",
		Translations:  bundle,
		GameData: &gamedata.GameData{Util: &gamedata.UtilData{Lures: map[int]gamedata.LureInfo{
			501: {},
		}}},
	}

	got := e.LureTranslate(map[string]any{}, &webhook.LureWebhook{LureID: 501, Latitude: 52.517, Longitude: 13.389}, "en")
	if called {
		t.Fatal("default-locale translate should not perform a localized geocode lookup")
	}
	if _, ok := got["addr"]; ok {
		t.Fatalf("default-locale translate should not shadow base addr, got %q", got["addr"])
	}
}

// TestAddGeoResultPassesDefaultLocale pins the base lookup against the
// operator's configured DefaultLocale, not blank. The per-language
// "skip when lang == DefaultLocale" optimization in
// addLocalizedGeoResult assumes the base entry IS the default-locale
// result — without an explicit language on the base call the provider
// returns whatever it picks for blank (Nominatim returns the local
// language for the place), and a user with `language == DefaultLocale`
// would end up with addresses in the wrong language.
//
// Reproduces the cross-border-deployment subtle behaviour flagged in
// the PR #127 review: operator in DE serving EN users would see
// German addresses for English users without this.
func TestAddGeoResultPassesDefaultLocale(t *testing.T) {
	var gotLanguage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLanguage = r.URL.Query().Get("accept-language")
		w.Write([]byte(`{"display_name":"x","address":{"city":"x"}}`))
	}))
	defer srv.Close()

	geocoder, err := geocoding.New(geocoding.Config{
		Provider:    "nominatim",
		ProviderURL: srv.URL,
		Timeout:     int((2 * time.Second) / time.Millisecond),
		CacheDetail: 3,
	})
	if err != nil {
		t.Fatalf("geocoding.New: %v", err)
	}

	e := &Enricher{Geocoder: geocoder, DefaultLocale: "en"}
	m := map[string]any{}
	e.addGeoResult(m, 52.517, 13.389)

	if gotLanguage != "en" {
		t.Errorf("base geocode accept-language=%q, want %q (DefaultLocale)", gotLanguage, "en")
	}
}
