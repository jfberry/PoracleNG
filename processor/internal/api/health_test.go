package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandleHealthReturnsCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", HandleHealth())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		Status       string            `json:"status"`
		Version      string            `json:"version"`
		Capabilities map[string]bool   `json:"capabilities"`
		Raw          map[string]any    `json:"-"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Status != "healthy" {
		t.Errorf("status: got %q, want %q", got.Status, "healthy")
	}
	// Version is whatever the test binary sets — just check the field
	// is present in the response.
	if _, ok := any(got.Version).(string); !ok {
		t.Errorf("version field missing or wrong type")
	}

	// Every capability documented in the Capabilities struct should
	// appear in the response. New keys land here automatically.
	expected := []string{"buttons", "snapshots", "autocreate", "tomlDts", "buttonResponseObject"}
	for _, k := range expected {
		v, ok := got.Capabilities[k]
		if !ok {
			t.Errorf("capabilities missing key %q (got %+v)", k, got.Capabilities)
			continue
		}
		// All caps are true on this branch.
		if !v {
			t.Errorf("capability %q reported false; want true", k)
		}
	}
}
