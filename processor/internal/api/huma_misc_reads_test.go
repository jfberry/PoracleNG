package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// stateWithFences builds a *state.Manager whose snapshot holds the given fences.
func stateWithFences(fences []geofence.Fence) *state.Manager {
	mgr := state.NewManager()
	mgr.Set(&state.State{Fences: fences})
	return mgr
}

func TestHumaGeofenceAll_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	mgr := stateWithFences([]geofence.Fence{{Name: "alpha", ID: 7}})
	RegisterGeofenceAll(humaAPI, mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/all", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/geofence/all = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	arr, ok := got["geofence"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("geofence = %v, want 1-element array", got["geofence"])
	}
	first := arr[0].(map[string]any)
	if first["name"] != "alpha" {
		t.Errorf("geofence[0].name = %v, want alpha", first["name"])
	}
}

func TestHumaGeofenceHash_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	mgr := stateWithFences([]geofence.Fence{{Name: "alpha", Path: [][2]float64{{1, 2}, {3, 4}}}})
	RegisterGeofenceHash(humaAPI, mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/all/hash", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/geofence/all/hash = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	areas, ok := got["areas"].(map[string]any)
	if !ok {
		t.Fatalf("areas = %v, want object", got["areas"])
	}
	hash, ok := areas["alpha"].(string)
	if !ok || len(hash) != 32 {
		t.Errorf("areas.alpha = %v, want 32-char md5 hex", areas["alpha"])
	}
}

func TestHumaGeofenceGeoJSON_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	mgr := stateWithFences([]geofence.Fence{{Name: "alpha", Path: [][2]float64{{1, 2}, {3, 4}, {5, 6}}}})
	RegisterGeofenceGeoJSON(humaAPI, mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/all/geojson", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/geofence/all/geojson = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	geo, ok := got["geoJSON"].(map[string]any)
	if !ok {
		t.Fatalf("geoJSON = %v, want object", got["geoJSON"])
	}
	if geo["type"] != "FeatureCollection" {
		t.Errorf("geoJSON.type = %v, want FeatureCollection", geo["type"])
	}
	feats, ok := geo["features"].([]any)
	if !ok || len(feats) != 1 {
		t.Fatalf("features = %v, want 1-element array", geo["features"])
	}
}

func TestHumaConfigSchema_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterConfigSchema(humaAPI)

	req := httptest.NewRequest(http.MethodGet, "/api/config/schema", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config/schema = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	sections, ok := got["sections"].([]any)
	if !ok || len(sections) == 0 {
		t.Fatalf("sections = %v, want non-empty array", got["sections"])
	}
}

func TestHumaMasterdataMonsters_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, Types: []int{13}, Attack: 1, Defense: 2, Stamina: 3},
		},
	}
	RegisterMasterdataMonsters(humaAPI, gd, i18n.NewBundle())

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/monsters", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/monsters = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	entry, ok := got["25_0"].(map[string]any)
	if !ok {
		t.Fatalf("body[25_0] = %v, want object; full: %v", got["25_0"], got)
	}
	if entry["id"] != float64(25) {
		t.Errorf("body[25_0].id = %v, want 25", entry["id"])
	}
	stats := entry["stats"].(map[string]any)
	if stats["baseAttack"] != float64(1) {
		t.Errorf("stats.baseAttack = %v, want 1", stats["baseAttack"])
	}
}

func TestHumaMasterdataMonsters_NilGameData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterMasterdataMonsters(humaAPI, nil, i18n.NewBundle())

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/monsters", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/monsters (nil gd) = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var arr []any
	if err := json.NewDecoder(w.Body).Decode(&arr); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	if len(arr) != 0 {
		t.Errorf("body = %v, want empty array", arr)
	}
}

func TestHumaMasterdataGrunts_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	// nil game data yields an empty map — still a valid 200 JSON object.
	RegisterMasterdataGrunts(humaAPI, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/masterdata/grunts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/masterdata/grunts = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if len(got) != 0 {
		t.Errorf("body = %v, want empty object", got)
	}
}

// fakeSnapStore is a minimal SnapshotReader returning a fixed snapshot or error.
type fakeSnapStore struct {
	gotKey string
	out    *snapshots.Snapshot
	err    error
}

func (f *fakeSnapStore) Read(_ context.Context, key string) (*snapshots.Snapshot, error) {
	f.gotKey = key
	return f.out, f.err
}

func TestHumaSnapshotGet_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	fs := &fakeSnapStore{out: &snapshots.Snapshot{MessageID: "m1", Target: "t1", AlertType: "raid"}}
	RegisterSnapshotGet(humaAPI, fs)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/m1?target=t1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/snapshots/m1?target=t1 = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if fs.gotKey != "t1:m1" {
		t.Errorf("store got key %q, want t1:m1", fs.gotKey)
	}
	got := decodeBody(t, w)
	if got["alertType"] != "raid" {
		t.Errorf("body.alertType = %v, want raid", got["alertType"])
	}
}

func TestHumaSnapshotGet_MissingTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterSnapshotGet(humaAPI, &fakeSnapStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/m1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("GET /api/snapshots/m1 (no target) = %d, want 422; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaSnapshotGet_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterSnapshotGet(humaAPI, &fakeSnapStore{err: snapshots.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/m1?target=t1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /api/snapshots/m1?target=t1 (miss) = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("problem+json status must be a JSON number; full: %v", got)
	}
}

func TestHumaSnapshotGet_StoreDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	// A nil snapshots.Store passed through the SnapshotReader interface — the
	// typed-nil guard must surface 503 (snapshots disabled).
	var disabled snapshots.Store
	RegisterSnapshotGet(humaAPI, disabled)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots/m1?target=t1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/snapshots/m1?target=t1 (disabled) = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}
