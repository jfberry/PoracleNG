package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/delivery"
)

// --- stubs -----------------------------------------------------------------

// stubTestProc implements bot.TestProcessor and records the parsed request.
type stubTestProc struct {
	err        error
	gotType    string
	gotWebhook json.RawMessage
	gotTarget  bot.TestTarget
	callCount  int
}

func (s *stubTestProc) ProcessTest(webhookType string, raw json.RawMessage, target bot.TestTarget) error {
	s.callCount++
	s.gotType = webhookType
	s.gotWebhook = raw
	s.gotTarget = target
	return s.err
}

// stubDeliverDispatcher implements deliverDispatcher and records jobs.
type stubDeliverDispatcher struct {
	jobs []*delivery.Job
}

func (s *stubDeliverDispatcher) Dispatch(job *delivery.Job) { s.jobs = append(s.jobs, job) }

// --- /test -----------------------------------------------------------------

func TestHumaTest_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	proc := &stubTestProc{}
	RegisterTest(api, proc)

	// Open body: polymorphic `webhook` object.
	body := []byte(`{
		"type":"pokemon",
		"target":{"id":"123","type":"discord:user","language":"en"},
		"webhook":{"pokemon_id":25,"iv":100,"latitude":1.2,"longitude":3.4}
	}`)
	w := postJSON(t, r, "/api/test", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if got.Status != "ok" {
		t.Fatalf("expected status ok, got %s", w.Body.String())
	}
	if proc.callCount != 1 {
		t.Fatalf("expected 1 ProcessTest call, got %d", proc.callCount)
	}
	if proc.gotType != "pokemon" || proc.gotTarget.ID != "123" {
		t.Fatalf("unexpected parsed request: type=%q target=%+v", proc.gotType, proc.gotTarget)
	}
	// Open webhook field preserved as raw JSON.
	var wh map[string]any
	if err := json.Unmarshal(proc.gotWebhook, &wh); err != nil {
		t.Fatalf("webhook not preserved as JSON: %v", err)
	}
	if wh["pokemon_id"].(float64) != 25 {
		t.Fatalf("webhook payload not parsed: %v", wh)
	}
}

// TestHumaTest_ResolvesDTSTypeNames confirms POST /api/test resolves `type`
// through resolveTestWireType before calling ProcessTest, so a caller may use
// a DTS template-type name (e.g. "monsterChanged") the same way !poracle-test
// does, not just ProcessTest's own raw wire spellings. Prior to this fix,
// req.Type was passed straight through, so {"type":"monsterChanged",...}
// reached the real ProcessTest's switch (which only recognizes
// "monster_changed") and 500'd with "unsupported test webhook type".
func TestHumaTest_ResolvesDTSTypeNames(t *testing.T) {
	cases := []struct {
		sendType string
		wantType string
	}{
		{"monsterChanged", "monster_changed"},
		{"monster", "pokemon"},
		{"maxbattle", "max_battle"},
		// Wire types must keep working unchanged (idempotent resolution).
		{"pokemon", "pokemon"},
		{"raid", "raid"},
		{"monster_changed", "monster_changed"},
		{"max_battle", "max_battle"},
		// CLI-display hyphenated forms must keep working.
		{"fort-update", "fort_update"},
		{"max-battle", "max_battle"},
		// invasion/lure are unaffected: they resolve to themselves, exactly
		// as the pre-fix passthrough behaved (see resolveTestWireType's doc
		// comment for why the pokestop-guard direction differs from
		// !poracle-test's resolveHookType).
		{"invasion", "invasion"},
		{"lure", "lure"},
	}

	for _, tc := range cases {
		t.Run(tc.sendType, func(t *testing.T) {
			r, api := newFeaturesTestAPI(t)
			proc := &stubTestProc{}
			RegisterTest(api, proc)

			body := []byte(`{
				"type":"` + tc.sendType + `",
				"target":{"id":"123","type":"discord:user","language":"en"},
				"webhook":{"pokemon_id":25,"iv":100,"latitude":1.2,"longitude":3.4}
			}`)
			w := postJSON(t, r, "/api/test", body)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}
			if proc.gotType != tc.wantType {
				t.Errorf("ProcessTest called with type=%q, want %q (sent %q)", proc.gotType, tc.wantType, tc.sendType)
			}
		})
	}
}

// TestHumaTest_UnknownTypePassesThrough confirms an unresolvable `type`
// reaches ProcessTest unchanged rather than being masked behind a different
// error — ProcessTest's own "unsupported test webhook type: %s" case is the
// authoritative error for this, and resolveTestWireType must not shadow it.
func TestHumaTest_UnknownTypePassesThrough(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	proc := &stubTestProc{}
	RegisterTest(api, proc)

	body := []byte(`{"type":"totally-bogus-type","target":{"id":"1"},"webhook":{"x":1}}`)
	w := postJSON(t, r, "/api/test", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (stub never errors), got %d body=%s", w.Code, w.Body.String())
	}
	if proc.gotType != "totally-bogus-type" {
		t.Errorf("ProcessTest called with type=%q, want unchanged %q", proc.gotType, "totally-bogus-type")
	}
}

func TestHumaTest_MissingFields400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterTest(api, &stubTestProc{})

	// Missing target.id.
	w := postJSON(t, r, "/api/test", []byte(`{"type":"pokemon","webhook":{"x":1}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaTest_ProcessError500(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterTest(api, &stubTestProc{err: errors.New("boom")})

	body := []byte(`{"type":"pokemon","target":{"id":"1"},"webhook":{"x":1}}`)
	w := postJSON(t, r, "/api/test", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaTest_MalformedBody400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterTest(api, &stubTestProc{})

	w := postJSON(t, r, "/api/test", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	// problem+json content type.
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected problem+json, got %q", ct)
	}
}

// --- /deliverMessages and /postMessage -------------------------------------

func TestHumaDeliverMessages_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	disp := &stubDeliverDispatcher{}
	RegisterDeliverMessages(api, "post-deliver-messages", "/deliverMessages", "Deliver pre-rendered messages", "canonical", disp)

	// Open body: []Job with a pre-rendered `message` RawMessage. One job is
	// missing target/type and must be skipped (not counted).
	body := []byte(`[
		{"target":"123","type":"discord:user","message":{"content":"hi"}},
		{"target":"","type":"discord:channel","message":{"content":"skip"}}
	]`)
	w := postJSON(t, r, "/api/deliverMessages", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status string `json:"status"`
		Queued int    `json:"queued"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "ok" || got.Queued != 1 {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if len(disp.jobs) != 1 {
		t.Fatalf("expected 1 dispatched job, got %d", len(disp.jobs))
	}
	if disp.jobs[0].Target != "123" {
		t.Fatalf("unexpected job target: %q", disp.jobs[0].Target)
	}
	// The freeform message RawMessage is preserved.
	if string(disp.jobs[0].Message) != `{"content":"hi"}` {
		t.Fatalf("message not preserved: %s", disp.jobs[0].Message)
	}
}

func TestHumaPostMessage_AliasesDeliverMessages(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	disp := &stubDeliverDispatcher{}
	// Same handler logic registered at the legacy alias path.
	RegisterDeliverMessages(api, "post-message", "/postMessage", "Deliver pre-rendered messages (legacy alias)", "alias", disp)

	body := []byte(`[{"target":"999","type":"telegram:user","message":{"text":"x"}}]`)
	w := postJSON(t, r, "/api/postMessage", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Status string `json:"status"`
		Queued int    `json:"queued"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "ok" || got.Queued != 1 {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if len(disp.jobs) != 1 || disp.jobs[0].Target != "999" {
		t.Fatalf("alias did not dispatch correctly: %+v", disp.jobs)
	}
}

func TestHumaDeliverMessages_BothPathsServeSameAPI(t *testing.T) {
	// Both ops registered on the SAME api instance, exactly as main.go does.
	r, api := newFeaturesTestAPI(t)
	disp := &stubDeliverDispatcher{}
	RegisterDeliverMessages(api, "post-deliver-messages", "/deliverMessages", "Deliver pre-rendered messages", "canonical", disp)
	RegisterDeliverMessages(api, "post-message", "/postMessage", "Deliver pre-rendered messages (legacy alias)", "alias", disp)

	body := []byte(`[{"target":"1","type":"discord:user","message":{"content":"a"}}]`)

	w1 := postJSON(t, r, "/api/deliverMessages", body)
	w2 := postJSON(t, r, "/api/postMessage", body)
	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Fatalf("expected both 200, got deliver=%d post=%d", w1.Code, w2.Code)
	}
	if len(disp.jobs) != 2 {
		t.Fatalf("expected 2 dispatched jobs across both paths, got %d", len(disp.jobs))
	}
}

func TestHumaDeliverMessages_NilDispatcher503(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDeliverMessages(api, "post-deliver-messages", "/deliverMessages", "Deliver pre-rendered messages", "canonical", (*delivery.Dispatcher)(nil))

	body := []byte(`[{"target":"1","type":"discord:user","message":{}}]`)
	w := postJSON(t, r, "/api/deliverMessages", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHumaDeliverMessages_WrongShape422(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDeliverMessages(api, "post-deliver-messages", "/deliverMessages", "Deliver pre-rendered messages", "canonical", &stubDeliverDispatcher{})

	// Object where the schema expects an array → huma schema validation rejects
	// it with 422 (the typed []apiDeliverJob body now documents + validates the
	// request shape, replacing the old open-body handler-level 400).
	w := postJSON(t, r, "/api/deliverMessages", []byte(`{"target":"1"}`))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected problem+json, got %q", ct)
	}
}

func TestHumaDeliverMessages_MalformedJSON400(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterDeliverMessages(api, "post-deliver-messages", "/deliverMessages", "Deliver pre-rendered messages", "canonical", &stubDeliverDispatcher{})

	// Syntactically invalid JSON is rejected with 400 before validation.
	w := postJSON(t, r, "/api/deliverMessages", []byte(`not json`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected problem+json, got %q", ct)
	}
}

// --- /resolve --------------------------------------------------------------

func TestHumaResolve_OK_NoPlatforms(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	// Empty deps: all platform sessions nil. The handler still parses the body
	// and returns {"status":"ok"} — proving the open body parses and routes.
	RegisterResolve(api, ResolveDeps{})

	// Open body: nested optional sections + destinations of unknown type.
	body := []byte(`{
		"discord":{"users":["111"],"roles":[]},
		"telegram":{"chats":["222"]},
		"destinations":["333"]
	}`)
	w := postJSON(t, r, "/api/resolve", body)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if got["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", w.Body.String())
	}
	// destinations key present (even if empty, since no platforms to resolve).
	if _, ok := got["destinations"]; !ok {
		t.Fatalf("expected destinations key, got %s", w.Body.String())
	}
	// No Discord/Telegram session ⇒ those sections omitted, matching legacy.
	if _, ok := got["discord"]; ok {
		t.Fatalf("did not expect discord section without session: %s", w.Body.String())
	}
}

func TestHumaResolve_EmptyBody_OK(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterResolve(api, ResolveDeps{})

	w := postJSON(t, r, "/api/resolve", []byte(`{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", w.Body.String())
	}
}

func TestHumaResolve_MalformedBody422(t *testing.T) {
	r, api := newFeaturesTestAPI(t)
	RegisterResolve(api, ResolveDeps{})

	// destinations wrong type (object, not array). Now that the request body
	// carries a documented schema (destinations: array), huma's pre-bind
	// validation rejects the type mismatch with a precise 422 problem+json
	// (previously the handler's json.Unmarshal surfaced it as a generic 400).
	// Still a 4xx problem+json rejection — the leaf TYPE documentation is worth
	// the sharper error.
	w := postJSON(t, r, "/api/resolve", []byte(`{"destinations":{"a":1}}`))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected problem+json, got %q", ct)
	}
}
