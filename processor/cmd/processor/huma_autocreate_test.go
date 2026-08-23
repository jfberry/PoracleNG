package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/config"
)

// newAutocreateTestAPI builds a gin engine with the shared huma instance and
// registers the autocreate ops against the given cfg and a nil bot. A nil bot
// is sufficient for the schema + delete ops (cfg only) and exercises the
// run op's bot-unavailable short-circuit (503) plus body validation.
func newAutocreateTestAPI(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := api.NewHumaAPI(r, r.Group("/api"), "test")
	registerAutocreateHuma(humaAPI, cfg, nil)
	return r
}

func decodeAutocreateBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v; raw: %s", err, w.Body.String())
	}
	return got
}

// TestHumaAutocreateSchema asserts the schema op serves the static editor
// metadata at 200 under {"status":"ok","schema":{...}}.
func TestHumaAutocreateSchema(t *testing.T) {
	r := newAutocreateTestAPI(t, &config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/autocreate/templates/schema", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET schema = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	schema, ok := got["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema not an object: %v", got["schema"])
	}
	if schema["backupNamePrefix"] != "channelTemplate.json.bak." {
		t.Errorf("backupNamePrefix = %v, want channelTemplate.json.bak.", schema["backupNamePrefix"])
	}
	chans, ok := schema["channelTypes"].([]any)
	if !ok || len(chans) != 2 || chans[0] != "text" {
		t.Errorf("channelTypes = %v, want [text voice]", schema["channelTypes"])
	}
}

// TestHumaAutocreateDeleteHappyPath writes a channel templates file with two
// entries, deletes one, and asserts a 200 {"status":"ok","backup":...}.
func TestHumaAutocreateDeleteHappyPath(t *testing.T) {
	baseDir := t.TempDir()
	writeChannelTemplates(t, baseDir, `[`+
		`{"name":"keep","definition":{"channels":[{"channelName":"keep-chan"}]}},`+
		`{"name":"drop","definition":{"channels":[{"channelName":"drop-chan"}]}}`+
		`]`)
	r := newAutocreateTestAPI(t, &config.Config{BaseDir: baseDir})

	req := httptest.NewRequest(http.MethodDelete, "/api/autocreate/templates/drop", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("DELETE drop = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if _, ok := got["backup"]; !ok {
		t.Errorf("response missing backup field: %v", got)
	}
}

// TestHumaAutocreateDeleteNotFound asserts deleting an unknown template yields
// a problem+json 404.
func TestHumaAutocreateDeleteNotFound(t *testing.T) {
	r := newAutocreateTestAPI(t, &config.Config{BaseDir: t.TempDir()})

	req := httptest.NewRequest(http.MethodDelete, "/api/autocreate/templates/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestHumaAutocreateRunNoBot asserts the run op short-circuits with 503 when no
// bot is wired, AND that an empty/partial body does NOT trigger a 422 — proving
// the body fields are optional (matching the legacy ShouldBindJSON behavior).
func TestHumaAutocreateRunNoBot(t *testing.T) {
	r := newAutocreateTestAPI(t, &config.Config{})

	cases := []struct {
		name string
		body string
	}{
		{"empty object body", `{}`},
		{"partial body (rule only)", `{"rule":"uk-areas"}`},
		{"flags only", `{"dry_run":true,"force":true}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/autocreate/run", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Bot is nil → 503, never a 422 from body validation.
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("POST run %s = %d, want 503; body: %s", c.body, w.Code, w.Body.String())
			}
		})
	}
}

func writeChannelTemplates(t *testing.T, baseDir, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "channelTemplate.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write channelTemplate.json: %v", err)
	}
}
