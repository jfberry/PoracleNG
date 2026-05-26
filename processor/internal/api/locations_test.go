package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func newTestLocationDeps(t *testing.T) (*TrackingDeps, *store.MockHumanStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mock := store.NewMockHumanStore()
	mock.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})
	deps := &TrackingDeps{
		Humans: mock,
		Config: &config.Config{},
	}
	return deps, mock
}

func TestLocations_List(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	if err := mock.SetLocation("u1", 0, 51.5, -0.1); err != nil {
		t.Fatalf("SetLocation: %v", err)
	}
	if _, err := mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}

	r := gin.New()
	r.GET("/api/humans/:id/locations", HandleListLocations(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Locations struct {
			Default *struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"default"`
			Named []struct {
				Label string `json:"label"`
			} `json:"named"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Locations.Default == nil || resp.Locations.Default.Latitude != 51.5 {
		t.Fatalf("default missing: %+v", resp.Locations.Default)
	}
	if len(resp.Locations.Named) != 1 || resp.Locations.Named[0].Label != "Home" {
		t.Fatalf("named missing: %+v", resp.Locations.Named)
	}
}

func TestLocations_GetOne_CaseInsensitive(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	if _, err := mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1}); err != nil {
		t.Fatalf("AddLocation: %v", err)
	}

	r := gin.New()
	r.GET("/api/humans/:id/locations/:label", HandleGetLocation(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations/home", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Home") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLocations_GetOne_NotFound(t *testing.T) {
	deps, _ := newTestLocationDeps(t)

	r := gin.New()
	r.GET("/api/humans/:id/locations/:label", HandleGetLocation(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/humans/u1/locations/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestLocations_Add_Single(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `{"label":"Home","latitude":51.5,"longitude":-0.1}`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got == nil {
		t.Fatalf("location not persisted")
	}
}

func TestLocations_Add_Array(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `[{"label":"Home","latitude":51.5,"longitude":-0.1},{"label":"Work","latitude":51.6,"longitude":-0.2}]`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	list, _ := mock.ListLocations("u1")
	if len(list) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(list))
	}
}

func TestLocations_Delete_Success(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.POST("/api/humans/:id/locations/:label/delete", HandleDeleteLocation(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/Home/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got != nil {
		t.Fatalf("location should be deleted")
	}
}

func TestLocations_Delete_Conflict(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})
	mock.LocationRefs = map[string][]store.ReferencingRule{
		"u1|home": {{Type: "pokemon", UID: 42}},
	}

	r := gin.New()
	r.POST("/api/humans/:id/locations/:label/delete", HandleDeleteLocation(deps))

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/Home/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "referencing_rules") || !strings.Contains(w.Body.String(), "pokemon") {
		t.Fatalf("response should include referencing rules: %s", w.Body.String())
	}
	got, _ := mock.GetLocation("u1", "Home")
	if got == nil {
		t.Fatalf("location should NOT have been deleted")
	}
}

func TestLocations_Add_DuplicateInResults(t *testing.T) {
	deps, mock := newTestLocationDeps(t)
	mock.AddLocation(store.UserLocation{ID: "u1", Label: "Home", Latitude: 51.5, Longitude: -0.1})

	r := gin.New()
	r.POST("/api/humans/:id/locations/add", HandleAddLocation(deps))

	body := `{"label":"Home","latitude":0,"longitude":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/locations/add", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK { // batch endpoint reports per-row results
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "duplicate") {
		t.Fatalf("expected duplicate report in body, got %s", w.Body.String())
	}
}
