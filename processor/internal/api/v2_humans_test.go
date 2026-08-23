package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// --- harness ----------------------------------------------------------------

// humanSquareFence builds an axis-aligned square fence centred on (lat,lon) with the
// given half-side. Used so MatchedAreaNames / PointInPolygon are predictable.
func humanSquareFence(name string, lat, lon, half float64) geofence.Fence {
	return geofence.Fence{
		Name:             name,
		UserSelectable:   true,
		DisplayInMatches: true,
		Path: [][2]float64{
			{lat - half, lon - half},
			{lat - half, lon + half},
			{lat + half, lon + half},
			{lat + half, lon - half},
		},
	}
}

// newV2HumansTestAPI wires the strict v2 humans endpoints against a mock human
// store and a state manager seeded with two square fences:
//   - "alpha" centred on (10,10)
//   - "beta"  centred on (20,20)
//
// It seeds human u1 (en, profile 1, located near alpha). cfg lets a test toggle
// area_security / available languages. Returns the engine, the mock store, and a
// pointer to a reload counter (incremented by ReloadFunc).
func newV2HumansTestAPI(t *testing.T, cfg *config.Config) (*gin.Engine, *store.MockHumanStore, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true,
		Language: "en", CurrentProfileNo: 1, Latitude: 10, Longitude: 10,
	})

	fences := []geofence.Fence{
		humanSquareFence("alpha", 10, 10, 1),
		humanSquareFence("beta", 20, 20, 1),
	}
	idx := geofence.NewSpatialIndex(fences) // mutates fences in place (NormalizedName)
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences, Geofence: idx})

	if cfg == nil {
		cfg = &config.Config{}
	}

	var reloads int32
	deps := &TrackingDeps{
		Humans:   humans,
		StateMgr: mgr,
		Config:   cfg,
		ReloadFunc: func() {
			atomic.AddInt32(&reloads, 1)
		},
	}

	RegisterV2Humans(humaAPI, deps)
	return r, humans, &reloads
}

// --- create -----------------------------------------------------------------

func TestV2Humans_Create_OK(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, &config.Config{
		General: config.GeneralConfig{Locale: "en"},
	})

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans",
		`{"id":"new1","name":"New User","latitude":5,"longitude":6}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := v2DecodeBody(t, w)
	human, ok := body["human"].(map[string]any)
	if !ok || human["id"] != "new1" {
		t.Fatalf("expected created human resource, got %v", body)
	}
	// Stored + default profile created.
	stored, _ := humans.Get("new1")
	if stored == nil || stored.Name != "New User" || stored.Type != "discord:user" {
		t.Fatalf("human not stored correctly: %+v", stored)
	}
	if !hasCall(humans, "Create") || !hasCall(humans, "CreateDefaultProfile") {
		t.Fatalf("expected Create + CreateDefaultProfile, calls=%v", humans.Calls)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Create_Conflict(t *testing.T) {
	r, _, reloads := newV2HumansTestAPI(t, nil)
	// u1 already exists.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans", `{"id":"u1","name":"Dup"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for existing human, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("conflict must not reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Create_RejectsMissingRequired(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans", `{"id":"x"}`) // name missing
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_Create_RejectsUnknownField(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans", `{"id":"x","name":"y","bogus":1}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown field, got %d: %s", w.Code, w.Body.String())
	}
}

// --- get one ----------------------------------------------------------------

func TestV2Humans_GetOne_OK(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	human, ok := v2DecodeBody(t, w)["human"].(map[string]any)
	if !ok || human["id"] != "u1" {
		t.Fatalf("expected human resource for u1, got %v", human)
	}
	// blocked_alerts is present on the resource as a read-only field (null here).
	if _, ok := human["blocked_alerts"]; !ok {
		t.Fatalf("resource should carry read-only blocked_alerts key: %v", human)
	}
}

func TestV2Humans_GetOne_404(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_GetOne_RejectsUnknownQuery(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1?bogus=1", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unknown query, got %d: %s", w.Code, w.Body.String())
	}
}

// --- areas (available) ------------------------------------------------------

func TestV2Humans_AreasGet_AllWhenUnrestricted(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/areas", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	areas := v2RulesArray(t, v2DecodeBody(t, w), "areas")
	if len(areas) != 2 {
		t.Fatalf("expected 2 available areas, got %d: %v", len(areas), areas)
	}
}

func TestV2Humans_AreasGet_404(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/nope/areas", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- enable / disable -------------------------------------------------------

func TestV2Humans_Enable(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	_ = humans.SetEnabled("u1", false)

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/enable", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	if h, _ := humans.Get("u1"); !h.Enabled {
		t.Fatalf("expected enabled=true")
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Disable(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/disable", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	if h, _ := humans.Get("u1"); h.Enabled {
		t.Fatalf("expected enabled=false")
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Enable_404(t *testing.T) {
	r, _, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/enable", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("404 must not reload")
	}
}

// --- admin-disable ----------------------------------------------------------

func TestV2Humans_AdminDisable_RoundTrip(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)

	// disabled:true → admin_disable set.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/admin-disable", `{"disabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	if h, _ := humans.Get("u1"); !h.AdminDisable {
		t.Fatalf("expected admin_disable=true")
	}

	// disabled:false → cleared.
	w = v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/admin-disable", `{"disabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.AdminDisable {
		t.Fatalf("expected admin_disable=false")
	}
	if atomic.LoadInt32(reloads) != 2 {
		t.Fatalf("expected 2 reloads, got %d", atomic.LoadInt32(reloads))
	}
	// Flag-only: it goes through Update, not SetAdminDisable.
	if hasCall(humans, "SetAdminDisable") {
		t.Fatalf("admin-disable must use Update (flag-only), not SetAdminDisable")
	}
	if !hasCall(humans, "Update") {
		t.Fatalf("expected Update call, calls=%v", humans.Calls)
	}
}

func TestV2Humans_AdminDisable_RejectsMissingField(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/admin-disable", `{}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing disabled, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_AdminDisable_RejectsWrongType(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/admin-disable", `{"disabled":"yes"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for string disabled, got %d: %s", w.Code, w.Body.String())
	}
}

// --- language ---------------------------------------------------------------

func TestV2Humans_Language_ValidCaseInsensitive(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, &config.Config{
		General: config.GeneralConfig{AvailableLanguages: map[string]config.LanguageEntry{"de": {}, "en": {}}},
	})
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"DE"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.Language != "de" {
		t.Fatalf("expected language de (configured casing), got %q", h.Language)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Language_Unavailable(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, &config.Config{
		General: config.GeneralConfig{AvailableLanguages: map[string]config.LanguageEntry{"de": {}}},
	})
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"en"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for unavailable language, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.Language != "en" {
		t.Fatalf("language must be unchanged on rejection, got %q", h.Language)
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("rejection must not reload")
	}
}

func TestV2Humans_Language_AnyWhenUnrestricted(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil) // no AvailableLanguages
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"zh-cn"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.Language != "zh-cn" {
		t.Fatalf("expected zh-cn, got %q", h.Language)
	}
}

func TestV2Humans_Language_EmptyRejected(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":" "}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for blank language, got %d: %s", w.Code, w.Body.String())
	}
}

// --- location ---------------------------------------------------------------

func TestV2Humans_SetLocation_BodyFloats(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"lat":33.5,"lon":-44.25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	h, _ := humans.Get("u1")
	if h.Latitude != 33.5 || h.Longitude != -44.25 {
		t.Fatalf("SetLocation not applied with body floats: %+v", h)
	}
	if !hasCall(humans, "SetLocation") {
		t.Fatalf("expected SetLocation call, calls=%v", humans.Calls)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_SetLocation_RejectsMissingLat(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"lon":5}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing lat, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_SetLocation_ForbiddenOutsideRestriction(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, &config.Config{
		Area: config.AreaConfig{Enabled: true},
	})
	// Restrict u1 to "alpha" (centred on 10,10). A point near beta (20,20) is outside.
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en",
		CurrentProfileNo: 1, AreaRestriction: []string{"alpha"},
	})
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"lat":20,"lon":20}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 outside restriction, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("forbidden must not reload")
	}
}

func TestV2Humans_SetLocation_AllowedInsideRestriction(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, &config.Config{
		Area: config.AreaConfig{Enabled: true},
	})
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en",
		CurrentProfileNo: 1, AreaRestriction: []string{"alpha"},
	})
	// (10,10) is inside alpha.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"lat":10,"lon":10}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 inside restriction, got %d: %s", w.Code, w.Body.String())
	}
}

// --- check-location ---------------------------------------------------------

func TestV2Humans_CheckLocation_DisabledAlwaysOK(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil) // area_security disabled
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?lat=99&lon=99", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ok, _ := v2DecodeBody(t, w)["locationOk"].(bool); !ok {
		t.Fatalf("expected locationOk=true when area_security disabled")
	}
}

func TestV2Humans_CheckLocation_InsideOutside(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, &config.Config{
		Area: config.AreaConfig{Enabled: true},
	})
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en",
		CurrentProfileNo: 1, AreaRestriction: []string{"alpha"},
	})

	// Inside alpha → true.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?lat=10&lon=10", "")
	if ok, _ := v2DecodeBody(t, w)["locationOk"].(bool); !ok {
		t.Fatalf("expected locationOk=true inside alpha")
	}
	// Outside (near beta) → false.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?lat=20&lon=20", "")
	if ok, _ := v2DecodeBody(t, w)["locationOk"].(bool); ok {
		t.Fatalf("expected locationOk=false outside alpha")
	}
}

func TestV2Humans_CheckLocation_RejectsMissingQuery(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?lat=10", "")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing lon, got %d: %s", w.Code, w.Body.String())
	}
}

// --- set areas --------------------------------------------------------------

func TestV2Humans_SetAreas_IntersectsAllowed(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	// alpha is a known fence; "ghost" is not → dropped.
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{"areas":["Alpha","ghost","alpha"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	h, _ := humans.Get("u1")
	if len(h.Area) != 1 || h.Area[0] != "alpha" {
		t.Fatalf("expected stored areas [alpha] (deduped, lowered, filtered), got %v", h.Area)
	}
	if !hasCall(humans, "SetArea") {
		t.Fatalf("expected SetArea call, calls=%v", humans.Calls)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_SetAreas_RejectsMissingField(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing areas, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_SetAreas_404(t *testing.T) {
	r, _, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/nope/areas", `{"areas":["alpha"]}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("404 must not reload")
	}
}

// --- helpers ----------------------------------------------------------------

// assertStatusOK asserts the mutation response body is {"status":"ok"}.
func assertStatusOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if s, _ := v2DecodeBody(t, w)["status"].(string); s != "ok" {
		t.Fatalf("expected {status:ok}, got %s", w.Body.String())
	}
}

func hasCall(m *store.MockHumanStore, name string) bool {
	return slices.Contains(m.Calls, name)
}
