package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/store"
)

func humanRouter(t *testing.T, available map[string]config.LanguageEntry) (*gin.Engine, *store.MockHumanStore, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mock := store.NewMockHumanStore()
	mock.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})

	var reloads int32
	deps := &TrackingDeps{
		Humans: mock,
		Config: &config.Config{General: config.GeneralConfig{AvailableLanguages: available}},
		ReloadFunc: func() {
			atomic.AddInt32(&reloads, 1)
		},
	}

	r := gin.New()
	g := r.Group("/api/humans")
	g.POST("/:id/language", HandleSetLanguage(deps))
	return r, mock, &reloads
}

func TestHumanAPI_SetLanguage(t *testing.T) {
	r, mock, reloads := humanRouter(t, map[string]config.LanguageEntry{
		"de": {},
		"en": {},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/language", bytes.NewBufferString(`{"language":"DE"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Status   string `json:"status"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != "ok" || resp.Language != "de" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	human, err := mock.Get("u1")
	if err != nil {
		t.Fatalf("get human: %v", err)
	}
	if human.Language != "de" {
		t.Errorf("language=%q, want de", human.Language)
	}
	if atomic.LoadInt32(reloads) != 1 {
		t.Errorf("reloads=%d, want 1", atomic.LoadInt32(reloads))
	}
}

func TestHumanAPI_SetLanguageRejectsUnavailable(t *testing.T) {
	r, mock, reloads := humanRouter(t, map[string]config.LanguageEntry{"de": {}})

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/language", bytes.NewBufferString(`{"language":"en"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	human, err := mock.Get("u1")
	if err != nil {
		t.Fatalf("get human: %v", err)
	}
	if human.Language != "en" {
		t.Errorf("language changed to %q", human.Language)
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Errorf("reloads=%d, want 0", atomic.LoadInt32(reloads))
	}
}

func TestHumanAPI_SetLanguageAcceptsAnyWhenUnrestricted(t *testing.T) {
	r, mock, _ := humanRouter(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/language", bytes.NewBufferString(`{"language":"zh-cn"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	human, err := mock.Get("u1")
	if err != nil {
		t.Fatalf("get human: %v", err)
	}
	if human.Language != "zh-cn" {
		t.Errorf("language=%q, want zh-cn", human.Language)
	}
}

func TestHumanAPI_SetLanguageMissingUser(t *testing.T) {
	r, _, reloads := humanRouter(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/humans/missing/language", bytes.NewBufferString(`{"language":"de"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Errorf("reloads=%d, want 0", atomic.LoadInt32(reloads))
	}
}

func TestHumanAPI_SetLanguageRequiresLanguage(t *testing.T) {
	r, _, reloads := humanRouter(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/humans/u1/language", bytes.NewBufferString(`{"language":" "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(reloads) != 0 {
		t.Errorf("reloads=%d, want 0", atomic.LoadInt32(reloads))
	}
}
