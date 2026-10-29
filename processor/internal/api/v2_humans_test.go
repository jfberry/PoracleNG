package api

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/guregu/null/v6"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
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

// drawnFence is the shape PoracleWeb.NET serves for a user-drawn geofence
// once #243 ships: userSelectable:false to keep it out of the picker, and
// exemptFromAreaSecurity because no community publishes a user's own fence.
func drawnFence(name string, lat, lon, half float64) geofence.Fence {
	f := privateFence(name, lat, lon, half)
	f.ExemptFromAreaSecurity = true
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
	r, humans, reloads, _, _ := newV2HumansTestAPIRules(t, cfg)
	return r, humans, reloads
}

// newV2HumansTestAPIRules is newV2HumansTestAPI plus pokemon and raid tracking
// mocks wired both into deps.Tracking and into the human mock's
// OverrideTables, so EditOverrideAreas acts on real rows.
func newV2HumansTestAPIRules(t *testing.T, cfg *config.Config) (*gin.Engine, *store.MockHumanStore, *int32, *store.MockTrackingStore[db.MonsterTrackingAPI], *store.MockTrackingStore[db.RaidTrackingAPI]) {
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
		humanSquareFence("gamma", 40, 40, 1),
		// A user-drawn private fence: served by the client, deliberately kept
		// out of the bot's !area picker (#215).
		drawnFence("private", 30, 30, 1),
		privateFence("lone", 50, 50, 1),
		humanSquareFence("Gent_Centrum", 60, 60, 1),
	}
	idx := geofence.NewSpatialIndex(fences) // mutates fences in place (NormalizedName)
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences, Geofence: idx})

	if cfg == nil {
		cfg = &config.Config{}
	}

	// A bundle with known locales, so language validation has a set to
	// validate against (#216).
	bundle := i18n.NewBundle()
	for _, code := range []string{"en", "de", "fr", "zh-cn"} {
		bundle.AddTranslator(i18n.NewTranslator(code, map[string]string{"x": "y"}))
	}

	mons := store.NewMockTrackingStore[db.MonsterTrackingAPI](store.MonsterGetUID, store.MonsterSetUID).
		WithIDScope(func(m *db.MonsterTrackingAPI) string { return m.ID }).
		WithProfileScope(func(m *db.MonsterTrackingAPI) int { return m.ProfileNo })
	raids := store.NewMockTrackingStore[db.RaidTrackingAPI](store.RaidGetUID, store.RaidSetUID).
		WithIDScope(func(m *db.RaidTrackingAPI) string { return m.ID }).
		WithProfileScope(func(m *db.RaidTrackingAPI) int { return m.ProfileNo })
	humans.OverrideTables = map[string]store.OverrideTable{
		"monsters": store.NewMockOverrideTable(mons,
			func(m *db.MonsterTrackingAPI) []string { return m.OverrideAreas },
			func(m *db.MonsterTrackingAPI, a []string) { m.OverrideAreas = a }),
		"raid": store.NewMockOverrideTable(raids,
			func(m *db.RaidTrackingAPI) []string { return m.OverrideAreas },
			func(m *db.RaidTrackingAPI, a []string) { m.OverrideAreas = a }),
	}

	var reloads int32
	deps := &TrackingDeps{
		Translations: bundle,
		Humans:       humans,
		Tracking:     &store.TrackingStores{Monsters: mons, Raids: raids},
		StateMgr:     mgr,
		Config:       cfg,
		ReloadFunc: func() {
			atomic.AddInt32(&reloads, 1)
		},
	}

	RegisterV2Humans(humaAPI, deps)
	return r, humans, &reloads, mons, raids
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
	if len(areas) != 6 {
		t.Fatalf("expected 6 available areas, got %d: %v", len(areas), areas)
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
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"latitude":33.5,"longitude":-44.25}`)
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
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"longitude":5}`)
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
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"latitude":20,"longitude":20}`)
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
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/location", `{"latitude":10,"longitude":10}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 inside restriction, got %d: %s", w.Code, w.Body.String())
	}
}

func TestV2Humans_ClearLocation(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/location", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertStatusOK(t, w)
	h, _ := humans.Get("u1")
	if h.Latitude != 0 || h.Longitude != 0 {
		t.Fatalf("location not cleared: %+v", h)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Fatalf("expected 1 reload, got %d", atomic.LoadInt32(reloads))
	}
}

func TestV2Humans_ClearLocation_IgnoresRestriction(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, &config.Config{
		Area: config.AreaConfig{Enabled: true},
	})
	humans.AddHuman(&store.Human{
		ID: "u1", Type: "discord:user", Name: "User1", Enabled: true, Language: "en",
		CurrentProfileNo: 1, AreaRestriction: []string{"alpha"}, Latitude: 10, Longitude: 10,
	})
	// POST {0,0} would be 403 here: 0,0 is outside alpha.
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/location", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a restricted human, got %d: %s", w.Code, w.Body.String())
	}
	h, _ := humans.Get("u1")
	if h.Latitude != 0 || h.Longitude != 0 {
		t.Fatalf("location not cleared: %+v", h)
	}
}

func TestV2Humans_ClearLocation_404(t *testing.T) {
	r, _, reloads := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/nobody/location", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatalf("404 must not reload")
	}
}

// A ?profile= naming an inactive profile writes that profile only. The matcher
// reads humans.latitude/longitude, so writing it there too would move (or, for
// DELETE, wipe) the location the active profile is matching against.
func TestV2Humans_Location_InactiveProfileLeavesLiveLocation(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		wantLat            float64
	}{
		{"set", http.MethodPost, `{"latitude":20,"longitude":20}`, 20},
		{"clear", http.MethodDelete, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, humans, _ := newV2HumansTestAPI(t, nil)
			humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Name: "Default", Latitude: 10, Longitude: 10})
			humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Weekend", Latitude: 5, Longitude: 5})

			w := v2DoReq(t, r, tc.method, "/api/v2/humans/u1/location?profile=2", tc.body)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if h, _ := humans.Get("u1"); h.Latitude != 10 || h.Longitude != 10 {
				t.Errorf("live location = %v,%v, want untouched 10,10", h.Latitude, h.Longitude)
			}
			profiles, _ := humans.GetProfiles("u1")
			for _, p := range profiles {
				if p.ProfileNo == 2 && p.Latitude != tc.wantLat {
					t.Errorf("profile 2 lat = %v, want %v", p.Latitude, tc.wantLat)
				}
			}
		})
	}
}

// --- check-location ---------------------------------------------------------

func TestV2Humans_CheckLocation_DisabledAlwaysOK(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil) // area_security disabled
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?latitude=99&longitude=99", "")
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
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?latitude=10&longitude=10", "")
	if ok, _ := v2DecodeBody(t, w)["locationOk"].(bool); !ok {
		t.Fatalf("expected locationOk=true inside alpha")
	}
	// Outside (near beta) → false.
	w = v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?latitude=20&longitude=20", "")
	if ok, _ := v2DecodeBody(t, w)["locationOk"].(bool); ok {
		t.Fatalf("expected locationOk=false outside alpha")
	}
}

func TestV2Humans_CheckLocation_RejectsMissingQuery(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/check-location?latitude=10", "")
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

// --- #216: language validation ----------------------------------------------

// On a deployment that configures available_languages an unknown code is
// refused; on one that configures nothing, any string was accepted and stored.
// "Unrestricted" should not mean "unvalidated" — the set of languages the
// server has translations for is known either way.
func TestV2Humans_Language_RejectsUnknownCodeWhenUnrestricted(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"zz"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for an unknown language, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.Language == "zz" {
		t.Errorf("rejected language must not be stored, got %q", h.Language)
	}
}

func TestV2Humans_Language_AcceptsKnownCodeWhenUnrestricted(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"de"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a loaded language, got %d: %s", w.Code, w.Body.String())
	}
	h, _ := humans.Get("u1")
	if h.Language != "de" {
		t.Errorf("stored language = %q, want %q", h.Language, "de")
	}
}

// The write lowercases; documented so a client comparing what it sent with
// what it reads back does not think the write failed.
func TestV2Humans_Language_LowercasesWhatItStores(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/language", `{"language":"DE"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); h.Language != "de" {
		t.Errorf("stored language = %q, want lowercased %q", h.Language, "de")
	}
}

// --- #229: the list must back an admin grid ---------------------------------

// The list projection omitted last_checked, disabled_date and notes, which the
// admin user grid renders as columns. Fetching them per-id is not an option at
// 2333 humans, so those three columns were the only reason a client still
// mapped the humans table directly. They are already selected by
// humanRowColumns for the single GET, so this is the same projection rather
// than new query work.
func TestV2Humans_List_CarriesAdminGridColumns(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	checked := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	humans.AddHuman(&store.Human{
		ID: "u9", Type: "discord:user", Name: "Dormant", Enabled: false,
		AdminDisable: true,
		LastChecked:  null.TimeFrom(checked),
		DisabledDate: null.TimeFrom(checked),
		Notes:        "asked to be paused",
	})

	got := listHumans(t, r, "?id=u9")
	if len(got.Humans) != 1 {
		t.Fatalf("listed %d, want 1", len(got.Humans))
	}

	// Decode again as raw JSON so absent keys are distinguishable from zero.
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans?id=u9", "")
	var raw struct {
		Humans []map[string]any `json:"humans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	h := raw.Humans[0]
	for _, k := range []string{"last_checked", "disabled_date", "notes"} {
		if _, ok := h[k]; !ok {
			t.Errorf("list item missing %q; keys: %v", k, h)
		}
	}
	if h["notes"] != "asked to be paused" {
		t.Errorf("notes = %v, want the stored note", h["notes"])
	}
}

// A human never checked and never disabled must report null rather than a zero
// timestamp, or an admin grid sorts 0001-01-01 to the top.
func TestV2Humans_List_NullTimestampsStayNull(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans?id=u1", "")
	var raw struct {
		Humans []map[string]any `json:"humans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v := raw.Humans[0]["last_checked"]; v != nil {
		t.Errorf("last_checked = %v, want null for a never-checked human", v)
	}
}

// --- #228: trusted must not lift the community restriction ------------------

// newAreaSecurityAPI builds a fixture with area_security on: the human belongs
// to one community whose allowed areas are alpha and beta, while "private" is
// a non-userSelectable fence and "gamma" belongs to nobody's community.
func newAreaSecurityAPI(t *testing.T) (*gin.Engine, *store.MockHumanStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha", "beta", "private"}},
	}
	r, humans, _ := newV2HumansTestAPI(t, cfg)

	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = []string{"alpha", "beta", "private"}
	humans.AddHuman(h)
	return r, humans
}

// trusted exists so a server-side client can assert "the user drew this fence,
// so let them select it". Its doc comment says it lifts the userSelectable
// filter ONLY. It also skipped community.FilterAreas, so a trusted call handed
// the human every fence on the instance — which makes the flag unusable for
// the one path that needs it: saving a selection containing both admin areas
// (which must stay community-filtered) and the user's own drawn fences.
func TestV2Humans_SetAreas_TrustedDoesNotLiftCommunityRestriction(t *testing.T) {
	r, _ := newAreaSecurityAPI(t)

	// gamma is a real fence but outside this human's community.
	code, got := setAreas(t, r, `{"areas":["alpha","gamma"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	for _, a := range got.Areas {
		if a == "gamma" {
			t.Errorf("trusted stored %q, which is outside the human's community; "+
				"trusted lifts userSelectable only. areas=%v rejected=%v", a, got.Areas, got.Rejected)
		}
	}
	if len(got.Rejected) != 1 || got.Rejected[0] != "gamma" {
		t.Errorf("rejected = %v, want [gamma]", got.Rejected)
	}
}

// The case trusted was built for must keep working: a non-userSelectable fence
// that IS inside the human's community.
func TestV2Humans_SetAreas_TrustedStillLiftsUserSelectableWithinCommunity(t *testing.T) {
	r, _ := newAreaSecurityAPI(t)

	code, got := setAreas(t, r, `{"areas":["private"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "private" {
		t.Errorf("areas = %v rejected = %v, want the in-community private fence stored",
			got.Areas, got.Rejected)
	}
}

// --- setAreas profile targeting ---------------------------------------------

// setAreas wrote human.CurrentProfileNo only, so a client could not target
// another profile — and removing an area from EVERY profile, which is what
// happens when a user deletes a fence they had drawn, had no API form at all
// and still needed a direct database write. ?profile= mirrors the convention
// the tracking endpoints and setLocation already use.
func TestV2Humans_SetAreas_TargetsNamedProfile(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Name: "Default"})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Weekend"})

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas?profile=2", `{"areas":["alpha"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := humans.AreaForProfile("u1", 2); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("profile 2 areas = %v, want [alpha]", got)
	}
	// The active profile must be untouched.
	if got := humans.AreaForProfile("u1", 1); len(got) != 0 {
		t.Errorf("profile 1 areas = %v, want untouched", got)
	}
	// So must the live copy the matcher reads (humans.area).
	if h, _ := humans.Get("u1"); len(h.Area) != 0 {
		t.Errorf("humans.area = %v, want untouched", h.Area)
	}
}

// all_profiles is the shape that actually answers "the user deleted a fence,
// drop it everywhere" in one call.
func TestV2Humans_SetAreas_AllProfiles(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Name: "Default"})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Name: "Weekend"})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 3, Name: "Holiday"})

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas?all_profiles=true", `{"areas":["beta"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	for _, no := range []int{1, 2, 3} {
		if got := humans.AreaForProfile("u1", no); len(got) != 1 || got[0] != "beta" {
			t.Errorf("profile %d areas = %v, want [beta]", no, got)
		}
	}
}

// Asking for both is a client bug, not something to guess at.
func TestV2Humans_SetAreas_ProfileAndAllProfilesConflict(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost,
		"/api/v2/humans/u1/areas?profile=2&all_profiles=true", `{"areas":["alpha"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for profile + all_profiles, got %d: %s", w.Code, w.Body.String())
	}
}

// A user-drawn fence appears in NO community's allowed_areas — that is what
// makes it user-drawn. So trusted adds it to the permitted set and
// community.FilterAreas then strips it straight back out, making trusted a
// no-op under area_security for precisely the fences it exists to allow.
//
// The #228 fixture masked this by listing "private" in the community's
// allowed_areas, which a real drawn fence never is.
func TestV2Humans_SetAreas_TrustedLiftsFenceInNoCommunity(t *testing.T) {
	// RED test for #243, committed skipped in 808bf0e7. The gamma assertion is #228's guard.

	cfg := &config.Config{}
	cfg.Area.Enabled = true
	// "drawn" is deliberately absent: no community publishes a user's fence.
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha", "beta"}},
	}
	r, humans, _ := newV2HumansTestAPI(t, cfg)
	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = []string{"alpha", "beta"}
	humans.AddHuman(h)

	// The mixed request a client actually sends: an admin area the user may
	// have, an admin area from another community, and the user's own fence.
	code, got := setAreas(t, r, `{"areas":["alpha","gamma","private"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	stored := map[string]bool{}
	for _, a := range got.Areas {
		stored[a] = true
	}
	if !stored["alpha"] {
		t.Errorf("alpha should be stored: areas=%v rejected=%v", got.Areas, got.Rejected)
	}
	// Another community's admin fence must still be refused — that is #228.
	if stored["gamma"] {
		t.Errorf("gamma is another community's area and must stay rejected: %v", got.Areas)
	}
	// And the user's own fence must be allowed — that is what trusted is for.
	if !stored["private"] {
		t.Errorf("trusted did not lift the user-drawn fence under area_security; "+
			"areas=%v rejected=%v", got.Areas, got.Rejected)
	}
}

// The exempt flag lifts the community filter, not userSelectable: without
// trusted, a drawn fence is still refused under area_security.
func TestV2Humans_SetAreas_ExemptStillNeedsTrustedWhenNotSelectable(t *testing.T) {
	r, _ := newCoastOnlyAPI(t)
	code, got := setAreas(t, r, `{"areas":["private"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 0 || len(got.Rejected) != 1 {
		t.Fatalf("areas=%v rejected=%v, want private rejected without trusted", got.Areas, got.Rejected)
	}
}

// Without the flag, trusted cannot reach a fence outside the community: the
// flag, not trusted, is what makes the drawn fence settable (#243).
func TestV2Humans_SetAreas_TrustedDoesNotLiftUnflaggedFenceInNoCommunity(t *testing.T) {
	r, _ := newCoastOnlyAPI(t)
	code, got := setAreas(t, r, `{"areas":["lone"],"trusted":true}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 0 || len(got.Rejected) != 1 || got.Rejected[0] != "lone" {
		t.Fatalf("areas=%v rejected=%v, want lone rejected", got.Areas, got.Rejected)
	}
}

// Gate decision (see plan): a community member is community-filtered even
// when area_restriction is nil, which reconciliation writes for communities
// with no location_fence. Before #243 such a member could set any fence.
func TestV2Humans_SetAreas_MemberWithoutRestrictionIsCommunityFiltered(t *testing.T) {
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "coast", AllowedAreas: []string{"alpha"}}}
	r, humans, _ := newV2HumansTestAPI(t, cfg)
	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = nil
	humans.AddHuman(h)

	code, got := setAreas(t, r, `{"areas":["alpha","beta"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "alpha" {
		t.Fatalf("areas=%v rejected=%v, want only alpha", got.Areas, got.Rejected)
	}
}

// D4: the listing reports exemptFromAreaSecurity, and under area_security
// includes exempt fences so a client can explain why they behave differently.
func TestV2Humans_AreasGet_ReportsExempt(t *testing.T) {
	r, _ := newCoastOnlyAPI(t) // private is drawn+exempt, in no community
	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/areas", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	exempt := map[string]bool{}
	for _, m := range v2RulesArray(t, v2DecodeBody(t, w), "areas") {
		name, _ := m["name"].(string)
		ex, ok := m["exemptFromAreaSecurity"].(bool)
		if !ok {
			t.Fatalf("area %q has no exemptFromAreaSecurity field: %v", name, m)
		}
		exempt[name] = ex
	}
	if _, listed := exempt["private"]; !listed || !exempt["private"] {
		t.Errorf("private must be listed and exempt: %v", exempt)
	}
	if _, listed := exempt["gamma"]; listed {
		t.Errorf("gamma is another community's fence and must not be listed: %v", exempt)
	}
	if _, listed := exempt["lone"]; listed {
		t.Errorf("lone is in no community and not exempt; must not be listed: %v", exempt)
	}
	if exempt["alpha"] {
		t.Errorf("alpha is not exempt: %v", exempt)
	}
}

// newCoastOnlyAPI: area_security on, u1 in "coast" (alpha, beta), and the
// drawn "private" fence in NO community, as a real drawn fence is.
func newCoastOnlyAPI(t *testing.T) (*gin.Engine, *store.MockHumanStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{
		{Name: "coast", AllowedAreas: []string{"alpha", "beta"}},
	}
	r, humans, _ := newV2HumansTestAPI(t, cfg)
	h, _ := humans.Get("u1")
	h.CommunityMembership = []string{"coast"}
	h.AreaRestriction = []string{"alpha", "beta"}
	humans.AddHuman(h)
	return r, humans
}

// TestV2Humans_SetAreas_EitherSpelling: a requested name resolves to the fence
// whatever its underscore/space spelling, and the fence's own spelling is
// stored.
func TestV2Humans_SetAreas_EitherSpelling(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	code, got := setAreas(t, r, `{"areas":["Gent Centrum","gent_centrum"]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(got.Areas) != 1 || got.Areas[0] != "gent_centrum" || len(got.Rejected) != 0 {
		t.Fatalf("got %+v, want areas [gent_centrum], rejected []", got)
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 1 || h.Area[0] != "gent_centrum" {
		t.Fatalf("stored humans.area = %v, want [gent_centrum]", h.Area)
	}
}

// POST /v2/humans stores area as the fences' own spelling, deduped by key.
// area_restriction keeps a spaced allowed_areas entry's fence.
func TestV2Humans_Create_CanonicalAreas(t *testing.T) {
	cfg := &config.Config{General: config.GeneralConfig{Locale: "en"}}
	cfg.Area.Enabled = true
	cfg.Area.Communities = []config.CommunityConfig{{Name: "be", AllowedAreas: []string{"gent centrum"}}}
	r, humans, _ := newV2HumansTestAPI(t, cfg)

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans",
		`{"id":"new1","name":"New","area":["Gent Centrum","gent_centrum","Alpha"],"community":["be"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	stored, _ := humans.Get("new1")
	if stored == nil || !slices.Equal(stored.Area, []string{"gent_centrum", "alpha"}) {
		t.Fatalf("stored area = %v, want [gent_centrum alpha]", stored)
	}
	if !slices.Equal(stored.AreaRestriction, []string{"gent_centrum"}) {
		t.Fatalf("area_restriction = %v, want [gent_centrum]", stored.AreaRestriction)
	}
}

// --- DELETE /v2/humans/{id}/areas (#252, #253) ------------------------------

type v2RemoveAreaResult struct {
	Name        string             `json:"name"`
	RemovedFrom []int              `json:"removed_from"`
	Broadened   map[string][]int64 `json:"broadened"`
	DryRun      bool               `json:"dry_run"`
}

func removeArea(t *testing.T, r *gin.Engine, query string) (int, v2RemoveAreaResult) {
	t.Helper()
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/u1/areas?"+query, "")
	var got v2RemoveAreaResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
		}
		if !json.Valid(w.Body.Bytes()) || !bytes.Contains(w.Body.Bytes(), []byte(`"broadened":{`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"removed_from":[`)) {
			t.Errorf("removed_from/broadened must never be null: %s", w.Body.String())
		}
	}
	return w.Code, got
}

// #253: drop one fence from every profile, leaving each profile's other
// areas intact — what ?all_profiles= on POST cannot do (#252).
func TestV2Humans_RemoveArea_AllProfiles(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"alpha", "private"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha", "private"}})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Area: []string{"beta", "private"}})

	code, got := removeArea(t, r, "name=private&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if !slices.Equal(got.RemovedFrom, []int{1, 2}) {
		t.Errorf("removed_from = %v, want [1 2]", got.RemovedFrom)
	}
	ps, _ := humans.GetProfiles("u1")
	for _, p := range ps {
		if slices.Contains(p.Area, "private") || len(p.Area) != 1 {
			t.Errorf("profile %d = %v, want one surviving area", p.ProfileNo, p.Area)
		}
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a state reload")
	}
}

// No permission check: removing can't widen alerts, and the fence is often
// already gone from the geofence feed.
func TestV2Humans_RemoveArea_UnknownFenceStillRemoved(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"deleted_fence"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"deleted_fence"}})

	code, got := removeArea(t, r, "name=deleted_fence")
	if code != http.StatusOK || !slices.Equal(got.RemovedFrom, []int{1}) {
		t.Fatalf("code=%d removed_from=%v, want 200 [1]", code, got.RemovedFrom)
	}
}

func TestV2Humans_RemoveArea_NotHeldIsIdempotent(t *testing.T) {
	r, humans, reloads := newV2HumansTestAPI(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	code, got := removeArea(t, r, "name=beta")
	if code != http.StatusOK || len(got.RemovedFrom) != 0 || got.RemovedFrom == nil {
		t.Fatalf("code=%d removed_from=%#v, want 200 and an empty (non-null) list", code, got.RemovedFrom)
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Error("nothing changed, so no reload expected")
	}
}

func TestV2Humans_RemoveArea_Validation(t *testing.T) {
	r, _, _ := newV2HumansTestAPI(t, nil)
	if c, _ := removeArea(t, r, "name=a&profile=2&all_profiles=true"); c != http.StatusUnprocessableEntity {
		t.Errorf("profile+all_profiles: got %d, want 422", c)
	}
	if c, _ := removeArea(t, r, ""); c != http.StatusUnprocessableEntity {
		t.Errorf("missing name: got %d, want 422", c)
	}
	w := v2DoReq(t, r, http.MethodDelete, "/api/v2/humans/nobody/areas?name=a", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown human: got %d, want 404", w.Code)
	}
}

func seedOverrideRules(mons *store.MockTrackingStore[db.MonsterTrackingAPI], raids *store.MockTrackingStore[db.RaidTrackingAPI]) {
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private", "alpha"}})
	_, _ = raids.Insert(&db.RaidTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})
}

func TestV2Humans_RemoveArea_StripsOverridesAndReportsBroadened(t *testing.T) {
	r, humans, reloads, mons, raids := newV2HumansTestAPIRules(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	seedOverrideRules(mons, raids)

	code, got := removeArea(t, r, "name=private")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if len(got.Broadened) != 2 || !slices.Equal(got.Broadened["pokemon"], []int64{1}) || !slices.Equal(got.Broadened["raid"], []int64{1}) {
		t.Errorf("broadened = %v, want pokemon:[1] raid:[1]", got.Broadened)
	}
	rows := mons.AllRows()
	if rows[0].OverrideAreas != nil {
		t.Errorf("rule 1 override = %v, want nil", rows[0].OverrideAreas)
	}
	if !slices.Equal(rows[1].OverrideAreas, []string{"alpha"}) {
		t.Errorf("rule 2 override = %v, want [alpha]", rows[1].OverrideAreas)
	}
	if got := raids.AllRows()[0].OverrideAreas; got != nil {
		t.Errorf("raid override = %v, want nil", got)
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a reload")
	}
}

func TestV2Humans_RemoveArea_DryRunWritesNothing(t *testing.T) {
	r, humans, reloads, mons, raids := newV2HumansTestAPIRules(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"private"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	seedOverrideRules(mons, raids)

	code, got := removeArea(t, r, "name=private&dry_run=true")
	if code != http.StatusOK || !got.DryRun {
		t.Fatalf("code=%d dry_run=%v", code, got.DryRun)
	}
	if !slices.Equal(got.RemovedFrom, []int{1}) || !slices.Equal(got.Broadened["pokemon"], []int64{1}) {
		t.Errorf("dry run result = %+v", got)
	}
	if ps, _ := humans.GetProfiles("u1"); !slices.Equal(ps[0].Area, []string{"private"}) {
		t.Errorf("profile written: %v", ps[0].Area)
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"private"}) {
		t.Errorf("humans.area written: %v", hh.Area)
	}
	if !slices.Equal(mons.AllRows()[1].OverrideAreas, []string{"private", "alpha"}) {
		t.Error("override written")
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Error("dry run must not reload")
	}
}

func TestV2Humans_RemoveArea_SpellingVariants(t *testing.T) {
	for _, q := range []string{"Private", "PRIVATE", "private"} {
		r, humans, _ := newV2HumansTestAPI(t, nil)
		humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private", "alpha"}})
		if code, got := removeArea(t, r, "name="+q); code != http.StatusOK || !slices.Equal(got.RemovedFrom, []int{1}) {
			t.Errorf("%s: code=%d removed_from=%v", q, code, got.RemovedFrom)
		}
	}
	for _, q := range []string{"Gent_Centrum", "gent%20centrum", "GENT+CENTRUM"} {
		r, humans, _ := newV2HumansTestAPI(t, nil)
		humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"gent_centrum", "alpha"}})
		if code, got := removeArea(t, r, "name="+q); code != http.StatusOK || !slices.Equal(got.RemovedFrom, []int{1}) {
			t.Errorf("%s: code=%d removed_from=%v", q, code, got.RemovedFrom)
		}
	}
}

// A real write that touches neither a profile list nor broadens a rule (an
// override shrinks but keeps another area) must still reload; dry run must not.
func TestV2Humans_RemoveArea_PartialOverrideStillReloads(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"alpha"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private", "alpha"}})

	code, got := removeArea(t, r, "name=private&dry_run=true")
	if code != http.StatusOK || len(got.RemovedFrom) != 0 || len(got.Broadened) != 0 {
		t.Fatalf("dry run: code=%d %+v", code, got)
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Fatal("dry run must not reload")
	}
	if code, _ = removeArea(t, r, "name=private"); code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !slices.Equal(mons.AllRows()[0].OverrideAreas, []string{"alpha"}) {
		t.Errorf("override = %v", mons.AllRows()[0].OverrideAreas)
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("a written override must trigger a reload")
	}
}

// --- PATCH /v2/humans/{id}/areas (#259) -------------------------------------

type v2RenameAreaResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	RenamedIn []int  `json:"renamed_in"`
	DryRun    bool   `json:"dry_run"`
}

func renameArea(t *testing.T, r *gin.Engine, query string) (int, v2RenameAreaResult) {
	t.Helper()
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/areas?"+query, "")
	var got v2RenameAreaResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v; raw: %s", err, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(`"renamed_in":[`)) {
			t.Errorf("renamed_in must never be null: %s", w.Body.String())
		}
	}
	return w.Code, got
}

func TestV2Humans_RenameArea_AllProfiles(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private", "alpha"}})
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Area: []string{"private"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})

	code, got := renameArea(t, r, "from=private&to=Gent_Centrum&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if got.To != "gent_centrum" || !slices.Equal(got.RenamedIn, []int{1, 2}) {
		t.Errorf("result = %+v", got)
	}
	ps, _ := humans.GetProfiles("u1")
	if !slices.Equal(ps[0].Area, []string{"gent_centrum", "alpha"}) || !slices.Equal(ps[1].Area, []string{"gent_centrum"}) {
		t.Errorf("profiles = %v / %v", ps[0].Area, ps[1].Area)
	}
	if o := mons.AllRows()[0].OverrideAreas; !slices.Equal(o, []string{"gent_centrum"}) {
		t.Errorf("override = %v", o)
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a reload")
	}
}

func TestV2Humans_RenameArea_ToNotLoadedIs422AndChangesNothing(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})

	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/areas?from=private&to=nowhere", "")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "area not permitted: nowhere") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if ps, _ := humans.GetProfiles("u1"); !slices.Equal(ps[0].Area, []string{"private"}) {
		t.Errorf("profile changed: %v", ps[0].Area)
	}
	if !slices.Equal(mons.AllRows()[0].OverrideAreas, []string{"private"}) {
		t.Error("override changed")
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Error("must not reload")
	}
}

func TestV2Humans_RenameArea_Idempotent(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	if code, got := renameArea(t, r, "from=private&to=gent_centrum"); code != http.StatusOK || !slices.Equal(got.RenamedIn, []int{1}) {
		t.Fatalf("first: code=%d %+v", code, got)
	}
	code, got := renameArea(t, r, "from=private&to=gent_centrum")
	if code != http.StatusOK || len(got.RenamedIn) != 0 {
		t.Errorf("second: code=%d %+v", code, got)
	}
}

func TestV2Humans_RenameArea_HoldsBothCollapses(t *testing.T) {
	r, humans, _, mons, _ := newV2HumansTestAPIRules(t, nil)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"gent_centrum", "private"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"gent centrum", "private"}})

	if code, _ := renameArea(t, r, "from=private&to=Gent_Centrum"); code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if ps, _ := humans.GetProfiles("u1"); !slices.Equal(ps[0].Area, []string{"gent_centrum"}) {
		t.Errorf("profile = %v", ps[0].Area)
	}
	if o := mons.AllRows()[0].OverrideAreas; !slices.Equal(o, []string{"gent_centrum"}) {
		t.Errorf("override = %v", o)
	}
}

func TestV2Humans_RenameArea_DryRunWritesNothing(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"private"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 1, Area: []string{"private"}})
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})

	code, got := renameArea(t, r, "from=private&to=gent_centrum&dry_run=true")
	if code != http.StatusOK || !got.DryRun || !slices.Equal(got.RenamedIn, []int{1}) {
		t.Fatalf("code=%d %+v", code, got)
	}
	if ps, _ := humans.GetProfiles("u1"); !slices.Equal(ps[0].Area, []string{"private"}) {
		t.Errorf("profile written: %v", ps[0].Area)
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"private"}) {
		t.Errorf("humans.area written: %v", hh.Area)
	}
	if !slices.Equal(mons.AllRows()[0].OverrideAreas, []string{"private"}) {
		t.Error("override written")
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Error("dry run must not reload")
	}
}

// A legacy (PoracleJS-migrated) human can have no profiles rows at all.
// ?all_profiles=true must still cover the current profile: humans.area and
// that profile's override_areas.
func TestV2Humans_RemoveArea_AllProfilesNoProfileRows(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"alpha", "private"}
	humans.AddHuman(h)
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private", "alpha"}})

	// The current profile has no profiles row; its humans.area still changes,
	// so it is reported, under dry_run too.
	code, got := removeArea(t, r, "name=private&all_profiles=true&dry_run=true")
	if code != http.StatusOK || !slices.Equal(got.RemovedFrom, []int{1}) {
		t.Fatalf("dry run: code=%d removed_from=%v, want [1]", code, got.RemovedFrom)
	}
	code, got = removeArea(t, r, "name=private&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !slices.Equal(got.RemovedFrom, []int{1}) {
		t.Errorf("removed_from = %v, want [1]", got.RemovedFrom)
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"alpha"}) {
		t.Errorf("humans.area = %v, want [alpha]", hh.Area)
	}
	if o := mons.AllRows()[0].OverrideAreas; !slices.Equal(o, []string{"alpha"}) {
		t.Errorf("override = %v, want [alpha]", o)
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a reload")
	}
}

// all_profiles targets the same profiles the all_profiles snapshot shows:
// here the current profile (1) has rules but no profiles row, and profile 2
// has a row. Building the targets from profiles rows alone skipped profile 1,
// so its humans.area and override_areas kept the removed name.
func TestV2Humans_RemoveArea_AllProfilesRowlessCurrentProfile(t *testing.T) {
	r, humans, _, mons, _ := newV2HumansTestAPIRules(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"alpha", "private"}
	humans.AddHuman(h)
	humans.SeedProfile(store.Profile{ID: "u1", ProfileNo: 2, Area: []string{"beta", "private"}})
	humans.RuleProfiles = map[string][]int{"u1": {1}}
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private", "alpha"}})

	code, got := removeArea(t, r, "name=private&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !slices.Equal(got.RemovedFrom, []int{1, 2}) {
		t.Errorf("removed_from = %v, want [1 2]", got.RemovedFrom)
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"alpha"}) {
		t.Errorf("humans.area = %v, want [alpha]", hh.Area)
	}
	if o := mons.AllRows()[0].OverrideAreas; !slices.Equal(o, []string{"alpha"}) {
		t.Errorf("profile 1 override = %v, want [alpha]", o)
	}
}

func TestV2Humans_RenameArea_AllProfilesNoProfileRows(t *testing.T) {
	r, humans, reloads, mons, _ := newV2HumansTestAPIRules(t, nil)
	h, _ := humans.Get("u1")
	h.Area = []string{"private", "alpha"}
	humans.AddHuman(h)
	_, _ = mons.Insert(&db.MonsterTrackingAPI{ID: "u1", ProfileNo: 1, OverrideAreas: []string{"private"}})

	code, got := renameArea(t, r, "from=private&to=Gent_Centrum&all_profiles=true&dry_run=true")
	if code != http.StatusOK || !slices.Equal(got.RenamedIn, []int{1}) {
		t.Fatalf("dry run: code=%d renamed_in=%v, want [1]", code, got.RenamedIn)
	}
	code, got = renameArea(t, r, "from=private&to=Gent_Centrum&all_profiles=true")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if !slices.Equal(got.RenamedIn, []int{1}) {
		t.Errorf("renamed_in = %v, want [1]", got.RenamedIn)
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"gent_centrum", "alpha"}) {
		t.Errorf("humans.area = %v, want [gent_centrum alpha]", hh.Area)
	}
	if o := mons.AllRows()[0].OverrideAreas; !slices.Equal(o, []string{"gent_centrum"}) {
		t.Errorf("override = %v, want [gent_centrum]", o)
	}
	if atomic.LoadInt32(reloads) == 0 {
		t.Error("expected a reload")
	}
}

// Set-areas shares the target resolution: all_profiles on a human with no
// profiles rows still writes humans.area.
func TestV2Humans_SetAreas_AllProfilesNoProfileRows(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, nil)
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas?all_profiles=true", `{"areas":["beta"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if hh, _ := humans.Get("u1"); !slices.Equal(hh.Area, []string{"beta"}) {
		t.Errorf("humans.area = %v, want [beta]", hh.Area)
	}
}
