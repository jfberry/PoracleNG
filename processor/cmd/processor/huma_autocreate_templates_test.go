package main

import (
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

// newAutocreateTemplatesTestAPI builds a gin engine with the shared huma
// instance and registers the three channel-template ops against the given cfg.
func newAutocreateTemplatesTestAPI(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	humaAPI := api.NewHumaAPI(r, r.Group("/api"), "test")
	registerAutocreateTemplatesHuma(humaAPI, cfg)
	return r
}

func postTemplates(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const validTemplatesBody = `{"templates":[` +
	`{"name":"uk","definition":{"channels":[{"channelName":"uk-rares"}]}}` +
	`]}`

// TestHumaAutocreateTemplatesGet asserts GET returns the live file contents as a
// real JSON array under {"status":"ok","templates":[...]}.
func TestHumaAutocreateTemplatesGet(t *testing.T) {
	baseDir := t.TempDir()
	writeChannelTemplates(t, baseDir,
		`[{"name":"uk","definition":{"channels":[{"channelName":"uk-rares"}]}}]`)
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	req := httptest.NewRequest(http.MethodGet, "/api/autocreate/templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET templates = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	templates, ok := got["templates"].([]any)
	if !ok {
		t.Fatalf("templates not a JSON array (open body must not base64-encode): %v", got["templates"])
	}
	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1; body: %s", len(templates), w.Body.String())
	}
	first, _ := templates[0].(map[string]any)
	if first["name"] != "uk" {
		t.Errorf("templates[0].name = %v, want uk", first["name"])
	}
}

// TestHumaAutocreateTemplatesGetMissingFile asserts a missing file yields the
// empty-array envelope rather than an error.
func TestHumaAutocreateTemplatesGetMissingFile(t *testing.T) {
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: t.TempDir()})

	req := httptest.NewRequest(http.MethodGet, "/api/autocreate/templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET templates (missing file) = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	templates, ok := got["templates"].([]any)
	if !ok || len(templates) != 0 {
		t.Errorf("templates = %v, want empty array", got["templates"])
	}
}

// TestHumaAutocreateTemplatesPostHappy writes a valid templates body to a TEMP
// BaseDir and asserts the file is written + a backup is reported. The temp dir
// keeps the test from clobbering the real config/channelTemplate.json.
func TestHumaAutocreateTemplatesPostHappy(t *testing.T) {
	baseDir := t.TempDir()
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	w := postTemplates(t, r, "/api/autocreate/templates", validTemplatesBody)
	if w.Code != http.StatusOK {
		t.Fatalf("POST templates = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if _, ok := got["backup"]; !ok {
		t.Errorf("response missing backup field: %v", got)
	}

	// Side effect: the file was written to the temp BaseDir, not the real one.
	written, err := os.ReadFile(filepath.Join(baseDir, "config", "channelTemplate.json"))
	if err != nil {
		t.Fatalf("expected channelTemplate.json written under temp BaseDir: %v", err)
	}
	if !strings.Contains(string(written), "uk-rares") {
		t.Errorf("written file missing posted content: %s", written)
	}
}

// TestHumaAutocreateTemplatesPostParseBoundary asserts a malformed JSON body and
// a missing templates field both surface as problem+json 400s (open schema must
// accept the bytes; the handler does the strict parse).
func TestHumaAutocreateTemplatesPostParseBoundary(t *testing.T) {
	baseDir := t.TempDir()
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"templates": [`},
		{"missing templates field", `{"foo":"bar"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := postTemplates(t, r, "/api/autocreate/templates", c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("POST %q = %d, want 400; body: %s", c.body, w.Code, w.Body.String())
			}
		})
	}

	// The malformed/missing posts must not have written a file.
	if _, err := os.Stat(filepath.Join(baseDir, "config", "channelTemplate.json")); !os.IsNotExist(err) {
		t.Errorf("parse-boundary post should not write a file, stat err = %v", err)
	}
}

// TestHumaAutocreateTemplatesValidateHappy asserts the validate op returns 200
// without writing any file.
func TestHumaAutocreateTemplatesValidateHappy(t *testing.T) {
	baseDir := t.TempDir()
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	w := postTemplates(t, r, "/api/autocreate/templates/validate", validTemplatesBody)
	if w.Code != http.StatusOK {
		t.Fatalf("POST validate = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeAutocreateBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}

	// validate never writes.
	if _, err := os.Stat(filepath.Join(baseDir, "config", "channelTemplate.json")); !os.IsNotExist(err) {
		t.Errorf("validate should not write a file, stat err = %v", err)
	}
}

// TestHumaAutocreateTemplatesValidateFailure asserts a blocking validation error
// (a template with no channels) yields a problem+json 400 and writes nothing.
func TestHumaAutocreateTemplatesValidateFailure(t *testing.T) {
	baseDir := t.TempDir()
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	invalid := `{"templates":[{"name":"bad","definition":{"channels":[]}}]}`
	w := postTemplates(t, r, "/api/autocreate/templates/validate", invalid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST validate (invalid) = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

// TestHumaAutocreateTemplatesPostValidationFailure asserts the write op also
// refuses a blocking validation error and leaves no file on disk.
func TestHumaAutocreateTemplatesPostValidationFailure(t *testing.T) {
	baseDir := t.TempDir()
	r := newAutocreateTemplatesTestAPI(t, &config.Config{BaseDir: baseDir})

	invalid := `{"templates":[{"name":"bad name","definition":{"channels":[{"channelName":"c"}]}}]}`
	w := postTemplates(t, r, "/api/autocreate/templates", invalid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST templates (invalid) = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(baseDir, "config", "channelTemplate.json")); !os.IsNotExist(err) {
		t.Errorf("invalid post should not write a file, stat err = %v", err)
	}
}
