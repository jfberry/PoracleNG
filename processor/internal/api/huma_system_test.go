package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestHumaReload_OK asserts that a reload op registered via RegisterReload
// serves the legacy {"status":"ok"} success body at HTTP 200 and invokes the
// supplied reload function exactly once.
func TestHumaReload_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	called := false
	RegisterReload(humaAPI, "test-reload-ok", http.MethodGet, "/reload", "Test reload", "Test reload op.", func() error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/reload = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"; full body: %v", got["status"], got)
	}
	if !called {
		t.Error("reload function was not invoked")
	}
}

// TestHumaReload_Error asserts that a failing reload function surfaces as a
// problem+json error: HTTP 500 with a numeric "status" (not the legacy
// {"status":"error"} envelope) and a "title" field.
func TestHumaReload_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := NewHumaAPI(r, r.Group("/api"), "test")

	RegisterReload(humaAPI, "test-reload-err", http.MethodGet, "/reload", "Test reload", "Test reload op.", func() error {
		return errors.New("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/reload = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// problem+json: status is a JSON number (500), not the legacy string "error".
	statusNum, ok := got["status"].(float64)
	if !ok {
		t.Fatalf("status = %v (%T), want JSON number 500; full body: %v", got["status"], got["status"], got)
	}
	if statusNum != float64(http.StatusInternalServerError) {
		t.Errorf("status = %v, want 500", statusNum)
	}
	if got["status"] == "error" {
		t.Errorf("body must not use legacy {status:\"error\"} envelope: %v", got)
	}
	if _, hasTitle := got["title"]; !hasTitle {
		t.Errorf("problem+json body must contain a \"title\" field: %v", got)
	}
}
