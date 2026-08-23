package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/delivery"
	"github.com/pokemon/poracleng/processor/internal/dts"
)

// --- stubs -----------------------------------------------------------------

// stubDTSWriter is a hand-controlled implementation of the write interfaces.
type stubDTSWriter struct {
	tmpl       *raymond.Template
	metadata   map[string]any
	saveErr    error
	savedCount int
	partials   map[string]string
}

func (s *stubDTSWriter) Get(_, _, _, _ string) *raymond.Template { return s.tmpl }

func (s *stubDTSWriter) SaveEntry(_ dts.DTSEntry) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.savedCount++
	return nil
}

func (s *stubDTSWriter) TemplateMetadata(_ bool) map[string]any { return s.metadata }

func (s *stubDTSWriter) Partials() map[string]string { return s.partials }

// stubEnrich implements EnrichService.
type stubEnrich struct {
	vars    map[string]any
	err     error
	gotType string
}

func (s *stubEnrich) EnrichWebhook(webhookType string, _ json.RawMessage, _, _ string) (map[string]any, error) {
	s.gotType = webhookType
	if s.err != nil {
		return nil, s.err
	}
	return s.vars, nil
}

// stubDispatcher implements dtsDispatcher and records dispatched jobs.
type stubDispatcher struct {
	jobs []*delivery.Job
}

func (s *stubDispatcher) Dispatch(job *delivery.Job) { s.jobs = append(s.jobs, job) }

// --- helpers ---------------------------------------------------------------

func postJSON(t *testing.T, r http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- config/templates ------------------------------------------------------

func TestHumaTemplateConfig_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	ts := &stubDTSWriter{metadata: map[string]any{"monster": map[string]any{"count": 2}}}
	RegisterTemplateConfig(api, ts)

	req := httptest.NewRequest(http.MethodGet, "/api/config/templates?includeDescriptions=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body["status"])
	}
	if _, ok := body["monster"]; !ok {
		t.Fatalf("expected merged metadata key 'monster', got %s", w.Body.String())
	}
}

// --- dts/render ------------------------------------------------------------

func TestHumaDTSRender_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	// Template renders a JSON object using the `view` variable `prefix`.
	tmpl, err := raymond.Parse(`{"content":"{{prefix}}track"}`)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	ts := &stubDTSWriter{tmpl: tmpl}
	RegisterDTSRender(api, ts)

	// Open body: arbitrary `view` map.
	body := []byte(`{"type":"help","id":"track","platform":"discord","language":"en","view":{"prefix":"!"}}`)
	w := postJSON(t, r, "/api/dts/render", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status  string `json:"status"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if got.Status != "ok" || got.Message.Content != "!track" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHumaDTSRender_NotFound404(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	ts := &stubDTSWriter{tmpl: nil} // Get returns nil → 404
	RegisterDTSRender(api, ts)

	w := postJSON(t, r, "/api/dts/render", []byte(`{"type":"nope","id":"x","platform":"discord"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSRender_MalformedBody400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	ts := &stubDTSWriter{}
	RegisterDTSRender(api, ts)

	// `view` is the wrong type (string, not object) → unmarshal into struct fails.
	w := postJSON(t, r, "/api/dts/render", []byte(`{"type":"help","view":"not-an-object"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- dts/templates (save) --------------------------------------------------

func TestHumaDTSSaveTemplates_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	ts := &stubDTSWriter{}
	RegisterDTSSaveTemplates(api, ts)

	// Open body: polymorphic `template` (here a string and an object across entries).
	body := []byte(`[
		{"type":"monster","platform":"discord","id":"1","template":"raw string body"},
		{"type":"raid","platform":"telegram","id":"5","template":{"content":"x","embed":{"title":"t"}}}
	]`)
	w := postJSON(t, r, "/api/dts/templates", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status string `json:"status"`
		Saved  int    `json:"saved"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "ok" || got.Saved != 2 {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if ts.savedCount != 2 {
		t.Fatalf("expected 2 SaveEntry calls, got %d", ts.savedCount)
	}
}

func TestHumaDTSSaveTemplates_EmptyArray400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSaveTemplates(api, &stubDTSWriter{})

	w := postJSON(t, r, "/api/dts/templates", []byte(`[]`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSaveTemplates_MissingFields400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSaveTemplates(api, &stubDTSWriter{})

	w := postJSON(t, r, "/api/dts/templates", []byte(`[{"type":"monster","id":"1"}]`)) // no platform
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSaveTemplates_AgnosticHelpEmptyPlatformOK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSaveTemplates(api, &stubDTSWriter{})

	// help is platform-agnostic: platform="" must be accepted (not 400) so an
	// override can shadow the "" fallback instead of duplicating it.
	body := []byte(`[{"type":"help","id":"fort","platform":"","template":"x"}]`)
	w := postJSON(t, r, "/api/dts/templates", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for agnostic help with empty platform, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSaveTemplates_Readonly403(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	ts := &stubDTSWriter{saveErr: errors.New("cannot overwrite readonly entry")}
	RegisterDTSSaveTemplates(api, ts)

	body := []byte(`[{"type":"monster","platform":"discord","id":"1","template":"x"}]`)
	w := postJSON(t, r, "/api/dts/templates", body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSaveTemplates_MalformedBody400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSaveTemplates(api, &stubDTSWriter{})

	// Not an array.
	w := postJSON(t, r, "/api/dts/templates", []byte(`{"type":"monster"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- dts/enrich ------------------------------------------------------------

func TestHumaDTSEnrich_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	svc := &stubEnrich{vars: map[string]any{"name": "Pikachu", "iv": 100}}
	RegisterDTSEnrich(api, svc)

	// Open body: arbitrary `webhook` object.
	body := []byte(`{"type":"pokemon","language":"en","platform":"discord","webhook":{"pokemon_id":25,"iv":100,"latitude":1.2}}`)
	w := postJSON(t, r, "/api/dts/enrich", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status    string         `json:"status"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "ok" || got.Variables["name"] != "Pikachu" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if svc.gotType != "pokemon" {
		t.Fatalf("expected enrich called with type pokemon, got %q", svc.gotType)
	}
}

func TestHumaDTSEnrich_MissingType400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSEnrich(api, &stubEnrich{})

	w := postJSON(t, r, "/api/dts/enrich", []byte(`{"webhook":{"x":1}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSEnrich_MissingWebhook400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSEnrich(api, &stubEnrich{})

	w := postJSON(t, r, "/api/dts/enrich", []byte(`{"type":"pokemon"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSEnrich_ServiceError500(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSEnrich(api, &stubEnrich{err: errors.New("boom")})

	w := postJSON(t, r, "/api/dts/enrich", []byte(`{"type":"pokemon","webhook":{"x":1}}`))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSEnrich_MalformedBody400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSEnrich(api, &stubEnrich{})

	w := postJSON(t, r, "/api/dts/enrich", []byte(`{"type":123}`)) // type wrong JSON type
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- dts/sendtest ----------------------------------------------------------

func TestHumaDTSSendTest_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	disp := &stubDispatcher{}
	ts := &stubDTSWriter{}
	// renderer nil → markers untouched (allowed).
	RegisterDTSSendTest(api, disp, ts, nil)

	// Open body: polymorphic `template` (object) + arbitrary `variables`.
	body := []byte(`{
		"template":{"content":"Hello {{name}}"},
		"variables":{"name":"World"},
		"target":{"id":"123","type":"discord:user"}
	}`)
	w := postJSON(t, r, "/api/dts/sendtest", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "ok" || got.Message != "sent" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if len(disp.jobs) != 1 {
		t.Fatalf("expected 1 dispatched job, got %d", len(disp.jobs))
	}
	if disp.jobs[0].Target != "123" || disp.jobs[0].Type != "discord:user" {
		t.Fatalf("unexpected job: %+v", disp.jobs[0])
	}
}

func TestHumaDTSSendTest_NilDispatcher503(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSendTest(api, nil, &stubDTSWriter{}, nil)

	body := []byte(`{"template":{"content":"x"},"target":{"id":"1"}}`)
	w := postJSON(t, r, "/api/dts/sendtest", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSendTest_MissingTemplate400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSendTest(api, &stubDispatcher{}, &stubDTSWriter{}, nil)

	w := postJSON(t, r, "/api/dts/sendtest", []byte(`{"target":{"id":"1"}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSendTest_MissingTarget400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSendTest(api, &stubDispatcher{}, &stubDTSWriter{}, nil)

	w := postJSON(t, r, "/api/dts/sendtest", []byte(`{"template":{"content":"x"}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDTSSendTest_MalformedBody400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDTSSendTest(api, &stubDispatcher{}, &stubDTSWriter{}, nil)

	w := postJSON(t, r, "/api/dts/sendtest", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
