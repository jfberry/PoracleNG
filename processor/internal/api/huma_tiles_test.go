package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// stubTileGen is a test TileURLGenerator that records its last call and returns
// a fixed URL (or "" to simulate a tile-generation failure).
type stubTileGen struct {
	url        string
	lastType   string
	lastData   map[string]any
	calledWith string
}

func (s *stubTileGen) GetPregeneratedTileURL(maptype string, data map[string]any, staticMapType string) string {
	s.lastType = maptype
	s.lastData = data
	s.calledWith = staticMapType
	return s.url
}

// stubFences is a test FenceStateGetter returning a fixed fence list.
type stubFences struct{ fences []geofence.Fence }

func (s stubFences) Fences() []geofence.Fence { return s.fences }

// stubWeather is a test WeatherProvider returning a fixed weather ID.
type stubWeather struct{ id int }

func (s stubWeather) GetCurrentWeatherInCell(string) int { return s.id }

func newTileTestAPI(t *testing.T, deps HumaTileDeps) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")
	RegisterTileEndpoints(humaAPI, deps)
	return r
}

func squareFence(name string) geofence.Fence {
	return geofence.Fence{
		Name:           name,
		NormalizedName: name,
		Path:           [][2]float64{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {0, 0}},
	}
}

func assertStatusURL(t *testing.T, w *httptest.ResponseRecorder, wantURL string) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if got["url"] != wantURL {
		t.Errorf("url = %v, want %q", got["url"], wantURL)
	}
}

func TestHumaLocationMap_OK(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/location.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/locationMap/51.5/-0.12", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/location.png")
	if gen.lastType != "location" {
		t.Errorf("maptype = %q, want location", gen.lastType)
	}
	if gen.lastData["latitude"] != 51.5 || gen.lastData["longitude"] != -0.12 {
		t.Errorf("data lat/lon = %v/%v, want 51.5/-0.12", gen.lastData["latitude"], gen.lastData["longitude"])
	}
}

func TestHumaDistanceMap_OK(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/distance.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/distanceMap/51.5/-0.12/500", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/distance.png")
	if gen.lastType != "distance" {
		t.Errorf("maptype = %q, want distance", gen.lastType)
	}
	if gen.lastData["distance"] != float64(500) {
		t.Errorf("distance = %v, want 500", gen.lastData["distance"])
	}
}

func TestHumaWeatherMap_OK_QueryOverride(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/weather.png"}
	// Weather provider would return 3, but the query override (7) wins.
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}, Weather: stubWeather{id: 3}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/weatherMap/51.5/-0.12?weather=7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/weather.png")
	if gen.lastType != "weather" {
		t.Errorf("maptype = %q, want weather", gen.lastType)
	}
	if gen.lastData["gameplay_condition"] != 7 {
		t.Errorf("gameplay_condition = %v, want 7 (query override)", gen.lastData["gameplay_condition"])
	}
}

func TestHumaWeatherMap_OK_TrackerLookup(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/weather.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}, Weather: stubWeather{id: 4}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/weatherMap/51.5/-0.12", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/weather.png")
	if gen.lastData["gameplay_condition"] != 4 {
		t.Errorf("gameplay_condition = %v, want 4 (tracker lookup)", gen.lastData["gameplay_condition"])
	}
}

func TestHumaGeofenceAreaMap_OK(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/area.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{fences: []geofence.Fence{squareFence("downtown")}}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/downtown/map", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/area.png")
	if gen.lastType != "area" {
		t.Errorf("maptype = %q, want area", gen.lastType)
	}
}

func TestHumaGeofenceAreaMap_NotFound(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/area.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{fences: []geofence.Fence{squareFence("downtown")}}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/nowhere/map", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	// Problem+json: numeric status, not the legacy {"status":"error"} envelope.
	got := decodeBody(t, w)
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("status = %v (%T), want JSON number (problem+json)", got["status"], got["status"])
	}
}

func TestHumaOverviewMap_OK(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/overview.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{fences: []geofence.Fence{squareFence("a"), squareFence("b")}}})

	req := httptest.NewRequest(http.MethodPost, "/api/geofence/overviewMap",
		bytes.NewReader([]byte(`{"areas":["a","b"]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertStatusURL(t, w, "http://tiles/overview.png")
	if gen.lastType != "areaoverview" {
		t.Errorf("maptype = %q, want areaoverview", gen.lastType)
	}
}

func TestHumaOverviewMap_EmptyAreas(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/overview.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}})

	req := httptest.NewRequest(http.MethodPost, "/api/geofence/overviewMap",
		bytes.NewReader([]byte(`{"areas":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaOverviewMap_NoMatch(t *testing.T) {
	gen := &stubTileGen{url: "http://tiles/overview.png"}
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{fences: []geofence.Fence{squareFence("a")}}})

	req := httptest.NewRequest(http.MethodPost, "/api/geofence/overviewMap",
		bytes.NewReader([]byte(`{"areas":["zzz"]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestHumaTile_TileGenFailure asserts an empty URL from the generator maps to a
// problem+json 500, matching the legacy tileJSONOK empty-URL path.
func TestHumaTile_TileGenFailure(t *testing.T) {
	gen := &stubTileGen{url: ""} // simulate tile-gen failure
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: gen, StateMgr: stubFences{}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/locationMap/51.5/-0.12", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

// TestHumaTile_NilStaticMap asserts a nil generator yields 503, matching the
// legacy "static map provider not configured" guard.
func TestHumaTile_NilStaticMap(t *testing.T) {
	r := newTileTestAPI(t, HumaTileDeps{StaticMap: nil, StateMgr: stubFences{}})

	req := httptest.NewRequest(http.MethodGet, "/api/geofence/locationMap/51.5/-0.12", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}

// TestHumaTile_NoGinPanic_StaticParamCoexist mounts the tile routes alongside
// the already-migrated static geofence reads and the {area} param route to
// confirm no gin route-tree conflict panics at registration.
func TestHumaTile_NoGinPanic_StaticParamCoexist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	mgr := stateWithFences([]geofence.Fence{squareFence("downtown")})
	RegisterGeofenceAll(humaAPI, mgr)
	RegisterGeofenceHash(humaAPI, mgr)
	RegisterGeofenceGeoJSON(humaAPI, mgr)
	RegisterTileEndpoints(humaAPI, HumaTileDeps{
		StaticMap: &stubTileGen{url: "http://tiles/x.png"},
		StateMgr:  stubFences{fences: []geofence.Fence{squareFence("downtown")}},
	})

	// Static sibling and param route must both resolve.
	for _, p := range []string{"/api/geofence/all", "/api/geofence/locationMap/1/2", "/api/geofence/downtown/map"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("GET %s = 404 (route not registered)", p)
		}
	}
}
