package api

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// newV2LocationsTestAPI wires the strict v2 saved-locations endpoints against a
// mock human store. It seeds human u1 (discord:user) and returns the engine, the
// mock store, and a reload counter incremented by ReloadFunc.
func newV2LocationsTestAPI(t *testing.T) (*gin.Engine, *store.MockHumanStore, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true,
		Language: "en", CurrentProfileNo: 1,
	})

	var reloads int32
	deps := &TrackingDeps{
		Humans: humans,
		Config: &config.Config{},
		ReloadFunc: func() {
			atomic.AddInt32(&reloads, 1)
		},
	}

	RegisterV2Locations(humaAPI, deps)
	return r, humans, &reloads
}

// --- list -------------------------------------------------------------------

func TestV2Locations_List(t *testing.T) {
	r, humans, _ := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("seed AddLocation: %v", err)
	}
	if err := humans.SetLocation("u1", 0, 10, 20); err != nil {
		t.Fatalf("seed SetLocation: %v", err)
	}

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/locations", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	locs, ok := body["locations"].(map[string]any)
	if !ok {
		t.Fatalf("missing locations object: %v", body)
	}
	named, ok := locs["named"].([]any)
	if !ok || len(named) != 1 {
		t.Fatalf("expected 1 named location, got %v", locs["named"])
	}
	first := named[0].(map[string]any)
	if first["label"] != "Home" {
		t.Fatalf("expected Home, got %v", first)
	}
	def, ok := locs["default"].(map[string]any)
	if !ok || def["latitude"] != float64(10) {
		t.Fatalf("expected default lat 10, got %v", locs["default"])
	}
}

func TestV2Locations_List_NoDefault(t *testing.T) {
	r, _, _ := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/locations", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	locs := v2DecodeBody(t, w)["locations"].(map[string]any)
	if _, present := locs["default"]; present {
		t.Fatalf("expected no default when lat/lon zero, got %v", locs["default"])
	}
	if named, _ := locs["named"].([]any); len(named) != 0 {
		t.Fatalf("expected empty named array, got %v", named)
	}
}

// --- get one ----------------------------------------------------------------

func TestV2Locations_Get_OK(t *testing.T) {
	r, humans, _ := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Work", Latitude: 1, Longitude: 2}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Case-insensitive label match.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/locations/work", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	loc := v2DecodeBody(t, w)["location"].(map[string]any)
	if loc["label"] != "Work" || loc["latitude"] != float64(1) {
		t.Fatalf("unexpected location: %v", loc)
	}
}

func TestV2Locations_Get_NotFound(t *testing.T) {
	r, _, _ := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/locations/missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- add --------------------------------------------------------------------

func TestV2Locations_Add_OK(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/locations",
		`{"label":"Gym","lat":3.5,"lon":4.5}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	got, _ := humans.GetLocation("u1", "Gym")
	if got == nil || got.Latitude != 3.5 || got.Longitude != 4.5 {
		t.Fatalf("location not stored: %+v", got)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Locations_Add_Duplicate(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Dup", Latitude: 1, Longitude: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/locations",
		`{"label":"Dup","lat":2,"lon":2}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("conflict must not reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Locations_Add_StrictRejectsUnknownField(t *testing.T) {
	r, _, _ := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/locations",
		`{"label":"X","lat":1,"lon":1,"bogus":true}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Locations_Add_StrictRejectsUnknownQuery(t *testing.T) {
	r, _, _ := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/locations?bogus=1",
		`{"label":"X","lat":1,"lon":1}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Locations_Add_MissingRequiredField(t *testing.T) {
	r, _, _ := newV2LocationsTestAPI(t)
	// Missing lat/lon (required).
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/locations", `{"label":"X"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing required, got %d: %s", w.Code, w.Body.String())
	}
}

// --- update (NEW) -----------------------------------------------------------

func TestV2Locations_Update_OK(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 1, Longitude: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/locations/home",
		`{"lat":9.9,"lon":8.8}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	got, _ := humans.GetLocation("u1", "Home")
	if got == nil || got.Latitude != 9.9 || got.Longitude != 8.8 {
		t.Fatalf("coords not updated: %+v", got)
	}
	if !hasCall(humans, "UpdateLocation") {
		t.Fatalf("expected UpdateLocation call, calls=%v", humans.Calls)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Locations_Update_NotFound(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/locations/missing",
		`{"lat":1,"lon":1}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing label, got %d: %s", w.Code, w.Body.String())
	}
	// Must not have attempted the update or reloaded.
	if hasCall(humans, "UpdateLocation") {
		t.Fatalf("must not call UpdateLocation when label missing")
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("404 must not reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Locations_Update_StrictRejectsUnknownField(t *testing.T) {
	r, humans, _ := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 1, Longitude: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/locations/home",
		`{"lat":1,"lon":1,"label":"x"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

// --- delete -----------------------------------------------------------------

func TestV2Locations_Delete_OK(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 1, Longitude: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/locations/home", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	if got, _ := humans.GetLocation("u1", "Home"); got != nil {
		t.Fatalf("location not deleted: %+v", got)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Locations_Delete_Referenced409(t *testing.T) {
	r, humans, reloads := newV2LocationsTestAPI(t)
	if _, err := humans.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 1, Longitude: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Seed a referencing rule keyed by id|lowercased-label.
	humans.LocationRefs = map[string][]store.ReferencingRule{
		"u1|home": {{Type: "pokemon", UID: 42}, {Type: "raid", UID: 7}},
	}

	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/locations/home", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 referenced, got %d: %s", w.Code, w.Body.String())
	}
	// RFC 9457 problem+json envelope.
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected application/problem+json, got %q", ct)
	}
	body := v2DecodeBody(t, w)
	// status is a numeric (JSON number) 409, not a string.
	if body["status"] != float64(409) {
		t.Fatalf("expected numeric status=409, got %v (%T)", body["status"], body["status"])
	}
	if title, _ := body["title"].(string); title == "" {
		t.Fatalf("expected a title, got %v", body["title"])
	}
	// Referencing rules carried in the snake_case extension field, snake_cased.
	refs, ok := body["referencing_rules"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("expected 2 referencing_rules, got %v", body["referencing_rules"])
	}
	first := refs[0].(map[string]any)
	if first["type"] != "pokemon" || first["uid"] != float64(42) {
		t.Fatalf("unexpected referencing rule shape: %v", first)
	}
	// Must not delete or reload when referenced.
	if got, _ := humans.GetLocation("u1", "Home"); got == nil {
		t.Fatalf("location should NOT be deleted when referenced")
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("409 must not reload, got %d", atomic.LoadInt32(reloads))
	}
}
