package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/geocoding"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// fakeWeather is a stub WeatherExporter returning a fixed cell→weather map.
type fakeWeather struct {
	gotCell string
	out     map[int64]int
}

func (f *fakeWeather) ExportCellWeather(cellID string) map[int64]int {
	f.gotCell = cellID
	return f.out
}

// fakeGeocoder is a stub ForwardGeocoder.
type fakeGeocoder struct {
	gotQuery string
	out      []geocoding.ForwardResult
	err      error
}

func (f *fakeGeocoder) Forward(query string) ([]geocoding.ForwardResult, error) {
	f.gotQuery = query
	return f.out, f.err
}

// decodeBody decodes a JSON response body into a generic map for assertions.
func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	return got
}

// TestHumaWeather_OK asserts the weather op serves the per-cell map at 200 and
// passes the cell query param through to the exporter.
func TestHumaWeather_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	fw := &fakeWeather{out: map[int64]int{42: 3}}
	RegisterWeather(humaAPI, fw)

	req := httptest.NewRequest(http.MethodGet, "/api/weather?cell=123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/weather?cell=123 = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if fw.gotCell != "123" {
		t.Errorf("exporter got cell %q, want \"123\"", fw.gotCell)
	}
	got := decodeBody(t, w)
	if got["42"] != float64(3) {
		t.Errorf("body[42] = %v, want 3; full: %v", got["42"], got)
	}
}

// TestHumaWeather_MissingCell asserts a missing cell param yields problem+json
// with a numeric status (422 from huma's required-validation), not the legacy
// {error:...} 400 envelope.
func TestHumaWeather_MissingCell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterWeather(humaAPI, &fakeWeather{})

	req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /api/weather (no cell) = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("problem+json status must be a JSON number; full: %v", got)
	}
	if _, ok := got["title"]; !ok {
		t.Errorf("problem+json body must contain a \"title\" field: %v", got)
	}
}

// TestHumaStatsRarity_OK asserts the rarity stats op emits the typed
// {groups:[{group, pokemon_ids}]} array, ascending by group.
func TestHumaStatsRarity_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterStatsRarity(humaAPI, func() map[int][]int {
		return map[int][]int{3: {25, 26}, 1: {1, 4}}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/rarity", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/rarity = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Groups []struct {
			Group      int   `json:"group"`
			PokemonIDs []int `json:"pokemon_ids"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if len(body.Groups) != 2 {
		t.Fatalf("want 2 groups, got %d: %s", len(body.Groups), w.Body.String())
	}
	if body.Groups[0].Group != 1 || body.Groups[1].Group != 3 {
		t.Errorf("groups not ascending by group: %v", body.Groups)
	}
	if len(body.Groups[1].PokemonIDs) != 2 || body.Groups[1].PokemonIDs[0] != 25 {
		t.Errorf("group 3 pokemon_ids = %v, want [25 26]", body.Groups[1].PokemonIDs)
	}
}

// TestHumaStatsShiny_OK asserts the shiny stats op emits the typed
// {pokemon:[{pokemon_id, total, seen, ratio}]} array, ascending by id.
func TestHumaStatsShiny_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterStatsShiny(humaAPI, func() map[int]tracker.ShinyStats {
		return map[int]tracker.ShinyStats{
			25: {Total: 512, Seen: 1, Ratio: 512},
			1:  {Total: 100, Seen: 2, Ratio: 50},
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/shiny", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/shiny = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Pokemon []struct {
			PokemonID int     `json:"pokemon_id"`
			Total     int64   `json:"total"`
			Seen      int64   `json:"seen"`
			Ratio     float64 `json:"ratio"`
		} `json:"pokemon"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if len(body.Pokemon) != 2 || body.Pokemon[0].PokemonID != 1 || body.Pokemon[1].PokemonID != 25 {
		t.Fatalf("shiny pokemon not ascending by id: %v", body.Pokemon)
	}
	if body.Pokemon[1].Total != 512 || body.Pokemon[1].Ratio != 512 {
		t.Errorf("pokemon 25 = %+v, want total=512 ratio=512", body.Pokemon[1])
	}
}

// TestHumaStatsShinyPossible_OK asserts the shiny-possible op preserves the
// {map:{id:true}} shape with string-encoded pokemon id keys.
func TestHumaStatsShinyPossible_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterStatsShinyPossible(humaAPI, func() map[string]any {
		return map[string]any{"map": map[int]bool{25: true, 1: true}}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/shiny-possible", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/shiny-possible = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Map map[string]bool `json:"map"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if !body.Map["25"] || !body.Map["1"] {
		t.Errorf("shiny-possible map = %v, want {1:true,25:true}", body.Map)
	}
}

// TestHumaGeocode_OK asserts the geocode op serves the forward results at 200
// and passes the q query param through to the geocoder.
func TestHumaGeocode_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	fg := &fakeGeocoder{out: []geocoding.ForwardResult{{Latitude: 1.5, Longitude: 2.5, City: "Townsville"}}}
	RegisterGeocode(humaAPI, fg)

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/forward?q=town", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/geocode/forward?q=town = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if fg.gotQuery != "town" {
		t.Errorf("geocoder got query %q, want \"town\"", fg.gotQuery)
	}
	var arr []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&arr); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if len(arr) != 1 || arr[0]["city"] != "Townsville" {
		t.Errorf("geocode body = %v, want [{city:Townsville,...}]", arr)
	}
}

// TestHumaGeocode_MissingQuery asserts an empty/missing q yields problem+json
// with a numeric status (422 from required-validation).
func TestHumaGeocode_MissingQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterGeocode(humaAPI, &fakeGeocoder{})

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/forward", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /api/geocode/forward (no q) = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("problem+json status must be a JSON number; full: %v", got)
	}
}

// TestHumaGeocode_Error asserts a geocoder lookup error surfaces as a 500
// problem+json.
func TestHumaGeocode_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterGeocode(humaAPI, &fakeGeocoder{err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/api/geocode/forward?q=town", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/geocode/forward?q=town (err) = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("problem+json status must be a JSON number; full: %v", got)
	}
}
