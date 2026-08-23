package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// newFeaturesTestAPI builds a gin engine with a fresh huma API mounted on /api.
func newFeaturesTestAPI(t *testing.T) (*gin.Engine, huma.API) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r, NewHumaAPI(r, r.Group("/api"), "test")
}

func summaryHumaDeps() (*SummaryDeps, *store.MockSummaryScheduleStore, *int32) {
	mock := store.NewMockSummaryScheduleStore()
	var triggered int32
	deps := &SummaryDeps{
		Schedules: mock,
		Dispatch:  func(_, _ string) { atomic.AddInt32(&triggered, 1) },
	}
	return deps, mock, &triggered
}

func TestHumaSummaryGet_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, mock, _ := summaryHumaDeps()
	mock.Seed(store.SummarySchedule{ID: "u1", AlertType: "quest", ActiveHours: `[{"day":1,"hours":7,"mins":30}]`})
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Status   string `json:"status"`
		Schedule struct {
			ID          string `json:"id"`
			AlertType   string `json:"alert_type"`
			ActiveHours []struct {
				Day   int `json:"day"`
				Hours int `json:"hours"`
				Mins  int `json:"mins"`
			} `json:"active_hours"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if body.Status != "ok" || body.Schedule.ID != "u1" || body.Schedule.AlertType != "quest" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	// active_hours is now a typed JSON array, not a raw blob.
	if len(body.Schedule.ActiveHours) != 1 {
		t.Fatalf("want 1 active_hours entry, got %d: %s", len(body.Schedule.ActiveHours), w.Body.String())
	}
	if e := body.Schedule.ActiveHours[0]; e.Day != 1 || e.Hours != 7 || e.Mins != 30 {
		t.Fatalf("active_hours[0] = %+v, want {day:1 hours:7 mins:30}", e)
	}
}

func TestHumaSummaryGet_Missing404(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummaryGet_UnknownAlertType400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1/raid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummaryList_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, mock, _ := summaryHumaDeps()
	mock.Seed(store.SummarySchedule{ID: "u1", AlertType: "quest", ActiveHours: `[]`})
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Status    string `json:"status"`
		Schedules []struct {
			ID        string `json:"id"`
			AlertType string `json:"alert_type"`
		} `json:"schedules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Status != "ok" || len(body.Schedules) != 1 || body.Schedules[0].ID != "u1" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHumaSummaryDelete_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, mock, _ := summaryHumaDeps()
	mock.Seed(store.SummarySchedule{ID: "u1", AlertType: "quest", ActiveHours: `[]`})
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHumaSummaryTrigger_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, triggered := summaryHumaDeps()
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodPost, "/api/summaries/u1/quest/trigger", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(triggered) != 1 {
		t.Fatalf("expected dispatch to fire once, got %d", atomic.LoadInt32(triggered))
	}
}

func TestHumaSummaryGet_FeatureDisabled503(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps := &SummaryDeps{} // Schedules nil, Dispatch nil
	RegisterSummaries(api, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- summary upsert (POST set) ---

// postSummarySet is a small helper that issues the upsert request and returns
// the recorder.
func postSummarySet(t *testing.T, r *gin.Engine, id, alertType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/summaries/"+id+"/"+alertType, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHumaSummarySet_ArrayOK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, mock, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":[{"day":1,"hours":7,"mins":30}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	// Stored as the marshaled JSON array.
	got, err := mock.Get("u1", "quest")
	if err != nil || got == nil {
		t.Fatalf("Get after Set: got=%v err=%v", got, err)
	}
	if got.ActiveHours != `[{"day":1,"hours":7,"mins":30}]` {
		t.Fatalf("stored active_hours = %q", got.ActiveHours)
	}
}

func TestHumaSummarySet_StringEncodedOK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, mock, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	// active_hours is a JSON STRING containing a JSON-encoded array.
	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":"[{\"day\":2,\"hours\":9,\"mins\":0}]"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (string-encoded leniency), got %d body=%s", w.Code, w.Body.String())
	}
	got, err := mock.Get("u1", "quest")
	if err != nil || got == nil {
		t.Fatalf("Get after Set: got=%v err=%v", got, err)
	}
	// Stored verbatim (the string branch passes the value through).
	if got.ActiveHours != `[{"day":2,"hours":9,"mins":0}]` {
		t.Fatalf("stored active_hours = %q", got.ActiveHours)
	}
}

func TestHumaSummarySet_ZeroPaddedIntsOK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	// Zero-padded "00" ints must still parse via flexToInt.
	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":[{"day":"00","hours":"07","mins":"00"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (flex int leniency), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummarySet_EmptyArrayClearsOK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty array clears), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummarySet_MissingActiveHours400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "quest", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummarySet_InvalidActiveHours400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":"not-json"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummarySet_UnknownAlertType400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps, _, _ := summaryHumaDeps()
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "raid", `{"active_hours":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaSummarySet_FeatureDisabled503(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	deps := &SummaryDeps{} // Schedules nil
	RegisterSummarySet(api, deps)

	w := postSummarySet(t, r, "u1", "quest", `{"active_hours":[]}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- command endpoint ---

func TestHumaCommand_MissingFields400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterCommand(api, nil)

	body := bytes.NewBufferString(`{"text":"","user_id":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/command", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaCommand_NilParser500(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterCommand(api, nil) // deps nil → parser nil

	body := bytes.NewBufferString(`{"text":"!version","user_id":"123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/command", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}
