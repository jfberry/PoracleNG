package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/pokemon/poracleng/processor/internal/buttonactions"
	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/dts"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

func errString(s string) error { return errors.New(s) }

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }

// newDTSTestAPI builds a gin engine with a fresh huma API mounted on /api.
func newDTSTestAPI(t *testing.T) (*gin.Engine, huma.API) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r, NewHumaAPI(r, r.Group("/api"), "test")
}

// stubTemplateReader is a hand-controlled dtsTemplateReader for the read tests.
// TemplateStore can't be populated from outside the dts package (unexported
// fields), so the read ops are tested against this stub through the interface.
type stubTemplateReader struct {
	entries     []dts.DTSEntry
	resolved    map[string]any    // entryKey → resolved template
	fileContent map[string]string // entryKey → templateFileContent
	getEntry    *dts.DTSEntry     // returned by GetEntry
	deleteErr   error             // returned by DeleteEntry
	partials    map[string]string
	cleared     bool
}

func keyOf(e dts.DTSEntry) string {
	return e.Type + "|" + e.Platform + "|" + e.Language + "|" + e.ID.String()
}

func (s *stubTemplateReader) FilteredEntries(_, _, _, _ string) []dts.DTSEntry {
	return s.entries
}

func (s *stubTemplateReader) ResolveEntryContent(entry dts.DTSEntry) (any, string) {
	k := keyOf(entry)
	return s.resolved[k], s.fileContent[k]
}

func (s *stubTemplateReader) DeleteEntry(_, _, _, _ string) error { return s.deleteErr }

func (s *stubTemplateReader) GetEntry(_, _, _, _ string) *dts.DTSEntry { return s.getEntry }

func (s *stubTemplateReader) Partials() map[string]string { return s.partials }

func (s *stubTemplateReader) ClearCache() { s.cleared = true }

// --- emoji ---

func TestHumaDTSEmoji_FullSet(t *testing.T) {
	r, api := newDTSTestAPI(t)
	emoji := dts.LoadEmoji(t.TempDir(), map[string]string{"poke": ":poke:"})
	RegisterDTSEmoji(api, emoji)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/emoji", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status field = %v, want ok", got["status"])
	}
	defaults, ok := got["defaults"].(map[string]any)
	if !ok || defaults["poke"] != ":poke:" {
		t.Errorf("defaults = %v, want poke=:poke:", got["defaults"])
	}
	if _, ok := got["platforms"]; !ok {
		t.Errorf("response missing platforms key: %v", got)
	}
	if _, ok := got["platform"]; ok {
		t.Errorf("full-set response should not carry a platform field: %v", got)
	}
}

func TestHumaDTSEmoji_PerPlatform(t *testing.T) {
	r, api := newDTSTestAPI(t)
	emoji := dts.LoadEmoji(t.TempDir(), map[string]string{"poke": ":poke:"})
	RegisterDTSEmoji(api, emoji)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/emoji?platform=discord", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["platform"] != "discord" {
		t.Errorf("platform = %v, want discord", got["platform"])
	}
	emojiMap, ok := got["emoji"].(map[string]any)
	if !ok || emojiMap["poke"] != ":poke:" {
		t.Errorf("emoji = %v, want poke=:poke:", got["emoji"])
	}
}

// --- templates GET ---

func TestHumaDTSGetTemplates_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	e := dts.DTSEntry{Type: "monster", Platform: "discord", Language: "en", ID: "1", Template: "raw"}
	st := &stubTemplateReader{
		entries:     []dts.DTSEntry{e},
		resolved:    map[string]any{keyOf(e): nil},
		fileContent: map[string]string{keyOf(e): "RESOLVED CONTENT"},
	}
	RegisterDTSGetTemplates(api, st)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/templates?type=monster", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	tmpls, ok := got["templates"].([]any)
	if !ok || len(tmpls) != 1 {
		t.Fatalf("templates = %v, want 1-element array", got["templates"])
	}
	first := tmpls[0].(map[string]any)
	if first["type"] != "monster" {
		t.Errorf("templates[0].type = %v, want monster", first["type"])
	}
	if first["templateFileContent"] != "RESOLVED CONTENT" {
		t.Errorf("templates[0].templateFileContent = %v, want RESOLVED CONTENT", first["templateFileContent"])
	}
}

// --- templates DELETE ---

func TestHumaDTSDeleteTemplate_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	st := &stubTemplateReader{}
	RegisterDTSDeleteTemplate(api, st)

	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=monster&platform=discord&id=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
}

func TestHumaDTSDeleteTemplate_MissingParams(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSDeleteTemplate(api, &stubTemplateReader{})

	// platform + id absent → handler-level 400.
	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=monster", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaDTSDeleteTemplate_NotFound(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSDeleteTemplate(api, &stubTemplateReader{deleteErr: errString("template not found")})

	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=monster&platform=discord&id=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaDTSDeleteTemplate_Readonly403(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSDeleteTemplate(api, &stubTemplateReader{deleteErr: errString("template monster/discord/1/en is readonly")})

	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=monster&platform=discord&id=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// --- templates/file PUT ---

func TestHumaDTSTemplateFileWrite_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	configDir := t.TempDir()
	st := &stubTemplateReader{
		getEntry: &dts.DTSEntry{Type: "fort-update", Platform: "discord", ID: "1", TemplateFile: "dts/fort.txt"},
	}
	RegisterDTSTemplateFileWrite(api, st, configDir)

	req := httptest.NewRequest(http.MethodPut, "/api/dts/templates/file?type=fort-update&platform=discord&id=1",
		stringsReader(`{"content":"hello world"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["templateFile"] != "dts/fort.txt" {
		t.Errorf("templateFile = %v, want dts/fort.txt", got["templateFile"])
	}
	// The file must have been written.
	data, err := os.ReadFile(filepath.Join(configDir, "dts", "fort.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want %q", string(data), "hello world")
	}
	if !st.cleared {
		t.Errorf("ClearCache was not called after a successful write")
	}
}

func TestHumaDTSTemplateFileWrite_NotFound(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSTemplateFileWrite(api, &stubTemplateReader{getEntry: nil}, t.TempDir())

	req := httptest.NewRequest(http.MethodPut, "/api/dts/templates/file?type=x&platform=discord&id=1",
		stringsReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaDTSTemplateFileWrite_InlineTemplate400(t *testing.T) {
	r, api := newDTSTestAPI(t)
	// Entry with no TemplateFile → uses inline JSON, not a templateFile.
	st := &stubTemplateReader{getEntry: &dts.DTSEntry{Type: "monster", Platform: "discord", ID: "1"}}
	RegisterDTSTemplateFileWrite(api, st, t.TempDir())

	req := httptest.NewRequest(http.MethodPut, "/api/dts/templates/file?type=monster&platform=discord&id=1",
		stringsReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestHumaDTSTemplateFileWrite_Readonly403(t *testing.T) {
	r, api := newDTSTestAPI(t)
	st := &stubTemplateReader{getEntry: &dts.DTSEntry{Type: "monster", Platform: "discord", ID: "1", TemplateFile: "dts/x.txt", Readonly: true}}
	RegisterDTSTemplateFileWrite(api, st, t.TempDir())

	req := httptest.NewRequest(http.MethodPut, "/api/dts/templates/file?type=monster&platform=discord&id=1",
		stringsReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// --- fields ---

func TestHumaDTSFieldTypes_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSFieldTypes(api)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/fields", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
	types, ok := got["types"].([]any)
	if !ok || len(types) == 0 {
		t.Fatalf("types = %v, want non-empty array", got["types"])
	}
}

func TestHumaDTSFields_KnownType(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSFields(api)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/fields/monster", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["type"] != "monster" {
		t.Errorf("type = %v, want monster", got["type"])
	}
	if _, ok := got["fields"].([]any); !ok {
		t.Errorf("fields = %v, want array", got["fields"])
	}
}

// TestHumaDTSFields_UnknownType asserts the gin handler's behavior is preserved:
// an unknown type returns 200 with just the common fields (NOT a 404).
func TestHumaDTSFields_UnknownType(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSFields(api)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/fields/not-a-real-type", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown type returns common fields); body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["type"] != "not-a-real-type" {
		t.Errorf("type = %v, want echoed back", got["type"])
	}
	if _, ok := got["fields"].([]any); !ok {
		t.Errorf("fields = %v, want common-fields array", got["fields"])
	}
}

// --- partials ---

func TestHumaDTSPartials_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	st := &stubTemplateReader{partials: map[string]string{"foo": "{{bar}}"}}
	RegisterDTSPartials(api, st)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/partials", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	partials, ok := got["partials"].(map[string]any)
	if !ok || partials["foo"] != "{{bar}}" {
		t.Errorf("partials = %v, want foo={{bar}}", got["partials"])
	}
}

// --- testdata ---

func TestHumaDTSTestdata_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	fallbackDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fallbackDir, "testdata.json"),
		[]byte(`[{"type":"pokemon","test":"a","location":"x","webhook":{}},{"type":"raid","test":"b","location":"y","webhook":{}}]`), 0644); err != nil {
		t.Fatal(err)
	}
	RegisterDTSTestdata(api, t.TempDir(), fallbackDir)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/testdata?type=pokemon", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	td, ok := got["testdata"].([]any)
	if !ok || len(td) != 1 {
		t.Fatalf("testdata = %v, want 1 filtered entry", got["testdata"])
	}
	if td[0].(map[string]any)["type"] != "pokemon" {
		t.Errorf("testdata[0].type = %v, want pokemon", td[0])
	}
}

func TestHumaDTSTestdata_NotFound(t *testing.T) {
	r, api := newDTSTestAPI(t)
	// Both dirs empty → no testdata.json → 404.
	RegisterDTSTestdata(api, t.TempDir(), t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/dts/testdata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// --- actions ---

func TestHumaButtonActions_OK(t *testing.T) {
	r, api := newDTSTestAPI(t)
	reg := buttonactions.NewRegistry()
	reg.Register(buttons.ActionMute, func(_ context.Context, _ *snapshots.Snapshot, _ buttons.Def, _ string, _ buttonactions.Deps) (buttonactions.Response, error) {
		return buttonactions.Response{}, nil
	})
	RegisterButtonActions(api, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/actions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	actions, ok := got["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions = %v, want 1-element array", got["actions"])
	}
	first := actions[0].(map[string]any)
	if first["name"] != buttons.ActionMute {
		t.Errorf("actions[0].name = %v, want %s", first["name"], buttons.ActionMute)
	}
	if first["required_scope"] != true {
		t.Errorf("mute action required_scope = %v, want true", first["required_scope"])
	}
}

func TestHumaButtonActions_NilRegistry503(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterButtonActions(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/dts/actions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", w.Code, w.Body.String())
	}
}

// Platform-agnostic types (help) save overrides with an empty platform —
// the DELETE endpoint must accept the same key shape instead of rejecting
// it with 400 before DeleteEntry is reached.
func TestHumaDTSDeleteTemplate_PlatformAgnosticEmptyPlatform(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSDeleteTemplate(api, &stubTemplateReader{})

	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=help&id=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for platform-agnostic delete; body: %s", w.Code, w.Body.String())
	}
}

// Non-agnostic types still require platform.
func TestHumaDTSDeleteTemplate_NonAgnosticStillRequiresPlatform(t *testing.T) {
	r, api := newDTSTestAPI(t)
	RegisterDTSDeleteTemplate(api, &stubTemplateReader{})

	req := httptest.NewRequest(http.MethodDelete, "/api/dts/templates?type=monster&id=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when platform missing for monster; body: %s", w.Code, w.Body.String())
	}
}
