package api

import (
	"encoding/json"
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

// privateFence is a square fence with userSelectable:false — the shape
// PoracleWeb.NET serves for user-drawn geofences.
func privateFence(name string, lat, lon, half float64) geofence.Fence {
	f := humanSquareFence(name, lat, lon, half)
	f.UserSelectable = false
	return f
}

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
		// A user-drawn private fence: served by the client, deliberately kept
		// out of the bot's !area picker (#215).
		privateFence("private", 30, 30, 1),
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
	if len(areas) != 3 {
		t.Fatalf("expected 3 available areas, got %d: %v", len(areas), areas)
	}
	// The listing reports every fence with its userSelectable flag, so a
	// client can tell which ones need trusted:true to set (#215).
	selectable := map[string]bool{}
	for _, m := range areas {
		name, _ := m["name"].(string)
		sel, _ := m["userSelectable"].(bool)
		selectable[name] = sel
	}
	if !selectable["alpha"] || selectable["private"] {
		t.Errorf("userSelectable flags = %v, want alpha true and private false", selectable)
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

// --- #214: list and delete -------------------------------------------------

// seedHumans adds extra humans of assorted types to the fixture store.
func seedHumans(humans *store.MockHumanStore) {
	humans.AddHuman(&store.Human{ID: "u2", Type: "discord:user", Name: "User2", Enabled: true})
	humans.AddHuman(&store.Human{ID: "w1", Type: "webhook", Name: "Hook1", Enabled: true})
	humans.AddHuman(&store.Human{ID: "w2", Type: "webhook", Name: "Hook2", Enabled: false})
}

type v2HumanListBody struct {
	Humans []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	} `json:"humans"`
}

func listHumans(t *testing.T, r *gin.Engine, query string) v2HumanListBody {
	t.Helper()
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans"+query, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v2/humans%s = %d: %s", query, w.Code, w.Body.String())
	}
	var got v2HumanListBody
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
	}
	return got
}

// "Who is registered on this instance" had no answer on either API version,
// so PoracleWeb.NET kept a repository over the Poracle database alive for its
// admin user list.
func TestV2Humans_List_ReturnsEveryone(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	seedHumans(humans)

	got := listHumans(t, r, "")
	if len(got.Humans) != 4 {
		t.Fatalf("listed %d humans, want 4: %+v", len(got.Humans), got.Humans)
	}
	// Deterministic order so clients can cache and diff.
	ids := make([]string, len(got.Humans))
	for i, h := range got.Humans {
		ids[i] = h.ID
	}
	want := []string{"u1", "u2", "w1", "w2"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want ascending %v", ids, want)
		}
	}
}

// ?type=webhook alone closes the delegated-webhook half of their need.
func TestV2Humans_List_FiltersByType(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	seedHumans(humans)

	got := listHumans(t, r, "?type=webhook")
	if len(got.Humans) != 2 {
		t.Fatalf("listed %d webhooks, want 2: %+v", len(got.Humans), got.Humans)
	}
	for _, h := range got.Humans {
		if h.Type != "webhook" {
			t.Errorf("got type %q in a ?type=webhook listing", h.Type)
		}
	}
}

// Batch read by id, same comma-separated form as the v2 bulk delete's ?uid=,
// so an admin page resolving many display names makes one request not N.
func TestV2Humans_List_BatchByID(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	seedHumans(humans)

	got := listHumans(t, r, "?id=u1,w2")
	if len(got.Humans) != 2 {
		t.Fatalf("listed %d, want 2: %+v", len(got.Humans), got.Humans)
	}
	if got.Humans[0].ID != "u1" || got.Humans[1].ID != "w2" {
		t.Errorf("ids = %+v, want u1 and w2", got.Humans)
	}
}

// An id that does not exist is simply absent, not a 404 — a batch read of
// mixed-validity ids should return what it found.
func TestV2Humans_List_BatchIgnoresUnknownIDs(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	seedHumans(humans)

	got := listHumans(t, r, "?id=u1,nope")
	if len(got.Humans) != 1 || got.Humans[0].ID != "u1" {
		t.Fatalf("got %+v, want just u1", got.Humans)
	}
}

// Deleting in the client's own database means knowing every table a human
// touches — PoracleNG's schema, not theirs, going stale silently.
func TestV2Humans_Delete_RemovesHuman(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	seedHumans(humans)

	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE = %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u2"); h != nil {
		t.Errorf("human u2 still present after delete: %+v", h)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Errorf("expected 1 reload after delete, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_Delete_UnknownHuman404(t *testing.T) {
	r, _, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/nope", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Errorf("a failed delete must not reload")
	}
}

// --- #215: setAreas must not drop names silently ----------------------------

type v2SetAreasResult struct {
	Areas    []string `json:"areas"`
	Rejected []string `json:"rejected"`
}

func setAreas(t *testing.T, r *gin.Engine, body string) (int, v2SetAreasResult) {
	t.Helper()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", body)
	var got v2SetAreasResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
		}
	}
	return w.Code, got
}

// A 200 and {"status":"ok"} over a request where a name was silently
// discarded is indistinguishable from one where everything was stored.
func TestV2Humans_SetAreas_ReportsWhatItStoredAndRejected(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)

	code, got := setAreas(t, r, `{"areas":["alpha","ghost"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "alpha" {
		t.Errorf("areas = %v, want [alpha]", got.Areas)
	}
	if len(got.Rejected) != 1 || got.Rejected[0] != "ghost" {
		t.Errorf("rejected = %v, want [ghost] — a dropped name must be reported", got.Rejected)
	}
}

// PoracleWeb.NET serves user-drawn private geofences with userSelectable:false
// deliberately, to keep their names out of the bot's !area picker and DM text.
// The same flag then made them unsettable by the very user who drew them, so
// every user-geofence change became a direct database write. The caller holds
// the API secret, which is what distinguishes a server-side client from a
// user, so it may assert trust explicitly.
func TestV2Humans_SetAreas_TrustedBypassesUserSelectable(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)

	// Without trust, a non-user-selectable fence is rejected...
	code, got := setAreas(t, r, `{"areas":["private"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 0 || len(got.Rejected) != 1 {
		t.Fatalf("untrusted: areas=%v rejected=%v, want the private fence rejected", got.Areas, got.Rejected)
	}

	// ...and with it, stored.
	code, got = setAreas(t, r, `{"areas":["private"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "private" {
		t.Fatalf("trusted: areas=%v rejected=%v, want the private fence stored", got.Areas, got.Rejected)
	}
	h, _ := humans.Get("u1")
	if len(h.Area) != 1 || h.Area[0] != "private" {
		t.Errorf("stored areas = %v, want [private]", h.Area)
	}
}

// trusted must not invent fences: a name that matches nothing is still
// rejected, so a typo cannot become a stored area that matches no geofence.
func TestV2Humans_SetAreas_TrustedStillRejectsUnknownNames(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	code, got := setAreas(t, r, `{"areas":["alpha","nosuchfence"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "alpha" {
		t.Errorf("areas = %v, want [alpha]", got.Areas)
	}
	if len(got.Rejected) != 1 || got.Rejected[0] != "nosuchfence" {
		t.Errorf("rejected = %v, want [nosuchfence] even when trusted", got.Rejected)
	}
}
