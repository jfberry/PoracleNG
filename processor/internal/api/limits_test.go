package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// [tracking] max_distance and max_areas on the API. Admins, non-user targets
// (channels, webhooks, ...) and v2 trusted requests are exempt.

func limitsCfg(maxDistance, maxAreas int, admins ...string) *config.Config {
	return &config.Config{
		Tracking: config.TrackingConfig{MaxDistance: maxDistance, MaxAreas: maxAreas},
		Discord:  config.DiscordConfig{Admins: admins},
	}
}

// newV1LimitsRouter serves v1 lure tracking and setAreas for human u1.
func newV1LimitsRouter(t *testing.T, cfg *config.Config, area []string) (*gin.Engine, *store.MockHumanStore, *store.MockTrackingStore[db.LureTrackingAPI]) {
	t.Helper()
	return newV1LimitsRouterType(t, cfg, area, "discord:user")
}

func newV1LimitsRouterType(t *testing.T, cfg *config.Config, area []string, humanType string) (*gin.Engine, *store.MockHumanStore, *store.MockTrackingStore[db.LureTrackingAPI]) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	humans := store.NewMockHumanStore()
	humans.AddHuman(&store.Human{ID: "u1", Type: humanType, Name: "User", Enabled: true,
		Language: "en", CurrentProfileNo: 1, Area: area})
	lures := store.NewMockTrackingStore(store.LureGetUID, store.LureSetUID)
	fences := []geofence.Fence{
		humanSquareFence("alpha", 10, 10, 1),
		humanSquareFence("beta", 20, 20, 1),
		humanSquareFence("gamma", 30, 30, 1),
	}
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences, Geofence: geofence.NewSpatialIndex(fences)})
	deps := &TrackingDeps{
		Humans:       humans,
		StateMgr:     mgr,
		Tracking:     &store.TrackingStores{Lures: lures},
		Config:       cfg,
		RowText:      &rowtext.Generator{DefaultTemplateName: "1"},
		Translations: i18n.NewBundle(),
	}
	r := gin.New()
	r.POST("/api/tracking/lure/:id", HandleCreateLure(deps))
	r.POST("/api/humans/:id/setAreas", HandleSetAreas(deps))
	r.POST("/api/humans", HandleCreateHuman(deps))
	return r, humans, lures
}

func v1Post(r http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestV1_DistanceClampedToMaxDistance(t *testing.T) {
	r, _, lures := newV1LimitsRouter(t, limitsCfg(1000, 0), nil)
	w := v1Post(r, "/api/tracking/lure/u1?silent=true", `[{"lure_id":501,"distance":5000}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	if rows := lures.AllRows(); len(rows) != 1 || rows[0].Distance != 1000 {
		t.Fatalf("distance should be clamped to 1000, got %+v", rows)
	}
}

func TestV1_DistanceAdminExempt(t *testing.T) {
	r, _, lures := newV1LimitsRouter(t, limitsCfg(1000, 0, "u1"), nil)
	w := v1Post(r, "/api/tracking/lure/u1?silent=true", `[{"lure_id":501,"distance":5000}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	if rows := lures.AllRows(); len(rows) != 1 || rows[0].Distance != 5000 {
		t.Fatalf("admin distance should be kept, got %+v", rows)
	}
}

func TestV1_OverrideAreasOverMaxAreas(t *testing.T) {
	r, _, lures := newV1LimitsRouter(t, limitsCfg(0, 2), nil)
	w := v1Post(r, "/api/tracking/lure/u1?silent=true", `[{"lure_id":501,"override_areas":["alpha","beta","gamma"]}]`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "maximum of 2 areas") {
		t.Fatalf("expected 400 max areas, got %d %s", w.Code, w.Body.String())
	}
	if len(lures.AllRows()) != 0 {
		t.Fatal("nothing should be stored")
	}
}

func TestV1_SetAreasOverMaxAreas(t *testing.T) {
	r, humans, _ := newV1LimitsRouter(t, limitsCfg(0, 2), nil)
	w := v1Post(r, "/api/humans/u1/setAreas", `["alpha","beta","gamma"]`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 0 {
		t.Fatalf("areas should be unchanged, got %v", h.Area)
	}
}

func TestV1_SetAreasGrandfatheredShrink(t *testing.T) {
	r, humans, _ := newV1LimitsRouter(t, limitsCfg(0, 1), []string{"alpha", "beta", "gamma"})
	w := v1Post(r, "/api/humans/u1/setAreas", `["alpha","beta"]`)
	if w.Code != http.StatusOK {
		t.Fatalf("shrinking an over-limit list should be allowed, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 2 {
		t.Fatalf("expected 2 areas, got %v", h.Area)
	}
}

func TestV1_SetAreasAdminExempt(t *testing.T) {
	r, humans, _ := newV1LimitsRouter(t, limitsCfg(0, 1, "u1"), nil)
	w := v1Post(r, "/api/humans/u1/setAreas", `["alpha","beta","gamma"]`)
	if w.Code != http.StatusOK {
		t.Fatalf("admin should be exempt, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 3 {
		t.Fatalf("expected 3 areas, got %v", h.Area)
	}
}

// --- v2 ---------------------------------------------------------------------

func TestV2_SetAreasOverMaxAreas(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, limitsCfg(0, 2))
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{"areas":["alpha","beta","gamma"]}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "maximum of 2 areas") {
		t.Fatalf("expected 422 max areas, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 0 {
		t.Fatalf("areas should be unchanged, got %v", h.Area)
	}
}

func TestV2_SetAreasTrustedExempt(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, limitsCfg(0, 2))
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{"areas":["alpha","beta","gamma"],"trusted":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("trusted should be exempt, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("u1"); len(h.Area) != 3 {
		t.Fatalf("expected 3 areas, got %v", h.Area)
	}
}

func TestV2_SetAreasGrandfathered(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, limitsCfg(0, 1))
	if err := humans.SetArea("u1", 1, []string{"alpha", "beta", "gamma"}); err != nil {
		t.Fatal(err)
	}
	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{"areas":["gamma","alpha"]}`); w.Code != http.StatusOK {
		t.Fatalf("shrinking should be allowed, got %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/areas", `{"areas":["gamma","beta"]}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("adding beta back over the limit should be refused, got %d %s", w.Code, w.Body.String())
	}
}

// newV2LimitsPokemonAPI is the v2 pokemon API for limit tests.
func newV2LimitsPokemonAPI(t *testing.T, cfg *config.Config) (*gin.Engine, *store.MockTrackingStore[db.MonsterTrackingAPI], func()) {
	t.Helper()
	r, ms, _, restore := newV2PokemonTestAPIWithConfig(t, cfg, nil)
	return r, ms, restore
}

func TestV2_OverrideAreasOverMaxAreas(t *testing.T) {
	r, ms, restore := newV2LimitsPokemonAPI(t, limitsCfg(0, 1))
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true",
		`[{"pokemon_id":25,"override_areas":["alpha","beta"]}]`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "maximum of 1 areas") {
		t.Fatalf("expected 422 max areas, got %d %s", w.Code, w.Body.String())
	}
	if len(ms.AllRows()) != 0 {
		t.Fatal("nothing should be stored")
	}
}

func TestV2_TrustedExemptFromBothLimits(t *testing.T) {
	r, ms, restore := newV2LimitsPokemonAPI(t, limitsCfg(1000, 1))
	defer restore()
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true&trusted=true",
		`[{"pokemon_id":25,"override_areas":["alpha","beta"]},{"pokemon_id":26,"distance":5000}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("trusted should be exempt, got %d %s", w.Code, w.Body.String())
	}
	for _, row := range ms.AllRows() {
		if row.PokemonID == 26 && row.Distance != 5000 {
			t.Errorf("trusted distance should be kept, got %d", row.Distance)
		}
	}
}

func TestV2_DistanceClampedUnlessAdmin(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  *config.Config
		want int
	}{
		{"user", limitsCfg(1000, 0), 1000},
		{"admin", limitsCfg(1000, 0, "u1"), 5000},
	} {
		r, ms, restore := newV2LimitsPokemonAPI(t, c.cfg)
		w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true",
			`[{"pokemon_id":25,"distance":5000}]`)
		restore()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: POST %d %s", c.name, w.Code, w.Body.String())
		}
		if rows := ms.AllRows(); len(rows) != 1 || rows[0].Distance != c.want {
			t.Errorf("%s: distance = %+v, want %d", c.name, rows, c.want)
		}
	}
}

// A rule stored over the limit (limit lowered later) can still be edited.
func TestV2_PatchGrandfatheredOverrideAreas(t *testing.T) {
	r, ms, restore := newV2LimitsPokemonAPI(t, limitsCfg(0, 1))
	defer restore()
	// Stored through a trusted write, which the limit does not apply to.
	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true&trusted=true",
		`[{"pokemon_id":25,"override_areas":["alpha","beta"]}]`); w.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	rows := ms.AllRows()
	if len(rows) != 1 {
		t.Fatalf("seed rows: %+v", rows)
	}
	uid := rows[0].UID
	w := v2DoReq(t, r, http.MethodPatch, "/api/v2/humans/u1/tracking/pokemon?silent=true",
		`{"uids":[`+strconv.FormatInt(uid, 10)+`],"set":{"clean":true}}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"updated"`) || strings.Contains(w.Body.String(), "maximum") {
		t.Fatalf("grandfathered PATCH should succeed, got %d %s", w.Code, w.Body.String())
	}
}

func TestV1_DistanceChannelTargetExempt(t *testing.T) {
	r, _, lures := newV1LimitsRouterType(t, limitsCfg(1000, 0), nil, "discord:channel")
	w := v1Post(r, "/api/tracking/lure/u1?silent=true", `[{"lure_id":501,"distance":5000}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	if rows := lures.AllRows(); len(rows) != 1 || rows[0].Distance != 5000 {
		t.Fatalf("channel distance should be kept, got %+v", rows)
	}
}

func TestV1_CreateHumanOverMaxAreas(t *testing.T) {
	r, humans, _ := newV1LimitsRouter(t, limitsCfg(0, 2), nil)
	w := v1Post(r, "/api/humans", `{"id":"new1","name":"New","area":"[\"alpha\",\"beta\",\"gamma\"]"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("new1"); h != nil {
		t.Fatal("human should not be created")
	}
}

func TestV2_CreateHumanOverMaxAreas(t *testing.T) {
	r, humans, _ := newV2HumansTestAPI(t, limitsCfg(0, 2))
	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans",
		`{"id":"new1","name":"New","area":["alpha","beta","gamma"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d %s", w.Code, w.Body.String())
	}
	if h, _ := humans.Get("new1"); h != nil {
		t.Fatal("human should not be created")
	}
}

// A stored over-limit rule may be resubmitted or edited with the same (or a
// smaller) list, but its list cannot be copied into a new rule or grown.
func TestV2_OverLimitListCannotBeCopiedOrGrown(t *testing.T) {
	r, ms, restore := newV2LimitsPokemonAPI(t, limitsCfg(0, 1))
	defer restore()
	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true&trusted=true",
		`[{"pokemon_id":25,"override_areas":["alpha","beta"]}]`); w.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	uid := strconv.FormatInt(ms.AllRows()[0].UID, 10)

	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true",
		`[{"pokemon_id":25,"override_areas":["alpha","beta"]}]`); w.Code != http.StatusOK {
		t.Errorf("resubmitting the stored rule: %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon?silent=true",
		`[{"pokemon_id":26,"override_areas":["alpha","beta"]}]`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("copying the list into a new rule: want 422, got %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/pokemon/"+uid+"?silent=true",
		`{"pokemon_id":25,"min_iv":90,"override_areas":["beta","alpha"]}`); w.Code != http.StatusOK {
		t.Errorf("PUT keeping the list: %d %s", w.Code, w.Body.String())
	}
	if w := v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/pokemon/"+uid+"?silent=true",
		`{"pokemon_id":25,"override_areas":["alpha","beta","gent_centrum"]}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("PUT growing the list: want 422, got %d %s", w.Code, w.Body.String())
	}
	if n := len(ms.AllRows()); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}
