package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/store"
)

// summaryRouter wires the five summary endpoints onto a fresh gin engine
// so route param parsing matches production. Returns the engine plus the
// mock-backed deps so tests can inspect calls / state.
func summaryRouter(t *testing.T) (*gin.Engine, *store.MockSummaryScheduleStore, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mock := store.NewMockSummaryScheduleStore()
	var triggered int32
	deps := &SummaryDeps{
		Schedules: mock,
		Dispatch:  func(id, alertType string) { atomic.AddInt32(&triggered, 1) },
	}
	r := gin.New()
	g := r.Group("/api/summaries")
	g.GET("/:id", HandleSummaryListForUser(deps))
	g.GET("/:id/:alertType", HandleSummaryGet(deps))
	g.POST("/:id/:alertType", HandleSummarySet(deps))
	g.DELETE("/:id/:alertType", HandleSummaryDelete(deps))
	g.POST("/:id/:alertType/trigger", HandleSummaryTrigger(deps))
	return r, mock, &triggered
}

func TestSummaryAPI_GetMissing404(t *testing.T) {
	r, _, _ := summaryRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing schedule, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSummaryAPI_SetThenGet(t *testing.T) {
	r, mock, _ := summaryRouter(t)

	body := bytes.NewBufferString(`{"active_hours":[{"day":1,"hours":7,"mins":30}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/summaries/u1/quest", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on set, got %d body=%s", w.Code, w.Body.String())
	}

	// Now GET must return what we stored.
	req = httptest.NewRequest(http.MethodGet, "/api/summaries/u1/quest", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d", w.Code)
	}

	var resp struct {
		Status   string `json:"status"`
		Schedule struct {
			ID          string          `json:"id"`
			AlertType   string          `json:"alert_type"`
			ActiveHours json.RawMessage `json:"active_hours"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Status != "ok" {
		t.Errorf("status=%q, want ok", resp.Status)
	}
	if resp.Schedule.ID != "u1" || resp.Schedule.AlertType != "quest" {
		t.Errorf("got %+v", resp.Schedule)
	}
	// Confirm the store actually has it (not just a round-trip in handler).
	got, _ := mock.Get("u1", "quest")
	if got == nil {
		t.Fatal("expected stored schedule, got nil")
	}
}

func TestSummaryAPI_SetAcceptsStringHours(t *testing.T) {
	// PoracleWeb sends active_hours as a stringified JSON value; we must
	// accept it without re-encoding it into nested-string nonsense.
	r, mock, _ := summaryRouter(t)

	body := bytes.NewBufferString(`{"active_hours":"[{\"day\":1,\"hours\":9,\"mins\":0}]"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/summaries/u1/quest", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	got, _ := mock.Get("u1", "quest")
	if got == nil {
		t.Fatal("expected schedule stored")
	}
	// Parse what got stored — both forms must round-trip to a valid array.
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(got.ActiveHours), &parsed); err != nil {
		t.Fatalf("stored value not valid JSON: %v raw=%q", err, got.ActiveHours)
	}
	if len(parsed) != 1 {
		t.Errorf("expected 1 entry, got %d", len(parsed))
	}
}

func TestSummaryAPI_Delete(t *testing.T) {
	r, mock, _ := summaryRouter(t)
	mock.Seed(store.SummarySchedule{
		ID: "u1", AlertType: "quest",
		ActiveHours: `[{"day":1,"hours":7,"mins":30}]`,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/summaries/u1/quest", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	got, _ := mock.Get("u1", "quest")
	if got != nil {
		t.Errorf("expected schedule deleted, still present: %+v", got)
	}
}

func TestSummaryAPI_Trigger(t *testing.T) {
	r, _, triggered := summaryRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/summaries/u1/quest/trigger", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(triggered) != 1 {
		t.Errorf("expected dispatch to fire once, got %d", *triggered)
	}
}

func TestSummaryAPI_ListForUser(t *testing.T) {
	r, mock, _ := summaryRouter(t)
	mock.Seed(store.SummarySchedule{
		ID: "u1", AlertType: "quest",
		ActiveHours: `[{"day":1,"hours":7,"mins":30}]`,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/summaries/u1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status    string                    `json:"status"`
		Schedules []summaryScheduleResponse `json:"schedules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Schedules) != 1 {
		t.Errorf("expected 1 schedule, got %d", len(resp.Schedules))
	}
}

func TestSummaryAPI_FeatureDisabled503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := &SummaryDeps{} // nil schedules + dispatch
	r := gin.New()
	g := r.Group("/api/summaries")
	g.GET("/:id/:alertType", HandleSummaryGet(deps))
	g.POST("/:id/:alertType/trigger", HandleSummaryTrigger(deps))

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/summaries/u1/quest"},
		{http.MethodPost, "/api/summaries/u1/quest/trigger"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: expected 503, got %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestSummaryAPI_SetMissingActiveHours400(t *testing.T) {
	r, _, _ := summaryRouter(t)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/summaries/u1/quest", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSummaryAPI_KnownAlertTypesIncludesQuest(t *testing.T) {
	if !slices.Contains(knownSummaryAlertTypes, "quest") {
		t.Errorf("knownSummaryAlertTypes must include quest, got %v", knownSummaryAlertTypes)
	}
}
