package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/config"
)

// newConfigTestAPI builds a gin engine with a fresh huma API mounted on /api.
func newConfigTestAPI(t *testing.T) (*gin.Engine, huma.API) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r, NewHumaAPI(r, r.Group("/api"), "test")
}

// getJSON issues a GET against the engine.
func getJSON(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// writeTempConfig creates a temp config dir holding a minimal config.toml and
// returns the dir. Tests point ConfigDeps.ConfigDir here so the real
// config/config.toml is never touched by the save path.
func writeTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const minimal = "[general]\nlocale = \"en\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(minimal), 0644); err != nil {
		t.Fatalf("write temp config.toml: %v", err)
	}
	return dir
}

// --- GET /config/poracleWeb ------------------------------------------------

func TestHumaConfigPoracleWeb_OK(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	cfg.General.Locale = "fr"
	cfg.General.DisableRaid = true
	RegisterConfigPoracleWeb(api, cfg)

	w := getJSON(t, r, "/api/config/poracleWeb")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if got["locale"] != "fr" {
		t.Errorf("locale = %v, want fr", got["locale"])
	}
	if _, ok := got["$schema"]; ok {
		t.Errorf("success body must not contain $schema: %v", got)
	}
	// disabledHooks must contain "raid" and admins must be non-null arrays.
	hooks, _ := got["disabledHooks"].([]any)
	foundRaid := false
	for _, h := range hooks {
		if h == "raid" {
			foundRaid = true
		}
	}
	if !foundRaid {
		t.Errorf("disabledHooks = %v, want to contain raid", hooks)
	}
	admins, ok := got["admins"].(map[string]any)
	if !ok {
		t.Fatalf("admins missing/wrong type: %v", got["admins"])
	}
	if admins["discord"] == nil || admins["telegram"] == nil {
		t.Errorf("admins.discord/telegram must be [] not null: %v", admins)
	}
}

// --- GET /config/values ----------------------------------------------------

func TestHumaConfigValues_OK(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	cfg.General.Locale = "de"
	RegisterConfigValues(api, ConfigDeps{Cfg: cfg, ConfigDir: t.TempDir()})

	w := getJSON(t, r, "/api/config/values")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Status     string         `json:"status"`
		Values     map[string]any `json:"values"`
		Overridden []string       `json:"overridden"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.Overridden == nil {
		t.Errorf("overridden must be [] not null")
	}
	general, ok := got.Values["general"].(map[string]any)
	if !ok {
		t.Fatalf("values.general missing: %v", got.Values)
	}
	if general["locale"] != "de" {
		t.Errorf("values.general.locale = %v, want de", general["locale"])
	}
}

func TestHumaConfigValues_SectionFilter(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	RegisterConfigValues(api, ConfigDeps{Cfg: cfg, ConfigDir: t.TempDir()})

	w := getJSON(t, r, "/api/config/values?section=general")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got.Values["general"]; !ok {
		t.Errorf("section-filtered values must include general: %v", got.Values)
	}
	if len(got.Values) != 1 {
		t.Errorf("section filter must return exactly one section, got %d: %v", len(got.Values), got.Values)
	}
}

// --- POST /config/values (save — writes config.toml to a TEMP path) --------

func TestHumaConfigSave_OK_TempPath(t *testing.T) {
	dir := writeTempConfig(t)
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	reloaded := false
	RegisterConfigSave(api, ConfigDeps{
		Cfg:       cfg,
		ConfigDir: dir,
		ReloadFn:  func() { reloaded = true },
	})

	// general.locale is a hot-reloadable string field in the schema.
	body := []byte(`{"general":{"locale":"de"}}`)
	w := postJSON(t, r, "/api/config/values", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	if got["saved"] != float64(1) {
		t.Errorf("saved = %v, want 1", got["saved"])
	}
	// locale is hot-reloadable → restart not required → reload fires.
	if got["restart_required"] != false {
		t.Errorf("restart_required = %v, want false", got["restart_required"])
	}
	if !reloaded {
		t.Errorf("ReloadFn must fire for a hot-reloadable change")
	}
	// In-memory config must reflect the applied override.
	if cfg.General.Locale != "de" {
		t.Errorf("in-memory locale = %q, want de", cfg.General.Locale)
	}
	// The temp config.toml must have been rewritten (and a backup made).
	written, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	if len(written) == 0 {
		t.Errorf("rewritten config.toml is empty")
	}
}

func TestHumaConfigSave_EmptyBody(t *testing.T) {
	dir := writeTempConfig(t)
	r, api := newConfigTestAPI(t)
	RegisterConfigSave(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: dir})

	w := postJSON(t, r, "/api/config/values", []byte(`{}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertProblemJSON(t, w)
}

func TestHumaConfigSave_MalformedBody(t *testing.T) {
	dir := writeTempConfig(t)
	r, api := newConfigTestAPI(t)
	RegisterConfigSave(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: dir})

	w := postJSON(t, r, "/api/config/values", []byte(`{not-json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertProblemJSON(t, w)
}

func TestHumaConfigSave_UnknownSection(t *testing.T) {
	dir := writeTempConfig(t)
	r, api := newConfigTestAPI(t)
	RegisterConfigSave(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: dir})

	w := postJSON(t, r, "/api/config/values", []byte(`{"nope":{"x":1}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertProblemJSON(t, w)
	// Ensure the temp config was NOT rewritten on a rejected save.
	written, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	if string(written) != "[general]\nlocale = \"en\"\n" {
		t.Errorf("config.toml must be unchanged on rejected save, got: %q", string(written))
	}
}

func TestHumaConfigSave_ValidationError_NotWritten(t *testing.T) {
	dir := writeTempConfig(t)
	r, api := newConfigTestAPI(t)
	RegisterConfigSave(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: dir})

	// geofence.paths with an absolute path is a hard validation error.
	body := []byte(`{"geofence":{"paths":["/etc/passwd"]}}`)
	w := postJSON(t, r, "/api/config/values", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	// The per-field issue must ride along in problem+json errors[].
	var got struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Detail != "validation failed" {
		t.Errorf("detail = %q, want %q", got.Detail, "validation failed")
	}
	if len(got.Errors) == 0 {
		t.Fatalf("validation errors must appear in errors[]: %s", w.Body.String())
	}
	foundPath := false
	for _, e := range got.Errors {
		if e.Location == "geofence.paths[0]" {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("errors[] must include geofence.paths[0]: %v", got.Errors)
	}
	// Config must NOT be rewritten when validation fails.
	written, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	if string(written) != "[general]\nlocale = \"en\"\n" {
		t.Errorf("config.toml must be unchanged on validation failure, got: %q", string(written))
	}
}

// --- POST /config/validate (dry-run, no save) ------------------------------

func TestHumaConfigValidate_OK_NoIssues(t *testing.T) {
	r, api := newConfigTestAPI(t)
	RegisterConfigValidate(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: t.TempDir()})

	w := postJSON(t, r, "/api/config/validate", []byte(`{"general":{"locale":"en"}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Status string            `json:"status"`
		Issues []ValidationIssue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if len(got.Issues) != 0 {
		t.Errorf("expected no issues, got %v", got.Issues)
	}
}

func TestHumaConfigValidate_ReportsIssues(t *testing.T) {
	r, api := newConfigTestAPI(t)
	RegisterConfigValidate(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: t.TempDir()})

	// validate returns 200 with issues in the body (NOT a 400) — it's a
	// dry-run preview. An absolute geofence path yields a severity:"error".
	w := postJSON(t, r, "/api/config/validate", []byte(`{"geofence":{"paths":["/etc/passwd"]}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Status string            `json:"status"`
		Issues []ValidationIssue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if len(got.Issues) == 0 {
		t.Fatalf("expected an error issue for an absolute geofence path")
	}
	if got.Issues[0].Severity != "error" {
		t.Errorf("severity = %q, want error", got.Issues[0].Severity)
	}
	if got.Issues[0].Field != "geofence.paths[0]" {
		t.Errorf("field = %q, want geofence.paths[0]", got.Issues[0].Field)
	}
}

func TestHumaConfigValidate_MalformedBody(t *testing.T) {
	r, api := newConfigTestAPI(t)
	RegisterConfigValidate(api, ConfigDeps{Cfg: &config.Config{}, ConfigDir: t.TempDir()})

	w := postJSON(t, r, "/api/config/validate", []byte(`{not-json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertProblemJSON(t, w)
}

// assertProblemJSON checks the body is RFC 9457 problem+json (numeric status,
// title) and not the legacy {"status":"error"} envelope.
func assertProblemJSON(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if _, ok := got["status"].(float64); !ok {
		t.Errorf("status must be a JSON number (problem+json), got %v (%T)", got["status"], got["status"])
	}
	if got["status"] == "error" {
		t.Errorf("must not use legacy {status:\"error\"} envelope: %v", got)
	}
	if _, ok := got["title"]; !ok {
		t.Errorf("problem+json body must contain title: %v", got)
	}
}

// --- #194: available_languages must be readable ----------------------------

// available_languages gates POST /humans/{id}/setLanguage but was exposed
// nowhere, so a client had no way to build an accurate language menu.
func TestHumaConfigPoracleWeb_AvailableLanguages(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	cfg.General.AvailableLanguages = map[string]config.LanguageEntry{
		"fr": {}, "de": {Poracle: "dasporacle"}, "en": {},
	}
	RegisterConfigPoracleWeb(api, cfg)

	w := getJSON(t, r, "/api/config/poracleWeb")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, present := got["availableLanguages"]
	if !present {
		t.Fatalf("availableLanguages missing from response: %v", got)
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("availableLanguages = %v, want an array", raw)
	}
	want := []string{"de", "en", "fr"} // sorted for a stable menu
	if len(list) != len(want) {
		t.Fatalf("availableLanguages = %v, want %v", list, want)
	}
	for i, code := range want {
		if list[i] != code {
			t.Errorf("availableLanguages[%d] = %v, want %s (sorted)", i, list[i], code)
		}
	}
}

// Unset means "every language allowed" (the write path only validates when
// the map is non-empty). The key must be present but null so a client can
// tell "no restriction" from "an empty allow-list" and still offer its full
// menu — returning [] here would make clients offer nothing.
func TestHumaConfigPoracleWeb_AvailableLanguagesUnsetIsNull(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]config.LanguageEntry
	}{
		{"unset", nil},
		{"empty", map[string]config.LanguageEntry{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, api := newConfigTestAPI(t)
			cfg := &config.Config{}
			cfg.General.AvailableLanguages = tc.cfg
			RegisterConfigPoracleWeb(api, cfg)

			w := getJSON(t, r, "/api/config/poracleWeb")
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			raw, present := got["availableLanguages"]
			if !present {
				t.Fatalf("availableLanguages key must always be present: %v", got)
			}
			if raw != nil {
				t.Errorf("availableLanguages = %v, want null when unrestricted", raw)
			}
		})
	}
}

// --- #195: disabledHooks accuracy ------------------------------------------

// disable_fort_update gates the webhook handler, the !fort command and the
// !tracked section, but was missing from disabledHooks — clients concluded
// fort changes were enabled when they weren't.
func TestHumaConfigPoracleWeb_DisabledHooksIncludesFort(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	cfg.General.DisableFortUpdate = true
	RegisterConfigPoracleWeb(api, cfg)

	w := getJSON(t, r, "/api/config/poracleWeb")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hooks, _ := got["disabledHooks"].([]any)
	found := false
	for _, h := range hooks {
		if h == "fort" {
			found = true
		}
	}
	if !found {
		t.Errorf("disabledHooks = %v, want to contain \"fort\"", hooks)
	}
}

// disable_pokestop enforces nothing anywhere in the processor (its last real
// use — the !tracked fort sections — moved to disable_fort_update in
// fa9b4912). Reporting it as a disabled hook tells clients a lie, so it must
// not appear even when set.
func TestHumaConfigPoracleWeb_DisabledHooksOmitsVestigialPokestop(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	// Deliberately setting the deprecated flag: the whole point of this test
	// is that setting it changes nothing in the response.
	cfg.General.DisablePokestop = true //nolint:staticcheck // SA1019: asserting the deprecated flag is ignored
	RegisterConfigPoracleWeb(api, cfg)

	w := getJSON(t, r, "/api/config/poracleWeb")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hooks, _ := got["disabledHooks"].([]any)
	for _, h := range hooks {
		if h == "pokestop" {
			t.Errorf("disabledHooks = %v, must not contain vestigial \"pokestop\"", hooks)
		}
	}
}

// --- #210: disable_showcase reaches disabledHooks ---------------------------

// disable_showcase gates ProcessShowcase and is documented in the config
// editor schema, but was never added to hookTypes — so a client could not tell
// showcase processing was off and went on offering a control that could never
// produce an alert. Same shape as the #195 fort omission.
func TestHumaConfigPoracleWeb_DisabledHooksIncludesShowcase(t *testing.T) {
	r, api := newConfigTestAPI(t)
	cfg := &config.Config{}
	cfg.General.DisableShowcase = true
	RegisterConfigPoracleWeb(api, cfg)

	w := getJSON(t, r, "/api/config/poracleWeb")
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hooks, _ := got["disabledHooks"].([]any)
	found := false
	for _, h := range hooks {
		if h == "showcase" {
			found = true
		}
	}
	if !found {
		t.Errorf("disabledHooks = %v, want to contain \"showcase\"", hooks)
	}
}
